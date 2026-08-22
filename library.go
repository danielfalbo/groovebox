package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// musicLibrary is the Syncthing music library scanned by /api/sync/local.
// Filesystem is the source of truth about existence; DB is truth of identity.
var musicLibrary = "/home/me/syncthing/archive/music"

var (
	reAnnual  = regexp.MustCompile(`\((\d{4})\)\s*$`)
	reNumeric = regexp.MustCompile(`^(\d{1,3})[-\._\s]+`)
	reRawSub  = regexp.MustCompile(`(?i)\[raw\s+recording\]`)
)

// splitAlbumTitle splits "Artist (1999)" into ("Artist", 1999).
func splitAlbumTitle(name string) (string, int) {
	year := 0
	if m := reAnnual.FindStringSubmatch(name); m != nil {
		year, _ = strconv.Atoi(m[1])
		name = strings.TrimSpace(strings.TrimSuffix(name, m[0]))
	}
	return strings.TrimSpace(name), year
}

// parseTrackNum extracts a leading track number.
func parseTrackNum(filename string) string {
	if m := reNumeric.FindStringSubmatch(filename); m != nil {
		return m[1]
	}
	return ""
}

// parseTrackTitle derives a clean title from the basename (without extension).
func parseTrackTitle(base string) string {
	t := base
	if m := reNumeric.FindStringSubmatch(t); m != nil {
		t = strings.TrimSpace(t[len(m[0]):])
	}
	if idx := strings.LastIndex(t, " - "); idx > 0 {
		if tail := strings.TrimSpace(t[idx+3:]); tail != "" {
			return tail
		}
	}
	return t
}

