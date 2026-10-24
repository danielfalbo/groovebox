#!/usr/bin/env python3
"""Bounded, resumable metadata-only enrichment. See docs/BPM.md.

No audio leaves the server. Only the isolated --analyze worker imports librosa.
"""
import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import fcntl
import json
import math
import os
from pathlib import Path
import re
import sqlite3
import signal
import subprocess
import sys
import time
import unicodedata
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
ALGORITHM = "librosa-0.11.0/three-window-v1"
DANCE_ARTISTS = ("Peggy GOU", "Demuja", "Inner City", "Stardust", "Robin S.",
                 "Shakedown", "Aly-Us", "Camelphat, Elderbrook", "Daft Punk", "Chic",
                 "The Rhythm Slaves", "Paul Woolford")


def norm(text):
    # Keep every word: radio edits, remixes, live versions and numbers differ.
    return " ".join(re.findall(r"[^\W_]+", unicodedata.normalize("NFKC", text or "").casefold()))


def artist_name(text):
    return re.sub(r" \(\d+\)$", "", text.strip())


def duration_close(a, b):
    return bool(a and b and abs(a - b) <= max(3000, a * .01))


def recording_match(track, rec):
    credits = rec.get("artist-credit", [])
    artists = ", ".join(c.get("artist", {}).get("name", c.get("name", ""))
                        for c in credits if isinstance(c, dict))
    if norm(track["artist"]) != norm(artists) or norm(track["title"]) != norm(rec.get("title")):
        return False
    # Search scores, popularity, album names and base-title similarity aren't evidence.
    return duration_close(track["duration_ms"], rec.get("length"))


def analyze(path):
    import librosa
    import numpy as np
    # Decode three interior windows to avoid intros/outros; each is bounded.
    info = json.loads(subprocess.check_output([
        "ffprobe", "-v", "quiet", "-show_format", "-of", "json", str(path)], timeout=15))
    duration = float(info["format"]["duration"])
    if duration < 45 or duration > 1200:
        return {"status": "unsupported-duration", "duration": duration}
    window = min(45, duration / 3)
    offsets = [max(0, duration * f - window / 2) for f in (.25, .5, .75)]
    estimates = []
    for offset in offsets:
        raw = subprocess.check_output(["ffmpeg", "-v", "error", "-nostdin", "-threads", "1",
            "-ss", str(offset), "-i", str(path), "-t", str(window), "-ac", "1", "-ar", "22050",
            "-f", "f32le", "pipe:1"], timeout=20)
        y = np.frombuffer(raw, dtype="<f4")
        if len(y) < 22050 * 10 or not np.isfinite(y).all() or float(np.max(np.abs(y))) < .0001:
            return {"status": "insufficient-signal"}
        tempo, beats = librosa.beat.beat_track(y=y, sr=22050, hop_length=256, units="time")
        if len(beats) < 16 or float(np.asarray(tempo).item()) <= 0:
            return {"status": "insufficient-beats"}
        # Fit all detected beats to reduce the quantization of the tempo bin.
        slope = np.polyfit(np.arange(len(beats)), beats, 1)[0]
        estimates.append(float(60 / slope))
    bpm = float(np.median(estimates))
    if not 20 <= bpm <= 400 or max(estimates) - min(estimates) > bpm * .04:
        return {"status": "inconsistent-windows", "estimates": estimates}
    return {"status": "ok", "bpm": round(bpm, 2), "algorithm": ALGORITHM,
            "offsets_seconds": offsets, "window_seconds": window,
            "window_bpms": estimates, "librosa_version": librosa.__version__}


class Budget:
    def __init__(self, seconds):
        self.deadline = time.monotonic() + seconds

    def check(self, reserve=0):
        if time.monotonic() + reserve >= self.deadline:
            raise RuntimeError("batch time budget reached; rerun to resume")


