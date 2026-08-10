package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const spotifyRedirectURI = "http://127.0.0.1:8787/callback"

type spotifyOAuthResult struct {
	code string
	err  error
}

type spotifyTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type spotifyPlaylistPage struct {
	Items []spotifyPlaylist `json:"items"`
	Next  string            `json:"next"`
}

type spotifyPlaylist struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type spotifyPlaylistItemsPage struct {
	Items []spotifyPlaylistItem `json:"items"`
	Next  string                `json:"next"`
}

type spotifyPlaylistItem struct {
	AddedAt string       `json:"added_at"`
	Item    spotifyTrack `json:"item"`
}

type spotifyTrack struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	DurationMs int    `json:"duration_ms"`
	TrackNum   int    `json:"track_number"`
	Artists    []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name        string `json:"name"`
		ReleaseDate string `json:"release_date"`
		Artists     []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	} `json:"album"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
}

func ImportSpotifyAccount(db *sql.DB) error {
	clientID := os.Getenv("SPOTIFY_CLIENT_ID")
	clientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")
	if clientSecret == "" {
		clientSecret = os.Getenv("SPOTIFY_TOKEN")
	}
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET (or legacy SPOTIFY_TOKEN) must be set")
	}

	accessToken, err := spotifyAuthorize(clientID, clientSecret)
	if err != nil {
		return err
	}

	playlists, err := spotifyPlaylists(accessToken)
	if err != nil {
		return err
	}
	log.Printf("Fetched %d Spotify playlists", len(playlists))

	imported := 0
	for _, playlist := range playlists {
		items, err := spotifyPlaylistItems(accessToken, playlist.ID)
		if err != nil {
			if strings.Contains(err.Error(), "HTTP 403") {
				log.Printf("Skipping Spotify playlist %q: Spotify does not allow its items to be read", playlist.Name)
				continue
			}
			return fmt.Errorf("fetch playlist %q: %w", playlist.Name, err)
		}
		if err := importSpotifyPlaylist(db, playlist, items); err != nil {
			return fmt.Errorf("store playlist %q: %w", playlist.Name, err)
		}
		imported++
		log.Printf("Imported Spotify playlist %q (%d items)", playlist.Name, len(items))
	}

	log.Printf("Imported %d Spotify playlists", imported)
	return nil
}

func spotifyAuthorize(clientID, clientSecret string) (string, error) {
	stateBytes := make([]byte, 24)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	listener, err := net.Listen("tcp", "127.0.0.1:8787")
	if err != nil {
		return "", fmt.Errorf("listen for Spotify callback at %s: %w", spotifyRedirectURI, err)
	}

	result := make(chan spotifyOAuthResult, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
			result <- spotifyOAuthResult{err: fmt.Errorf("Spotify returned invalid OAuth state")}
			return
		}
		if spotifyErr := r.URL.Query().Get("error"); spotifyErr != "" {
			http.Error(w, "Spotify authorization denied", http.StatusForbidden)
			result <- spotifyOAuthResult{err: fmt.Errorf("Spotify authorization denied: %s", spotifyErr)}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Spotify did not return an authorization code", http.StatusBadRequest)
			result <- spotifyOAuthResult{err: fmt.Errorf("Spotify did not return an authorization code")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<p>Spotify authorization complete. Return to Groovebox terminal.</p>")
		result <- spotifyOAuthResult{code: code}
	})}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Spotify OAuth callback server error: %v", err)
		}
	}()
	defer server.Close()

	params := url.Values{
		"client_id":     {clientID},
		"response_type": {"code"},
		"redirect_uri":  {spotifyRedirectURI},
		"scope":         {"playlist-read-private playlist-read-collaborative"},
		"state":         {state},
	}
	log.Printf("Open this URL in browser and approve Spotify access:\nhttps://accounts.spotify.com/authorize?%s", params.Encode())

	select {
	case oauthResult := <-result:
		if oauthResult.err != nil {
			return "", oauthResult.err
		}
		return spotifyExchangeCode(clientID, clientSecret, oauthResult.code)
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("timed out waiting for Spotify authorization")
	}
}

func spotifyExchangeCode(clientID, clientSecret, code string) (string, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {spotifyRedirectURI},
	}
	req, err := http.NewRequest(http.MethodPost, "https://accounts.spotify.com/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Spotify token exchange HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token spotifyTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("Spotify token exchange returned no access token")
	}
	return token.AccessToken, nil
}

func spotifyPlaylists(accessToken string) ([]spotifyPlaylist, error) {
	next := "https://api.spotify.com/v1/me/playlists?limit=50"
	var playlists []spotifyPlaylist
	for next != "" {
		var page spotifyPlaylistPage
		if err := spotifyGet(accessToken, next, &page); err != nil {
			return nil, err
		}
		playlists = append(playlists, page.Items...)
		next = page.Next
	}
	return playlists, nil
}

func spotifyPlaylistItems(accessToken, playlistID string) ([]spotifyPlaylistItem, error) {
	next := "https://api.spotify.com/v1/playlists/" + url.PathEscape(playlistID) + "/items?limit=50&additional_types=track"
	var items []spotifyPlaylistItem
	for next != "" {
		var page spotifyPlaylistItemsPage
		if err := spotifyGet(accessToken, next, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		next = page.Next
	}
	return items, nil
}

func spotifyGet(accessToken, endpoint string, target interface{}) error {
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			resp.Body.Close()
			if wait < 1 {
				wait = 1
			}
			time.Sleep(time.Duration(wait) * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("Spotify API error HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		err = json.NewDecoder(resp.Body).Decode(target)
		resp.Body.Close()
		return err
	}
	return fmt.Errorf("Spotify API rate limit exceeded")
}

func importSpotifyPlaylist(db *sql.DB, playlist spotifyPlaylist, items []spotifyPlaylistItem) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var playlistID string
	err = tx.QueryRow("SELECT id FROM playlists WHERE spotify_id = ?", playlist.ID).Scan(&playlistID)
	if err == sql.ErrNoRows {
		// Reuse historical CSV import when its playlist name matches exactly.
		err = tx.QueryRow("SELECT id FROM playlists WHERE name = ? AND spotify_id IS NULL AND description LIKE 'Imported from Spotify%' LIMIT 1", playlist.Name).Scan(&playlistID)
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	playlistCreatedAt := spotifyPlaylistCreatedAt(items)
	if playlistID == "" {
		playlistID = uuid.New().String()
		if _, err := tx.Exec("INSERT INTO playlists (id, spotify_id, name, description, created_at) VALUES (?, ?, ?, ?, ?)", playlistID, playlist.ID, playlist.Name, playlist.Description, playlistCreatedAt); err != nil {
			return err
		}
	} else if _, err := tx.Exec("UPDATE playlists SET spotify_id = ?, name = ?, description = ?, created_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", playlist.ID, playlist.Name, playlist.Description, playlistCreatedAt, playlistID); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM playlist_tracks WHERE playlist_id = ?", playlistID); err != nil {
		return err
	}

	position := 0
	for _, item := range items {
		track := item.Item
		if track.ID == "" || track.Type != "track" {
			continue
		}
		position++

		albumID, err := spotifyAlbumID(tx, track)
		if err != nil {
			return err
		}
		trackID, err := spotifyTrackID(tx, albumID, track)
		if err != nil {
			return err
		}
		addedAt := playlistCreatedAt
		if timestamp, ok := spotifyTime(item.AddedAt); ok {
			addedAt = timestamp.Format("2006-01-02 15:04:05")
		}
		if _, err := tx.Exec("INSERT INTO playlist_tracks (playlist_id, track_id, position, added_at) VALUES (?, ?, ?, ?)", playlistID, trackID, position, addedAt); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func spotifyPlaylistCreatedAt(items []spotifyPlaylistItem) string {
	var earliest time.Time
	for _, item := range items {
		timestamp, ok := spotifyTime(item.AddedAt)
		if ok && (earliest.IsZero() || timestamp.Before(earliest)) {
			earliest = timestamp
		}
	}
	if earliest.IsZero() {
		return time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	return earliest.Format("2006-01-02 15:04:05")
}

func spotifyTime(value string) (time.Time, bool) {
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return timestamp.UTC(), true
}

func spotifyAlbumID(tx *sql.Tx, track spotifyTrack) (string, error) {
	artist := spotifyArtistNames(track.Album.Artists)
	if artist == "" {
		artist = spotifyArtistNames(track.Artists)
	}
	var albumID string
	err := tx.QueryRow("SELECT id FROM albums WHERE LOWER(title) = LOWER(?) AND LOWER(artist) = LOWER(?)", track.Album.Name, artist).Scan(&albumID)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if albumID != "" {
		return albumID, nil
	}

	year := 0
	if len(track.Album.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(track.Album.ReleaseDate[:4])
	}
	coverURL := ""
	if len(track.Album.Images) > 0 {
		coverURL = track.Album.Images[0].URL
	}
	albumID = uuid.New().String()
	_, err = tx.Exec(`INSERT INTO albums (id, title, artist, release_year, cover_image_url, streaming_notes)
		VALUES (?, ?, ?, ?, ?, ?)`, albumID, track.Album.Name, artist, year, coverURL, "Spotify Album")
	return albumID, err
}

func spotifyTrackID(tx *sql.Tx, albumID string, track spotifyTrack) (string, error) {
	artist := spotifyArtistNames(track.Artists)
	var trackID string
	err := tx.QueryRow("SELECT id FROM tracks WHERE spotify_id = ?", track.ID).Scan(&trackID)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if trackID != "" {
		_, err = tx.Exec(`UPDATE tracks SET album_id = ?, title = ?, artist = ?, track_number = ?, duration_ms = ?, isrc = ? WHERE id = ?`,
			albumID, track.Name, artist, strconv.Itoa(track.TrackNum), track.DurationMs, track.ExternalIDs.ISRC, trackID)
		return trackID, err
	}

	trackID = uuid.New().String()
	_, err = tx.Exec(`INSERT INTO tracks (id, album_id, title, artist, track_number, duration_ms, isrc, spotify_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, trackID, albumID, track.Name, artist, strconv.Itoa(track.TrackNum), track.DurationMs, track.ExternalIDs.ISRC, track.ID)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec("INSERT INTO search_fts (target_type, target_id, title, artist) VALUES ('track', ?, ?, ?)", trackID, track.Name, artist)
	return trackID, err
}

func spotifyArtistNames(artists []struct {
	Name string `json:"name"`
}) string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if artist.Name != "" {
			names = append(names, artist.Name)
		}
	}
	return strings.Join(names, ", ")
}
