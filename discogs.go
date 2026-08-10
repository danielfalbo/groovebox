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
	"sync"
	"time"

	"github.com/google/uuid"
)

type SyncProgress struct {
	IsSyncing     bool   `json:"is_syncing"`
	Stage         string `json:"stage"` // "idle", "authenticating", "collection", "wantlist", "database", "deduping"
	CurrentPage   int    `json:"current_page"`
	TotalPages    int    `json:"total_pages"`
	ItemsFetched  int    `json:"items_fetched"`
	TotalItems    int    `json:"total_items"`
	Message       string `json:"message"`
	LastError     string `json:"last_error,omitempty"`
	LastSyncedAt  string `json:"last_synced_at,omitempty"`
	LastDedupedAt string `json:"last_deduped_at,omitempty"`
}

var (
	syncMutex    sync.Mutex
	currentProgress SyncProgress
)

func GetSyncProgress() SyncProgress {
	syncMutex.Lock()
	defer syncMutex.Unlock()
	return currentProgress
}

func updateSyncProgress(update func(*SyncProgress)) {
	syncMutex.Lock()
	defer syncMutex.Unlock()
	update(&currentProgress)
}

type DiscogsIdentityResponse struct {
	Username string `json:"username"`
}

type DiscogsArtist struct {
	Name string `json:"name"`
}

type DiscogsFormat struct {
	Name         string   `json:"name"`
	Qty          string   `json:"qty"`
	Descriptions []string `json:"descriptions"`
}

type DiscogsBasicInfo struct {
	ID         int             `json:"id"`
	MasterID   int             `json:"master_id"`
	Title      string          `json:"title"`
	Year       int             `json:"year"`
	CoverImage string          `json:"cover_image"`
	Thumb      string          `json:"thumb"`
	Artists    []DiscogsArtist `json:"artists"`
	Formats    []DiscogsFormat `json:"formats"`
	Labels     []struct {
		Name  string `json:"name"`
		Catno string `json:"catno"`
	} `json:"labels"`
}

type DiscogsWantItem struct {
	ID               int              `json:"id"`
	BasicInformation DiscogsBasicInfo `json:"basic_information"`
}

type DiscogsWantlistResponse struct {
	Pagination struct {
		Pages int `json:"pages"`
		Items int `json:"items"`
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
		Items int `json:"items"`
	} `json:"pagination"`
	Releases []DiscogsCollectionItem `json:"releases"`
}

