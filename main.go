package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type StatsResponse struct {
	TotalTracks    int `json:"total_tracks"`
	TotalAlbums    int `json:"total_albums"`
	TotalPlaylists int `json:"total_playlists"`
}

type PlaylistSummary struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	TrackCount   int      `json:"track_count"`
	CreatedAt    string   `json:"created_at"`
	CoverArtURLs []string `json:"cover_art_urls"`
}

type TrackDetail struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	DurationMs    int    `json:"duration_ms"`
	SpotifyID     string `json:"spotify_id"`
	AlbumID       string `json:"album_id"`
	AlbumTitle    string `json:"album_title"`
	CoverImageURL string `json:"cover_image_url"`
	Position      int    `json:"position"`
}

type ReleaseVersion struct {
	ID                string `json:"id"`
	DiscogsReleaseID  int    `json:"discogs_release_id"`
	Title             string `json:"title"`
	Artist            string `json:"artist"`
	Label             string `json:"label"`
	CatalogNumber     string `json:"catalog_number"`
	ReleaseYear       int    `json:"release_year"`
	CoverImageURL     string `json:"cover_image_url"`
	FormatDescription string `json:"format_description"`
	Source            string `json:"source"`
	HasVinyl          bool   `json:"has_vinyl"`
}

type AlbumDetailResponse struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Artist          string           `json:"artist"`
	ReleaseYear     int              `json:"release_year"`
	DiscogsMasterID int              `json:"discogs_master_id"`
	CoverImageURL   string           `json:"cover_image_url"`
	HasVinyl        bool             `json:"has_vinyl"`
	InWantlist      bool             `json:"in_wantlist"`
	StreamingNotes  string           `json:"streaming_notes"`
	Tracks          []TrackDetail    `json:"tracks"`
	Versions        []ReleaseVersion `json:"versions"`
}

type AlbumSummary struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Artist         string `json:"artist"`
	ReleaseYear    int    `json:"release_year"`
	CoverImageURL  string `json:"cover_image_url"`
	HasVinyl       bool   `json:"has_vinyl"`
	InWantlist     bool   `json:"in_wantlist"`
	StreamingNotes string `json:"streaming_notes"`
	VersionCount   int    `json:"version_count"`
	TrackCount     int    `json:"track_count"`
}

