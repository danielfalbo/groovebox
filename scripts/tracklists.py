#!/usr/bin/env python3
"""Audit/reconcile every owned Discogs release, retaining curated recordings.

Only the official release tree defines completeness. No title words, versions,
artist credits or Discogs positions are discarded. Network is outside DB locks.
"""
import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import sqlite3
import sys
import time
import unicodedata
import uuid

from enrich_bpm import HTTP, Budget

ROOT = Path(__file__).resolve().parent.parent
VERSION = 'discogs-tracklists-v1'


def now():
    return datetime.now(timezone.utc).isoformat(timespec='seconds')


def exact(text):
    # Only case/whitespace/Unicode equivalence. Keep punctuation and mix words.
    return ' '.join(unicodedata.normalize('NFC', text or '').casefold().split())


def artist_credit(artists):
    result = ''
    for i, a in enumerate(artists):
        result += a.get('anv') or a.get('name', '')
        if i < len(artists)-1:
            join = a.get('join', '')
            result += (join + ' ') if join in (',', ';') else (' ' + join + ' ' if join else ', ')
    return result


def duration_ms(value):
    if not value:
        return None
    if not re.fullmatch(r'\d+(?::[0-5]\d){1,2}', value):
        raise ValueError('invalid official duration: ' + value)
    total = 0
    for part in value.split(':'):
        total = total * 60 + int(part)
    return total * 1000


def flatten(data, fallback=''):
    """Preserve every node in source order; groups aren't extra recordings.

    Leaf tracks, including untitled-position tracks, are never silently skipped.
    Unpositioned leaves get path identity and remain explicitly flagged. A
    medley's parent and components remain visible without inventing a BPM for
    the combined parent. All original fields survive in raw_json/metadata_json.
    """
    entries, issues = [], []
    if not isinstance(data.get('tracklist'), list) or not data['tracklist']:
        raise ValueError('official release has no usable tracklist')

    def walk(nodes, parent='', depth=0, artists=None, context=()):
        if depth > 20 or len(entries) > 10000:
            raise ValueError('tracklist hierarchy exceeds safety bound')
        heading_artists = artists
        section = ()
        for i, node in enumerate(nodes):
            if not isinstance(node, dict):
                raise ValueError('invalid official tracklist entry')
            path = f'{parent}.{i+1}' if parent else str(i+1)
            kind = node.get('type_', 'track')
            if kind not in ('heading', 'index', 'track'):
                issues.append({'path': path, 'reason': 'unknown-entry-type', 'type': kind})
            children = node.get('sub_tracks') or []
            if not isinstance(children, list):
                raise ValueError('invalid sub_tracks')
            credit = node.get('artists') or heading_artists or []
            title = node.get('title', '')
            position = node.get('position', '')
            leaf = kind == 'track' and not children
            if not title.strip():
                issues.append({'path': path, 'reason': 'missing-title'})
            if leaf and not position.strip():
                issues.append({'path': path, 'reason': 'unpositioned-track'})
            if kind == 'index' and not children:
                issues.append({'path': path, 'reason': 'empty-index'})
            entries.append(dict(path=path, ordinal=len(entries), parent_path=parent,
                                depth=depth, entry_type=kind, position=position,
                                title=title, artist=artist_credit(credit) or fallback,
                                duration_ms=duration_ms(node.get('duration', '')),
                                is_track=int(leaf), context=context + section,
                                raw_json=json.dumps(node, ensure_ascii=False)))
            if children:
                walk(children, path, depth+1, credit, context + section + (title,))
            if kind == 'heading':
                # A heading can supply artists for subsequent siblings.
                heading_artists = node.get('artists') or artists
                section = (title,)
    walk(data['tracklist'], artists=data.get('artists', []))
    if not any(e['is_track'] for e in entries):
        issues.append({'reason': 'no-track-leaves'})
    slots = Counter((e['position'], e['context']) for e in entries if e['is_track'] and e['position'])
    issues.extend({'reason': 'duplicate-official-position', 'position': p, 'context': c, 'count': n}
                  for (p,c), n in slots.items() if n > 1)
    return entries, issues


