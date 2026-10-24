package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// One maintained reconciler is used by normal Discogs sync, the CLI and BPM
// tooling. The service's WorkingDirectory is the repository; Python is stdlib.
// There is no DB transaction or connection lock during network/subprocess work.
func refreshOwnedTracklists(db *sql.DB, inventory []DiscogsCollectionItem) error {
	var seq int
	var name, dbPath string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &dbPath); err != nil {
		return err
	}
	if dbPath == "" {
		return fmt.Errorf("tracklist refresh requires a file database")
	}
	if err := os.MkdirAll(".bpm-cache", 0700); err != nil {
		return err
	}
	args := []string{"scripts/tracklists.py", "--db", dbPath, "--max-seconds", "1200", "--report", ".bpm-cache/sync-tracklists.json"}
	if inventory == nil {
		args = append(args, "--verify-collection")
	} else {
		file, err := os.CreateTemp(".bpm-cache", "collection-sync-*.json")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		err = json.NewEncoder(file).Encode(map[string]any{"verified_at": time.Now().UTC().Format(time.RFC3339), "items": len(inventory), "releases": inventory})
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		args = append(args, "--inventory", filepath.Clean(file.Name()))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 21*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", args...)
	// Inherit logs, but never credentials in command arguments or reports.
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("owned tracklists incomplete (%v); see .bpm-cache/sync-tracklists.json", err)
	}
	log.Println("Every current owned Discogs release tracklist verified; .bpm-cache/sync-tracklists.json")
	return nil
}

type DiscogsTracklistEntry struct {
	Path       string `json:"path"`
	ParentPath string `json:"parent_path"`
	Depth      int    `json:"depth"`
	Type       string `json:"type"`
	Position   string `json:"position"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	DurationMs *int   `json:"duration_ms"`
	IsTrack    bool   `json:"is_track"`
	TrackID    string `json:"track_id"`
}

type DiscogsTracklist struct {
	Warning   string                  `json:"warning,omitempty"`
	ReleaseID string                  `json:"release_id"`
	Status    string                  `json:"status"`
	CheckedAt string                  `json:"checked_at"`
	FetchedAt string                  `json:"fetched_at"`
	Expected  int                     `json:"expected_tracks"`
	Mapped    int                     `json:"mapped_tracks"`
	Report    json.RawMessage         `json:"report"`
	Entries   []DiscogsTracklistEntry `json:"entries"`
}

func albumOwnedTracklists(db *sql.DB, albumID string) ([]DiscogsTracklist, error) {
	rows, err := db.Query(`SELECT rv.id,COALESCE(t.status,'unverified'),COALESCE(t.checked_at,''),COALESCE(t.fetched_at,''),
 COALESCE(t.expected_tracks,0),COALESCE(t.mapped_tracks,0),COALESCE(t.report_json,'{}')
 FROM release_versions rv LEFT JOIN discogs_tracklists t ON t.release_id=rv.id
 WHERE rv.album_id=? AND rv.source='collection' ORDER BY rv.discogs_release_id,rv.id`, albumID)
	if err != nil {
		return nil, err
	}
	result := []DiscogsTracklist{}
	for rows.Next() {
		var t DiscogsTracklist
		var report string
		if err := rows.Scan(&t.ReleaseID, &t.Status, &t.CheckedAt, &t.FetchedAt, &t.Expected, &t.Mapped, &report); err != nil {
			rows.Close()
			return nil, err
		}
		t.Report = json.RawMessage(report)
		t.Entries = []DiscogsTracklistEntry{}
		result = append(result, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		t := &result[i]
		entries, err := db.Query(`SELECT path,parent_path,depth,entry_type,position,title,artist,duration_ms,is_track,COALESCE(track_id,'')
 FROM discogs_tracklist_entries WHERE release_id=? ORDER BY ordinal`, t.ReleaseID)
		if err != nil {
			return nil, err
		}
		for entries.Next() {
			var e DiscogsTracklistEntry
			if err := entries.Scan(&e.Path, &e.ParentPath, &e.Depth, &e.Type, &e.Position, &e.Title, &e.Artist, &e.DurationMs, &e.IsTrack, &e.TrackID); err != nil {
				entries.Close()
				return nil, err
			}
			t.Entries = append(t.Entries, e)
		}
		err = entries.Err()
		entries.Close()
		if err != nil {
			return nil, err
		}
		// Manual relinks/deletions must never leave a stale completeness badge.
		var valid, extra int
		if err := db.QueryRow(`SELECT count(*) FROM discogs_tracklist_entries e JOIN tracks tr ON tr.id=e.track_id
 WHERE e.release_id=? AND e.is_track=1 AND tr.release_id=e.release_id AND tr.album_id=? AND tr.title=e.title AND tr.artist=e.artist AND COALESCE(tr.track_number,'')=e.position`, t.ReleaseID, albumID).Scan(&valid); err != nil {
			return nil, err
		}
		if err := db.QueryRow(`SELECT count(*) FROM tracks tr WHERE tr.release_id=? AND NOT EXISTS
 (SELECT 1 FROM discogs_tracklist_entries e WHERE e.release_id=tr.release_id AND e.track_id=tr.id AND e.is_track=1)`, t.ReleaseID).Scan(&extra); err != nil {
			return nil, err
		}
		if t.Status == "complete" && (valid != t.Expected || extra > 0) {
			t.Status = "changed"
			t.Warning = "Recording links changed since verification; refresh this release"
			t.Mapped = valid
		}
		if t.Status == "complete" {
			fetched, err := time.Parse(time.RFC3339, t.FetchedAt)
			if err != nil || time.Since(fetched) > 30*24*time.Hour {
				t.Status = "stale"
				t.Warning = "Refresh official release metadata to verify current coverage"
			}
		}
	}
	return result, nil
}
