package main

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func tempoDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := initDB(filepath.Join(t.TempDir(), "music.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func tempoExec(t *testing.T, db *sql.DB, sql string) {
	t.Helper()
	if _, err := db.Exec(sql); err != nil {
		t.Fatal(err)
	}
}
func TestTempoMigrationPreservesLegacyTracks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Real pre-feature schema, with tempo fields absent.
	legacy := schemaSQL
	for _, line := range strings.Split(schemaSQL, "\n") {
		if strings.Contains(line, "    bpm") {
			legacy = strings.Replace(legacy, line+"\n", "", 1)
		}
	}
	tempoExec(t, db, legacy)
	tempoExec(t, db, "INSERT INTO tracks(id,title) VALUES('old','Keep me')")
	db.Close()
	for i := 0; i < 2; i++ {
		db, err = initDB(path)
		if err != nil {
			t.Fatal(err)
		}
		var title string
		var bpm *float64
		if err = db.QueryRow("SELECT title,bpm FROM tracks WHERE id='old'").Scan(&title, &bpm); err != nil || title != "Keep me" || bpm != nil {
			t.Fatalf("legacy changed: %q %v %v", title, bpm, err)
		}
		db.Close()
	}
}
func TestTempoRangeValidation(t *testing.T) {
	for _, q := range []string{"bpm_min=NaN", "bpm_min=Inf", "bpm_min=19", "bpm_max=401", "bpm_min=130&bpm_max=120", "bpm_min=oops"} {
		v, _ := url.ParseQuery(q)
		if _, err := parseTempoRange(v); err == nil {
			t.Fatalf("accepted %s", q)
		}
	}
	v, _ := url.ParseQuery("bpm_min=120&bpm_max=125&vinyl=1")
	r, err := parseTempoRange(v)
	if err != nil || !r.Vinyl || !r.matches(120) || !r.matches(125) || r.matches(119.9) || validBPM(math.NaN()) {
		t.Fatal("range boundaries")
	}
}
func TestCollectionTempoUsesActualOwnedRecordings(t *testing.T) {
	db := tempoDB(t)
	tempoExec(t, db, `INSERT INTO albums(id,title,artist,in_collection) VALUES('a','Two tempos','Artist',1);
 INSERT INTO release_versions(id,album_id,title,artist,source,has_vinyl,format_description) VALUES
 ('vinyl','a','Two tempos','Artist','collection',1,'Vinyl'),('want','a','Two tempos','Artist','wantlist',1,'Vinyl');
 INSERT INTO tracks(id,album_id,release_id,title,bpm) VALUES
 ('slow','a','vinyl','Slow',90),('fast','a','vinyl','Fast',150),('unknown','a','vinyl','Unknown',NULL),
 ('wrong-mix','a','want','Wrong mix',120),('catalog','a',NULL,'Catalog only',120);`)
	min, max := 120., 125.
	result, err := collectionTempos(db, TempoRange{Min: &min, Max: &max, Vinyl: true})
	if err != nil {
		t.Fatal(err)
	}
	a := result["a"]
	if a.BPMTotal != 3 || a.BPMKnown != 2 || len(a.MatchingTracks) != 0 {
		t.Fatalf("misleading average or unowned version used: %+v", a)
	}
	tempoExec(t, db, "INSERT INTO tracks(id,album_id,release_id,title,bpm) VALUES('match','a','vinyl','Actual match',125)")
	result, err = collectionTempos(db, TempoRange{Min: &min, Max: &max, Vinyl: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result["a"].MatchingTracks) != 1 || result["a"].MatchingTracks[0].ID != "match" {
		t.Fatal(result)
	}
	// Indexed local audio is owned, but isn't evidence for a vinyl pressing.
	tempoExec(t, db, `INSERT INTO audio_files(id,album_id,track_id,relpath) VALUES('file','a','catalog','song.flac')`)
	result, err = collectionTempos(db, TempoRange{Min: &min, Max: &max})
	if err != nil || len(result["a"].MatchingTracks) != 2 {
		t.Fatalf("local ownership: %v %+v", err, result)
	}
}
func TestManualTempoAPI(t *testing.T) {
	db := tempoDB(t)
	tempoExec(t, db, "INSERT INTO tracks(id,title) VALUES('t','Track')")
	handler := trackBPMHandler(db)
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{
		{"GET", "/api/tracks/t/bpm", "", 405}, {"PUT", "/api/tracks/t/bpm", `{}`, 400},
		{"PUT", "/api/tracks/t/bpm", `{"bpm":0}`, 400}, {"PUT", "/api/tracks/t/bpm", `{"bpm":"120"}`, 400},
		{"PUT", "/api/tracks/missing/bpm", `{"bpm":120}`, 404},
		{"PUT", "/api/tracks/t/bpm", `{"bpm":123.4}`, 200}, {"PUT", "/api/tracks/t/bpm", `{"bpm":null}`, 200},
		{"DELETE", "/api/tracks/t/bpm", "", 200},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d %s", tc.method, tc.body, w.Code, w.Body.String())
		}
		if tc.code == 200 {
			var source sql.NullString
			var bpm *float64
			db.QueryRow("SELECT bpm,bpm_source FROM tracks WHERE id='t'").Scan(&bpm, &source)
			if tc.method == "PUT" && source.String != "manual" {
				t.Fatal("manual lock missing")
			}
			if tc.method == "DELETE" && (source.Valid || bpm != nil) {
				t.Fatal("reset failed")
			}
			var data map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
		}
	}
}
