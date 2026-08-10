package main

import (
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AppleMusicTrack struct {
	TrackID     int
	Name        string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	DurationMs  int
	Year        int
	TrackNumber int
	ISRC        string
	DateAdded   string
}

type AppleMusicPlaylist struct {
	Name         string
	Master       bool
	DistKind     int
	TrackIDs     []int
	PersistentID string
}

func parseAppleMusicXML(xmlPath string) (map[int]AppleMusicTrack, []AppleMusicPlaylist, error) {
	file, err := os.Open(xmlPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open XML: %w", err)
	}
	defer file.Close()

	decoder := xml.NewDecoder(file)

	tracks := make(map[int]AppleMusicTrack)
	var playlists []AppleMusicPlaylist

	// State machine tracking XML context
	var path []string
	var currentKey string

	// For track dict parsing
	inTracksDict := false
	inTrackItemDict := false
	var currentTrack AppleMusicTrack

	// For playlists parsing
	inPlaylistsArray := false
	inPlaylistItemDict := false
	inPlaylistItemsArray := false
	var currentPlaylist AppleMusicPlaylist

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("xml decoding error: %w", err)
		}

		switch elem := tok.(type) {
		case xml.StartElement:
			tagName := elem.Name.Local
			path = append(path, tagName)

			if len(path) == 3 && path[1] == "dict" && currentKey == "Tracks" && tagName == "dict" {
				inTracksDict = true
			} else if inTracksDict && len(path) == 4 && tagName == "dict" {
				inTrackItemDict = true
				currentTrack = AppleMusicTrack{}
			} else if len(path) == 3 && path[1] == "dict" && currentKey == "Playlists" && tagName == "array" {
				inPlaylistsArray = true
			} else if inPlaylistsArray && len(path) == 4 && tagName == "dict" {
				inPlaylistItemDict = true
				currentPlaylist = AppleMusicPlaylist{TrackIDs: []int{}}
			} else if inPlaylistItemDict && currentKey == "Playlist Items" && tagName == "array" {
				inPlaylistItemsArray = true
			}

		case xml.EndElement:
			tagName := elem.Name.Local

			if inTrackItemDict && tagName == "dict" && len(path) == 4 {
				inTrackItemDict = false
				if currentTrack.TrackID > 0 && currentTrack.Name != "" {
					tracks[currentTrack.TrackID] = currentTrack
				}
			} else if inTracksDict && tagName == "dict" && len(path) == 3 {
				inTracksDict = false
			} else if inPlaylistItemsArray && tagName == "array" {
				inPlaylistItemsArray = false
			} else if inPlaylistItemDict && tagName == "dict" && len(path) == 4 {
				inPlaylistItemDict = false
				playlists = append(playlists, currentPlaylist)
			} else if inPlaylistsArray && tagName == "array" && len(path) == 3 {
				inPlaylistsArray = false
			}

			if len(path) > 0 {
				path = path[:len(path)-1]
			}

		case xml.CharData:
			text := strings.TrimSpace(string(elem))
			if text == "" {
				continue
			}

			parentTag := ""
			if len(path) > 0 {
				parentTag = path[len(path)-1]
			}

			if parentTag == "key" {
				currentKey = text
			} else if inTrackItemDict {
				switch currentKey {
				case "Track ID":
					currentTrack.TrackID, _ = strconv.Atoi(text)
				case "Name":
					currentTrack.Name = text
				case "Artist":
					currentTrack.Artist = text
				case "Album Artist":
					currentTrack.AlbumArtist = text
				case "Album":
					currentTrack.Album = text
				case "Genre":
					currentTrack.Genre = text
				case "Total Time":
					currentTrack.DurationMs, _ = strconv.Atoi(text)
				case "Year":
					currentTrack.Year, _ = strconv.Atoi(text)
				case "Track Number":
					currentTrack.TrackNumber, _ = strconv.Atoi(text)
				case "ISRC":
					currentTrack.ISRC = text
				case "Date Added":
					currentTrack.DateAdded = text
				}
			} else if inPlaylistItemDict {
				if inPlaylistItemsArray {
					if currentKey == "Track ID" {
						if tid, err := strconv.Atoi(text); err == nil {
							currentPlaylist.TrackIDs = append(currentPlaylist.TrackIDs, tid)
						}
					}
				} else {
					switch currentKey {
					case "Name":
						currentPlaylist.Name = text
					case "Master":
						currentPlaylist.Master = (text == "true")
					case "Distinguished Kind":
						currentPlaylist.DistKind, _ = strconv.Atoi(text)
					case "Playlist Persistent ID":
						currentPlaylist.PersistentID = text
					}
				}
			}
		}
	}

	return tracks, playlists, nil
}