class HTTP:
    def __init__(self, cache, budget, refresh=False):
        self.cache, self.budget = cache, budget
        self.refresh = refresh
        cache.mkdir(parents=True, exist_ok=True)
        self.requests = Counter()

    def get(self, url):
        # Coordinate independent/resumed batches so aggregate request rates stay low.
        host = urllib.parse.urlsplit(url).netloc
        with (self.cache / ("rate-" + host)).open("a+") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            return self._get(url, lock)

    def _get(self, url, lock):
        key = hashlib.sha256(url.encode()).hexdigest()
        dest = self.cache / (key + ".json")
        if not self.refresh and dest.exists() and time.time() - dest.stat().st_mtime < 30 * 86400:
            return json.loads(dest.read_text())
        host = urllib.parse.urlsplit(url).netloc
        self.budget.check(18)
        # 24 req/min for unauthenticated Discogs; <= 1/sec MusicBrainz.
        interval = 2.5 if host == "api.discogs.com" else 1.1
        lock.seek(0)
        last = float(lock.read() or 0)
        time.sleep(max(0, interval - (time.time() - last)))
        lock.seek(0); lock.truncate(); lock.write(str(time.time())); lock.flush()
        request = urllib.request.Request(url, headers={
            "User-Agent": "Groovebox/1.0 (https://github.com/danielfalbo/groovebox; personal music library)",
            "Accept": "application/json"})
        self.requests[host] += 1
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                payload = response.read(3_000_001)
                if len(payload) > 3_000_000:
                    raise ValueError("response too large")
                value = json.loads(payload)
        except urllib.error.HTTPError as exc:
            if exc.code == 404:
                value = {"_not_found": True}
            elif exc.code in (401, 403, 429):
                # Stop the batch, don't loop against access/rate limits.
                raise RuntimeError(f"{host} HTTP {exc.code}; batch stopped, retry later") from None
            else:
                raise OSError(f"{host} HTTP {exc.code}") from None
        tmp = dest.with_suffix(".tmp")
        tmp.write_text(json.dumps(value))
        tmp.replace(dest)
        return value


def query_url(base, **params):
    return base + "?" + urllib.parse.urlencode(params)


def external_result(track, http):
    if not track["duration_ms"] or track["duration_ms"] < 30000:
        return {"status": "missing-duration"}
    if not track["artist"] or norm(track["artist"]) in ("various", "various artists"):
        return {"status": "missing-artist"}
    def quote(s):
        return '"' + s.replace('\\', '\\\\').replace('"', '\\"') + '"'
    # ISRC lookup when available; no loose fallbacks from failed ISRC identities.
    isrc = (track["isrc"] or "").strip()
    query = "isrc:" + isrc if re.fullmatch(r"[A-Z]{2}[A-Z0-9]{3}\d{7}", isrc) else (
        "recording:" + quote(track["title"]) + " AND artist:" + quote(track["artist"]))
    mb_url = query_url("https://musicbrainz.org/ws/2/recording/", query=query, fmt="json", limit=25)
    data = http.get(mb_url)
    if data.get("count", 0) > 25:
        return {"status": "ambiguous-search"}
    matches = {r["id"]: r for r in data.get("recordings", []) if recording_match(track, r)}
    if len(matches) != 1:
        return {"status": "no-exact-recording" if not matches else "ambiguous-recording"}
    mbid, rec = next(iter(matches.items()))
    ab_url = f"https://acousticbrainz.org/api/v1/{mbid}/low-level"
    ab = http.get(ab_url)
    bpm = ab.get("rhythm", {}).get("bpm")
    if not isinstance(bpm, (float, int)) or not math.isfinite(bpm) or not 20 <= bpm <= 400:
        return {"status": "no-archived-bpm", "mbid": mbid}
    meta = ab.get("metadata", {})
    tags = meta.get("tags", {})
    # An MBID can have mislabelled submissions. Check the submitted audio too.
    titles = tags.get("title", [])
    artists = tags.get("artist", [])
    length = meta.get("audio_properties", {}).get("length", 0) * 1000
    if not any(norm(t) == norm(track["title"]) for t in titles) or not any(
            norm(a) == norm(track["artist"]) for a in artists) or not duration_close(track["duration_ms"], length):
        return {"status": "archive-version-mismatch", "mbid": mbid}
    return {"status": "ok", "bpm": round(bpm, 2), "url": ab_url, "mbid": mbid,
            "match": "unique exact title+artist+duration" + ("; ISRC lookup" if query.startswith("isrc:") else ""),
            "recording_title": rec["title"], "recording_duration_ms": rec.get("length"),
            "archive_duration_ms": length, "archive_version": meta.get("version", {})}


