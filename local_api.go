package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const musicRootEnv = "GROOVEBOX_MUSIC_ROOT"

// libRoot returns the music library root (overridable for tests).
func libRoot() string {
	if r := os.Getenv(musicRootEnv); r != "" {
		return r
	}
	return musicLibrary
}

// LocalAlbum is one album that has local audio files.
type LocalAlbum struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	ReleaseYear int    `json:"release_year"`
	CoverURL    string `json:"cover_image_url"`
	TrackCount  int    `json:"track_count"`
	RawCount    int    `json:"raw_count"`
}

// LocalFileRow is a DB representation of an audio_files row with context.
type LocalFileRow struct {
	ID         string `json:"id"`
	AlbumID    string `json:"album_id"`
	TrackID    *string `json:"track_id,omitempty"`
	ReleaseID  *string `json:"release_id,omitempty"`
	Relpath    string `json:"relpath"`
	AbsPath    string `json:"abs_path,omitempty"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	Format     string `json:"format"`
	BitDepth   int    `json:"bit_depth"`
	SampleRate int    `json:"sample_rate"`
	SizeBytes  int64  `json:"size_bytes"`
	DurationMs int64  `json:"duration_ms"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	CoverURL   string `json:"cover_url,omitempty"`
}

// localAlbumRows returns all albums that have at least one local audio file.
func localAlbumRows(db *sql.DB) ([]LocalAlbum, error) {
	rows, err := db.Query(`
		SELECT DISTINCT a.id, a.title, a.artist, COALESCE(a.release_year,0), COALESCE(a.cover_image_url,''),
		       (SELECT COUNT(*) FROM audio_files f WHERE f.album_id=a.id AND f.kind='track'),
		       (SELECT COUNT(*) FROM audio_files f WHERE f.album_id=a.id AND f.kind='raw')
		FROM audio_files f JOIN albums a ON f.album_id=a.id
		ORDER BY a.artist ASC, a.title ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []LocalAlbum
	for rows.Next() {
		var alb LocalAlbum
		var tc, rc int
		if err := rows.Scan(&alb.ID, &alb.Title, &alb.Artist, &alb.ReleaseYear, &alb.CoverURL, &tc, &rc); err == nil {
			alb.TrackCount = tc
			alb.RawCount = rc
			list = append(list, alb)
		}
	}
	return list, nil
}

// localFilesForAlbum collects all audio_files (tracks + raws) for an album.
func localFilesForAlbum(db *sql.DB, albumID string) ([]LocalFileRow, error) {
	rows, err := db.Query(`
		SELECT f.id, f.album_id, COALESCE(f.track_id,''), COALESCE(f.release_id,''), f.relpath, f.kind,
		       COALESCE(f.source,''), COALESCE(f.format,''), COALESCE(f.bit_depth,0), COALESCE(f.sample_rate,0),
		       COALESCE(f.size_bytes,0), COALESCE(f.duration_ms,0),
		       COALESCE(t.title,''), COALESCE(t.artist,''), COALESCE(a.title,''), COALESCE(a.cover_image_url,'')
		FROM audio_files f
		LEFT JOIN tracks t ON f.track_id=t.id
		LEFT JOIN albums a ON f.album_id=a.id
		WHERE f.album_id=?
		ORDER BY f.kind ASC, f.relpath ASC`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	root := libRoot()
	var list []LocalFileRow
	for rows.Next() {
		var f LocalFileRow
		var trackID, releaseID string
		if err := rows.Scan(&f.ID, &f.AlbumID, &trackID, &releaseID, &f.Relpath, &f.Kind, &f.Source, &f.Format,
			&f.BitDepth, &f.SampleRate, &f.SizeBytes, &f.DurationMs, &f.Title, &f.Artist, &f.Album, &f.CoverURL); err == nil {
			if trackID != "" {
				f.TrackID = &trackID
			}
			if releaseID != "" {
				f.ReleaseID = &releaseID
			}
			f.AbsPath = filepath.Join(root, filepath.FromSlash(f.Relpath))
			list = append(list, f)
		}
	}
	return list, nil
}

// fileToQueueEntry converts an audio_files row into a QueueEntry.
func fileToQueueEntry(f LocalFileRow) QueueEntry {
	title, artist := f.Title, f.Artist
	if f.Kind == "raw" {
		title = strings.TrimSuffix(filepath.Base(f.Relpath), filepath.Ext(f.Relpath))
	}
	if artist == "" {
		artist = f.Album
	}
	return QueueEntry{
		FileID:   f.ID,
		Title:    title,
		Artist:   artist,
		AlbumID:  f.AlbumID,
		Album:    f.Album,
		Path:     f.AbsPath,
		CoverURL: f.CoverURL,
		Duration: f.DurationMs,
		Kind:     f.Kind,
	}
}

// buildAlbumQueue returns the ordered queue of on-disk playable files.
func buildAlbumQueue(db *sql.DB, albumID string) []QueueEntry {
	files, err := localFilesForAlbum(db, albumID)
	if err != nil {
		return nil
	}
	var queue []QueueEntry
	for _, f := range files {
		if _, serr := os.Stat(f.AbsPath); serr != nil {
			continue
		}
		queue = append(queue, fileToQueueEntry(f))
	}
	return queue
}

// --- HTTP handlers ---

func postSyncLocal(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if GetSyncProgress().IsSyncing {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "Sync already in progress"})
			return
		}
		go func() {
			if err := scanLibrary(db); err != nil {
				log.Printf("local sync error: %v", err)
			}
		}()
		json.NewEncoder(w).Encode(map[string]string{"status": "started", "message": "Local library sync started in background"})
	}
}

// serveLocalCover serves a local cover image (folder.jpg / large_cover.jpg).
// rel is a musicLibrary-relative path; verification + content-type applied.
func serveLocalCover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("rel")
		if rel == "" {
			http.Error(w, "rel required", 400)
			return
		}
		clean := filepath.Clean(rel)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			http.Error(w, "bad path", 400)
			return
		}
		p := filepath.Join(libRoot(), clean)
		if _, err := os.Stat(p); err != nil {
			http.Error(w, "not found", 404)
			return
		}
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		default:
			w.Header().Set("Content-Type", "image/jpeg")
		}
		http.ServeFile(w, r, p)
	}
}

func getLocalAlbums(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		list, err := localAlbumRows(db)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(list)
	}
}

func getLocalAlbum(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/local/albums/")
		if id == "" {
			http.Error(w, "album id required", 400)
			return
		}
		files, err := localFilesForAlbum(db, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(files)
	}
}

// playerKey decodes an int from a JSON body key.
func playerKey(r *http.Request, key string) int {
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if v, ok := body[key].(float64); ok {
		return int(v)
	}
	return -1
}

func getPlaybackState() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(player.State())
	}
}

func playAlbum(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body struct {
			AlbumID string `json:"album_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AlbumID == "" {
			http.Error(w, "album_id required", 400)
			return
		}
		queue := buildAlbumQueue(db, body.AlbumID)
		if len(queue) == 0 {
			http.Error(w, "album has no local audio", 404)
			return
		}
		player.PlayQueue(queue, 0)
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "started", "count": len(queue)})
	}
}