func ImportAppleMusicLibrary(db *sql.DB, xmlPath string) error {
	log.Printf("Parsing Apple Music Library from %s...", xmlPath)
	tracksMap, playlists, err := parseAppleMusicXML(xmlPath)
	if err != nil {
		return fmt.Errorf("failed to parse Apple Music XML: %w", err)
	}

	log.Printf("Found %d tracks and %d total playlist entries in Apple Music XML", len(tracksMap), len(playlists))

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Ingest/Link all tracks into SQLite
	appleToDBTrackID := make(map[int]string)

	importedTracksCount := 0
	importedAlbumsCount := 0

	for appleTrackID, tr := range tracksMap {
		albumTitle := tr.Album
		if albumTitle == "" {
			albumTitle = "Unknown Album"
		}
		albumArtist := tr.AlbumArtist
		if albumArtist == "" {
			albumArtist = tr.Artist
		}
		if albumArtist == "" {
			albumArtist = "Unknown Artist"
		}
		trackArtist := tr.Artist
		if trackArtist == "" {
			trackArtist = albumArtist
		}

		var albumID string
		err := tx.QueryRow("SELECT id FROM albums WHERE LOWER(title) = LOWER(?) AND LOWER(artist) = LOWER(?)", albumTitle, albumArtist).Scan(&albumID)
		if err == sql.ErrNoRows {
			albumID = uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO albums (id, title, artist, release_year, streaming_notes)
				VALUES (?, ?, ?, ?, ?)`,
				albumID, albumTitle, albumArtist, tr.Year, "Apple Music Import",
			)
			if err != nil {
				log.Printf("Warning: failed to insert album '%s': %v", albumTitle, err)
			} else {
				importedAlbumsCount++
			}
		}

		var dbTrackID string
		if tr.ISRC != "" {
			_ = tx.QueryRow("SELECT id FROM tracks WHERE isrc = ?", tr.ISRC).Scan(&dbTrackID)
		}
		if dbTrackID == "" {
			_ = tx.QueryRow("SELECT id FROM tracks WHERE LOWER(title) = LOWER(?) AND LOWER(artist) = LOWER(?)", tr.Name, trackArtist).Scan(&dbTrackID)
		}

		if dbTrackID == "" {
			dbTrackID = uuid.New().String()
			trackNumStr := ""
			if tr.TrackNumber > 0 {
				trackNumStr = fmt.Sprintf("%d", tr.TrackNumber)
			}
			_, err = tx.Exec(`
				INSERT INTO tracks (id, album_id, title, artist, track_number, duration_ms, isrc, apple_music_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				dbTrackID, albumID, tr.Name, trackArtist, trackNumStr, tr.DurationMs, tr.ISRC, fmt.Sprintf("%d", appleTrackID),
			)
			if err != nil {
				log.Printf("Warning: failed to insert track '%s': %v", tr.Name, err)
				continue
			}

			_, _ = tx.Exec(`
				INSERT INTO search_fts (target_type, target_id, title, artist, isrc)
				VALUES ('track', ?, ?, ?, ?)`,
				dbTrackID, tr.Name, trackArtist, tr.ISRC,
			)
			importedTracksCount++
		} else {
			// Update track with apple_music_id or isrc if missing
			if tr.ISRC != "" {
				_, _ = tx.Exec("UPDATE tracks SET apple_music_id = ?, isrc = COALESCE(NULLIF(isrc, ''), ?) WHERE id = ?", fmt.Sprintf("%d", appleTrackID), tr.ISRC, dbTrackID)
			} else {
				_, _ = tx.Exec("UPDATE tracks SET apple_music_id = ? WHERE id = ? AND apple_music_id IS NULL", fmt.Sprintf("%d", appleTrackID), dbTrackID)
			}
		}

		appleToDBTrackID[appleTrackID] = dbTrackID
	}

	log.Printf("Ingested/Linked %d tracks and %d new albums from Apple Music", importedTracksCount, importedAlbumsCount)

	// 2. Ingest playlists (skip master library and standard smart playlists)
	importedPlaylistsCount := 0
	for _, pl := range playlists {
		if pl.Master || pl.DistKind > 0 || len(pl.TrackIDs) == 0 {
			continue
		}
		if pl.Name == "Library" || pl.Name == "Downloaded" || pl.Name == "Music" {
			continue
		}

		var createdAtStr string
		// If playlist name matches YYYY-MM format, set created_at directly to YYYY-MM-01 00:00:00
		if len(pl.Name) == 7 && pl.Name[4] == '-' {
			if _, pErr := time.Parse("2006-01", pl.Name); pErr == nil {
				createdAtStr = pl.Name + "-01 00:00:00"
			}
		}

		if createdAtStr == "" {
			// Find earliest date_added among tracks in this playlist as creation date proxy
			var earliestTime *time.Time
			for _, tid := range pl.TrackIDs {
				if tr, ok := tracksMap[tid]; ok && tr.DateAdded != "" {
					if t, pErr := time.Parse(time.RFC3339, tr.DateAdded); pErr == nil {
						if earliestTime == nil || t.Before(*earliestTime) {
							earliestTime = &t
						}
					}
				}
			}

			createdAtStr = "2026-01-01 00:00:00"
			if earliestTime != nil {
				createdAtStr = earliestTime.Format("2006-01-02 15:04:05")
			}
		}

		var playlistID string
		err := tx.QueryRow("SELECT id FROM playlists WHERE name = ?", pl.Name).Scan(&playlistID)
		if err == sql.ErrNoRows {
			playlistID = uuid.New().String()
			_, err = tx.Exec("INSERT INTO playlists (id, name, description, created_at) VALUES (?, ?, ?, ?)",
				playlistID, pl.Name, fmt.Sprintf("Imported from Apple Music (%d tracks)", len(pl.TrackIDs)), createdAtStr)
			if err != nil {
				log.Printf("Warning: failed to create playlist '%s': %v", pl.Name, err)
				continue
			}
		}

		pos := 1
		for _, appleTrackID := range pl.TrackIDs {
			if dbTrackID, ok := appleToDBTrackID[appleTrackID]; ok {
				_, _ = tx.Exec(`
					INSERT OR IGNORE INTO playlist_tracks (playlist_id, track_id, position)
					VALUES (?, ?, ?)`,
					playlistID, dbTrackID, pos,
				)
				pos++
			}
		}
		importedPlaylistsCount++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Successfully imported %d Apple Music playlists into Groovebox!", importedPlaylistsCount)
	return nil
}
