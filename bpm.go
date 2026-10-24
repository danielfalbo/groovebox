package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
