#!/usr/bin/env python3
"""Apply reviewed public recording metadata or evidenced duplicate estimates.

No network access. Sources and exact album tracklists are reviewed separately;
this is not a scraper. Missing duration requires a full ordered album match.
"""
import argparse
from collections import Counter
import json
import re
from pathlib import Path
import sqlite3
from enrich_bpm import norm, duration_close, write_bpm


def reviewed_matches(owned, evidence, album):
    artists = {norm(a) for a in [evidence['artist'], *evidence.get('artist_aliases', [])]}
    if norm(album['title']) != norm(evidence['album']) or norm(album['artist']) not in artists:
        return []
    source = evidence['tracks']
    # A complete, ordered core album anchors missing-duration slots to the
    # recording rather than an arbitrary same-title single/edit/live performance.
    full = len(owned) >= 4 and len(source) >= len(owned) and all(
        norm(t['title']) == norm(s['title']) and norm(t['artist']) in artists
        for t,s in zip(owned, source))
    out = []
    for t in owned:
        candidates = [s for s in source if norm(t['title']) == norm(s['title'])
                      and norm(t['artist']) in artists]
        if len(candidates) != 1:
            continue
        s = candidates[0]
        if not isinstance(s.get('bpm'), (int,float)) or not 20 <= s['bpm'] <= 400 or not s.get('duration_ms'):
            continue
        if t['duration_ms']:
            if not duration_close(t['duration_ms'],s['duration_ms']):continue
        elif not full:
            continue
        out.append((t, {'status':'ok', 'bpm':s['bpm'], 'url':evidence['url'],
            'provider':'GetSongBPM', 'access':'public indexed metadata; reviewed, no authenticated API',
            'license':'CC BY 4.0', 'license_url':'https://creativecommons.org/licenses/by/4.0/',
            'retrieved_at':evidence['retrieved_at'], 'artist':evidence['artist'],
            'album':evidence['album'], 'recording_title':s['title'],
            'artist_alias_evidence':evidence.get('artist_alias_evidence'),
            'recording_duration_ms':s['duration_ms'],
            'match':'exact artist+album+full title; '+('duration corroborated' if t['duration_ms'] else 'complete ordered core tracklist'),
            'confidence':'recording metadata verified; provider tempo estimate, not a verified beat grid'}))
    return out


def duplicate_matches(target, known):
    # Restrict transfers to the same canonical album, full title/artist and
    # corroborating duration. Same album alone is never sufficient.
    matches = [s for s in known if target['album_id'] and target['album_id']==s['album_id']
               and norm(target['title'])==norm(s['title']) and norm(target['artist'])==norm(s['artist'])
               and duration_close(target['duration_ms'],s['duration_ms'])
               and not (s['bpm_source'] or '').endswith('recording-copy')]
    if not matches or max(s['bpm'] for s in matches)-min(s['bpm'] for s in matches)>1:
        return None
    # Manual tempos aren't propagated: an override may deliberately correct
    # only a particular pressing, not the whole recording.
    matches = [s for s in matches if s['bpm_source']!='manual']
    if not matches:return None
    s = sorted(matches, key=lambda s:(not (s['bpm_source'] or '').startswith('local:'),s['id']))[0]
    return {'status':'ok','bpm':s['bpm'],'source_track_id':s['id'],
            'source_bpm_source':s['bpm_source'],'source_evidence':json.loads(s['bpm_provenance'] or '{}'),
            'source_duration_ms':s['duration_ms'],'target_duration_ms':target['duration_ms'],
            'match':'same canonical album; exact full title+artist; duration agrees within max(3s,1%)',
            'confidence':'recording transfer based on metadata; retains original tempo-estimate limitations'}


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--db',default='music.db');p.add_argument('--evidence',default='docs/bpm-reviewed-metadata.json')
    p.add_argument('--report',default='.bpm-cache/reviewed-results.json');p.add_argument('--probe',action='store_true')
    args=p.parse_args()
    db=sqlite3.connect(f'file:{Path(args.db).resolve()}?mode=rw',uri=True,timeout=5);db.row_factory=sqlite3.Row
    evidence=json.loads(Path(args.evidence).read_text())
    records=[];stats=Counter()
    for e in evidence['albums']:
        releases=db.execute("""SELECT rv.*,a.title album_title,a.artist album_artist FROM release_versions rv
            JOIN albums a ON a.id=rv.album_id WHERE rv.source='collection' AND rv.discogs_release_id=?""",(e['discogs_release_id'],)).fetchall()
        for r in releases:
            owned=db.execute('SELECT * FROM tracks WHERE release_id=? ORDER BY track_number,id',(r['id'],)).fetchall()
            owned=sorted(owned,key=lambda t:tuple((0,int(p)) if p.isdigit() else (1,p.casefold())
                         for p in re.split(r'(\d+)',str(t['track_number'] or ''))))
            matches=reviewed_matches(owned,e,{'title':r['album_title'],'artist':r['album_artist']})
            stats['reviewed_matched']+=len(matches)
            for t,result in matches:
                if t['bpm'] is not None or t['bpm_source']=='manual':continue
                written=0 if args.probe else write_bpm(db,t['id'],result,'external:getsongbpm',only_unknown=True)
                stats['reviewed_written']+=written;records.append({'id':t['id'],'title':t['title'],**result,'written':written})
    known=db.execute('SELECT * FROM tracks WHERE bpm IS NOT NULL').fetchall()
    unknown=db.execute("""SELECT DISTINCT t.* FROM tracks t LEFT JOIN release_versions rv ON rv.id=t.release_id
        WHERE t.bpm IS NULL AND COALESCE(t.bpm_source,'')!='manual' AND (rv.source='collection' OR EXISTS
        (SELECT 1 FROM audio_files f WHERE f.track_id=t.id AND f.kind='track'))""").fetchall()
    for t in unknown:
        result=duplicate_matches(t,known)
        if not result:continue
        written=0 if args.probe else write_bpm(db,t['id'],result,'external:recording-copy',only_unknown=True)
        stats['duplicates_written']+=written;records.append({'id':t['id'],'title':t['title'],**result,'written':written})
    report={'stats':dict(stats),'probe':args.probe,'records':records}
    Path(args.report).write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps(report['stats']))

if __name__=='__main__':main()