func main() {
	syncDiscogsFlag := flag.Bool("sync-discogs", false, "Sync Discogs collection & wantlist into database")
	dedupeAlbumsFlag := flag.Bool("dedupe-albums", false, "Safely merge duplicate master albums based on title normalization & track overlap")
	dbPath := flag.String("db", "music.db", "Path to SQLite database")
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	db, err := initDB(*dbPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()

	if *syncDiscogsFlag {
		log.Println("Starting Discogs collection & wantlist sync...")
		if err := SyncDiscogs(db); err != nil {
			log.Fatalf("Discogs sync failed: %v", err)
		}
		log.Println("Discogs sync completed successfully!")
		if len(os.Args) > 1 && strings.Contains(os.Args[1], "sync-discogs") {
			return
		}
	}

	if *dedupeAlbumsFlag {
		log.Println("Running lossless album deduplication...")
		mergedCount, err := DedupeAlbums(db)
		if err != nil {
			log.Fatalf("Album deduplication failed: %v", err)
		}
		log.Printf("Album deduplication completed! Merged %d duplicate album records.", mergedCount)
		return
	}

	// API Routes
	http.HandleFunc("/api/sync/discogs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		prog := GetSyncProgress()
		if prog.IsSyncing {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "Sync already in progress"})
			return
		}

		go func() {
			if err := SyncDiscogs(db); err != nil {
				log.Printf("Background Discogs sync error: %v", err)
			}
		}()

		json.NewEncoder(w).Encode(map[string]string{"status": "started", "message": "Discogs sync started in background"})
	})

	http.HandleFunc("/api/sync/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetSyncProgress())
	})

	http.HandleFunc("/api/albums/dedupe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		prog := GetSyncProgress()
		if prog.IsSyncing {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "Operation already in progress"})
			return
		}

		go func() {
			_, err := DedupeAlbums(db)
			if err != nil {
				log.Printf("Album deduplication error: %v", err)
			}
		}()

		json.NewEncoder(w).Encode(map[string]string{"status": "started", "message": "Album deduplication started in background"})
	})

	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var stats StatsResponse
		_ = db.QueryRow("SELECT COUNT(*) FROM tracks").Scan(&stats.TotalTracks)
		_ = db.QueryRow("SELECT COUNT(*) FROM albums").Scan(&stats.TotalAlbums)
		_ = db.QueryRow("SELECT COUNT(*) FROM playlists").Scan(&stats.TotalPlaylists)
		json.NewEncoder(w).Encode(stats)
	})

	http.HandleFunc("/api/playlists", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var req struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
				http.Error(w, "Valid playlist name is required", 400)
				return
			}
			newID := uuid.New().String()
			now := time.Now().Format("2006-01-02 15:04:05")
			_, err := db.Exec(`INSERT INTO playlists (id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
				newID, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), now, now)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": newID, "name": req.Name, "description": req.Description})
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", 405)
			return
		}

		sortOrder := r.URL.Query().Get("sort")
		orderBy := "p.name ASC"
		switch sortOrder {
		case "name_desc":
			orderBy = "p.name DESC"
		case "date_desc":
			orderBy = "p.created_at DESC"
		case "date_asc":
			orderBy = "p.created_at ASC"
		case "name_asc":
			orderBy = "p.name ASC"
		}

		query := fmt.Sprintf(`
			SELECT p.id, p.name, COALESCE(p.description, ''), COUNT(pt.track_id), COALESCE(p.created_at, '')
			FROM playlists p
			LEFT JOIN playlist_tracks pt ON p.id = pt.playlist_id
			GROUP BY p.id
			ORDER BY %s`, orderBy)

		rows, err := db.Query(query)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var playlists []PlaylistSummary
		for rows.Next() {
			var p PlaylistSummary
			if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.TrackCount, &p.CreatedAt); err == nil {
				coverRows, cErr := db.Query(`
					SELECT DISTINCT a.cover_image_url
					FROM playlist_tracks pt
					JOIN tracks t ON pt.track_id = t.id
					JOIN albums a ON t.album_id = a.id
					WHERE pt.playlist_id = ? AND a.cover_image_url IS NOT NULL AND a.cover_image_url != ''
					ORDER BY pt.position ASC
					LIMIT 4`, p.ID)
				if cErr == nil {
					for coverRows.Next() {
						var url string
						if coverRows.Scan(&url) == nil {
							p.CoverArtURLs = append(p.CoverArtURLs, url)
						}
					}
					coverRows.Close()
				}
				playlists = append(playlists, p)
			}
		}
		json.NewEncoder(w).Encode(playlists)
	})

	http.HandleFunc("/api/playlists/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		subPath := strings.TrimPrefix(r.URL.Path, "/api/playlists/")
		parts := strings.Split(strings.Trim(subPath, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			http.Error(w, "Playlist ID required", 400)
			return
		}
		playlistID := parts[0]

		// Handle /api/playlists/:id/tracks
		if len(parts) >= 2 && parts[1] == "tracks" {
			if r.Method == http.MethodPost {
				var req struct {
					TrackID string `json:"track_id"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TrackID == "" {
					http.Error(w, "track_id is required", 400)
					return
				}
				var maxPos int
				_ = db.QueryRow("SELECT COALESCE(MAX(position), 0) FROM playlist_tracks WHERE playlist_id = ?", playlistID).Scan(&maxPos)
				now := time.Now().Format("2006-01-02 15:04:05")
				_, err := db.Exec("INSERT OR IGNORE INTO playlist_tracks (playlist_id, track_id, position, added_at) VALUES (?, ?, ?, ?)",
					playlistID, req.TrackID, maxPos+1, now)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				_, _ = db.Exec("UPDATE playlists SET updated_at = ? WHERE id = ?", now, playlistID)
				json.NewEncoder(w).Encode(map[string]bool{"success": true})
				return
			}

			if r.Method == http.MethodDelete {
				trackID := r.URL.Query().Get("track_id")
				posStr := r.URL.Query().Get("position")
				if trackID == "" && posStr == "" {
					http.Error(w, "track_id or position query parameter required", 400)
					return
				}

				if posStr != "" {
					pos, _ := strconv.Atoi(posStr)
					_, err := db.Exec("DELETE FROM playlist_tracks WHERE playlist_id = ? AND position = ?", playlistID, pos)
					if err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
				} else {
					_, err := db.Exec("DELETE FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?", playlistID, trackID)
					if err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
				}

				// Re-index remaining track positions sequentially
				rows, err := db.Query("SELECT track_id FROM playlist_tracks WHERE playlist_id = ? ORDER BY position ASC", playlistID)
				if err == nil {
					var remainingTracks []string
					for rows.Next() {
						var tID string
						if rows.Scan(&tID) == nil {
							remainingTracks = append(remainingTracks, tID)
						}
					}
					rows.Close()

					tx, _ := db.Begin()
					if tx != nil {
						_, _ = tx.Exec("DELETE FROM playlist_tracks WHERE playlist_id = ?", playlistID)
						now := time.Now().Format("2006-01-02 15:04:05")
						for idx, tID := range remainingTracks {
							_, _ = tx.Exec("INSERT INTO playlist_tracks (playlist_id, track_id, position, added_at) VALUES (?, ?, ?, ?)", playlistID, tID, idx+1, now)
						}
						_, _ = tx.Exec("UPDATE playlists SET updated_at = ? WHERE id = ?", now, playlistID)
						_ = tx.Commit()
					}
				}

				json.NewEncoder(w).Encode(map[string]bool{"success": true})
				return
			}
		}

		if r.Method == http.MethodPut {
			var req struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
				http.Error(w, "Valid playlist name is required", 400)
				return
			}
			now := time.Now().Format("2006-01-02 15:04:05")
			res, err := db.Exec("UPDATE playlists SET name = ?, description = ?, updated_at = ? WHERE id = ?",
				strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), now, playlistID)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			affected, _ := res.RowsAffected()
			if affected == 0 {
				http.Error(w, "Playlist not found", 404)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"id": playlistID, "name": req.Name, "description": req.Description})
			return
		}

		if r.Method == http.MethodDelete {
			_, err := db.Exec("DELETE FROM playlist_tracks WHERE playlist_id = ?", playlistID)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			res, err := db.Exec("DELETE FROM playlists WHERE id = ?", playlistID)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			affected, _ := res.RowsAffected()
			if affected == 0 {
				http.Error(w, "Playlist not found", 404)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", 405)
			return
		}

		rows, err := db.Query(`
			SELECT t.id, t.title, t.artist, t.duration_ms, COALESCE(t.spotify_id, ''),
			       COALESCE(t.album_id, ''), COALESCE(a.title, ''), COALESCE(a.cover_image_url, ''), pt.position
			FROM playlist_tracks pt
			JOIN tracks t ON pt.track_id = t.id
			LEFT JOIN albums a ON t.album_id = a.id
			WHERE pt.playlist_id = ?
			ORDER BY pt.position ASC`, playlistID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var tracks []TrackDetail
		for rows.Next() {
			var t TrackDetail
			if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.DurationMs, &t.SpotifyID, &t.AlbumID, &t.AlbumTitle, &t.CoverImageURL, &t.Position); err == nil {
				tracks = append(tracks, t)
			}
		}
		json.NewEncoder(w).Encode(tracks)
	})

	http.HandleFunc("/api/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var req struct {
				Title       string `json:"title"`
				Artist      string `json:"artist"`
				AlbumTitle  string `json:"album_title"`
				DurationMs  int    `json:"duration_ms"`
				SpotifyID   string `json:"spotify_id"`
				CoverArtURL string `json:"cover_image_url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
				http.Error(w, "Track title is required", 400)
				return
			}

			req.Title = strings.TrimSpace(req.Title)
			req.Artist = strings.TrimSpace(req.Artist)
			if req.Artist == "" {
				req.Artist = "Unknown Artist"
			}
			req.AlbumTitle = strings.TrimSpace(req.AlbumTitle)
			if req.AlbumTitle == "" {
				req.AlbumTitle = "Single"
			}

			// Find existing canonical album or create one
			var albumID string
			err := db.QueryRow("SELECT id FROM albums WHERE LOWER(title) = LOWER(?) AND LOWER(artist) = LOWER(?)", req.AlbumTitle, req.Artist).Scan(&albumID)
			if err != nil {
				albumID = uuid.New().String()
				now := time.Now().Format("2006-01-02 15:04:05")
				_, err = db.Exec("INSERT INTO albums (id, title, artist, cover_image_url, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
					albumID, req.AlbumTitle, req.Artist, req.CoverArtURL, now, now)
				if err != nil {
					http.Error(w, "Failed to create album: "+err.Error(), 500)
					return
				}
			}

			trackID := uuid.New().String()
			now := time.Now().Format("2006-01-02 15:04:05")
			_, err = db.Exec(`INSERT INTO tracks (id, album_id, title, artist, duration_ms, spotify_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				trackID, albumID, req.Title, req.Artist, req.DurationMs, req.SpotifyID, now)
			if err != nil {
				http.Error(w, "Failed to create track: "+err.Error(), 500)
				return
			}

			// Insert into search index
			_, _ = db.Exec("INSERT INTO search_fts (target_type, target_id, title, artist) VALUES ('track', ?, ?, ?)", trackID, req.Title, req.Artist)

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"id":          trackID,
				"title":       req.Title,
				"artist":      req.Artist,
				"album_id":    albumID,
				"album_title": req.AlbumTitle,
			})
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", 405)
			return
		}

		rows, err := db.Query(`
			SELECT t.id, t.title, t.artist, t.duration_ms, COALESCE(t.spotify_id, ''),
			       COALESCE(t.album_id, ''), COALESCE(a.title, ''), COALESCE(a.cover_image_url, ''), 0
			FROM tracks t
			LEFT JOIN albums a ON t.album_id = a.id
			ORDER BY t.created_at DESC, t.title ASC
			LIMIT 500`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var tracks []TrackDetail
		for rows.Next() {
			var t TrackDetail
			if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.DurationMs, &t.SpotifyID, &t.AlbumID, &t.AlbumTitle, &t.CoverImageURL, &t.Position); err == nil {
				tracks = append(tracks, t)
			}
		}
		json.NewEncoder(w).Encode(tracks)
	})

	type ArtistSummary struct {
		Name       string `json:"name"`
		ImageURL   string `json:"image_url"`
		AlbumCount int    `json:"album_count"`
		TrackCount int    `json:"track_count"`
	}

	http.HandleFunc("/api/artists", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := db.Query(`
			WITH all_artists AS (
				SELECT DISTINCT artist FROM albums WHERE artist IS NOT NULL AND artist != ''
				UNION
				SELECT DISTINCT artist FROM tracks WHERE artist IS NOT NULL AND artist != ''
			)
			SELECT a.artist,
			       COALESCE(al.cover_image_url, ''),
			       (SELECT COUNT(*) FROM albums WHERE artist = a.artist) as album_count,
			       (SELECT COUNT(*) FROM tracks WHERE artist = a.artist) as track_count
			FROM all_artists a
			LEFT JOIN albums al ON al.artist = a.artist AND al.cover_image_url IS NOT NULL AND al.cover_image_url != ''
			GROUP BY a.artist
			ORDER BY album_count DESC, track_count DESC, a.artist ASC
			LIMIT 500`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var artists []ArtistSummary
		for rows.Next() {
			var a ArtistSummary
			if err := rows.Scan(&a.Name, &a.ImageURL, &a.AlbumCount, &a.TrackCount); err == nil {
				artists = append(artists, a)
			}
		}
		json.NewEncoder(w).Encode(artists)
	})

	type ArtistDetailResponse struct {
		Name   string         `json:"name"`
		Albums []AlbumSummary `json:"albums"`
		Tracks []TrackDetail  `json:"tracks"`
	}

	http.HandleFunc("/api/artists/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		artistName := r.URL.Path[len("/api/artists/"):]
		if artistName == "" {
			http.Error(w, "Artist name required", 400)
			return
		}

		var detail ArtistDetailResponse
		detail.Name = artistName

		// Query albums by artist
		aRows, aErr := db.Query(`
			SELECT a.id, a.title, a.artist, COALESCE(a.release_year, 0), COALESCE(a.cover_image_url, ''),
			       COALESCE(a.has_vinyl, 0), COALESCE(a.in_wantlist, 0), COALESCE(a.streaming_notes, ''),
			       (SELECT COUNT(*) FROM release_versions rv WHERE rv.album_id = a.id) as version_count,
			       (SELECT COUNT(*) FROM tracks t WHERE t.album_id = a.id) as track_count
			FROM albums a
			WHERE LOWER(a.artist) = LOWER(?)
			ORDER BY a.release_year DESC, a.title ASC`, artistName)
		if aErr == nil {
			for aRows.Next() {
				var alb AlbumSummary
				var hasVinylInt, inWantlistInt int
				if aRows.Scan(&alb.ID, &alb.Title, &alb.Artist, &alb.ReleaseYear, &alb.CoverImageURL, &hasVinylInt, &inWantlistInt, &alb.StreamingNotes, &alb.VersionCount, &alb.TrackCount) == nil {
					alb.HasVinyl = hasVinylInt == 1
					alb.InWantlist = inWantlistInt == 1
					detail.Albums = append(detail.Albums, alb)
				}
			}
			aRows.Close()
		}

		// Query tracks by artist
		tRows, tErr := db.Query(`
			SELECT t.id, t.title, t.artist, t.duration_ms, COALESCE(t.spotify_id, ''),
			       COALESCE(t.album_id, ''), COALESCE(a.title, ''), COALESCE(a.cover_image_url, ''), 0
			FROM tracks t
			LEFT JOIN albums a ON t.album_id = a.id
			WHERE LOWER(t.artist) = LOWER(?)
			ORDER BY t.title ASC`, artistName)
		if tErr == nil {
			for tRows.Next() {
				var t TrackDetail
				if tRows.Scan(&t.ID, &t.Title, &t.Artist, &t.DurationMs, &t.SpotifyID, &t.AlbumID, &t.AlbumTitle, &t.CoverImageURL, &t.Position) == nil {
					detail.Tracks = append(detail.Tracks, t)
				}
			}
			tRows.Close()
		}

		json.NewEncoder(w).Encode(detail)
	})

	type AlbumSummary struct {
		ID             string `json:"id"`
		Title          string `json:"title"`
		Artist         string `json:"artist"`
		ReleaseYear    int    `json:"release_year"`
		CoverImageURL  string `json:"cover_image_url"`
		HasVinyl       bool   `json:"has_vinyl"`
		InCollection   bool   `json:"in_collection"`
		InWantlist     bool   `json:"in_wantlist"`
		PrimaryFormat  string `json:"primary_format"`
		StreamingNotes string `json:"streaming_notes"`
		VersionCount   int    `json:"version_count"`
		TrackCount     int    `json:"track_count"`
	}

	type AlbumCounts struct {
		All        int `json:"all"`
		Collection int `json:"collection"`
		Wantlist   int `json:"wantlist"`
	}

	http.HandleFunc("/api/albums/counts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var counts AlbumCounts
		db.QueryRow("SELECT COUNT(*) FROM albums").Scan(&counts.All)
		db.QueryRow("SELECT COUNT(*) FROM albums WHERE in_collection = 1").Scan(&counts.Collection)
		db.QueryRow("SELECT COUNT(*) FROM albums WHERE in_wantlist = 1").Scan(&counts.Wantlist)
		json.NewEncoder(w).Encode(counts)
	})

	http.HandleFunc("/api/albums", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		filter := r.URL.Query().Get("filter")
		q := strings.TrimSpace(r.URL.Query().Get("q"))

		var whereClauses []string
		var args []interface{}

		if filter == "collection" {
			whereClauses = append(whereClauses, "a.in_collection = 1")
		} else if filter == "wantlist" {
			whereClauses = append(whereClauses, "a.in_wantlist = 1")
		}

		if q != "" {
			whereClauses = append(whereClauses, "(LOWER(a.title) LIKE ? OR LOWER(a.artist) LIKE ?)")
			searchTerm := "%" + strings.ToLower(q) + "%"
			args = append(args, searchTerm, searchTerm)
		}

		whereStr := ""
		if len(whereClauses) > 0 {
			whereStr = "WHERE " + strings.Join(whereClauses, " AND ")
		}

		// Collection view sorts by most-recently-added-to-Discogs-collection first;
		// albums without a recorded date (e.g. added before this field existed) sort last.
		orderBy := "a.title ASC"
		if filter == "collection" {
			orderBy = "CASE WHEN a.collection_added_at IS NULL THEN 1 ELSE 0 END ASC, a.collection_added_at DESC, a.title ASC"
		}

		query := fmt.Sprintf(`
			SELECT a.id, a.title, a.artist, COALESCE(a.release_year, 0), COALESCE(a.cover_image_url, ''),
			       COALESCE(a.has_vinyl, 0), COALESCE(a.in_collection, 0), COALESCE(a.in_wantlist, 0), COALESCE(a.streaming_notes, ''),
			       COALESCE((SELECT rv.format_description FROM release_versions rv WHERE rv.album_id = a.id AND rv.source = 'collection' AND rv.format_description IS NOT NULL AND rv.format_description != '' LIMIT 1), ''),
			       (SELECT COUNT(*) FROM release_versions rv WHERE rv.album_id = a.id) as version_count,
			       (SELECT COUNT(*) FROM tracks t WHERE t.album_id = a.id) as track_count
			FROM albums a
			%s
			ORDER BY %s
			LIMIT 5000`, whereStr, orderBy)

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var albums []AlbumSummary
		for rows.Next() {
			var alb AlbumSummary
			var hasVinylInt, inCollectionInt, inWantlistInt int
			if err := rows.Scan(&alb.ID, &alb.Title, &alb.Artist, &alb.ReleaseYear, &alb.CoverImageURL, &hasVinylInt, &inCollectionInt, &inWantlistInt, &alb.StreamingNotes, &alb.PrimaryFormat, &alb.VersionCount, &alb.TrackCount); err == nil {
				alb.HasVinyl = hasVinylInt == 1
				alb.InCollection = inCollectionInt == 1
				alb.InWantlist = inWantlistInt == 1
				albums = append(albums, alb)
			}
		}
		json.NewEncoder(w).Encode(albums)
	})

	http.HandleFunc("/api/albums/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		albumID := r.URL.Path[len("/api/albums/"):]
		if albumID == "" {
			http.Error(w, "Album ID required", 400)
			return
		}

		var alb AlbumDetailResponse
		var masterID int
		var hasVinylInt, inWantlistInt int
		err := db.QueryRow(`
			SELECT id, title, artist, COALESCE(release_year, 0), COALESCE(discogs_master_id, 0),
			       COALESCE(cover_image_url, ''), COALESCE(has_vinyl, 0), COALESCE(in_wantlist, 0), COALESCE(streaming_notes, '')
			FROM albums WHERE id = ?`, albumID).Scan(
			&alb.ID, &alb.Title, &alb.Artist, &alb.ReleaseYear, &masterID, &alb.CoverImageURL, &hasVinylInt, &inWantlistInt, &alb.StreamingNotes,
		)
		if err != nil {
			http.Error(w, "Album not found", 404)
			return
		}
		alb.DiscogsMasterID = masterID
		alb.HasVinyl = hasVinylInt == 1
		alb.InWantlist = inWantlistInt == 1

		// Get tracks
		tRows, tErr := db.Query(`
			SELECT t.id, t.title, t.artist, t.duration_ms, COALESCE(t.spotify_id, ''),
			       COALESCE(t.album_id, ''), a.title, COALESCE(a.cover_image_url, ''), 0
			FROM tracks t
			JOIN albums a ON t.album_id = a.id
			WHERE t.album_id = ?
			ORDER BY CAST(t.track_number AS INTEGER) ASC, t.title ASC`, albumID)
		if tErr == nil {
			for tRows.Next() {
				var t TrackDetail
				if tRows.Scan(&t.ID, &t.Title, &t.Artist, &t.DurationMs, &t.SpotifyID, &t.AlbumID, &t.AlbumTitle, &t.CoverImageURL, &t.Position) == nil {
					alb.Tracks = append(alb.Tracks, t)
				}
			}
			tRows.Close()
		}

		// Get release versions
		vRows, vErr := db.Query(`
			SELECT id, COALESCE(discogs_release_id, 0), title, artist, COALESCE(label, ''),
			       COALESCE(catalog_number, ''), COALESCE(release_year, 0), COALESCE(cover_image_url, ''),
			       COALESCE(format_description, ''), source, COALESCE(has_vinyl, 0)
			FROM release_versions
			WHERE album_id = ?
			ORDER BY release_year ASC`, albumID)
		if vErr == nil {
			for vRows.Next() {
				var v ReleaseVersion
				var vHasVinyl int
				if vRows.Scan(&v.ID, &v.DiscogsReleaseID, &v.Title, &v.Artist, &v.Label, &v.CatalogNumber, &v.ReleaseYear, &v.CoverImageURL, &v.FormatDescription, &v.Source, &vHasVinyl) == nil {
					v.HasVinyl = vHasVinyl == 1
					alb.Versions = append(alb.Versions, v)
				}
			}
			vRows.Close()
		}

		json.NewEncoder(w).Encode(alb)
	})

	http.HandleFunc("/api/autocomplete", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(q) < 1 {
			json.NewEncoder(w).Encode([]map[string]string{})
			return
		}

		type AutoItem struct {
			Title      string `json:"title"`
			Artist     string `json:"artist"`
			AlbumTitle string `json:"album_title"`
		}

		likePattern := "%" + strings.ToLower(q) + "%"
		rows, err := db.Query(`
			SELECT t.title, COALESCE(t.artist, ''), COALESCE(a.title, '')
			FROM tracks t
			LEFT JOIN albums a ON t.album_id = a.id
			WHERE LOWER(t.title) LIKE ? OR LOWER(t.artist) LIKE ?
			ORDER BY CASE WHEN LOWER(t.title) LIKE ? THEN 0 ELSE 1 END, t.title ASC
			LIMIT 15`, likePattern, likePattern, strings.ToLower(q)+"%")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var results []AutoItem
		seen := make(map[string]bool)
		for rows.Next() {
			var item AutoItem
			if err := rows.Scan(&item.Title, &item.Artist, &item.AlbumTitle); err == nil {
				key := strings.ToLower(item.Title + " - " + item.Artist)
				if !seen[key] {
					seen[key] = true
					results = append(results, item)
				}
			}
		}
		json.NewEncoder(w).Encode(results)
	})

	http.HandleFunc("/api/autocomplete/online", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(q) < 2 {
			json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}

		apiURL := fmt.Sprintf("https://itunes.apple.com/search?term=%s&media=music&entity=song&limit=10", url.QueryEscape(q))
		resp, err := http.Get(apiURL)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer resp.Body.Close()

		var itunesRes struct {
			Results []struct {
				TrackName      string `json:"trackName"`
				ArtistName     string `json:"artistName"`
				CollectionName string `json:"collectionName"`
				TrackTimeMillis int   `json:"trackTimeMillis"`
				ArtworkUrl100  string `json:"artworkUrl100"`
			} `json:"results"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&itunesRes); err != nil {
			json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}

		type OnlineResult struct {
			Title       string `json:"title"`
			Artist      string `json:"artist"`
			AlbumTitle  string `json:"album_title"`
			DurationMs  int    `json:"duration_ms"`
			CoverArtURL string `json:"cover_image_url"`
		}

		var results []OnlineResult
		for _, item := range itunesRes.Results {
			// Upgrade 100x100 artwork to 300x300 for higher fidelity cover art
			highResCover := strings.Replace(item.ArtworkUrl100, "100x100bb", "300x300bb", 1)
			results = append(results, OnlineResult{
				Title:       item.TrackName,
				Artist:      item.ArtistName,
				AlbumTitle:  item.CollectionName,
				DurationMs:  item.TrackTimeMillis,
				CoverArtURL: highResCover,
			})
		}
		json.NewEncoder(w).Encode(results)
	})

	http.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		query := r.URL.Query().Get("q")
		if query == "" {
			json.NewEncoder(w).Encode([]TrackDetail{})
			return
		}

		ftsQuery := query + "*"
		rows, err := db.Query(`
			SELECT t.id, t.title, t.artist, t.duration_ms, COALESCE(t.spotify_id, ''),
			       COALESCE(t.album_id, ''), COALESCE(a.title, ''), COALESCE(a.cover_image_url, ''), 0
			FROM search_fts fts
			JOIN tracks t ON fts.target_id = t.id
			LEFT JOIN albums a ON t.album_id = a.id
			WHERE search_fts MATCH ?
			LIMIT 50`, ftsQuery)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var tracks []TrackDetail
		for rows.Next() {
			var t TrackDetail
			if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.DurationMs, &t.SpotifyID, &t.AlbumID, &t.AlbumTitle, &t.CoverImageURL, &t.Position); err == nil {
				tracks = append(tracks, t)
			}
		}
		json.NewEncoder(w).Encode(tracks)
	})

	// Static file serving, with SPA fallback to index.html for client-side routes
	// (e.g. /albums/123, /artists/Some+Name) so deep links and page refreshes work.
	fs := http.FileServer(http.Dir("public"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		cleanPath := filepath.Clean(r.URL.Path)
		if fileInfo, err := os.Stat(filepath.Join("public", cleanPath)); err == nil && !fileInfo.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join("public", "index.html"))
	})

	log.Printf("Server listening on http://localhost:%d", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func NormalizeAlbumTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	// Strip parenthetical and bracketed edition/remaster noise
	re := regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*(remaster|deluxe|edition|anniversary|version|bonus|expanded|legacy|special)[^\)\]]*[\)\]]`)
	t = re.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

func DedupeAlbums(db *sql.DB) (int, error) {
	updateSyncProgress(func(p *SyncProgress) {
		p.IsSyncing = true
		p.Stage = "deduping"
		p.Message = "Finding candidate duplicate albums..."
		p.ItemsFetched = 0
		p.TotalItems = 0
		p.LastError = ""
	})

	defer func() {
		updateSyncProgress(func(p *SyncProgress) {
			p.IsSyncing = false
			p.Stage = "idle"
			p.LastDedupedAt = time.Now().Format("2006-01-02 15:04:05")
		})
	}()

	// Query candidates artist by artist to leverage idx_albums_artist B-tree index
	artistRows, err := db.Query("SELECT DISTINCT artist FROM albums WHERE artist IS NOT NULL AND artist != ''")
	if err != nil {
		updateSyncProgress(func(p *SyncProgress) {
			p.LastError = err.Error()
		})
		return 0, fmt.Errorf("failed to query artists: %w", err)
	}
	defer artistRows.Close()

	var artists []string
	for artistRows.Next() {
		var a string
		if artistRows.Scan(&a) == nil {
			artists = append(artists, a)
		}
	}
	artistRows.Close()

	type albumMeta struct {
		id              string
		title           string
		artist          string
		hasVinyl        bool
		inWantlist      bool
		discogsMasterID int
	}

	type pair struct {
		a1 albumMeta
		a2 albumMeta
	}

	var candidates []pair
	for idx, artist := range artists {
		if idx%50 == 0 {
			updateSyncProgress(func(p *SyncProgress) {
				p.Message = fmt.Sprintf("Scanning artists (%d/%d)...", idx+1, len(artists))
			})
		}

		aRows, err := db.Query(`
			SELECT id, title, artist, COALESCE(has_vinyl, 0), COALESCE(in_wantlist, 0), COALESCE(discogs_master_id, 0)
			FROM albums
			WHERE artist = ?`, artist)
		if err != nil {
			continue
		}

		var list []albumMeta
		for aRows.Next() {
			var m albumMeta
			var v, w, dm int
			if aRows.Scan(&m.id, &m.title, &m.artist, &v, &w, &dm) == nil {
				m.hasVinyl = (v == 1)
				m.inWantlist = (w == 1)
				m.discogsMasterID = dm
				list = append(list, m)
			}
		}
		aRows.Close()

		if len(list) < 2 {
			continue
		}

		for i := 0; i < len(list); i++ {
			for j := i + 1; j < len(list); j++ {
				m1 := list[i]
				m2 := list[j]
				norm1 := NormalizeAlbumTitle(m1.title)
				norm2 := NormalizeAlbumTitle(m2.title)

				if (m1.discogsMasterID > 0 && m1.discogsMasterID == m2.discogsMasterID) || (norm1 != "" && norm1 == norm2) {
					candidates = append(candidates, pair{a1: m1, a2: m2})
				}
			}
		}
	}

	updateSyncProgress(func(p *SyncProgress) {
		p.TotalItems = len(candidates)
		p.Message = fmt.Sprintf("Evaluating %d album candidate pairs...", len(candidates))
	})

	totalMerged := 0
	for idx, c := range candidates {
		updateSyncProgress(func(p *SyncProgress) {
			p.CurrentPage = idx + 1
			p.ItemsFetched = totalMerged
			p.Message = fmt.Sprintf("Merging duplicates (%d/%d)...", idx+1, len(candidates))
		})

		// Verify evidence: shared track count >= 1 or matching discogs_master_id
		var sharedTracks int
		err := db.QueryRow(`
			SELECT COUNT(*)
			FROM tracks tr1
			JOIN tracks tr2 ON tr1.album_id = ? AND tr2.album_id = ?
			WHERE LOWER(SUBSTR(tr1.title, 1, 6)) = LOWER(SUBSTR(tr2.title, 1, 6))`,
			c.a1.id, c.a2.id).Scan(&sharedTracks)

		norm1 := NormalizeAlbumTitle(c.a1.title)
		norm2 := NormalizeAlbumTitle(c.a2.title)
		isMatch := (err == nil && sharedTracks >= 1) || (c.a1.discogsMasterID > 0 && c.a1.discogsMasterID == c.a2.discogsMasterID) || (norm1 != "" && norm1 == norm2)
		if !isMatch {
			continue
		}

		// Determine canonical master album (prefer has_vinyl, then in_wantlist, then shortest title)
		canonical := c.a1
		secondary := c.a2
		if (!c.a1.hasVinyl && c.a2.hasVinyl) || (!c.a1.inWantlist && c.a2.inWantlist) || (len(c.a2.title) < len(c.a1.title)) {
			canonical = c.a2
			secondary = c.a1
		}

		tx, err := db.Begin()
		if err != nil {
			continue
		}

		// 1. Move release_versions to canonical album
		_, _ = tx.Exec("UPDATE release_versions SET album_id = ? WHERE album_id = ?", canonical.id, secondary.id)

		// 2. Re-calculate flags on canonical album strictly from linked release_versions
		var hasVinylCount, inWantlistCount int
		_ = tx.QueryRow("SELECT COUNT(*) FROM release_versions WHERE album_id = ? AND source = 'collection' AND has_vinyl = 1", canonical.id).Scan(&hasVinylCount)
		_ = tx.QueryRow("SELECT COUNT(*) FROM release_versions WHERE album_id = ? AND source = 'wantlist'", canonical.id).Scan(&inWantlistCount)

		hasVinylVal := 0
		if hasVinylCount > 0 {
			hasVinylVal = 1
		}
		inWantlistVal := 0
		if inWantlistCount > 0 {
			inWantlistVal = 1
		}

		_, _ = tx.Exec("UPDATE albums SET has_vinyl = ?, in_wantlist = ? WHERE id = ?", hasVinylVal, inWantlistVal, canonical.id)
		if canonical.discogsMasterID == 0 && secondary.discogsMasterID > 0 {
			_, _ = tx.Exec("UPDATE albums SET discogs_master_id = ? WHERE id = ?", secondary.discogsMasterID, canonical.id)
		}

		// 3. Move tracks to canonical album, skipping duplicate track titles
		tRows, tErr := tx.Query("SELECT id, title FROM tracks WHERE album_id = ?", secondary.id)
		if tErr == nil {
			var secondaryTracks []struct{ id, title string }
			for tRows.Next() {
				var tid, ttitle string
				if tRows.Scan(&tid, &ttitle) == nil {
					secondaryTracks = append(secondaryTracks, struct{ id, title string }{tid, ttitle})
				}
			}
			tRows.Close()

			for _, st := range secondaryTracks {
				var count int
				_ = tx.QueryRow("SELECT COUNT(*) FROM tracks WHERE album_id = ? AND LOWER(title) = LOWER(?)", canonical.id, st.title).Scan(&count)
				if count > 0 {
					// Duplicate track exists on canonical album -> update playlist_tracks references to canonical track
					var canonicalTrackID string
					_ = tx.QueryRow("SELECT id FROM tracks WHERE album_id = ? AND LOWER(title) = LOWER(?) LIMIT 1", canonical.id, st.title).Scan(&canonicalTrackID)
					if canonicalTrackID != "" {
						_, _ = tx.Exec("UPDATE OR IGNORE playlist_tracks SET track_id = ? WHERE track_id = ?", canonicalTrackID, st.id)
						_, _ = tx.Exec("DELETE FROM playlist_tracks WHERE track_id = ?", st.id)
						_, _ = tx.Exec("DELETE FROM search_fts WHERE target_type = 'track' AND target_id = ?", st.id)
						_, _ = tx.Exec("DELETE FROM tracks WHERE id = ?", st.id)
					}
				} else {
					// Reassign track to canonical album
					_, _ = tx.Exec("UPDATE tracks SET album_id = ? WHERE id = ?", canonical.id, st.id)
				}
			}
		}

		// 4. Delete redundant secondary album
		_, _ = tx.Exec("DELETE FROM albums WHERE id = ?", secondary.id)

		if err := tx.Commit(); err == nil {
			totalMerged++
		} else {
			tx.Rollback()
		}
	}

	updateSyncProgress(func(p *SyncProgress) {
		p.Message = fmt.Sprintf("Deduplication complete! Merged %d albums.", totalMerged)
	})

	return totalMerged, nil
}