def install_schema(db):
    db.executescript((ROOT / 'tracklists.sql').read_text())


def index_track(db, tid, title, artist):
    if db.execute("SELECT 1 FROM search_fts WHERE target_type='track' AND target_id=?", (tid,)).fetchone():
        # Retain any existing ISRC/catalog search fields too.
        db.execute("UPDATE search_fts SET title=?,artist=? WHERE target_type='track' AND target_id=?", (title,artist,tid))
    else:
        db.execute("INSERT INTO search_fts(target_type,target_id,title,artist) VALUES('track',?,?,?)", (tid,title,artist))


def contextual_recording(db, track):
    """BPM lookup includes index/medley titles, never a generic 'Part 1'."""
    result = dict(track)
    if not db.execute("SELECT 1 FROM sqlite_master WHERE name='discogs_tracklist_entries'").fetchone():
        return result
    entries = db.execute('SELECT release_id,parent_path FROM discogs_tracklist_entries WHERE track_id=?', (track['id'],)).fetchall()
    if len(entries) != 1:
        return result
    release_id, parent = entries[0]
    titles = [track['title']]
    while parent:
        row = db.execute('SELECT title,parent_path,entry_type FROM discogs_tracklist_entries WHERE release_id=? AND path=?', (release_id,parent)).fetchone()
        if not row:
            break
        title,parent,kind = row
        if kind in ('index','track'):
            titles.insert(0,title)
    result['title'] = ' / '.join(titles)
    return result


def legacy_artist_equal(old, new, data, entry):
    if exact(old) == exact(new):
        return True
    # Recognise only the previous importer's documented rendering of these
    # same official artist objects. Never guess arbitrary aliases or split '&'.
    node = json.loads(entry['raw_json'])
    artists = node.get('artists') or data.get('artists', [])
    legacy = ', '.join(re.sub(r' \(\d+\)$', '', a.get('name', '')) for a in artists)
    return exact(old) == exact(legacy)


