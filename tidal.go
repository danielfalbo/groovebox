package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Tidal two-way playlist sync.
//
// Mirrors discogs.go: a pure-Go HTTP client against Tidal's OAuth + API + a
// background reconcile engine. Credentials come from env / .env (same pattern
// as DISCOGS_TOKEN). OAuth tokens are persisted in a gitignored JSON file next
// to the database (never in music.db, which is git-tracked).
//
// The sync is a small, safe 3-way merge per connected playlist:
//   - add: a track present on one side but not the other is added to the other.
//   - delete: a track is only removed from a side if it was in our last-known
//     snapshot (we knew about it) and vanished from the source side, and it was
//     not freshly added on the target side this pass (an add during a staggered
//     delete always wins — we never lose a newly added track).
//   - order: we append only, never fight reorders between the two sides.
//
// Join key is ISRC when present, else normalized "artist | title". The two
// sides are linked via Tidal's track UUID stored in tracks.tidal_id.
// ---------------------------------------------------------------------------

const (
	tidalAuthURL   = "https://auth.tidal.com/v1/oauth2/device_authorization"
	tidalTokenURL  = "https://auth.tidal.com/v1/oauth2/token"
	tidalAPIv1     = "https://api.tidal.com/v1/"
	tidalAPIv2     = "https://api.tidal.com/v2/"
	tidalOpenAPIV2 = "https://openapi.tidal.com/v2/"
	tidalScope     = "r_usr w_usr w_sub"
	tidalUserAgent = "groovebox/1.0"
	// refresh this long before real expiry so a token is (almost) always fresh
	tidalExpiryGrace = 2 * time.Minute
)

// TidalClient holds reusable creds + the cached OAuth token.
type TidalClient struct {
	ClientID     string
	ClientSecret string
	tokenFile    string
	mu           sync.Mutex
	token        *tidalOAuthToken
	pending      *tidalPendingLogin
	http         *http.Client
	etag         string // last-known playlist etag for mutations
}

type tidalOAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	UserID       string    `json:"user_id"`
	CountryCode  string    `json:"country_code"`
	Expiry       time.Time `json:"expiry"`
}

type tidalPendingLogin struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	VerifyURL  string `json:"verification_uri_complete"`
	ExpiresAt  int64  `json:"expires_at"`
	Interval   int    `json:"interval"`
}

// ---------------------------------------------------------------------------
// Credentials
// ---------------------------------------------------------------------------

func getTidalCreds() (id, secret string) {
	// The Tidal device-authorization (limited-input) client credentials are
	// public — they ship hardcoded in the open-source tidalapi Python library.
	// We use them as defaults so device login works out-of-the-box.
	id = "fX2JxdmntZWK0ixT"
	secret = "1Nn9AfDAjxrgJFJbKNWLeAyKGVGmINuXPPLHVXAvxAg="

	// Override via env or .env only with TIDAL_DEVICE_* (must be Limited Input
	// Device credentials — the TIDAL_CLIENT_ID/SECRET env vars are for a
	// different OAuth flow and won't work for device login).
	if envID := os.Getenv("TIDAL_DEVICE_CLIENT_ID"); envID != "" {
		id = envID
	}
	if envSecret := os.Getenv("TIDAL_DEVICE_SECRET"); envSecret != "" {
		secret = envSecret
	}

	for _, p := range []string{".env", "../discogs-albums/.env"} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "TIDAL_DEVICE_CLIENT_ID=") {
				id = strings.TrimPrefix(line, "TIDAL_DEVICE_CLIENT_ID=")
			}
			if strings.HasPrefix(line, "TIDAL_DEVICE_SECRET=") {
				secret = strings.TrimPrefix(line, "TIDAL_DEVICE_SECRET=")
			}
		}
	}
	return id, secret
}