func getDiscogsToken() string {
	if token := os.Getenv("DISCOGS_TOKEN"); token != "" {
		return token
	}

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
	syncMutex.Lock()
	if currentProgress.IsSyncing {
		syncMutex.Unlock()
		return fmt.Errorf("sync is already in progress")
	}
	currentProgress = SyncProgress{
		IsSyncing: true,
		Stage:     "authenticating",
		Message:   "Authenticating with Discogs...",
	}
	syncMutex.Unlock()

	defer func() {
		updateSyncProgress(func(p *SyncProgress) {
			p.IsSyncing = false
		})
	}()

	token := getDiscogsToken()
	if token == "" {
		err := fmt.Errorf("DISCOGS_TOKEN not set in environment or ../discogs-albums/.env")
		updateSyncProgress(func(p *SyncProgress) {
			p.LastError = err.Error()
			p.Message = "Sync failed: " + err.Error()
		})
		return err
	}

	var identity DiscogsIdentityResponse
	if err := discogsRequest(token, "/oauth/identity", &identity); err != nil {
		errFmt := fmt.Errorf("failed to fetch Discogs identity: %w", err)
		updateSyncProgress(func(p *SyncProgress) {
			p.LastError = errFmt.Error()
			p.Message = "Sync failed: " + errFmt.Error()
		})
		return errFmt
	}
	log.Printf("Authenticated with Discogs as user @%s", identity.Username)

	// 1. Fetch Collection Releases
	updateSyncProgress(func(p *SyncProgress) {
		p.Stage = "collection"
		p.Message = "Fetching collection releases..."
	})

	page := 1
	totalPages := 1
	var collectionReleases []DiscogsBasicInfo

	for page <= totalPages {
		var resp DiscogsCollectionResponse
		path := fmt.Sprintf("/users/%s/collection/folders/0/releases?page=%d&per_page=100", identity.Username, page)
		if err := discogsRequest(token, path, &resp); err != nil {
			errFmt := fmt.Errorf("failed fetching collection page %d: %w", page, err)
			updateSyncProgress(func(p *SyncProgress) {
				p.LastError = errFmt.Error()
				p.Message = "Sync failed: " + errFmt.Error()
			})
			return errFmt
		}
		totalPages = resp.Pagination.Pages
		for _, item := range resp.Releases {
			collectionReleases = append(collectionReleases, item.BasicInformation)
		}
		
		colPage := page
		colTotalPages := totalPages
		colFetched := len(collectionReleases)
		colTotalItems := resp.Pagination.Items

		updateSyncProgress(func(p *SyncProgress) {
			p.CurrentPage = colPage
			p.TotalPages = colTotalPages
			p.ItemsFetched = colFetched
			p.TotalItems = colTotalItems
			p.Message = fmt.Sprintf("Fetching collection page %d/%d (%d items)", colPage, colTotalPages, colFetched)
		})
		page++
	}
	log.Printf("Fetched %d collection releases from Discogs", len(collectionReleases))

	// 2. Fetch Wantlist Items
	updateSyncProgress(func(p *SyncProgress) {
		p.Stage = "wantlist"
		p.Message = "Fetching wantlist releases..."
	})

	page = 1
	totalPages = 1
	var wantlistReleases []DiscogsBasicInfo

	for page <= totalPages {
		var resp DiscogsWantlistResponse
		path := fmt.Sprintf("/users/%s/wants?page=%d&per_page=100", identity.Username, page)
		if err := discogsRequest(token, path, &resp); err != nil {
			errFmt := fmt.Errorf("failed fetching wantlist page %d: %w", page, err)
			updateSyncProgress(func(p *SyncProgress) {
				p.LastError = errFmt.Error()
				p.Message = "Sync failed: " + errFmt.Error()
			})
			return errFmt
		}
		totalPages = resp.Pagination.Pages
		for _, item := range resp.Wants {
			wantlistReleases = append(wantlistReleases, item.BasicInformation)
		}

		wPage := page
		wTotalPages := totalPages
		wFetched := len(wantlistReleases)
		wTotalItems := resp.Pagination.Items

		updateSyncProgress(func(p *SyncProgress) {
			p.CurrentPage = wPage
			p.TotalPages = wTotalPages
			p.ItemsFetched = wFetched
			p.TotalItems = wTotalItems
			p.Message = fmt.Sprintf("Fetching wantlist page %d/%d (%d / %d items)", wPage, wTotalPages, wFetched, wTotalItems)
		})
		page++
	}
	log.Printf("Fetched %d wantlist releases from Discogs", len(wantlistReleases))

	// 3. Database Upsert Stage
	updateSyncProgress(func(p *SyncProgress) {
		p.Stage = "database"
		p.Message = "Updating database records..."
	})

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	processDiscogsItem := func(info DiscogsBasicInfo, source string, hasVinyl bool) {
		artist := "Unknown"
		if len(info.Artists) > 0 {
			var artistNames []string
			for _, a := range info.Artists {
				name := a.Name
				if idx := strings.Index(name, " ("); idx != -1 && strings.HasSuffix(name, ")") {
					name = name[:idx]
				}
				artistNames = append(artistNames, name)
			}
			artist = strings.Join(artistNames, ", ")
		}

		cover := info.CoverImage
		if cover == "" {
			cover = info.Thumb
		}

		var formatDesc string
		if len(info.Formats) > 0 {
			f := info.Formats[0]
			desc := f.Name
			if len(f.Descriptions) > 0 {
				desc += " (" + strings.Join(f.Descriptions, ", ") + ")"
			}
			formatDesc = desc
		}

		labelName := ""
		catNo := ""
		if len(info.Labels) > 0 {
			labelName = info.Labels[0].Name
			catNo = info.Labels[0].Catno
		}

		var albumID string
		if info.MasterID > 0 {
			_ = tx.QueryRow("SELECT id FROM albums WHERE discogs_master_id = ?", info.MasterID).Scan(&albumID)
		}
		if albumID == "" {
			_ = tx.QueryRow("SELECT id FROM albums WHERE LOWER(title) = LOWER(?) AND LOWER(artist) = LOWER(?)", info.Title, artist).Scan(&albumID)
		}

		if albumID == "" {
			albumID = uuid.New().String()
			var masterIDSql interface{} = nil
			if info.MasterID > 0 {
				masterIDSql = info.MasterID
			}

			isVinylFormat := strings.Contains(strings.ToLower(formatDesc), "vinyl") || strings.Contains(strings.ToLower(formatDesc), "flexi")
			hasVinylInt := 0
			if isVinylFormat {
				hasVinylInt = 1
			}
			inCollectionInt := 0
			if source == "collection" {
				inCollectionInt = 1
			}
			wantlistInt := 0
			if source == "wantlist" {
				wantlistInt = 1
			}

			_, err := tx.Exec(`
				INSERT INTO albums (id, title, artist, release_year, discogs_master_id, cover_image_url, has_vinyl, in_collection, in_wantlist, streaming_notes)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				albumID, info.Title, artist, info.Year, masterIDSql, cover, hasVinylInt, inCollectionInt, wantlistInt, "Discogs "+source,
			)
			if err != nil {
				log.Printf("Error inserting album %s: %v", info.Title, err)
				return
			}
		} else {
			if source == "collection" {
				_, _ = tx.Exec("UPDATE albums SET in_collection = 1, cover_image_url = COALESCE(NULLIF(cover_image_url, ''), ?) WHERE id = ?", cover, albumID)
			}
			if strings.Contains(strings.ToLower(formatDesc), "vinyl") || strings.Contains(strings.ToLower(formatDesc), "flexi") {
				_, _ = tx.Exec("UPDATE albums SET has_vinyl = 1, cover_image_url = COALESCE(NULLIF(cover_image_url, ''), ?) WHERE id = ?", cover, albumID)
			}
			if source == "wantlist" {
				_, _ = tx.Exec("UPDATE albums SET in_wantlist = 1 WHERE id = ?", albumID)
			}
			if info.MasterID > 0 {
				_, _ = tx.Exec("UPDATE albums SET discogs_master_id = ? WHERE id = ? AND discogs_master_id IS NULL", info.MasterID, albumID)
			}
		}

		isVinylFormat := false
		if strings.Contains(strings.ToLower(formatDesc), "vinyl") || strings.Contains(strings.ToLower(formatDesc), "flexi") {
			isVinylFormat = true
		}

		hasVinylInt := 0
		if isVinylFormat {
			hasVinylInt = 1
		}

		var versionID string
		err := tx.QueryRow("SELECT id FROM release_versions WHERE discogs_release_id = ?", info.ID).Scan(&versionID)
		if err == sql.ErrNoRows {
			versionID = uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO release_versions (id, album_id, discogs_release_id, title, artist, label, catalog_number, release_year, cover_image_url, format_description, source, has_vinyl)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				versionID, albumID, info.ID, info.Title, artist, labelName, catNo, info.Year, cover, formatDesc, source, hasVinylInt,
			)
			if err != nil {
				log.Printf("Error inserting release version %s (ID %d): %v", info.Title, info.ID, err)
			}
		} else if err == nil {
			_, _ = tx.Exec(`
				UPDATE release_versions SET album_id = ?, label = ?, catalog_number = ?, format_description = ?, source = ?, has_vinyl = ?
				WHERE id = ?`,
				albumID, labelName, catNo, formatDesc, source, hasVinylInt, versionID,
			)
		}
	}

	for _, info := range collectionReleases {
		processDiscogsItem(info, "collection", true)
	}

	for _, info := range wantlistReleases {
		processDiscogsItem(info, "wantlist", false)
	}

	if err := tx.Commit(); err != nil {
		errFmt := fmt.Errorf("failed committing Discogs sync: %w", err)
		updateSyncProgress(func(p *SyncProgress) {
			p.LastError = errFmt.Error()
			p.Message = "Sync failed: " + errFmt.Error()
		})
		return errFmt
	}

	nowStr := time.Now().Format("2006-01-02 15:04:05")
	updateSyncProgress(func(p *SyncProgress) {
		p.Stage = "idle"
		p.Message = "Sync completed successfully!"
		p.LastSyncedAt = nowStr
	})

	log.Println("Discogs collection & wantlist sync completed successfully!")
	return nil
}