def external_job_key(track):
    identity = ["acousticbrainz/exact-v1", track["title"], track["artist"], track["duration_ms"], track["isrc"]]
    return hashlib.sha256(json.dumps(identity).encode()).hexdigest()


def write_bpm(db, track_id, result, source):
    # Atomic guard protects a manual correction even if it arrives mid-analysis.
    cur = db.execute("""UPDATE tracks SET bpm=?, bpm_source=?, bpm_provenance=?, bpm_updated_at=?
        WHERE id=? AND COALESCE(bpm_source,'') != 'manual'
        AND (?='local:librosa' OR bpm_source IS NULL OR bpm_source NOT LIKE 'local:%')""",
        (result["bpm"], source, json.dumps(result, ensure_ascii=False),
         datetime.now(timezone.utc).isoformat(), track_id, source))
    db.commit()
    return cur.rowcount


def tracklist_rows(data):
    def walk(rows):
        for t in rows:
            if t.get("sub_tracks"):
                yield from walk(t["sub_tracks"])
            elif t.get("type_", "track") == "track" and t.get("title") and t.get("position"):
                yield t
    return list(walk(data.get("tracklist", [])))


def import_tracklist(db, release, data):
    inserted = 0
    with db:
        for t in tracklist_rows(data):
            artist = ", ".join(artist_name(a["name"]) for a in t.get("artists", data.get("artists", []))) or release["artist"]
            dur = t.get("duration", "")
            duration = 0
            if re.fullmatch(r"\d+:\d{2}", dur):
                mins, secs = map(int, dur.split(":")); duration = (mins * 60 + secs) * 1000
            tid = str(uuid.uuid5(uuid.NAMESPACE_URL,
                f"discogs:{release['discogs_release_id']}:{t['position']}:{t['title']}"))
            # Don't alter any pre-existing recording identity or overwrite manual data.
            existing = db.execute("SELECT id FROM tracks WHERE release_id=? AND track_number=? AND title=?",
                                  (release["id"], t["position"], t["title"])).fetchone()
            if existing:
                continue
            cur = db.execute("""INSERT OR IGNORE INTO tracks
                (id,album_id,release_id,title,artist,track_number,duration_ms)
                VALUES (?,?,?,?,?,?,?)""", (tid, release["album_id"], release["id"], t["title"], artist, t["position"], duration))
            if cur.rowcount:
                db.execute("INSERT INTO search_fts (target_type,target_id,title,artist) VALUES ('track',?,?,?)",
                           (tid, t["title"], artist))
                inserted += 1
    return inserted


