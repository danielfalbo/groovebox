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
	AlbumTitle    string `json:"album_title"`
	CoverImageURL string `json:"cover_image_url"`
	Position      int    `json:"position"`
}

func main() {
	importDir := flag.String("import-spotify", "", "Path to directory containing Spotify export CSVs")
	syncDiscogsFlag := flag.Bool("sync-discogs", false, "Sync Discogs collection & wantlist into database")
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
	http.HandleFunc("/api/sync/discogs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := SyncDiscogs(db); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Discogs sync completed"})
	})
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
				// Query up to 4 unique cover image URLs for 2x2 grid cover
				coverRows, cErr := db.Query(`
					SELECT DISTINCT r.cover_image_url
					FROM playlist_tracks pt
					JOIN tracks t ON pt.track_id = t.id
					JOIN releases r ON t.release_id = r.id
					WHERE pt.playlist_id = ? AND r.cover_image_url IS NOT NULL AND r.cover_image_url != ''
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

	http.HandleFunc("/api/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := db.Query(`
			SELECT t.id, t.title, t.artist, t.duration_ms, COALESCE(t.spotify_id, ''),
			       COALESCE(r.title, ''), COALESCE(r.cover_image_url, ''), 0
			FROM tracks t
			LEFT JOIN releases r ON t.release_id = r.id
			ORDER BY t.title ASC
			LIMIT 500`)
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

	type ArtistSummary struct {
		Name       string `json:"name"`
		TrackCount int    `json:"track_count"`
	}

	http.HandleFunc("/api/artists", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := db.Query(`
			SELECT artist, COUNT(*) as track_count
			FROM tracks
			WHERE artist IS NOT NULL AND artist != ''
			GROUP BY artist
			ORDER BY track_count DESC, artist ASC
			LIMIT 300`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var artists []ArtistSummary
		for rows.Next() {
			var a ArtistSummary
			if err := rows.Scan(&a.Name, &a.TrackCount); err == nil {
				artists = append(artists, a)
			}
		}
		json.NewEncoder(w).Encode(artists)
	})

	type AlbumSummary struct {
		ID             string `json:"id"`
		Title          string `json:"title"`
		Artist         string `json:"artist"`
		ReleaseYear    int    `json:"release_year"`
		CoverImageURL  string `json:"cover_image_url"`
		HasVinyl       bool   `json:"has_vinyl"`
		StreamingNotes string `json:"streaming_notes"`
		TrackCount     int    `json:"track_count"`
	}

	http.HandleFunc("/api/albums", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := db.Query(`
			SELECT r.id, r.title, r.artist, COALESCE(r.release_year, 0), COALESCE(r.cover_image_url, ''),
			       COALESCE(r.has_vinyl, 0), COALESCE(r.streaming_notes, ''), COUNT(t.id) as track_count
			FROM releases r
			LEFT JOIN tracks t ON r.id = t.release_id
			GROUP BY r.id
			ORDER BY r.title ASC
			LIMIT 500`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var albums []AlbumSummary
		for rows.Next() {
			var alb AlbumSummary
			var hasVinylInt int
			if err := rows.Scan(&alb.ID, &alb.Title, &alb.Artist, &alb.ReleaseYear, &alb.CoverImageURL, &hasVinylInt, &alb.StreamingNotes, &alb.TrackCount); err == nil {
				alb.HasVinyl = hasVinylInt == 1
				albums = append(albums, alb)
			}
		}
		json.NewEncoder(w).Encode(albums)
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