// playFile builds the containing album queue and starts it at the given file.
func playFile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body struct {
			FileID string `json:"file_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FileID == "" {
			http.Error(w, "file_id required", 400)
			return
		}
		var albumID string
		if err := db.QueryRow("SELECT album_id FROM audio_files WHERE id=?", body.FileID).Scan(&albumID); err != nil {
			http.Error(w, "file not found", 404)
			return
		}
		queue := buildAlbumQueue(db, albumID)
		idx := -1
		for i, e := range queue {
			if e.FileID == body.FileID {
				idx = i
				break
			}
		}
		if len(queue) == 0 || idx < 0 {
			http.Error(w, "file not playable", 404)
			return
		}
		player.PlayQueue(queue, idx)
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "started", "index": idx, "count": len(queue)})
	}
}

func linkRaw(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body struct {
			FileID    string `json:"file_id"`
			ReleaseID string `json:"release_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FileID == "" {
			http.Error(w, "file_id required", 400)
			return
		}
		if body.ReleaseID != "" {
			if err := db.QueryRow("SELECT id FROM release_versions WHERE id=?", body.ReleaseID).Scan(&body.ReleaseID); err != nil {
				http.Error(w, "invalid release_id", 400)
				return
			}
		}
		_, err := db.Exec("UPDATE audio_files SET release_id=? WHERE id=?", body.ReleaseID, body.FileID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

func setVolume() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		v := playerKey(r, "volume")
		if v < 0 {
			http.Error(w, "volume required", 400)
			return
		}
		player.SetVolume(v)
		json.NewEncoder(w).Encode(map[string]int{"volume": v})
	}
}