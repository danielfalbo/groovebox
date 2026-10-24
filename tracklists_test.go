package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type discogsTransport func(*http.Request) (*http.Response, error)

func (f discogsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscogsSyncOwnedWantlistOverlapAndRefreshAfterCommit(t *testing.T) {
	db := tempoDB(t)
	t.Setenv("DISCOGS_TOKEN", "test-not-a-real-token")
	oldTransport, oldRefresh := http.DefaultTransport, refreshDiscogsTracklists
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
		refreshDiscogsTracklists = oldRefresh
		currentProgress = SyncProgress{}
	})
	currentProgress = SyncProgress{}
	info := `{"id":123,"master_id":45,"title":"Album","artists":[{"name":"Artist"}],"formats":[{"name":"Vinyl","qty":"2"}]}`
	http.DefaultTransport = discogsTransport(func(r *http.Request) (*http.Response, error) {
		var body string
		switch {
		case r.URL.Path == "/oauth/identity":
			body = `{"username":"fixture"}`
		case strings.Contains(r.URL.Path, "/collection/"):
			body = `{"pagination":{"pages":1,"items":1},"releases":[{"id":123,"date_added":"2026-10-24T12:00:00Z","basic_information":` + info + `}]}`
		case strings.HasSuffix(r.URL.Path, "/wants"):
			body = `{"pagination":{"pages":1,"items":1},"wants":[{"id":123,"basic_information":` + info + `}]}`
		default:
			return nil, fmt.Errorf("unexpected network %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	called := false
	refreshDiscogsTracklists = func(db *sql.DB, inventory []DiscogsCollectionItem) error {
		called = true
		if len(inventory) != 1 || inventory[0].ID != 123 {
			t.Fatal("incomplete refresh inventory")
		}
		var source string
		var owned, wanted int
		if err := db.QueryRow(`SELECT rv.source,a.in_collection,a.in_wantlist FROM release_versions rv JOIN albums a ON a.id=rv.album_id WHERE discogs_release_id=123`).Scan(&source, &owned, &wanted); err != nil {
			t.Fatal(err)
		}
		if source != "collection" || owned != 1 || wanted != 1 {
			t.Fatalf("ownership overwritten: %s %d %d", source, owned, wanted)
		}
		return nil
	}
	if err := SyncDiscogs(db); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("normal sync did not refresh tracklists")
	}
}

func TestOwnedTracklistAPIPreservesTreeOrderAndRejectsStaleCompleteness(t *testing.T) {
	db := tempoDB(t)
	tempoExec(t, db, `INSERT INTO albums(id,title,artist) VALUES('a','Album','Artist');
 INSERT INTO release_versions(id,album_id,title,artist,source,discogs_release_id) VALUES('r','a','Album','Artist','collection',123),('w','a','Wrong pressing','Artist','wantlist',456);
 INSERT INTO tracks(id,album_id,release_id,title,artist,track_number,bpm,bpm_source) VALUES('t','a','r','Part 1','Artist','2-01',NULL,'manual'),('catalog','a',NULL,'Unowned remix','Artist','1',128,NULL);
 INSERT INTO discogs_tracklist_entries(release_id,path,ordinal,parent_path,depth,entry_type,position,title,artist,is_track,track_id,raw_json) VALUES
 ('r','1',0,'',0,'heading','','Disc Two','Artist',0,NULL,'{}'),
 ('r','2',1,'',0,'index','','Suite (Extended)','Artist',0,NULL,'{}'),
 ('r','2.1',2,'2',1,'track','2-01','Part 1','Artist',1,'t','{}');`)
	_, err := db.Exec(`INSERT INTO discogs_tracklists VALUES('r','complete',?,?, 'hash',1,1,'{}','{}')`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	lists, err := albumOwnedTracklists(db, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 1 || lists[0].Status != "complete" || len(lists[0].Entries) != 3 || lists[0].Entries[2].ParentPath != "2" || lists[0].Entries[2].TrackID != "t" {
		t.Fatalf("lost hierarchy: %+v", lists)
	}
	encoded, err := json.Marshal(lists)
	if err != nil || !json.Valid(encoded) {
		t.Fatal("invalid API JSON", err)
	}
	tempoExec(t, db, "UPDATE tracks SET release_id=NULL WHERE id='t'")
	lists, err = albumOwnedTracklists(db, "a")
	if err != nil || lists[0].Status != "changed" || lists[0].Mapped != 0 {
		t.Fatalf("false completeness after relink: %+v %v", lists, err)
	}
}

func TestAlbumDedupeRetainsSameTitleDifferentPressingsAndAllReferences(t *testing.T) {
	db := tempoDB(t)
	tempoExec(t, db, `INSERT INTO albums(id,title,artist,in_collection) VALUES('a','Album','Artist',1),('b','Album (Remastered)','Artist',1);
 INSERT INTO release_versions(id,album_id,title,artist,source) VALUES('r1','a','Album','Artist','collection'),('r2','b','Album','Artist','collection');
 INSERT INTO tracks(id,album_id,release_id,title,bpm,bpm_source,bpm_provenance) VALUES('t1','a','r1','Song',128,'manual','keep'),('t2','b','r2','Song',NULL,'manual','null lock');
 INSERT INTO playlists(id,name) VALUES('p','Keep');
 INSERT INTO playlist_tracks(playlist_id,track_id,position) VALUES('p','t1',1),('p','t2',2);
 INSERT INTO audio_files(id,track_id,relpath) VALUES('file','t2','keep.flac');`)
	merged, err := DedupeAlbums(db)
	if err != nil || merged != 1 {
		t.Fatalf("merge failed: %d %v", merged, err)
	}
	for _, table := range []string{"tracks", "playlist_tracks", "release_versions"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 2 {
			t.Fatalf("%s lost IDs/references %d %v", table, n, err)
		}
	}
	var release, source, provenance, audio string
	var bpm *float64
	if err := db.QueryRow("SELECT release_id,bpm,bpm_source,bpm_provenance FROM tracks WHERE id='t2'").Scan(&release, &bpm, &source, &provenance); err != nil {
		t.Fatal(err)
	}
	if release != "r2" || bpm != nil || source != "manual" || provenance != "null lock" {
		t.Fatal("curation changed")
	}
	if err := db.QueryRow("SELECT track_id FROM audio_files WHERE id='file'").Scan(&audio); err != nil || audio != "t2" {
		t.Fatal("audio link lost")
	}
}