// scanLibrary walks the music root and updates the DB (one-way fs -> DB).
func scanLibrary(db *sql.DB) error {
	updateSyncProgress(func(p *SyncProgress) {
		p.IsSyncing = true
		p.Stage = "scanning"
		p.Message = "Scanning local music library..."
		p.LastError = ""
		p.ItemsFetched = 0
		p.TotalItems = 0
	})
	defer func() {
		updateSyncProgress(func(p *SyncProgress) {
			p.IsSyncing = false
			p.Stage = "idle"
			p.LastSyncedAt = time.Now().Format("2006-01-02 15:04:05")
		})
	}()

	if _, err := os.Stat(musicLibrary); err != nil {
		out := fmt.Errorf("music library not found at %s: %w", musicLibrary, err)
		updateSyncProgress(func(p *SyncProgress) { p.LastError = out.Error() })
		return out
	}

	var audioFiles []string
	err := filepath.Walk(musicLibrary, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".flac" || ext == ".wav" {
			audioFiles = append(audioFiles, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	seen := map[string]bool{}
	for _, af := range audioFiles {
		if serr := scanFile(db, af); serr != nil {
			log.Printf("scan: %s: %v", af, serr)
		} else {
			if rel, rerr := filepath.Rel(musicLibrary, af); rerr == nil {
				seen[filepath.ToSlash(rel)] = true
			}
		}
	}

	// Drop rows for files no longer present on disk.
	existing, eq := db.Query("SELECT relpath FROM audio_files")
	if eq == nil {
		var drop []string
		for existing.Next() {
			var rp string
			existing.Scan(&rp)
			if !seen[rp] {
				drop = append(drop, rp)
			}
		}
		existing.Close()
		for _, rp := range drop {
			_, _ = db.Exec("DELETE FROM audio_files WHERE relpath = ?", rp)
			log.Printf("scan: removed absent file %s", rp)
		}
	}

	updateSyncProgress(func(p *SyncProgress) {
		p.ItemsFetched = len(audioFiles)
		p.Message = fmt.Sprintf("Scan complete: %d audio files.", len(audioFiles))
	})
	log.Printf("local scan complete: %d audio files", len(audioFiles))
	return nil
}

// scanFile maps one audio file into albums/tracks/audio_files.
func scanFile(db *sql.DB, path string) error {
	rel, err := filepath.Rel(musicLibrary, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return fmt.Errorf("too shallow: %s", rel)
	}
	artist := parts[0]
	albumName, year := splitAlbumTitle(parts[1])

	inRaw := false
	source := "playback"
	for _, comp := range parts {
		lc := strings.ToLower(comp)
		if reRawSub.MatchString(comp) {
			inRaw = true
		}
		if strings.HasPrefix(comp, "[") {
			switch {
			case strings.Contains(lc, "cd") && strings.Contains(lc, "rip"):
				source = "cd"
			case strings.Contains(lc, "vinyl"):
				source = "vinyl"
			case strings.Contains(lc, "playback"):
				source = "playback"
			}
		}
	}
	kind := "track"
	if inRaw {
		kind = "raw"
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")

	albumID, err := ensureAlbum(db, artist, albumName, year, localCoverURL(db, artist, parts[1]))
	if err != nil {
		return err
	}
	releaseID := ensureRelease(db, albumID, artist, albumName, source)

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	trackNum := parseTrackNum(base)
	title := parseTrackTitle(base)
	dur, bitDepth, sampleRate := probeAudio(path)
	sha := shaFile(path)
	var size, mtime int64
	if info, ierr := os.Stat(path); ierr == nil {
		size = info.Size()
		mtime = info.ModTime().Unix()
	}

	if kind == "track" {
		trackID, terr := ensureTrack(db, albumID, artist, title, trackNum, dur)
		if terr != nil {
			return terr
		}
		_, werr := db.Exec(`
			INSERT INTO audio_files (id, album_id, track_id, release_id, relpath, kind, source, format, bit_depth, sample_rate, size_bytes, mtime, sha256, duration_ms)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(relpath) DO UPDATE SET
				track_id=excluded.track_id, release_id=excluded.release_id, kind=excluded.kind,
				source=excluded.source, format=excluded.format, bit_depth=excluded.bit_depth,
				sample_rate=excluded.sample_rate, size_bytes=excluded.size_bytes, mtime=excluded.mtime,
				sha256=excluded.sha256, duration_ms=excluded.duration_ms, updated_at=CURRENT_TIMESTAMP`,
			uuid.New().String(), albumID, trackID, releaseID, rel, kind, source, format, bitDepth, sampleRate, size, mtime, sha, dur)
		return werr
	}

	_, werr := db.Exec(`
		INSERT INTO audio_files (id, album_id, release_id, relpath, kind, source, format, bit_depth, sample_rate, size_bytes, mtime, sha256, duration_ms)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(relpath) DO UPDATE SET
			album_id=excluded.album_id, release_id=excluded.release_id, kind=excluded.kind,
			source=excluded.source, format=excluded.format, bit_depth=excluded.bit_depth,
			sample_rate=excluded.sample_rate, size_bytes=excluded.size_bytes, mtime=excluded.mtime,
			sha256=excluded.sha256, duration_ms=excluded.duration_ms, updated_at=CURRENT_TIMESTAMP`,
		uuid.New().String(), albumID, releaseID, rel, kind, source, format, bitDepth, sampleRate, size, mtime, sha, dur)
	return werr
}

// localCoverURL looks for local art (folder.jpg / large_cover.jpg / cover.jpg)
// in the on-disk album directory (albumDir is the raw dir name, e.g. "Running (2016)"),
// and returns a served URL, or "". ensureAlbum only sets it when cover_image_url is empty,
// so it acts as a fallback to remote art.
func localCoverURL(db *sql.DB, artist, albumDir string) string {
	dir := filepath.Join(musicLibrary, filepath.FromSlash(artist+"/"+albumDir))
	find := func(names ...string) string {
		for _, n := range names {
			if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
				return n
			}
		}
		return ""
	}
	reldir := filepath.ToSlash(filepath.Join(artist, albumDir))
	if n := find("folder.jpg", "large_cover.jpg", "cover.jpg", "folder.png"); n != "" {
		return coverURLFor(filepath.Join(reldir, n))
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, n := range []string{"folder.jpg", "large_cover.jpg", "cover.jpg"} {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), n)); err == nil {
				return coverURLFor(filepath.ToSlash(filepath.Join(reldir, e.Name(), n)))
			}
		}
	}
	return ""
}