def reconcile(db, release, data, fetched_at=None):
    if data.get('id') != release['discogs_release_id']:
        raise ValueError('official release ID does not match owned pressing')
    entries, issues = flatten(data, release['artist'])
    payload = json.dumps(data, ensure_ascii=False, sort_keys=True)
    report = dict(inserted=0, updated=0, expected_tracks=sum(e['is_track'] for e in entries),
                  mapped_tracks=0, conflicts=issues, retained_unmapped=[])
    db.execute('BEGIN IMMEDIATE')
    try:
        # Revalidate ownership under the write lock; do not race a sync/relink.
        owned = db.execute("SELECT album_id,discogs_release_id FROM release_versions WHERE id=? AND source='collection'",
                           (release['id'],)).fetchone()
        if owned is None or tuple(owned) != (release['album_id'], release['discogs_release_id']):
            raise ValueError('owned release identity changed during fetch')
        cols = [x[1] for x in db.execute('PRAGMA table_info(tracks)')]
        rows = [dict(zip(cols, row)) for row in db.execute('SELECT * FROM tracks WHERE release_id=?', (release['id'],))]
        report['existing_tracks_before'] = len(rows)
        report['official_nodes'] = len(entries)
        report['official_missing_durations'] = sum(e['is_track'] and e['duration_ms'] is None for e in entries)
        report['updated_fields'] = {}
        prior = {r[0]: r[1] for r in db.execute('SELECT path,track_id FROM discogs_tracklist_entries WHERE release_id=?', (release['id'],))}
        old_metadata = db.execute('SELECT metadata_json FROM discogs_tracklists WHERE release_id=?', (release['id'],)).fetchone()
        old_entries = flatten(json.loads(old_metadata[0]), release['artist'])[0] if old_metadata else []
        old_scopes = {e['path']:e['context'] for e in old_entries}
        used = set()
        positions = Counter((e['position'], e['context']) for e in entries if e['is_track'])
        raw_positions = Counter(e['position'] for e in entries if e['is_track'])
        identities = Counter((exact(e['title']), exact(e['artist'])) for e in entries if e['is_track'])
        for e in entries:
            e['track_id'] = None
            if not e['is_track']:
                continue
            matching = [r for r in rows if r['id'] not in used and exact(r['title']) == exact(e['title'])
                        and legacy_artist_equal(r.get('artist'), e['artist'], data, e)]
            same_slot = [r for r in matching if exact(r.get('track_number')) == exact(e['position'])]
            chosen = None
            previous = next((r for r in same_slot if r['id'] == prior.get(e['path']) and old_scopes.get(e['path']) == e['context']), None)
            if previous:
                chosen = previous
            elif e['position'] and positions[(e['position'],e['context'])] == 1 and len(same_slot) == 1:
                chosen = same_slot[0]
            elif len(same_slot) > 1:
                report['conflicts'].append(dict(path=e['path'], reason='duplicate-existing-slot', ids=[r['id'] for r in same_slot]))
                continue
            elif not e['position'] and prior.get(e['path']):
                chosen = next((r for r in matching if r['id'] == prior[e['path']]), None)
            elif len(matching) == 1 and identities[(exact(e['title']), exact(e['artist']))] == 1:
                # Correct a wrong position only on this exact pressing, unique
                # full recording identity. Album/catalog overlap isn't evidence.
                chosen = matching[0]
            occupied = [r for r in rows if r['id'] not in used and e['position'] and
                        exact(r.get('track_number')) == exact(e['position'])]
            if chosen and raw_positions[e['position']] == 1 and any(r['id'] != chosen['id'] for r in occupied):
                report['conflicts'].append(dict(path=e['path'], reason='position-correction-collision',
                    ids=[r['id'] for r in occupied]))
                continue
            if e['position'] and positions[(e['position'],e['context'])] > 1:
                report['conflicts'].append(dict(path=e['path'], reason='ambiguous-official-slot'))
                continue
            if chosen is None and occupied:
                report['conflicts'].append(dict(path=e['path'], reason='slot-recording-conflict',
                    position=e['position'], official_title=e['title'], existing=[dict(id=r['id'], title=r['title'], artist=r.get('artist')) for r in occupied]))
                continue
            if chosen is None and matching:
                report['conflicts'].append(dict(path=e['path'], reason='ambiguous-recording', ids=[r['id'] for r in matching]))
                continue
            if chosen:
                tid = chosen['id']
                if tid in prior.values():
                    # A changed index/medley may designate a different version
                    # even when the leaf is still called 'Part 1'.
                    old_context = contextual_recording(db, chosen)['title']
                    old_path = next(path for path,track_id in prior.items() if track_id==tid)
                    ancestors=[];parent=e['parent_path']
                    while parent:
                        node=next(x for x in entries if x['path']==parent)
                        if node['entry_type'] in ('index','track'):ancestors.insert(0,node['title'])
                        parent=node['parent_path']
                    if exact(old_context) != exact(' / '.join(ancestors+[e['title']])) or old_scopes.get(old_path) != e['context']:
                        report['conflicts'].append(dict(path=e['path'],reason='recording-context-changed',track_id=tid))
                        continue
                # Metadata-only updates. No BPM/provenance/ISRC/streaming IDs,
                # audio links, playlist membership or canonical association writes.
                # Unknown official duration must not erase a measured duration.
                dur = e['duration_ms'] if e['duration_ms'] is not None else chosen.get('duration_ms')
                if e['duration_ms'] and chosen.get('duration_ms') and abs(dur-chosen['duration_ms']) > max(3000, dur*.01) and (
                        chosen.get('bpm') is not None or chosen.get('bpm_source') or chosen.get('isrc') or chosen.get('spotify_id') or chosen.get('tidal_id')
                        or db.execute('SELECT 1 FROM audio_files WHERE track_id=?', (tid,)).fetchone()
                        or db.execute('SELECT 1 FROM playlist_tracks WHERE track_id=?', (tid,)).fetchone()):
                    report['conflicts'].append(dict(path=e['path'], reason='protected-duration-disagreement',
                        track_id=tid, existing_ms=chosen['duration_ms'], official_ms=dur))
                    dur = chosen['duration_ms']
                values = (e['title'], e['artist'], e['position'], dur)
                if values != (chosen['title'], chosen.get('artist'), chosen.get('track_number'), chosen.get('duration_ms')):
                    for k,v in zip(('title','artist','track_number','duration_ms'),values):
                        if v != chosen.get(k):report['updated_fields'][k]=report['updated_fields'].get(k,0)+1
                    db.execute('UPDATE tracks SET title=?,artist=?,track_number=?,duration_ms=? WHERE id=?', (*values, tid))
                    index_track(db,tid,e['title'],e['artist'])
                    report['updated'] += 1
            else:
                # Stable by pressing and official slot. Path is used only when
                # Discogs provides no physical position, never silently invented.
                key = e['position'] or 'path:' + e['path']
                if e['position'] and raw_positions[e['position']] > 1:
                    key = json.dumps([e['position'],e['context']],ensure_ascii=False)
                tid = str(uuid.uuid5(uuid.NAMESPACE_URL, f"groovebox:discogs-slot:{release['discogs_release_id']}:{key}"))
                if db.execute('SELECT 1 FROM tracks WHERE id=?', (tid,)).fetchone():
                    report['conflicts'].append(dict(path=e['path'], reason='stable-id-conflict', track_id=tid))
                    continue
                db.execute('INSERT INTO tracks(id,album_id,release_id,title,artist,track_number,duration_ms) VALUES(?,?,?,?,?,?,?)',
                           (tid,release['album_id'],release['id'],e['title'],e['artist'],e['position'],e['duration_ms']))
                index_track(db,tid,e['title'],e['artist'])
                report['inserted'] += 1
            used.add(tid)
            e['track_id'] = tid
            report['mapped_tracks'] += 1
        report['retained_unmapped'] = [dict(id=r['id'], position=r.get('track_number'), title=r['title']) for r in rows if r['id'] not in used]
        if report['retained_unmapped']:
            report['conflicts'].append(dict(reason='retained-unmapped-owned-tracks', count=len(report['retained_unmapped'])))
        report['status'] = 'complete' if report['expected_tracks'] and not report['conflicts'] and report['mapped_tracks'] == report['expected_tracks'] else 'conflict'
        report['checked_at'] = now()
        report['fetched_at'] = fetched_at
        db.execute('DELETE FROM discogs_tracklist_entries WHERE release_id=?', (release['id'],))
        for e in entries:
            db.execute('''INSERT INTO discogs_tracklist_entries
                (release_id,path,ordinal,parent_path,depth,entry_type,position,title,artist,duration_ms,is_track,track_id,raw_json)
                VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)''', (release['id'], *(e[k] for k in ('path','ordinal','parent_path','depth','entry_type','position','title','artist','duration_ms','is_track','track_id','raw_json'))))
        db.execute('''INSERT INTO discogs_tracklists(release_id,status,checked_at,fetched_at,source_sha256,expected_tracks,mapped_tracks,report_json,metadata_json)
            VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(release_id) DO UPDATE SET
            status=excluded.status,checked_at=excluded.checked_at,fetched_at=excluded.fetched_at,source_sha256=excluded.source_sha256,
            expected_tracks=excluded.expected_tracks,mapped_tracks=excluded.mapped_tracks,report_json=excluded.report_json,metadata_json=excluded.metadata_json''',
            (release['id'],report['status'],report['checked_at'],fetched_at,hashlib.sha256(payload.encode()).hexdigest(),report['expected_tracks'],report['mapped_tracks'],json.dumps(report,ensure_ascii=False),payload))
        db.commit()
        return report
    except BaseException:
        db.rollback()
        raise


