package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type DiscogsIdentityResponse struct {
	Username string `json:"username"`
}

type DiscogsArtist struct {
	Name string `json:"name"`
}

type DiscogsBasicInfo struct {
	ID         int             `json:"id"`
	MasterID   int             `json:"master_id"`
	Title      string          `json:"title"`
	Year       int             `json:"year"`
	CoverImage string          `json:"cover_image"`
	Thumb      string          `json:"thumb"`
	Artists    []DiscogsArtist `json:"artists"`
}

type DiscogsWantItem struct {
	ID               int              `json:"id"`
	BasicInformation DiscogsBasicInfo `json:"basic_information"`
}

type DiscogsWantlistResponse struct {
	Pagination struct {
		Pages int `json:"pages"`
	} `json:"pagination"`
	Wants []DiscogsWantItem `json:"wants"`
}

type DiscogsCollectionItem struct {
	ID               int              `json:"id"`
	BasicInformation DiscogsBasicInfo `json:"basic_information"`
}

type DiscogsCollectionResponse struct {
	Pagination struct {
		Pages int `json:"pages"`
	} `json:"pagination"`
	Releases []DiscogsCollectionItem `json:"releases"`
}

func getDiscogsToken() string {
	if token := os.Getenv("DISCOGS_TOKEN"); token != "" {
		return token
	}

	// Fallback to checking ../discogs-albums/.env
	envPaths := []string{
		".env",
		"../discogs-albums/.env",
	}

	for _, p := range envPaths {
		if data, err := os.ReadFile(p); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "DISCOGS_TOKEN=") {
					return strings.TrimPrefix(line, "DISCOGS_TOKEN=")
				}
			}
		}
	}
	return ""
}

func discogsRequest(token, path string, target interface{}) error {
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("https://api.discogs.com%s", path)

	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", fmt.Sprintf("Discogs token=%s", token))
		req.Header.Set("User-Agent", "my-music-lib/1.0")

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == 429 {
			resp.Body.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("Discogs API error HTTP %d: %s", resp.StatusCode, string(body))
		}

		return json.NewDecoder(resp.Body).Decode(target)
	}

	return fmt.Errorf("Discogs API rate limit exceeded after retries")
}

func SyncDiscogs(db *sql.DB) error {
	token := getDiscogsToken()
	if token == "" {
		return fmt.Errorf("DISCOGS_TOKEN not set in environment or ../discogs-albums/.env")
	}

	var identity DiscogsIdentityResponse
	if err := discogsRequest(token, "/oauth/identity", &identity); err != nil {
		return fmt.Errorf("failed to fetch Discogs identity: %w", err)
	}
	log.Printf("Authenticated with Discogs as user @%s", identity.Username)

	// 1. Fetch Collection Releases (has_vinyl = 1)
	page := 1
	totalPages := 1
	var collectionReleases []DiscogsBasicInfo

	for page <= totalPages {
		var resp DiscogsCollectionResponse
		path := fmt.Sprintf("/users/%s/collection/folders/0/releases?page=%d&per_page=100", identity.Username, page)
		if err := discogsRequest(token, path, &resp); err != nil {
			return fmt.Errorf("failed fetching collection page %d: %w", page, err)
		}
		totalPages = resp.Pagination.Pages
		for _, item := range resp.Releases {
			collectionReleases = append(collectionReleases, item.BasicInformation)
		}
		page++
	}
	log.Printf("Fetched %d collection releases from Discogs", len(collectionReleases))

	// 2. Fetch Wantlist Items (has_vinyl = 0)
	page = 1
	totalPages = 1
	var wantlistReleases []DiscogsBasicInfo

	for page <= totalPages {
		var resp DiscogsWantlistResponse
		path := fmt.Sprintf("/users/%s/wants?page=%d&per_page=100", identity.Username, page)
		if err := discogsRequest(token, path, &resp); err != nil {
			return fmt.Errorf("failed fetching wantlist page %d: %w", page, err)
		}
		totalPages = resp.Pagination.Pages
		for _, item := range resp.Wants {
			wantlistReleases = append(wantlistReleases, item.BasicInformation)
		}
		page++
	}
	log.Printf("Fetched %d wantlist releases from Discogs", len(wantlistReleases))

	// Upsert Collection Items (has_vinyl = 1)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, info := range collectionReleases {
		artist := "Unknown"
		if len(info.Artists) > 0 {
			var artistNames []string
			for _, a := range info.Artists {
				artistNames = append(artistNames, a.Name)
			}
			artist = strings.Join(artistNames, ", ")
		}

		cover := info.CoverImage
		if cover == "" {
			cover = info.Thumb
		}

		var releaseID string
		err := tx.QueryRow("SELECT id FROM releases WHERE discogs_id = ? OR (title = ? AND artist = ?)", info.ID, info.Title, artist).Scan(&releaseID)
		if err == sql.ErrNoRows {
			releaseID = uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO releases (id, title, artist, release_year, discogs_id, cover_image_url, has_vinyl, streaming_notes)
				VALUES (?, ?, ?, ?, ?, ?, 1, 'Discogs Collection')`,
				releaseID, info.Title, artist, info.Year, info.ID, cover,
			)
			if err != nil {
				log.Printf("Error inserting Discogs collection release %s: %v", info.Title, err)
			}
		} else if err == nil {
			_, _ = tx.Exec("UPDATE releases SET has_vinyl = 1, discogs_id = ?, cover_image_url = COALESCE(NULLIF(cover_image_url, ''), ?) WHERE id = ?", info.ID, cover, releaseID)
		}
	}

	// Upsert Wantlist Items (has_vinyl = 0, streaming_notes = 'Discogs Wantlist')
	for _, info := range wantlistReleases {
		artist := "Unknown"
		if len(info.Artists) > 0 {
			var artistNames []string
			for _, a := range info.Artists {
				artistNames = append(artistNames, a.Name)
			}
			artist = strings.Join(artistNames, ", ")
		}

		cover := info.CoverImage
		if cover == "" {
			cover = info.Thumb
		}

		var releaseID string
		err := tx.QueryRow("SELECT id FROM releases WHERE discogs_id = ?", info.ID).Scan(&releaseID)
		if err == sql.ErrNoRows {
			releaseID = uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO releases (id, title, artist, release_year, discogs_id, cover_image_url, has_vinyl, streaming_notes)
				VALUES (?, ?, ?, ?, ?, ?, 0, 'Discogs Wantlist')`,
				releaseID, info.Title, artist, info.Year, info.ID, cover,
			)
			if err != nil {
				log.Printf("Error inserting Discogs wantlist release %s: %v", info.Title, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed committing Discogs sync: %w", err)
	}

	log.Println("Discogs collection & wantlist sync completed successfully!")
	return nil
}
