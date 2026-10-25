package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TempoRange struct {
	Min, Max *float64
	Vinyl    bool
}

func (b TempoRange) active() bool { return b.Min != nil || b.Max != nil }
func (b TempoRange) matches(v float64) bool {
	return (b.Min == nil || v >= *b.Min) && (b.Max == nil || v <= *b.Max)
}
func parseTempoRange(q url.Values) (TempoRange, error) {
	b := TempoRange{Vinyl: q.Get("vinyl") == "1"}
	for _, field := range []struct {
		name   string
		target **float64
	}{{"bpm_min", &b.Min}, {"bpm_max", &b.Max}} {
		if s := q.Get(field.name); s != "" {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil || !validBPM(v) {
				return b, fmt.Errorf("invalid BPM")
			}
			*field.target = &v
		}
	}
	if b.Min != nil && b.Max != nil && *b.Min > *b.Max {
		return b, fmt.Errorf("inverted BPM range")
	}
	return b, nil
}
func validBPM(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 20 && v <= 400 }

type TempoSummary struct {
	BPMKnown       int           `json:"bpm_known"`
	BPMTotal       int           `json:"bpm_total"`
	MatchingTracks []TrackDetail `json:"matching_tracks,omitempty"`
}

// Only explicitly owned release tracks and indexed local files are eligible.
// A catalog/Spotify track attached to the same canonical album isn't proof that
// the recording is on the owned pressing. Vinyl mode requires that pressing.
func collectionTempos(db *sql.DB, bounds TempoRange) (map[string]TempoSummary, error) {
	rows, err := db.Query(`SELECT t.id,t.album_id,t.title,COALESCE(t.artist,''),
 COALESCE(t.track_number,''),COALESCE(t.release_id,''),t.bpm,COALESCE(t.bpm_source,''),
 COALESCE(t.bpm_provenance,''),COALESCE(t.bpm_updated_at,''),
 CASE WHEN rv.source='collection' THEN COALESCE(rv.format_description,'Owned release') || ' · ' || COALESCE(rv.catalog_number,'') || ' · Discogs ' || COALESCE(rv.discogs_release_id,'') ELSE 'Local audio' END
 FROM tracks t JOIN albums a ON a.id=t.album_id
 LEFT JOIN release_versions rv ON rv.id=t.release_id
 WHERE a.in_collection=1 AND
 ((rv.source='collection') OR EXISTS (SELECT 1 FROM audio_files f WHERE f.track_id=t.id AND f.kind='track'))
 AND (?=0 OR (rv.source='collection' AND rv.has_vinyl=1))
 ORDER BY t.bpm,t.track_number,t.title`, bounds.Vinyl)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]TempoSummary{}
	for rows.Next() {
		var t TrackDetail
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.Artist, &t.TrackNumber, &t.ReleaseID, &t.BPM, &t.BPMSource, &t.BPMProvenance, &t.BPMUpdatedAt, &t.TempoVersion); err != nil {
			return nil, err
		}
		b := result[t.AlbumID]
		b.BPMTotal++
		if t.BPM != nil {
			b.BPMKnown++
			if bounds.active() && bounds.matches(*t.BPM) {
				b.MatchingTracks = append(b.MatchingTracks, t)
			}
		}
		result[t.AlbumID] = b
	}
	return result, rows.Err()
}

type CollectionRecordRow struct {
	ID            string        `json:"id"`
	Title         string        `json:"title"`
	Artist        string        `json:"artist"`
	CoverImageURL string        `json:"cover_image_url"`
	HasVinyl      bool          `json:"has_vinyl"`
	Formats       string        `json:"formats"`
	Tracks        []TrackDetail `json:"tracks"`
	MinBPM        *float64      `json:"min_bpm"`
}

