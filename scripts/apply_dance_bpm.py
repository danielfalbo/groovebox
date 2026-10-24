#!/usr/bin/env python3
"""Apply finite, individually reviewed pressing metadata; never fetch a provider.

The evidence file records original store titles, identifiers and explicit review
of title/context differences. This is not a fuzzy matcher or a site scraper.
"""
import argparse
import json
import math
from pathlib import Path
import sqlite3
from urllib.parse import urlsplit

from enrich_bpm import write_bpm


IDENTITY = ('id', 'release_id', 'title', 'artist', 'track_number', 'duration_ms')


def matches(track, entry):
    """Fail closed if any reviewed local identity changed, including duration."""
    expected = entry.get('expected', {})
    bpm = entry.get('bpm')
    return (all(k in expected and track[k] == expected[k] for k in IDENTITY)
            and track['discogs_release_id'] == entry.get('discogs_release_id')
            and track['source'] == 'collection' and track['has_vinyl'] == 1
            and track['bpm'] is None and track['bpm_source'] != 'manual'
            and not (track['bpm_source'] or '').startswith('local:')
            and type(bpm) in (int, float) and math.isfinite(bpm) and 20 <= bpm <= 400
            and urlsplit(entry.get('url', '')).scheme == 'https'
            and urlsplit(entry.get('url', '')).netloc == 'www.decks.de'
            and bool(entry.get('source_title')) and bool(entry.get('catalog_number'))
            and bool(entry.get('source_artist')) and bool(entry.get('source_release_title'))
            and bool(entry.get('identity_review')) and bool(entry.get('retrieved_at')))


def apply_entry(db, entry, probe=False):
    # Lock only during identity validation + the guarded update. No network or
    # analysis occurs inside this short transaction. An interrupted rerun skips
    # previously committed rows; concurrent manual edits always win.
    db.execute('BEGIN IMMEDIATE')
    try:
        track = db.execute('''SELECT t.*,rv.discogs_release_id,rv.source,rv.has_vinyl
            FROM tracks t JOIN release_versions rv ON rv.id=t.release_id
            WHERE t.id=?''', (entry['expected']['id'],)).fetchone()
        if track is None or not matches(track, entry):
            db.rollback()
            return 0
        if probe:
            db.rollback()
            return 1
        result = dict(entry, status='ok', provider='decks.de',
                      access='finite review of public product metadata; no audio or account',
                      confidence='pressing/mix identity reviewed; store tempo estimate, not a verified beat grid')
        return write_bpm(db, track['id'], result, 'external:decks', only_unknown=True)
    except BaseException:
        db.rollback()
        raise


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--db', default='music.db')
    p.add_argument('--evidence', default='docs/bpm-dance-reviewed.json')
    p.add_argument('--report', default='.bpm-cache/dance-import.json')
    p.add_argument('--probe', action='store_true')
    args = p.parse_args()
    data = json.loads(Path(args.evidence).read_text())
    db = sqlite3.connect(f'file:{Path(args.db).resolve()}?mode=rw', uri=True, timeout=5)
    db.row_factory = sqlite3.Row
    records = []
    for entry in data['entries']:
        accepted = apply_entry(db, entry, args.probe)
        records.append({'id': entry['expected']['id'], 'title': entry['expected']['title'],
                        'bpm': entry['bpm'], 'accepted': accepted})
    report = dict(probe=args.probe, reviewed=len(records), accepted=sum(r['accepted'] for r in records),
                  records=records)
    Path(args.report).write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print(json.dumps({k:v for k,v in report.items() if k != 'records'}))
    db.close()


if __name__ == '__main__':
    main()
