package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// SpotifyCSVRecord represents a row in the Notion Spotify export CSV
type SpotifyCSVRecord struct {
	TrackURI          string
	TrackName         string
	ArtistURIs        string
	ArtistNames       string
	AlbumURI          string
	AlbumName         string
	AlbumArtistURIs   string
	AlbumArtistNames  string
	AlbumReleaseDate  string
	AlbumImageURL     string
	DiscNumber        int
	TrackNumber       int
	TrackDurationMs   int
	TrackPreviewURL   string
	Explicit          bool
	Popularity        int
	AddedBy           string
	AddedAt           string
}

func parseSpotifyCSV(filePath string) ([]SpotifyCSVRecord, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}

	headerMap := make(map[string]int)
	for i, h := range headers {
		cleanH := strings.TrimPrefix(h, "\ufeff")
		cleanH = strings.TrimSpace(cleanH)
		headerMap[cleanH] = i
	}

	var records []SpotifyCSVRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Warning: skipping invalid row in %s: %v", filePath, err)
			continue
		}

		getVal := func(key string) string {
			if idx, ok := headerMap[key]; ok && idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}

		parseInt := func(key string) int {
			val := getVal(key)
			if val == "" {
				return 0
			}
			n, _ := strconv.Atoi(val)
			return n
		}

		rec := SpotifyCSVRecord{
			TrackURI:         getVal("Track URI"),
			TrackName:        getVal("Track Name"),
			ArtistNames:      getVal("Artist Name(s)"),
			AlbumURI:         getVal("Album URI"),
			AlbumName:        getVal("Album Name"),
			AlbumArtistNames: getVal("Album Artist Name(s)"),
			AlbumReleaseDate: getVal("Album Release Date"),
			AlbumImageURL:    getVal("Album Image URL"),
			DiscNumber:       parseInt("Disc Number"),
			TrackNumber:      parseInt("Track Number"),
			TrackDurationMs:  parseInt("Track Duration (ms)"),
			TrackPreviewURL:  getVal("Track Preview URL"),
			Explicit:         getVal("Explicit") == "Yes",
			Popularity:       parseInt("Popularity"),
			AddedBy:          getVal("Added By"),
			AddedAt:          getVal("Added At"),
		}

		if rec.TrackName != "" {
			records = append(records, rec)
		}
	}

	return records, nil
}

func ImportSpotifyCSVDirectory(db *sql.DB, dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("failed to read import directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".csv") || strings.HasSuffix(entry.Name(), "_all.csv") {
			continue
		}

		filePath := filepath.Join(dirPath, entry.Name())
		playlistName := strings.TrimSuffix(entry.Name(), ".csv")
		if idx := strings.LastIndex(playlistName, " "); idx != -1 && len(playlistName)-idx > 20 {
			playlistName = playlistName[:idx]
		}
		playlistName = strings.ReplaceAll(playlistName, "_", " ")
		playlistName = strings.Title(playlistName)

		records, err := parseSpotifyCSV(filePath)
		if err != nil {
			log.Printf("Error parsing playlist CSV %s: %v", entry.Name(), err)
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		playlistID := uuid.New().String()
		_, err = tx.Exec("INSERT INTO playlists (id, name, description) VALUES (?, ?, ?)",
			playlistID, playlistName, fmt.Sprintf("Imported from Spotify Notion Export (%d tracks)", len(records)))
		if err != nil {
			tx.Rollback()
			log.Printf("Error creating playlist %s: %v", playlistName, err)
			continue
		}

		for pos, rec := range records {
			spotifyTrackID := strings.TrimPrefix(rec.TrackURI, "spotify:track:")
			spotifyAlbumID := strings.TrimPrefix(rec.AlbumURI, "spotify:album:")

			var releaseID string
			err := tx.QueryRow("SELECT id FROM releases WHERE title = ? AND artist = ?", rec.AlbumName, rec.AlbumArtistNames).Scan(&releaseID)
			if err == sql.ErrNoRows {
				releaseID = uuid.New().String()
				var year int
				if len(rec.AlbumReleaseDate) >= 4 {
					year, _ = strconv.Atoi(rec.AlbumReleaseDate[len(rec.AlbumReleaseDate)-4:])
				}
				_, err = tx.Exec(`
					INSERT INTO releases (id, title, artist, release_year, cover_image_url, streaming_notes)
					VALUES (?, ?, ?, ?, ?, ?)`,
					releaseID, rec.AlbumName, rec.AlbumArtistNames, year, rec.AlbumImageURL, "Spotify Album "+spotifyAlbumID,
				)
				if err != nil {
					log.Printf("Error inserting release %s: %v", rec.AlbumName, err)
				}
			}

			var trackID string
			err = tx.QueryRow("SELECT id FROM tracks WHERE spotify_id = ?", spotifyTrackID).Scan(&trackID)
			if err == sql.ErrNoRows {
				trackID = uuid.New().String()
				trackNumStr := fmt.Sprintf("%d", rec.TrackNumber)
				_, err = tx.Exec(`
					INSERT INTO tracks (id, release_id, title, artist, track_number, duration_ms, spotify_id)
					VALUES (?, ?, ?, ?, ?, ?, ?)`,
					trackID, releaseID, rec.TrackName, rec.ArtistNames, trackNumStr, rec.TrackDurationMs, spotifyTrackID,
				)
				if err != nil {
					log.Printf("Error inserting track %s: %v", rec.TrackName, err)
					continue
				}

				_, _ = tx.Exec(`
					INSERT INTO search_fts (target_type, target_id, title, artist)
					VALUES ('track', ?, ?, ?)`,
					trackID, rec.TrackName, rec.ArtistNames,
				)
			}

			_, err = tx.Exec(`
				INSERT INTO playlist_tracks (playlist_id, track_id, position)
				VALUES (?, ?, ?)`,
				playlistID, trackID, pos+1,
			)
			if err != nil {
				log.Printf("Error linking track to playlist: %v", err)
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Error committing playlist %s: %v", playlistName, err)
		} else {
			log.Printf("Imported playlist '%s' (%d tracks)", playlistName, len(records))
		}
	}

	return nil
}