def run(args):
    budget = Budget(args.max_seconds)
    def stop_batch(signum, frame):
        budget.deadline = 0
    signal.signal(signal.SIGTERM, stop_batch)
    signal.signal(signal.SIGINT, stop_batch)
    cache = Path(args.cache)
    http = HTTP(cache / "http", budget, args.refresh)
    db = sqlite3.connect(f"file:{Path(args.db).resolve()}?mode=rw", uri=True, timeout=5)
    db.row_factory = sqlite3.Row
    db.execute("PRAGMA foreign_keys=ON")
    if "bpm" not in [r[1] for r in db.execute("PRAGMA table_info(tracks)")]:
        raise RuntimeError("Apply Go migrations first: ./groovebox -migrate-only")
    jobs = sqlite3.connect(cache / "jobs.sqlite3", timeout=5)
    jobs.execute("CREATE TABLE IF NOT EXISTS jobs (key TEXT PRIMARY KEY, result TEXT NOT NULL)")
    stats = Counter()
    records = []
    if args.mode == "tracklists":
        rows = db.execute("""SELECT rv.*,a.artist FROM release_versions rv JOIN albums a ON a.id=rv.album_id
            WHERE rv.source='collection' AND rv.discogs_release_id IS NOT NULL ORDER BY a.artist,rv.id""").fetchall()
    elif args.mode == "local":
        rows = db.execute("""SELECT f.*,t.bpm_source,t.bpm FROM audio_files f JOIN tracks t ON t.id=f.track_id
            WHERE f.kind='track' ORDER BY f.track_id,f.relpath""").fetchall()
    else:
        rows = db.execute("""SELECT DISTINCT t.* FROM tracks t JOIN albums a ON a.id=t.album_id
            LEFT JOIN release_versions rv ON rv.id=t.release_id
            WHERE a.in_collection=1 AND (rv.source='collection' OR EXISTS
                (SELECT 1 FROM audio_files f WHERE f.track_id=t.id AND f.kind='track'))
            ORDER BY a.artist,t.track_number,t.title""").fetchall()
    if args.sample_dance:
        album_ids = {r[0] for r in db.execute("SELECT id,artist FROM albums") if r[1] in DANCE_ARTISTS}
        rows = [r for r in rows if r["album_id"] in album_ids]
    if args.mode == "acousticbrainz" and not args.probe:
        rows = [r for r in rows if not r["bpm_source"] or (args.refresh and r["bpm_source"] == "external:acousticbrainz")]
    if args.mode == "acousticbrainz" and not args.refresh:
        # Apply all completed outcomes before an unavailable provider can stop
        # the batch. Otherwise a cached success later in album order can starve.
        rows.sort(key=lambda r: jobs.execute("SELECT 1 FROM jobs WHERE key=?", (external_job_key(r),)).fetchone() is None)
    print(json.dumps({"stage": args.mode, "eligible": len(rows), "limit": args.limit,
                      "max_seconds": args.max_seconds, "probe": args.probe}), flush=True)
    seen_tracks = set()
    consecutive_errors = 0
    try:
        for row in rows:
            budget.check()
            if stats["attempted"] >= args.limit:
                break
            track_id = row["track_id"] if args.mode == "local" else row["id"]
            if args.mode == "local":
                if track_id in seen_tracks:
                    stats["duplicate-file"] += 1; continue
                seen_tracks.add(track_id)
            if args.mode != "tracklists" and row["bpm_source"] == "manual":
                stats["manual-protected"] += 1; continue
            if args.mode == "local" and row["bpm_source"] == "local:librosa" and not args.refresh:
                stats["already-local"] += 1; continue
            stats["processed"] += 1
            try:
                io_before = sum(http.requests.values())
                analyzed = False
                if args.mode == "tracklists":
                    stats["attempted"] += 1
                    data = http.get(f"https://api.discogs.com/releases/{row['discogs_release_id']}")
                    if data.get("id") != row["discogs_release_id"]:
                        result = {"status": "release-unavailable"}
                    else:
                        n = import_tracklist(db, row, data)
                        stats["tracks-inserted"] += n
                        result = {"status": "ok", "tracks_inserted": n, "tracklist_size": len(tracklist_rows(data))}
                else:
                    if args.mode == "local":
                        path = (Path(args.music_root) / row["relpath"]).resolve()
                        path.relative_to(Path(args.music_root).resolve())
                        stat = path.stat()
                        identity = [ALGORITHM, str(path), stat.st_size, stat.st_mtime_ns]
                    key = (hashlib.sha256(json.dumps(identity).encode()).hexdigest()
                           if args.mode == "local" else external_job_key(row))
                    cached = None if args.refresh else jobs.execute("SELECT result FROM jobs WHERE key=?", (key,)).fetchone()
                    if cached:
                        result = json.loads(cached[0]); stats["cached"] += 1
                    else:
                        stats["attempted"] += 1
                        if args.mode == "local":
                            budget.check(95)
                            analyzed = True
                            env = dict(os.environ, OMP_NUM_THREADS="1", OPENBLAS_NUM_THREADS="1", NUMBA_NUM_THREADS="1", MKL_NUM_THREADS="1")
                            output = subprocess.check_output([str(Path(args.python).absolute()), str(Path(__file__).resolve()),
                                "--analyze", str(path)], env=env, timeout=90, stderr=subprocess.PIPE)
                            result = json.loads(output)
                            result.update({"file_id": row["id"], "relpath": row["relpath"],
                                           "size_bytes": stat.st_size, "mtime_ns": stat.st_mtime_ns})
                        else:
                            result = external_result(row, http)
                        jobs.execute("INSERT OR REPLACE INTO jobs VALUES (?,?)", (key, json.dumps(result))); jobs.commit()
                    if result["status"] == "ok" and not args.probe:
                        source = "local:librosa" if args.mode == "local" else "external:acousticbrainz"
                        stats["written"] += write_bpm(db, track_id, result, source)
                if analyzed or sum(http.requests.values()) > io_before:
                    consecutive_errors = 0
                stats[result["status"]] += 1
                record = {"id": track_id, "title": row["title"] if "title" in row.keys() else row["relpath"], **result}
                records.append(record)
                print(json.dumps({"progress": stats["processed"], **record}, ensure_ascii=False), flush=True)
            except (OSError, ValueError, subprocess.SubprocessError) as exc:
                # A retryable file/network failure isn't marked complete.
                stats["errors"] += 1
                consecutive_errors += 1
                failure = {"id": track_id, "status": "error", "error": type(exc).__name__, "detail": (exc.stderr.decode(errors="replace")[-1200:] if isinstance(exc, subprocess.CalledProcessError) and exc.stderr else str(exc))}
                records.append(failure)
                print(json.dumps(failure), flush=True)
                if consecutive_errors >= 3:
                    raise RuntimeError("Three consecutive failures; batch stopped. Rerun later to resume.")
    except (TimeoutError, RuntimeError) as exc:
        stats["batch-stopped"] += 1
        print(str(exc), flush=True)
    report = {"mode": args.mode, "probe": args.probe, "sample_dance": args.sample_dance,
              "date": datetime.now(timezone.utc).isoformat(), "eligible": len(rows),
              "stats": dict(stats), "requests": dict(http.requests), "records": records}
    print(json.dumps({"summary": report["stats"], "requests": report["requests"]}), flush=True)
    if args.report:
        Path(args.report).write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    db.close(); jobs.close()
    return 1 if stats["errors"] or stats["batch-stopped"] else 0


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("mode", nargs="?", choices=["local", "tracklists", "acousticbrainz"])
    p.add_argument("--analyze", help=argparse.SUPPRESS)
    p.add_argument("--db", default=str(ROOT / "music.db"))
    p.add_argument("--cache", default=str(ROOT / ".bpm-cache"))
    p.add_argument("--music-root", default=os.environ.get("GROOVEBOX_MUSIC_ROOT", "/home/me/syncthing/archive/music"))
    p.add_argument("--python", default=str(ROOT / ".venv-bpm/bin/python"))
    p.add_argument("--limit", type=int, default=100)
    p.add_argument("--max-seconds", type=int, default=600)
    p.add_argument("--refresh", action="store_true", help="Retry cached outcomes; manual values remain protected")
    p.add_argument("--sample-dance", action="store_true")
    p.add_argument("--probe", action="store_true", help="External coverage research without BPM writes")
    p.add_argument("--report")
    args = p.parse_args()
    if args.analyze:
        print(json.dumps(analyze(args.analyze))); return 0
    if not args.mode or args.limit < 1 or not 30 <= args.max_seconds <= 3600:
        p.error("mode required; limit must be positive and max-seconds between 30 and 3600")
    if args.probe and args.mode != "acousticbrainz":
        p.error("--probe applies only to acousticbrainz metadata")
    return run(args)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (RuntimeError, sqlite3.Error) as exc:
        print(str(exc), file=sys.stderr); sys.exit(1)