// coverURLFor builds a served URL under /api/local/cover?rel=<rel>.
func coverURLFor(relPath string) string {
	return "/api/local/cover?rel=" + url.QueryEscape(relPath)
}

func ensureAlbum(db *sql.DB, artist, title string, year int, coverURL string) (string, error) {
	var id string
	err := db.QueryRow("SELECT id FROM albums WHERE LOWER(artist)=LOWER(?) AND LOWER(title)=LOWER(?)", artist, title).Scan(&id)
	if err == nil {
		if coverURL != "" {
			_, _ = db.Exec("UPDATE albums SET cover_image_url=? WHERE id=? AND (cover_image_url IS NULL OR cover_image_url='')", coverURL, id)
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err = db.Exec("INSERT INTO albums (id, title, artist, release_year, cover_image_url, created_at, updated_at) VALUES (?,?,?,?,?,?,?)",
		id, title, artist, year, coverURL, now, now)
	if err != nil {
		return "", err
	}
	log.Printf("local: created album %q - %q", artist, title)
	return id, nil
}

func ensureRelease(db *sql.DB, albumID, artist, title, source string) string {
	var id string
	err := db.QueryRow("SELECT id FROM release_versions WHERE album_id=? AND source=? LIMIT 1", albumID, source).Scan(&id)
	if err == nil {
		return id
	}
	id = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	_, _ = db.Exec("INSERT INTO release_versions (id, album_id, title, artist, format_description, source, created_at) VALUES (?,?,?,?,?,?,?)",
		id, albumID, title, artist, source, source, now)
	return id
}

func ensureTrack(db *sql.DB, albumID, artist, title, trackNum string, durMs int64) (string, error) {
	var id string
	err := db.QueryRow("SELECT id FROM tracks WHERE album_id=? AND LOWER(title)=LOWER(?)", albumID, title).Scan(&id)
	if err == sql.ErrNoRows {
		id = uuid.New().String()
		now := time.Now().Format("2006-01-02 15:04:05")
		_, ierr := db.Exec("INSERT INTO tracks (id, album_id, title, artist, track_number, duration_ms, created_at) VALUES (?,?,?,?,?,?,?)",
			id, albumID, title, artist, trackNum, durMs, now)
		if ierr != nil {
			return "", ierr
		}
		_, _ = db.Exec("INSERT OR IGNORE INTO search_fts (target_type, target_id, title, artist) VALUES ('track',?,?,?)", id, title, artist)
	} else if err != nil {
		return "", err
	} else if durMs > 0 {
		_, _ = db.Exec("UPDATE tracks SET duration_ms=? WHERE id=?", durMs, id)
	}
	return id, nil
}

// probeAudio returns (duration_ms, bit_depth, sample_rate) via ffprobe.
func probeAudio(path string) (int64, int, int) {
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return 0, 0, 0
	}
	var res struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			BitsPerSample int    `json:"bits_per_sample"`
			SampleRate    string `json:"sample_rate"`
			CodecType     string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return 0, 0, 0
	}
	var ms int64
	if f, e := strconv.ParseFloat(res.Format.Duration, 64); e == nil {
		ms = int64(f * 1000)
	}
	bid, srate := 0, 0
	for _, s := range res.Streams {
		if s.CodecType == "audio" {
			bid = s.BitsPerSample
			srate, _ = strconv.Atoi(s.SampleRate)
			break
		}
	}
	return ms, bid, srate
}

func shaFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if rerr != nil {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}