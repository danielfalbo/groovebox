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
	importDir := flag.String("import-spotify", "", "Path to directory containing Spotify export CSVs")
	importSpotifyAccount := flag.Bool("import-spotify-account", false, "Import current Spotify account playlists through OAuth")
	importSpotifyTop := flag.Bool("import-spotify-top", false, "Import all-time top tracks from Spotify as a ranked playlist (requires user-top-read scope)")
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

	if *importSpotifyAccount {
		log.Println("Starting Spotify account import...")
		if err := ImportSpotifyAccount(db); err != nil {
			log.Fatalf("Spotify account import failed: %v", err)
		}
		log.Println("Spotify account import completed successfully!")
		return
	}

	if *importSpotifyTop {
		log.Println("Starting Spotify top tracks import...")
		if err := ImportSpotifyTopTracks(db); err != nil {
			log.Fatalf("Spotify top tracks import failed: %v", err)
		}
		log.Println("Spotify top tracks import completed successfully!")
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
		playlistID := r.URL.Path[len("/api/playlists/"):]
		if playlistID == "" {
			http.Error(w, "Playlist ID required", 400)
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
		rows, err := db.Query(`
			SELECT t.id, t.title, t.artist, t.duration_ms, COALESCE(t.spotify_id, ''),
			       COALESCE(t.album_id, ''), COALESCE(a.title, ''), COALESCE(a.cover_image_url, ''), 0
			FROM tracks t
			LEFT JOIN albums a ON t.album_id = a.id
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
		InWantlist     bool   `json:"in_wantlist"`
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
		db.QueryRow("SELECT COUNT(*) FROM albums WHERE has_vinyl = 1").Scan(&counts.Collection)
		db.QueryRow("SELECT COUNT(*) FROM albums WHERE in_wantlist = 1").Scan(&counts.Wantlist)
		json.NewEncoder(w).Encode(counts)
	})

	http.HandleFunc("/api/albums", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		filter := r.URL.Query().Get("filter")

		whereClause := ""
		if filter == "collection" {
			whereClause = "WHERE a.has_vinyl = 1"
		} else if filter == "wantlist" {
			whereClause = "WHERE a.in_wantlist = 1"
		}

		query := fmt.Sprintf(`
			SELECT a.id, a.title, a.artist, COALESCE(a.release_year, 0), COALESCE(a.cover_image_url, ''),
			       COALESCE(a.has_vinyl, 0), COALESCE(a.in_wantlist, 0), COALESCE(a.streaming_notes, ''),
			       (SELECT COUNT(*) FROM release_versions rv WHERE rv.album_id = a.id) as version_count,
			       (SELECT COUNT(*) FROM tracks t WHERE t.album_id = a.id) as track_count
			FROM albums a
			%s
			ORDER BY a.title ASC
			LIMIT 5000`, whereClause)

		rows, err := db.Query(query)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var albums []AlbumSummary
		for rows.Next() {
			var alb AlbumSummary
			var hasVinylInt, inWantlistInt int
			if err := rows.Scan(&alb.ID, &alb.Title, &alb.Artist, &alb.ReleaseYear, &alb.CoverImageURL, &hasVinylInt, &inWantlistInt, &alb.StreamingNotes, &alb.VersionCount, &alb.TrackCount); err == nil {
				alb.HasVinyl = hasVinylInt == 1
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

	// Static file serving
	fs := http.FileServer(http.Dir("public"))
	http.Handle("/", fs)

	log.Printf("Server listening on http://localhost:%d", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
