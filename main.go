package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

type StatsResponse struct {
	TotalTracks    int `json:"total_tracks"`
	TotalReleases  int `json:"total_releases"`
	TotalPlaylists int `json:"total_playlists"`
}

type PlaylistSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TrackCount  int    `json:"track_count"`
}

type TrackDetail struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	DurationMs    int    `json:"duration_ms"`
	SpotifyID     string `json:"spotify_id"`
	AlbumTitle    string `json:"album_title"`
	CoverImageURL string `json:"cover_image_url"`
	Position      int    `json:"position"`
}

func main() {
	importDir := flag.String("import-spotify", "", "Path to directory containing Spotify export CSVs")
	dbPath := flag.String("db", "music.db", "Path to SQLite database")
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	db, err := initDB(*dbPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()

	if *importDir != "" {
		log.Printf("Starting Spotify CSV import from %s...", *importDir)
		if err := ImportSpotifyCSVDirectory(db, *importDir); err != nil {
			log.Fatalf("Import failed: %v", err)
		}
		log.Println("Import completed successfully!")
		if len(os.Args) > 2 && os.Args[1] == "-import-spotify" || (len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "-import-spotify")) {
			return
		}
	}

	// API Routes
	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var stats StatsResponse
		_ = db.QueryRow("SELECT COUNT(*) FROM tracks").Scan(&stats.TotalTracks)
		_ = db.QueryRow("SELECT COUNT(*) FROM releases").Scan(&stats.TotalReleases)
		_ = db.QueryRow("SELECT COUNT(*) FROM playlists").Scan(&stats.TotalPlaylists)
		json.NewEncoder(w).Encode(stats)
	})

	http.HandleFunc("/api/playlists", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := db.Query(`
			SELECT p.id, p.name, COALESCE(p.description, ''), COUNT(pt.track_id)
			FROM playlists p
			LEFT JOIN playlist_tracks pt ON p.id = pt.playlist_id
			GROUP BY p.id
			ORDER BY p.name ASC`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var playlists []PlaylistSummary
		for rows.Next() {
			var p PlaylistSummary
			if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.TrackCount); err == nil {
				playlists = append(playlists, p)
			}
		}
		json.NewEncoder(w).Encode(playlists)
	})

	http.HandleFunc("/api/playlists/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		playlistID := r.URL.Path[len("/api/playlists/"):]
		if playlistID == "" {
			http.Error(w, "Playlist ID required", 400)
			return
		}

		rows, err := db.Query(`
			SELECT t.id, t.title, t.artist, t.duration_ms, COALESCE(t.spotify_id, ''),
			       COALESCE(r.title, ''), COALESCE(r.cover_image_url, ''), pt.position
			FROM playlist_tracks pt
			JOIN tracks t ON pt.track_id = t.id
			LEFT JOIN releases r ON t.release_id = r.id
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
			if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.DurationMs, &t.SpotifyID, &t.AlbumTitle, &t.CoverImageURL, &t.Position); err == nil {
				tracks = append(tracks, t)
			}
		}
		json.NewEncoder(w).Encode(tracks)
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
			       COALESCE(r.title, ''), COALESCE(r.cover_image_url, ''), 0
			FROM search_fts fts
			JOIN tracks t ON fts.target_id = t.id
			LEFT JOIN releases r ON t.release_id = r.id
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
			if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.DurationMs, &t.SpotifyID, &t.AlbumTitle, &t.CoverImageURL, &t.Position); err == nil {
				tracks = append(tracks, t)
			}
		}
		json.NewEncoder(w).Encode(tracks)
	})

	// Static web server
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Server listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server stopped: %v", err)
	}
}