def read_token():
    token = os.getenv('DISCOGS_TOKEN', '')
    for p in (ROOT / '.env', ROOT.parent / 'discogs-albums/.env'):
        if token:
            break
        if p.exists():
            for line in p.read_text().splitlines():
                if line.strip().startswith('DISCOGS_TOKEN='):
                    token = line.strip().split('=',1)[1].strip('\"\'')
                    break
    return token


def current_inventory(http):
    # Collection is private: reuse the existing read credential, never write or
    # change authentication. Release metadata still uses anonymous requests.
    import urllib.request
    token = read_token()
    if not token:
        raise ValueError('cannot verify private collection freshness: no existing token')
    def request(url):
        http.budget.check(20)
        # The same file lock/rate path as anonymous metadata; no token on disk.
        import fcntl
        with (http.cache / 'rate-api.discogs.com').open('a+') as lock:
            while True:
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB); break
                except BlockingIOError:
                    http.budget.check(20); time.sleep(.2)
            lock.seek(0); last = float(lock.read() or 0)
            time.sleep(max(0, 2.5-(time.time()-last)))
            lock.seek(0);lock.truncate();lock.write(str(time.time()));lock.flush()
            req = urllib.request.Request(url,headers={'User-Agent':'Groovebox/1.0','Authorization':'Discogs token='+token})
            http.requests['api.discogs.com:collection-read'] += 1
            with urllib.request.urlopen(req,timeout=15) as response:
                raw = response.read(3_000_001)
                if len(raw)>3_000_000:raise ValueError('collection response too large')
                return json.loads(raw)
    def get(url):
        import urllib.error
        for attempt in range(3):
            try:
                return request(url)
            except urllib.error.HTTPError as exc:
                if exc.code < 500 or attempt == 2:
                    raise
            except OSError:
                if attempt == 2:raise
            http.budget.check(20)
            time.sleep(2**attempt)
    user = get('https://api.discogs.com/oauth/identity')['username']
    items, page, pages, expected = [], 1, 1, None
    while page <= pages:
        d = get(f'https://api.discogs.com/users/{user}/collection/folders/0/releases?per_page=100&page={page}')
        pages = d['pagination']['pages']
        if pages > 100:raise ValueError('collection pagination exceeds safety bound')
        if expected is not None and expected != d['pagination']['items']:
            raise ValueError('collection changed during pagination; retry')
        expected=d['pagination']['items'];items.extend(d['releases']);page+=1
    if len(items) != expected:raise ValueError('incomplete collection pagination')
    return dict(verified_at=now(),items=expected,releases=items)


