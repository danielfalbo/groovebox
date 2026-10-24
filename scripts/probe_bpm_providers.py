#!/usr/bin/env python3
"""Read-only comparison on actual owned dance pressings; no audio requests.

Indexed pages are discovery evidence, never automatic BPM authority. In particular
SongBPM/Tunebat forbid automated collection, so this tool does not fetch them.
"""
import argparse
from collections import Counter
import json
from pathlib import Path
import re
import sqlite3
import subprocess
import time
from urllib.parse import urlsplit
from enrich_bpm import Budget, HTTP, DANCE_ARTISTS, norm, query_url, duration_close, external_result


def deezer_candidates(track, search):
    return [r for r in search.get('data', [])
            if norm(r.get('title')) == norm(track['title'])
            and norm(r.get('artist', {}).get('name')) == norm(track['artist'])
            and (not track['duration_ms'] or duration_close(track['duration_ms'], r.get('duration', 0)*1000))]


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--db', default='music.db')
    p.add_argument('--report', default='docs/bpm-provider-sample.json')
    p.add_argument('--max-seconds', type=int, default=300)
    args = p.parse_args()
    budget = Budget(args.max_seconds)
    http = HTTP(Path('.bpm-cache/http'), budget)
    db = sqlite3.connect(f'file:{Path(args.db).resolve()}?mode=ro', uri=True)
    db.row_factory = sqlite3.Row
    rows = db.execute("""SELECT t.*,rv.discogs_release_id,a.title AS album_title FROM tracks t
        JOIN albums a ON a.id=t.album_id JOIN release_versions rv ON rv.id=t.release_id WHERE rv.source='collection'
        AND rv.has_vinyl=1 ORDER BY rv.discogs_release_id,t.track_number,t.id""").fetchall()
    sample = []
    for artist in DANCE_ARTISTS:
        sample.extend([dict(r) for r in rows if norm(r['artist']) == norm(artist)][:2])
    records = []
    stopped = set()
    for t in sample:
        try:
            budget.check(25)
        except RuntimeError:
            break
        record = {k:t[k] for k in ('id','artist','title','discogs_release_id','track_number','duration_ms')}
        # Search engine results only; no restricted provider page requests.
        query = f"{t['artist']} {t['title']} BPM"
        cache = Path('.bpm-cache/research');cache.mkdir(exist_ok=True)
        cached_index = cache/(t['id']+'-index.txt')
        if cached_index.exists():
            raw = cached_index.read_text()
        else:
            try:
                result = subprocess.run(['/home/me/.pi/agent/bin/web', query], capture_output=True,
                                        text=True, timeout=20)
                raw = result.stdout
            except subprocess.TimeoutExpired:
                raw = 'search timed out'
            cached_index.write_text(raw)
        record['index_query'] = query
        record['index_urls'] = re.findall(r'^  (https?://\S+)', raw, re.M)
        record['index_status'] = 'discovery-only; title/version/duration must be verified independently'
        if 'deezer' not in stopped:
            try:
                url = query_url('https://api.deezer.com/search', q=f'{t["artist"]} {t["title"]}',limit=25)
                data = http.get(url)
                if data.get('error'):
                    raise RuntimeError('Deezer API error: ' + json.dumps(data['error']))
                candidates = deezer_candidates(t, data)
                record['deezer_search_results'] = len(data.get('data',[]))
                record['deezer_candidates'] = []
                for r in candidates[:3]:
                    detail = http.get(f'https://api.deezer.com/track/{r["id"]}')
                    record['deezer_candidates'].append({k:detail.get(k) for k in ('id','title','duration','bpm','isrc','link')})
                record['deezer_status'] = ('nonzero-bpm-needs-identity-review' if any(
                    r.get('bpm',0)>0 for r in record['deezer_candidates']) else 'no-exact-nonzero-bpm')
            except (OSError,RuntimeError) as exc:
                record['deezer_status'] = str(exc)
                if isinstance(exc,RuntimeError):stopped.add('deezer')
        if 'musicbrainz' not in stopped:
            try:
                record['acousticbrainz'] = external_result(t,http)
            except (OSError,RuntimeError) as exc:
                record['acousticbrainz'] = {'status':'error','detail':str(exc)}
                if isinstance(exc,RuntimeError):stopped.add('musicbrainz')
        records.append(record)
        print(json.dumps(record,ensure_ascii=False),flush=True)
        time.sleep(1.1)
    counts = Counter(urlsplit(u).netloc for r in records for u in r['index_urls'])
    report = {'date':'2026-10-24','selection':'First two owned-vinyl slots per DANCE_ARTISTS artist, ordered by release ID and position; no replacement of failed queries',
              'sample_size':len(sample),'completed':len(records),'index_result_domains':dict(counts),
              'requests':dict(http.requests),'records':records}
    Path(args.report).write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')


if __name__ == '__main__':
    main()