func newTidalClient(dbPath string) (*TidalClient, error) {
	id, secret := getTidalCreds()
	if id == "" || secret == "" {
		return nil, fmt.Errorf("TIDAL_CLIENT_ID and TIDAL_SECRET must be set (env or .env)")
	}
	return &TidalClient{
		ClientID:     id,
		ClientSecret: secret,
		tokenFile:    filepath.Join(filepath.Dir(dbPath), ".tidal-session.json"),
		http:         &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (t *TidalClient) authenticated() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.token != nil && t.token.AccessToken != ""
}

// Ready loads any saved token (and refreshes if stale) so auth state is real.
// Returns true if we hold a usable access token.
func (t *TidalClient) Ready() bool {
	return t.ensureFresh() == nil
}

// EnsureAuth returns nil if a usable session exists (loading/refreshing as needed).
func (t *TidalClient) EnsureAuth() error {
	if t.authenticated() {
		return t.ensureFresh()
	}
	if err := t.loadToken(); err != nil {
		return err
	}
	if t.authenticated() {
		return t.ensureFresh()
	}
	return fmt.Errorf("not authenticated with Tidal")
}

// ---------------------------------------------------------------------------
// OAuth device flow
// ---------------------------------------------------------------------------

func (t *TidalClient) StartDeviceLogin() (tidalPendingLogin, error) {
	form := url.Values{"client_id": {t.ClientID}, "scope": {tidalScope}}
	resp, err := t.http.PostForm(tidalAuthURL, form)
	if err != nil {
		return tidalPendingLogin{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return tidalPendingLogin{}, fmt.Errorf("device_authorization HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var j struct {
		DeviceCode string `json:"deviceCode"`
		UserCode   string `json:"userCode"`
		VerifyURI  string `json:"verificationUriComplete"`
		ExpiresIn  int    `json:"expiresIn"`
		Interval   int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		return tidalPendingLogin{}, err
	}
	pending := tidalPendingLogin{
		DeviceCode: j.DeviceCode,
		UserCode:   j.UserCode,
		VerifyURL:  j.VerifyURI,
		ExpiresAt:  time.Now().Unix() + int64(j.ExpiresIn),
		Interval:   j.Interval,
	}
	t.mu.Lock()
	t.pending = &pending
	t.mu.Unlock()
	return pending, nil
}

// PollDeviceLogin performs one token exchange attempt for a pending login.
// Returns (done, err). done=false,nil means "keep waiting".
func (t *TidalClient) PollDeviceCode() (bool, error) {
	t.mu.Lock()
	pending := t.pending
	t.mu.Unlock()
	if pending == nil {
		return t.authenticated(), nil
	}
	if time.Now().Unix() > pending.ExpiresAt {
		return false, fmt.Errorf("device login link expired")
	}
	form := url.Values{
		"client_id":     {t.ClientID},
		"client_secret": {t.ClientSecret},
		"device_code":   {pending.DeviceCode},
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
		"scope":         {tidalScope},
	}
	resp, err := t.http.PostForm(tidalTokenURL, form)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		switch e.Error {
		case "authorization_pending", "slow_down":
			return false, nil // keep waiting
		case "expired_token", "access_denied":
			t.finalLogin()
			return false, fmt.Errorf("device login %s", e.Error)
		}
		return false, fmt.Errorf("device auth token HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return false, err
	}
	t.mu.Lock()
	t.pending = nil
	t.token = &tidalOAuthToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
		Expiry:       time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}
	t.mu.Unlock()

	if err := t.FetchSession(); err != nil {
		return true, err
	}
	return true, t.SaveToken()
}

func (t *TidalClient) finalLogin() {
	t.mu.Lock()
	t.pending = nil
	t.mu.Unlock()
}

// FetchSession populates user id/country from /sessions.
func (t *TidalClient) FetchSession() error {
	body, err := t.req("GET", tidalAPIv1, "sessions", nil, nil, nil)
	if err != nil {
		return err
	}
	var j struct {
		CountryCode string `json:"countryCode"`
		UserID      int64  `json:"userId"`
	}
	if err := json.Unmarshal([]byte(body), &j); err != nil {
		return err
	}
	t.mu.Lock()
	if t.token != nil {
		t.token.UserID = strconv.FormatInt(j.UserID, 10)
		t.token.CountryCode = j.CountryCode
	}
	t.mu.Unlock()
	return nil
}

func (t *TidalClient) SaveToken() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token == nil {
		return fmt.Errorf("no token to save")
	}
	data, err := json.Marshal(t.token)
	if err != nil {
		return err
	}
	return os.WriteFile(t.tokenFile, data, 0600)
}

func (t *TidalClient) loadToken() error {
	data, err := os.ReadFile(t.tokenFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var tok tidalOAuthToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return err
	}
	if tok.Expiry.IsZero() {
		return fmt.Errorf("saved Tidal session has no expiry; re-authenticate")
	}
	t.mu.Lock()
	t.token = &tok
	t.mu.Unlock()
	return nil
}

func (t *TidalClient) refreshToken() error {
	t.mu.Lock()
	tok := t.token
	t.mu.Unlock()
	if tok == nil || tok.RefreshToken == "" {
		return fmt.Errorf("no refresh token; re-authenticate with Tidal")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
		"client_id":     {t.ClientID},
		"client_secret": {t.ClientSecret},
	}
	resp, err := t.http.PostForm(tidalTokenURL, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("refresh_token HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var n struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &n); err != nil {
		return err
	}
	t.mu.Lock()
	t.token.AccessToken = n.AccessToken
	t.token.TokenType = n.TokenType
	if n.RefreshToken != "" {
		t.token.RefreshToken = n.RefreshToken
	}
	t.token.Expiry = time.Now().Add(time.Duration(n.ExpiresIn) * time.Second)
	t.mu.Unlock()
	return t.SaveToken()
}

// ensureFresh ensures we have a non-expired access token.
func (t *TidalClient) ensureFresh() error {
	t.mu.Lock()
	tok := t.token
	t.mu.Unlock()
	if tok == nil {
		if err := t.loadToken(); err != nil {
			return err
		}
		t.mu.Lock()
		tok = t.token
		t.mu.Unlock()
	}
	if tok == nil {
		return fmt.Errorf("not authenticated with Tidal")
	}
	if time.Until(tok.Expiry) < tidalExpiryGrace {
		return t.refreshToken()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Authenticated HTTP
// ---------------------------------------------------------------------------

func (t *TidalClient) req(method, base, path string, params, data url.Values, reqHeaders map[string]string) (string, error) {
	if err := t.ensureFresh(); err != nil {
		return "", err
	}
	t.mu.Lock()
	token := ""
	typ := ""
	if t.token != nil {
		token = t.token.AccessToken
		typ = t.token.TokenType
	}
	t.mu.Unlock()
	auth := ""
	if token != "" {
		auth = typ + " " + token
	}
	body, err := t.doReq(method, base, path, params, data, reqHeaders, auth)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "token has expired") {
		if rerr := t.refreshToken(); rerr != nil {
			return "", rerr
		}
		t.mu.Lock()
		if t.token != nil {
			auth = t.token.TokenType + " " + t.token.AccessToken
		}
		t.mu.Unlock()
		body, err = t.doReq(method, base, path, params, data, reqHeaders, auth)
	}
	return body, err
}

func (t *TidalClient) doReq(method, base, path string, params, data url.Values, reqHeaders map[string]string, auth string) (string, error) {
	// Tidal v1 requires countryCode and sessionId as default query params.
	t.mu.Lock()
	countryCode := ""
	if t.token != nil {
		countryCode = t.token.CountryCode
	}
	t.mu.Unlock()

	merged := url.Values{}
	if countryCode != "" {
		merged.Set("countryCode", countryCode)
	}
	// Copy caller-provided params (these override defaults)
	if params != nil {
		for k, vs := range params {
			merged[k] = vs
		}
	}

	full := base + path
	if len(merged) > 0 {
		full += "?" + merged.Encode()
	}
	var rd io.Reader
	if data != nil {
		rd = strings.NewReader(data.Encode())
	}
	req, err := http.NewRequest(method, full, rd)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", tidalUserAgent)
	req.Header.Set("Accept", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if data != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range reqHeaders {
		req.Header.Set(k, v)
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Tidal %s %s HTTP %d: %s", method, path, resp.StatusCode, truncate(string(b), 300))
	}
	return string(b), nil
}

func (t *TidalClient) postForm(base, path string, form url.Values) (int, string, error) {
	resp, err := t.http.PostForm(base+path, form)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

// ---------------------------------------------------------------------------
// Tidal API operations
// ---------------------------------------------------------------------------

type tidalUserPlaylist struct {
	UUID        string
	Title       string
	Description string
	NumTracks   int
}

func (t *TidalClient) userID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token == nil {
		return ""
	}
	return t.token.UserID
}

func (t *TidalClient) UserPlaylists() ([]tidalUserPlaylist, error) {
	uid := t.userID()
	if uid == "" {
		return nil, fmt.Errorf("not authenticated with Tidal")
	}
	var out []tidalUserPlaylist
	order := 0
	for {
		params := url.Values{"limit": {"50"}, "offset": {fmt.Sprint(order * 50)}}
		body, err := t.req("GET", tidalAPIv1, "users/"+uid+"/playlists", params, nil, nil)
		if err != nil {
			return nil, err
		}
		var j struct {
			Items []struct {
				UUID           string `json:"uuid"`
				Title          string `json:"title"`
				Description    string `json:"description"`
				NumberOfTracks int    `json:"numberOfTracks"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &j); err != nil {
			return nil, err
		}
		if len(j.Items) == 0 {
			break
		}
		for _, it := range j.Items {
			out = append(out, tidalUserPlaylist{UUID: it.UUID, Title: it.Title, Description: it.Description, NumTracks: it.NumberOfTracks})
		}
		order++
		if len(j.Items) < 50 {
			break
		}
	}
	return out, nil
}

// TidalTrack is a Tidal playlist track identity.
type TidalTrack struct {
	ID       string
	Title    string
	Artist   string
	ISRC     string
	Duration int // seconds
}

func (t *TidalClient) PlaylistTracks(uuid string) ([]TidalTrack, error) {
	var tracks []TidalTrack
	order := 0
	for {
		params := url.Values{"limit": {"100"}, "offset": {fmt.Sprint(order * 100)}}
		body, err := t.req("GET", tidalAPIv1, "playlists/"+uuid+"/tracks", params, nil, nil)
		if err != nil {
			return nil, err
		}
		var j struct {
			Items []struct {
				ID       int64  `json:"id"`
				Title    string `json:"title"`
				ISRC     string `json:"isrc"`
				Duration int    `json:"duration"`
				Artists  []struct {
					Name string `json:"name"`
				} `json:"artists"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &j); err != nil {
			return nil, err
		}
		if len(j.Items) == 0 {
			break
		}
		for _, it := range j.Items {
			artist := ""
			if len(it.Artists) > 0 {
				artist = it.Artists[0].Name
			}
			tracks = append(tracks, TidalTrack{ID: strconv.FormatInt(it.ID, 10), Title: it.Title, ISRC: it.ISRC, Artist: artist, Duration: it.Duration})
		}
		order++
		if len(j.Items) < 100 {
			break
		}
	}
	return tracks, nil
}

// fetchETag gets the current etag for a playlist (required for mutations).
func (t *TidalClient) fetchETag(uuid string) error {
	if err := t.ensureFresh(); err != nil {
		return err
	}
	t.mu.Lock()
	auth := ""
	cc := ""
	if t.token != nil {
		auth = t.token.TokenType + " " + t.token.AccessToken
		cc = t.token.CountryCode
	}
	t.mu.Unlock()

	full := tidalAPIv1 + "playlists/" + uuid + "?limit=1"
	if cc != "" {
		full += "&countryCode=" + cc
	}
	req, _ := http.NewRequest("GET", full, nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("User-Agent", tidalUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if etag := resp.Header.Get("etag"); etag != "" {
		t.mu.Lock()
		t.etag = etag
		t.mu.Unlock()
	}
	return nil
}

func (t *TidalClient) CreatePlaylist(title, description string) (string, error) {
	params := url.Values{"name": {title}, "description": {description}, "folderId": {"root"}}
	body, err := t.req("PUT", tidalAPIv2, "my-collection/playlists/folders/create-playlist", params, nil, nil)
	if err != nil {
		return "", err
	}
	var j struct {
		Data struct {
			UUID string `json:"uuid"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &j); err != nil {
		return "", err
	}
	if j.Data.UUID == "" {
		return "", fmt.Errorf("Tidal create-playlist returned no uuid")
	}
	return j.Data.UUID, nil
}

func (t *TidalClient) AddTracks(uuid string, trackIDs []string) ([]string, error) {
	if len(trackIDs) == 0 {
		return nil, nil
	}
	// Fetch etag required for playlist mutations.
	_ = t.fetchETag(uuid)
	t.mu.Lock()
	etag := t.etag
	t.mu.Unlock()
	hdrs := map[string]string{}
	if etag != "" {
		hdrs["If-None-Match"] = etag
	}
	var added []string
	for start := 0; start < len(trackIDs); start += 50 {
		end := start + 50
		if end > len(trackIDs) {
			end = len(trackIDs)
		}
		data := url.Values{
			"trackIds":            {strings.Join(trackIDs[start:end], ",")},
			"toIndex":             {"-1"},
			"onDupes":             {"SKIP"},
			"onArtifactNotFound":  {"SKIP"},
		}
		params := url.Values{"limit": {"100"}}
		body, err := t.req("POST", tidalAPIv1, "playlists/"+uuid+"/items", params, data, hdrs)
		if err != nil {
			return nil, err
		}
		var j struct {
			AddedItemIDs []string `json:"addedItemIds"`
		}
		_ = json.Unmarshal([]byte(body), &j)
		added = append(added, j.AddedItemIDs...)
	}
	return added, nil
}

func (t *TidalClient) RemoveByIndices(uuid string, indices []int) error {
	if len(indices) == 0 {
		return nil
	}
	_ = t.fetchETag(uuid)
	t.mu.Lock()
	etag := t.etag
	t.mu.Unlock()
	hdrs := map[string]string{}
	if etag != "" {
		hdrs["If-None-Match"] = etag
	}
	var idxStrs []string
	for _, i := range indices {
		idxStrs = append(idxStrs, fmt.Sprint(i))
	}
	for start := 0; start < len(idxStrs); start += 50 {
		end := start + 50
		if end > len(idxStrs) {
			end = len(idxStrs)
		}
		_, err := t.req("DELETE", tidalAPIv1, "playlists/"+uuid+"/items/"+strings.Join(idxStrs[start:end], ","), nil, nil, hdrs)
		if err != nil {
			return err
		}
	}
	return nil
}

// ResolveTidalID searches Tidal v1 by title+artist and matches by ISRC.
// Returns the Tidal track ID if found.
func (t *TidalClient) ResolveTidalID(isrc, title, artist string) (string, bool) {
	query := title
	if artist != "" {
		query = title + " " + artist
	}
	params := url.Values{"query": {query}, "limit": {"20"}, "types": {"TRACKS"}}
	body, err := t.req("GET", tidalAPIv1, "search", params, nil, nil)
	if err != nil {
		return "", false
	}
	var j struct {
		Tracks struct {
			Items []struct {
				ID    int64  `json:"id"`
				ISRC  string `json:"isrc"`
				Title string `json:"title"`
			} `json:"items"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(body), &j); err != nil {
		return "", false
	}
	for _, tr := range j.Tracks.Items {
		if isrc != "" && strings.EqualFold(tr.ISRC, isrc) {
			return strconv.FormatInt(tr.ID, 10), true
		}
	}
	// No ISRC to match — take the first result as best effort.
	if len(j.Tracks.Items) > 0 && isrc == "" {
		return strconv.FormatInt(j.Tracks.Items[0].ID, 10), true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Sync engine
// ---------------------------------------------------------------------------

var tidalKeyRe = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeTidalKey(s string) string {
	return tidalKeyRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "")
}

func tidalTrackKey(isrc, artist, title string) string {
	if isrc != "" {
		return "isrc:" + strings.ToLower(isrc)
	}
	return "t:" + normalizeTidalKey(artist) + "|" + normalizeTidalKey(title)
}

// ConnectLocalPlaylist creates the Tidal playlist for a local playlist and links it.
func ConnectLocalPlaylist(db *sql.DB, tc *TidalClient, playlistID string) (string, error) {
	var name, desc string
	if err := db.QueryRow("SELECT name, COALESCE(description,'') FROM playlists WHERE id=?", playlistID).Scan(&name, &desc); err != nil {
		return "", fmt.Errorf("playlist not found: %w", err)
	}
	var existing string
	if err := db.QueryRow("SELECT tidal_playlist_id FROM playlists WHERE id=? AND tidal_playlist_id IS NOT NULL", playlistID).Scan(&existing); err == nil && existing != "" {
		return existing, nil // already connected
	}
	remoteID, err := tc.CreatePlaylist(name, desc)
	if err != nil {
		return "", err
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err = db.Exec("UPDATE playlists SET tidal_playlist_id=?, tidal_connected_at=?, tidal_last_synced_at=? WHERE id=?",
		remoteID, now, now, playlistID)
	if err != nil {
		return "", err
	}
	log.Printf("tidal: connected local playlist %q to Tidal playlist %s", name, remoteID)
	return remoteID, nil
}

// DisconnectPlaylist unlinks a playlist from Tidal without deleting either side.
func DisconnectPlaylist(db *sql.DB, playlistID string) error {
	_, err := db.Exec(`UPDATE playlists SET
		tidal_playlist_id=NULL, tidal_direction='bidirectional',
		tidal_connected_at=NULL, tidal_last_synced_at=NULL, tidal_last_error=NULL,
		tidal_snap_local=NULL, tidal_snap_tidal=NULL WHERE id=?`, playlistID)
	return err
}

// SyncOneConnection reconciles a single connected playlist.
func SyncOneConnection(db *sql.DB, tc *TidalClient, playlistID string) (int, int, error) {
	var name, remoteID, snapLocal, snapTidal string
	err := db.QueryRow(`
		SELECT COALESCE(name,''), COALESCE(tidal_playlist_id,''), COALESCE(tidal_snap_local,''), COALESCE(tidal_snap_tidal,'')
		FROM playlists WHERE id=?`, playlistID).Scan(&name, &remoteID, &snapLocal, &snapTidal)
	if err != nil {
		return 0, 0, err
	}
	if remoteID == "" {
		return 0, 0, fmt.Errorf("playlist %s not connected to Tidal", playlistID)
	}

	// --- Local membership (ordered). ---
	type localItem struct {
		trackID, title, artist, isrc, tidalID string
	}
	rows, err := db.Query(`
		SELECT t.id, COALESCE(t.title,''), COALESCE(t.artist,''), COALESCE(t.isrc,''), COALESCE(t.tidal_id,'')
		FROM playlist_tracks pt JOIN tracks t ON pt.track_id = t.id
		WHERE pt.playlist_id = ? ORDER BY pt.position ASC`, playlistID)
	if err != nil {
		return 0, 0, err
	}
	var localItems []localItem
	for rows.Next() {
		var li localItem
		if err := rows.Scan(&li.trackID, &li.title, &li.artist, &li.isrc, &li.tidalID); err == nil {
			localItems = append(localItems, li)
		}
	}
	rows.Close()

	currentLocal := map[string]string{} // key -> trackID
	var localKeyOrder []string
	for _, li := range localItems {
		k := tidalTrackKey(li.isrc, li.artist, li.title)
		if _, ok := currentLocal[k]; !ok {
			currentLocal[k] = li.trackID
			localKeyOrder = append(localKeyOrder, k)
		}
	}

	// --- Pull remote. ---
	remoteTracks, err := tc.PlaylistTracks(remoteID)
	if err != nil {
		return 0, 0, err
	}
	currentRemote := map[string]string{} // key -> tidal id
	var remoteKeyOrder []string
	remoteMeta := map[string]TidalTrack{}
	for _, tr := range remoteTracks {
		k := tidalTrackKey(tr.ISRC, tr.Artist, tr.Title)
		if _, dup := currentRemote[k]; dup {
			continue
		}
		currentRemote[k] = tr.ID
		remoteMeta[k] = tr
		remoteKeyOrder = append(remoteKeyOrder, k)
	}

	lastLocal := keysSet(snapLocal)
	lastRemote := keysSet(snapTidal)

	addedLocal := 0
	addedRemote := 0

	// Add: remote (Tidal) -> local.
	for _, k := range remoteKeyOrder {
		if _, exists := currentLocal[k]; exists {
			continue
		}
		tr := remoteMeta[k]
		localID, err := ensureLocalTrack(db, tr)
		if err != nil {
			log.Printf("tidal: create local track %s: %v", tr.Title, err)
			continue
		}
		if err := appendToPlaylist(db, playlistID, localID); err != nil {
			continue
		}
		currentLocal[k] = localID
		localKeyOrder = append(localKeyOrder, k)
		addedLocal++
	}

	// Add: local -> Tidal.
	var toAdd []string
	for _, k := range localKeyOrder {
		if _, exists := currentRemote[k]; exists {
			continue
		}
		var isrc, artist, title, tidalID string
		_ = db.QueryRow("SELECT COALESCE(isrc,''),COALESCE(artist,''),COALESCE(title,''),COALESCE(tidal_id,'') FROM tracks WHERE id=?", currentLocal[k]).
			Scan(&isrc, &artist, &title, &tidalID)
		tid := tidalID
		if tid == "" && isrc != "" {
			if id, ok := tc.ResolveTidalID(isrc, title, artist); ok {
				tid = id
				_, _ = db.Exec("UPDATE tracks SET tidal_id=? WHERE id=?", id, currentLocal[k])
			}
		}
		if tid == "" {
			continue // no local mapping to a Tidal track; skip (never blind-guess)
		}
		toAdd = append(toAdd, tid)
		addedRemote++
	}
	if len(toAdd) > 0 {
		if _, err := tc.AddTracks(remoteID, toAdd); err != nil {
			return addedLocal, addedRemote, fmt.Errorf("adding %d tracks to Tidal: %w", len(toAdd), err)
		}
	}

	// Delete: Tidal removed a track we knew about -> remove locally.
	for k := range lastRemote {
		if _, onLocal := currentLocal[k]; !onLocal {
			continue // it is/was not local
		}
		if _, onRemote := currentRemote[k]; !onRemote {
			// was in snapshot (we knew it), gone on Tidal, present locally now, and
			// not freshly added on Tidal this pass -> safe to remove locally.
			if trackID := currentLocal[k]; trackID != "" {
				_, _ = db.Exec("DELETE FROM playlist_tracks WHERE playlist_id=? AND track_id=?", playlistID, trackID)
			}
		}
	}

	// Delete: local removed a song -> remove from Tidal (by current index).
	for k := range lastLocal {
		if _, onRemote := currentRemote[k]; !onRemote {
			continue
		}
		if _, onLocal := currentLocal[k]; !onLocal {
			for i, rk := range remoteKeyOrder {
				if rk == k {
					_ = tc.RemoveByIndices(remoteID, []int{i})
					break
				}
			}
		}
	}

	// Persist fresh snapshots.
	_, _ = db.Exec("UPDATE playlists SET tidal_snap_local=? , tidal_snap_tidal=?, tidal_last_synced_at=? WHERE id=?",
		savesKeys(localKeyOrder), savesKeys(remoteKeyOrder), time.Now().Format("2006-01-02 15:04:05"), playlistID)

	return addedLocal, addedRemote, nil
}

// PullTidalPlaylists lists Tidal playlists, creating local counterparts that
// aren't connected yet, then returns all connected playlist ids.
func PullTidalPlaylists(db *sql.DB, tc *TidalClient) ([]string, error) {
	pls, err := tc.UserPlaylists()
	if err != nil {
		return nil, err
	}
	var connected []string
	for _, p := range pls {
		// already connected?
		var localID string
		err := db.QueryRow("SELECT id FROM playlists WHERE tidal_playlist_id=?", p.UUID).Scan(&localID)
		if err == nil {
			connected = append(connected, localID)
			continue
		}
		// create a local playlist for this Tidal playlist and auto-connect it.
		newID := uuid.New().String()
		now := time.Now().Format("2006-01-02 15:04:05")
		name := p.Title
		if name == "" {
			name = "Tidal Playlist"
		}
		_, err = db.Exec(`INSERT INTO playlists (id, name, description, created_at, updated_at,
			tidal_playlist_id, tidal_connected_at, tidal_last_synced_at) VALUES (?,?,?,?,?,?,?,?)`,
			newID, name, p.Description, now, now, p.UUID, now, now)
		if err != nil {
			log.Printf("tidal: failed creating local playlist for %q: %v", p.Title, err)
			continue
		}
		log.Printf("Tidal: auto-connected playlist %q (Tidal %s) as %s", p.Title, p.UUID, newID)
		connected = append(connected, newID)
	}
	return connected, nil
}

// RunTidalSync pulls all Tidal playlists (auto-connecting new ones) and
// reconciles every connected playlist, both directions.
func RunTidalSync(db *sql.DB, tc *TidalClient) error {
	updateSyncProgress(func(p *SyncProgress) {
		p.IsTidalSyncing = true
		p.TidalStage = "pull"
		p.TidalMessage = "Listing Tidal playlists..."
		p.TidalLastError = ""
	})
	defer func() {
		updateSyncProgress(func(p *SyncProgress) {
			p.IsTidalSyncing = false
			p.TidalLastSyncedAt = time.Now().Format("2006-01-02 15:04:05")
		})
	}()

	if err := tc.EnsureAuth(); err != nil {
		updateSyncProgress(func(p *SyncProgress) { p.TidalLastError = err.Error(); p.TidalMessage = "Auth failed: " + err.Error() })
		return err
	}
	ids, err := PullTidalPlaylists(db, tc)
	if err != nil {
		updateSyncProgress(func(p *SyncProgress) { p.TidalLastError = err.Error(); p.TidalMessage = "Pull failed: " + err.Error() })
		return err
	}

	updateSyncProgress(func(p *SyncProgress) {
		p.TidalConnected = len(ids)
		p.TidalStage = "sync"
	})

	synced := 0
	for _, id := range ids {
		_, _, err := SyncOneConnection(db, tc, id)
		if err != nil {
			updateSyncProgress(func(p *SyncProgress) { p.TidalLastError = err.Error() })
			log.Printf("tidal sync %s: %v", id, err)
			continue
		}
		synced++
	}
	updateSyncProgress(func(p *SyncProgress) {
		p.TidalSynced = synced
		p.TidalStage = "idle"
		p.TidalMessage = "Tidal sync complete"
	})
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func keysSet(s string) map[string]bool {
	m := map[string]bool{}
	if strings.TrimSpace(s) == "" {
		return m
	}
	var keys []string
	if err := json.Unmarshal([]byte(s), &keys); err == nil {
		for _, k := range keys {
			m[k] = true
		}
	}
	return m
}

func savesKeys(keys []string) string {
	b, _ := json.Marshal(keys)
	return string(b)
}

func appendToPlaylist(db *sql.DB, playlistID, trackID string) error {
	// Guard against duplicates: if the track is already in the playlist, skip.
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id=? AND track_id=?", playlistID, trackID).Scan(&count)
	if count > 0 {
		return nil
	}
	var max int
	_ = db.QueryRow("SELECT COALESCE(MAX(position),0) FROM playlist_tracks WHERE playlist_id=?", playlistID).Scan(&max)
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := db.Exec("INSERT OR IGNORE INTO playlist_tracks (playlist_id, track_id, position, added_at) VALUES (?,?,?,?)",
		playlistID, trackID, max+1, now)
	if err == nil {
		_, _ = db.Exec("UPDATE playlists SET updated_at=? WHERE id=?", now, playlistID)
	}
	return err
}

// ensureLocalTrack inserts a Tidal track (with its canonical album) if unknown.
// On any match (ISRC or title+artist), it backfills missing ISRC + tidal_id so
// subsequent syncs produce the same join key on both sides.
func ensureLocalTrack(db *sql.DB, tr TidalTrack) (string, error) {
	// 1) exact match by isrc
	if tr.ISRC != "" {
		var id string
		err := db.QueryRow("SELECT id FROM tracks WHERE isrc=? LIMIT 1", tr.ISRC).Scan(&id)
		if err == nil {
			backfillTrack(db, id, tr)
			return id, nil
		}
	}
	// 2) normalized title+artist match
	var existing string
	err := db.QueryRow("SELECT id FROM tracks WHERE lower(title)=lower(?) AND lower(artist)=lower(?) LIMIT 1", tr.Title, tr.Artist).Scan(&existing)
	if err == nil {
		backfillTrack(db, existing, tr)
		return existing, nil
	}
	// 3) create album + track
	albumID := findOrCreateTidalAlbum(db, tr.Artist, tr.Title)
	trackID := uuid.New().String()
	_, err = db.Exec(`INSERT INTO tracks (id, album_id, title, artist, duration_ms, isrc, tidal_id) VALUES (?,?,?,?,?,?,?)`,
		trackID, albumID, tr.Title, tr.Artist, tr.Duration*1000, tr.ISRC, tr.ID)
	if err != nil {
		return "", err
	}
	_, _ = db.Exec("INSERT INTO search_fts (target_type, target_id, title, artist) VALUES ('track',?,?,?)", trackID, tr.Title, tr.Artist)
	return trackID, nil
}

// backfillTrack updates missing ISRC and tidal_id on an existing track from
// the Tidal counterpart, so future syncs match on the same key.
func backfillTrack(db *sql.DB, trackID string, tr TidalTrack) {
	if tr.ISRC != "" {
		_, _ = db.Exec("UPDATE tracks SET isrc=? WHERE id=? AND (isrc IS NULL OR isrc='')", tr.ISRC, trackID)
	}
	if tr.ID != "" {
		_, _ = db.Exec("UPDATE tracks SET tidal_id=? WHERE id=? AND (tidal_id IS NULL OR tidal_id='')", tr.ID, trackID)
	}
}

// findOrCreateTidalAlbum returns an album by normalized title+artist or creates one.
func findOrCreateTidalAlbum(db *sql.DB, artist, title string) string {
	var id string
	err := db.QueryRow("SELECT id FROM albums WHERE lower(title)=lower(?) AND lower(artist)=lower(?) LIMIT 1", title, artist).Scan(&id)
	if err == nil {
		return id
	}
	id = uuid.New().String()
	_, _ = db.Exec(`INSERT INTO albums (id, title, artist, streaming_notes) VALUES (?,?,?,?)`, id, title, artist, "Tidal")
	_, _ = db.Exec("INSERT INTO search_fts (target_type, target_id, title, artist) VALUES ('album',?,?,?)", id, title, artist)
	return id
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}