def restore_owned_membership(db, inventory):
    """Restore collection priority if the same release is also on wantlist.

    No deletion/demotion: stale owned rows and missing release IDs are blockers.
    New release summaries are inserted with exact collection evidence. Tracklists
    still require the full release response and are audited separately.
    """
    current = {r['id'] for r in inventory['releases']}
    restored, missing, added = [], [], []
    summaries = {r['id']: r for r in inventory['releases']}
    with db:
        for rid in sorted(current):
            r=db.execute('SELECT id,source,album_id FROM release_versions WHERE discogs_release_id=?',(rid,)).fetchone()
            if not r:
                info=summaries[rid].get('basic_information', {})
                if info.get('id') != rid or not info.get('title') or not info.get('artists'):
                    missing.append(rid);continue
                artist=artist_credit(info['artists'])
                album=db.execute('SELECT id FROM albums WHERE discogs_master_id=?',(info.get('master_id') or None,)).fetchone()
                if not album:
                    candidates=db.execute('SELECT id FROM albums WHERE LOWER(title)=LOWER(?) AND LOWER(artist)=LOWER(?)',(info['title'],artist)).fetchall()
                    if len(candidates)>1:missing.append(rid);continue
                    album=candidates[0] if candidates else None
                aid=album[0] if album else str(uuid.uuid5(uuid.NAMESPACE_URL,f'groovebox:discogs-album:{rid}'))
                formats=info.get('formats',[])
                desc='; '.join(' '.join([f.get('qty',''),f.get('name',''),', '.join(f.get('descriptions',[]))]).strip() for f in formats)
                vinyl=int(any(f.get('name','').lower() in ('vinyl','flexi-disc') for f in formats))
                cover=info.get('cover_image') or info.get('thumb') or ''
                if not album:
                    db.execute('INSERT INTO albums(id,title,artist,release_year,discogs_master_id,cover_image_url,in_collection,has_vinyl,streaming_notes) VALUES(?,?,?,?,?,?,1,?,?)',
                        (aid,info['title'],artist,info.get('year'),info.get('master_id') or None,cover,vinyl,'Discogs collection'))
                    db.execute("INSERT INTO search_fts(target_type,target_id,title,artist) VALUES('album',?,?,?)",(aid,info['title'],artist))
                version=str(uuid.uuid5(uuid.NAMESPACE_URL,f'groovebox:discogs-release:{rid}'))
                label=(info.get('labels') or [{}])[0]
                db.execute("INSERT INTO release_versions(id,album_id,discogs_release_id,title,artist,label,catalog_number,release_year,cover_image_url,format_description,source,has_vinyl) VALUES(?,?,?,?,?,?,?,?,?,?,'collection',?)",
                    (version,aid,rid,info['title'],artist,label.get('name'),label.get('catno'),info.get('year'),cover,desc,vinyl))
                r=dict(id=version,source='collection',album_id=aid)
                added.append(rid)
            if r['source']!='collection':
                db.execute("UPDATE release_versions SET source='collection' WHERE id=?",(r['id'],))
                restored.append(rid)
            db.execute('UPDATE albums SET in_collection=1,has_vinyl=EXISTS(SELECT 1 FROM release_versions WHERE album_id=albums.id AND source=\'collection\' AND has_vinyl=1) WHERE id=?',(r['album_id'],))
            date=summaries[rid].get('date_added')
            if date:
                db.execute("UPDATE albums SET collection_added_at=? WHERE id=? AND (collection_added_at IS NULL OR collection_added_at<?)",(date,r['album_id'],date))
    owned={r[0] for r in db.execute("SELECT discogs_release_id FROM release_versions WHERE source='collection'")}
    return dict(items=inventory['items'],unique_releases=len(current),verified_at=inventory['verified_at'],
                restored_collection_source=restored,added_release_ids=added,missing_release_ids=missing,stale_owned_release_ids=sorted(owned-current,key=str))