// Owned collection records with their pickable tracks for building DJ-set
// tracklists. Eligible tracks mirror the tempo filter: they must belong to an
// owned collection pressing or a locally indexed file (a catalog/Spotify track
// on the same canonical album isn't proof the recording is in the crate).
// Records (and the tracks within them) are surfaced in ascending known-BPM
// order so a builder can click top-to-bottom for a smooth energy ramp;
// unknown-BPM records/tracks sort last, kept stable.
func collectionRecords(db *sql.DB) ([]CollectionRecordRow, error) {
	rows, err := db.Query(`
		SELECT a.id, a.title, a.artist, COALESCE(a.cover_image_url,''), COALESCE(a.has_vinyl,0),
		       t.id, t.title, COALESCE(t.artist,a.artist), COALESCE(t.duration_ms,0),
		       COALESCE(t.track_number,''), t.bpm, COALESCE(t.release_id,''),
		       COALESCE((SELECT format_description || ' · ' || COALESCE(catalog_number,'') FROM release_versions rv WHERE rv.id = t.release_id), '')
		FROM albums a
		JOIN tracks t ON t.album_id = a.id
		WHERE a.in_collection = 1
		  AND ( EXISTS (SELECT 1 FROM release_versions rv WHERE rv.id = t.release_id AND rv.source = 'collection')
		        OR EXISTS (SELECT 1 FROM audio_files f WHERE f.track_id = t.id AND f.kind = 'track') )
		ORDER BY a.title ASC, t.track_number, t.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := map[string]*CollectionRecordRow{}
	var order []string
	for rows.Next() {
		var aid, atitle, aartist, acover, formats string
		var hasVinyl int
		var t TrackDetail
		if err := rows.Scan(&aid, &atitle, &aartist, &acover, &hasVinyl, &t.ID, &t.Title, &t.Artist, &t.DurationMs, &t.TrackNumber, &t.BPM, &t.ReleaseID, &formats); err != nil {
			return nil, err
		}
		g, ok := groups[aid]
		if !ok {
			g = &CollectionRecordRow{ID: aid, Title: atitle, Artist: aartist, CoverImageURL: acover, HasVinyl: hasVinyl == 1, Formats: formats}
			groups[aid] = g
			order = append(order, aid)
		}
		g.Tracks = append(g.Tracks, t)
	}

	result := make([]CollectionRecordRow, 0, len(order))
	for _, aid := range order {
		g := groups[aid]
		sortTracksByBPM(g.Tracks)
		var min *float64
		for i := range g.Tracks {
			if g.Tracks[i].BPM != nil && (min == nil || *g.Tracks[i].BPM < *min) {
				v := *g.Tracks[i].BPM
				min = &v
			}
		}
		g.MinBPM = min
		result = append(result, *g)
	}

	// Ascending lowest known BPM; unknown-BPM records last (stable by title).
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i].MinBPM, result[j].MinBPM
		switch {
		case a == nil && b == nil:
			return result[i].Title < result[j].Title
		case a == nil:
			return false
		case b == nil:
			return true
		default:
			return *a < *b
		}
	})
	return result, nil
}

func sortTracksByBPM(tracks []TrackDetail) {
	sort.SliceStable(tracks, func(i, j int) bool {
		a, b := tracks[i].BPM, tracks[j].BPM
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return *a < *b
	})
}

// Reorder a playlist's tracks by BPM (stable), e.g. to shape a DJ set into an
// ascending-energy ramp. Tracks without BPM keep their relative order and move
// to the end. direction: "asc" (default) or "desc".
func sortPlaylistByBPM(db *sql.DB, playlistID, direction string) error {
	rows, err := db.Query(`
		SELECT pt.track_id, t.bpm
		FROM playlist_tracks pt
		LEFT JOIN tracks t ON t.id = pt.track_id
		WHERE pt.playlist_id = ?
		ORDER BY pt.position ASC`, playlistID)
	if err != nil {
		return err
	}
	type item struct {
		trackID string
		bpm     *float64
	}
	var items []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.trackID, &it.bpm) == nil {
			items = append(items, it)
		}
	}
	rows.Close()

	// Unknown BPM always sorts last (stable); known values asc (default) or desc.
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].bpm, items[j].bpm
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		if direction == "desc" {
			return *a > *b
		}
		return *a < *b
	})

	now := time.Now().Format("2006-01-02 15:04:05")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM playlist_tracks WHERE playlist_id = ?", playlistID); err != nil {
		return err
	}
	for i, it := range items {
		if _, err := tx.Exec("INSERT INTO playlist_tracks (playlist_id, track_id, position, added_at) VALUES (?, ?, ?, ?)", playlistID, it.trackID, i+1, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE playlists SET updated_at = ? WHERE id = ?", now, playlistID); err != nil {
		return err
	}
	return tx.Commit()
}

// Swap a single playlist track with its up/down neighbour so a DJ can put a
// specific record next in the set without rebuilding the playlist.
func movePlaylistTrack(db *sql.DB, playlistID string, position int, direction string) error {
	if direction != "up" && direction != "down" {
		return fmt.Errorf("direction must be up or down")
	}
	offset := 1
	if direction == "up" {
		offset = -1
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT track_id FROM playlist_tracks WHERE playlist_id = ? ORDER BY position ASC", playlistID)
	if err != nil {
		return err
	}
	var items []string
	for rows.Next() {
		var tid string
		if rows.Scan(&tid) == nil {
			items = append(items, tid)
		}
	}
	rows.Close()
	if position-1 >= len(items) {
		return fmt.Errorf("track at position %d not found", position)
	}
	target := position - 1 + offset
	if target < 0 || target >= len(items) {
		return nil // no neighbour — already at the edge; no-op success
	}
	items[position-1], items[target] = items[target], items[position-1]

	now := time.Now().Format("2006-01-02 15:04:05")
	if _, err := tx.Exec("DELETE FROM playlist_tracks WHERE playlist_id = ?", playlistID); err != nil {
		return err
	}
	for i, tid := range items {
		if _, err := tx.Exec("INSERT INTO playlist_tracks (playlist_id, track_id, position, added_at) VALUES (?, ?, ?, ?)", playlistID, tid, i+1, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE playlists SET updated_at = ? WHERE id = ?", now, playlistID); err != nil {
		return err
	}
	return tx.Commit()
}

// A manual NULL is an explicit lock too. DELETE removes the lock and makes
// the track eligible for automatic enrichment again.
func trackBPMHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/tracks/"), "/bpm")
		if id == "" || strings.Contains(id, "/") || !strings.HasSuffix(r.URL.Path, "/bpm") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPut && r.Method != http.MethodDelete {
			w.Header().Set("Allow", "PUT, DELETE")
			http.Error(w, "Method not allowed", 405)
			return
		}
		var value *float64
		var source, provenance any
		if r.Method == http.MethodPut {
			var body map[string]json.RawMessage
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			if err := dec.Decode(&body); err != nil || body["bpm"] == nil {
				http.Error(w, "Expected {bpm: number or null}", 400)
				return
			}
			if err := json.Unmarshal(body["bpm"], &value); err != nil || (value != nil && !validBPM(*value)) {
				http.Error(w, "BPM must be null or between 20 and 400", 400)
				return
			}
			source = "manual"
			provenance = `{"method":"manual override"}`
		}
		res, err := db.Exec("UPDATE tracks SET bpm=?,bpm_source=?,bpm_provenance=?,bpm_updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?", value, source, provenance, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": id, "bpm": value, "bpm_source": source})
	}
}