def run(args):
    budget=Budget(args.max_seconds)
    def stop(*_):budget.deadline=0
    signal.signal(signal.SIGTERM,stop);signal.signal(signal.SIGINT,stop)
    cache=Path(args.cache);cache.mkdir(parents=True,exist_ok=True)
    http=HTTP(cache/'http',budget,args.refresh,args.cached_only)
    db=sqlite3.connect(f'file:{Path(args.db).resolve()}?mode=rw',uri=True,timeout=5)
    db.row_factory=sqlite3.Row;db.execute('PRAGMA foreign_keys=ON')
    install_schema(db)
    report=dict(version=VERSION,started_at=now(),inventory=None,inventory_error=None,records=[])
    if args.verify_collection:
        try:
            inventory=current_inventory(http)
            report['inventory']=restore_owned_membership(db,inventory)
            (cache/'collection-inventory.json').write_text(json.dumps(inventory,ensure_ascii=False))
        except Exception as exc:report['inventory_error']=str(exc)
    elif args.inventory:
        inventory=json.loads(Path(args.inventory).read_text())
        report['inventory']=restore_owned_membership(db,inventory)
    rows=db.execute("SELECT * FROM release_versions WHERE source='collection' ORDER BY artist,discogs_release_id,id").fetchall()
    consecutive_errors = 0
    for r in rows:
        record=dict(release_id=r['id'],discogs_release_id=r['discogs_release_id'],artist=r['artist'],title=r['title'])
        try:
            budget.check()
            if not r['discogs_release_id'] or r['discogs_release_id']<=0:
                raise ValueError('missing official release ID')
            url=f"https://api.discogs.com/releases/{r['discogs_release_id']}"
            data=http.get(url)
            dest=http.cache/(hashlib.sha256(url.encode()).hexdigest()+'.json')
            fetched_at=datetime.fromtimestamp(dest.stat().st_mtime,timezone.utc).isoformat(timespec='seconds') if dest.exists() else now()
            record.update(reconcile(db,r,data,fetched_at))
            consecutive_errors = 0
        except Exception as exc:
            record.update(status='error',error=str(exc))
            if isinstance(exc, RuntimeError) or 'HTTP 429' in str(exc) or 'HTTP 403' in str(exc) or 'HTTP 401' in str(exc):
                http.cached_only=True
                http.refresh=False
            if isinstance(exc, OSError):
                consecutive_errors += 1
                if consecutive_errors >= 3:
                    http.cached_only=True
                    http.refresh=False
            # Mark stale/error explicitly but retain the last good metadata tree.
            with db:
                db.execute("UPDATE discogs_tracklists SET status='error',report_json=? WHERE release_id=?",(json.dumps(record),r['id']))
        report['records'].append(record)
        print(json.dumps(record,ensure_ascii=False),flush=True)
    report['finished_at']=now();report['requests']=dict(http.requests)
    report['totals']=dict(Counter(r['status'] for r in report['records']))
    report['totals'].update(releases=len(rows),inserted=sum(r.get('inserted',0) for r in report['records']),
                            updated=sum(r.get('updated',0) for r in report['records']),
                            expected_tracks=sum(r.get('expected_tracks',0) for r in report['records']),
                            mapped_tracks=sum(r.get('mapped_tracks',0) for r in report['records']))
    # Old album flags alone cannot prove a particular owned pressing.
    report['collection_flags_without_owned_release']=[dict(r) for r in db.execute("""SELECT id,artist,title FROM albums a WHERE in_collection=1 AND NOT EXISTS
        (SELECT 1 FROM release_versions rv WHERE rv.album_id=a.id AND rv.source='collection')""")]
    inv=report['inventory']
    report['complete']=bool(inv and not report['inventory_error'] and not inv['missing_release_ids'] and not inv['stale_owned_release_ids']
                            and not report['collection_flags_without_owned_release'] and report['totals'].get('complete')==len(rows))
    dest=Path(args.report);dest.parent.mkdir(parents=True,exist_ok=True)
    tmp=dest.with_suffix('.tmp');tmp.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');tmp.replace(dest)
    print(json.dumps({k:v for k,v in report.items() if k not in ('records',)}),flush=True)
    db.close()
    return report


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--db',default='music.db');p.add_argument('--cache',default='.bpm-cache')
    p.add_argument('--max-seconds',type=int,default=1200)
    p.add_argument('--report',default='.bpm-cache/collection-tracklists.json')
    p.add_argument('--cached-only',action='store_true');p.add_argument('--refresh',action='store_true')
    group=p.add_mutually_exclusive_group()
    group.add_argument('--verify-collection',action='store_true');group.add_argument('--inventory')
    args=p.parse_args()
    if args.cached_only and args.verify_collection:p.error('cached-only cannot verify current collection')
    result=run(args)
    return 0 if result['complete'] else 2


if __name__=='__main__':
    sys.exit(main())
