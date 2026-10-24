import json
from contextlib import redirect_stdout
from io import StringIO
import hashlib
import signal
import urllib.error
import tempfile
from types import SimpleNamespace
from unittest.mock import patch
from pathlib import Path
import sqlite3
import sys
import unittest
sys.path.insert(0, str(Path(__file__).parent))
from apply_reviewed_bpm import reviewed_matches, duplicate_matches
from enrich_bpm import external_result, import_tracklist, norm, recording_match, write_bpm, run, ALGORITHM, external_job_key, HTTP, Budget, CacheMiss


class FakeHTTP:
    def __init__(self, recordings, archive):
        self.recordings, self.archive, self.urls = recordings, archive, []

    def get(self, url):
        self.urls.append(url)
        return ({'count': len(self.recordings), 'recordings': self.recordings}
                if 'musicbrainz.org' in url else self.archive)


class BPMTests(unittest.TestCase):
    def setUp(self):
        self.track = dict(title='Song (Club Mix)', artist='Artist', duration_ms=360000, isrc=None)
        self.recording = dict(id='mbid', title='Song (Club Mix)', length=361000,
                              **{'artist-credit': [{'artist': {'name': 'Artist'}}]})
        self.archive = dict(rhythm={'bpm': 123.45}, metadata={
            'tags': {'title': ['Song (Club Mix)'], 'artist': ['Artist']},
            'audio_properties': {'length': 360.5}})

    def test_version_and_duration_must_match(self):
        self.assertTrue(recording_match(self.track, self.recording))
        for title in ['Song', 'Song (Radio Edit)', 'Song (Live)', 'Song (Club Mix 2)']:
            self.assertFalse(recording_match(self.track, dict(self.recording, title=title)))
        self.assertFalse(recording_match(self.track, dict(self.recording, length=300000)))
        self.assertFalse(recording_match(dict(self.track, duration_ms=0), self.recording))
        self.assertNotEqual(norm('Song (Club Mix)'), norm('Song (Radio Edit)'))

    def test_external_unique_and_archive_validation(self):
        h = FakeHTTP([self.recording], self.archive)
        self.assertEqual(external_result(self.track, h)['bpm'], 123.45)
        ambiguous = dict(self.recording, id='other')
        self.assertEqual(external_result(self.track, FakeHTTP([self.recording, ambiguous], self.archive))['status'], 'ambiguous-recording')
        self.archive['metadata']['tags']['title'] = ['Song (Radio Edit)']
        self.assertEqual(external_result(self.track, FakeHTTP([self.recording], self.archive))['status'], 'archive-version-mismatch')
        self.assertEqual(external_result(dict(self.track, duration_ms=0), h)['status'], 'archive-version-mismatch')

    def test_missing_duration_requires_unique_identity_and_archive_corroboration(self):
        track = dict(self.track, duration_ms=0)
        result = external_result(track, FakeHTTP([self.recording], self.archive))
        self.assertEqual(result['bpm'], 123.45)
        self.assertIn('MB/archive duration agrees', result['match'])
        self.assertEqual(external_result(track, FakeHTTP(
            [self.recording, dict(self.recording, id='other', length=300000)], self.archive))['status'],
            'ambiguous-recording')
        self.assertEqual(external_result(track, FakeHTTP(
            [dict(self.recording, length=None)], self.archive))['status'], 'archive-version-mismatch')
        self.archive['metadata']['audio_properties']['length'] = 250
        self.assertEqual(external_result(track, FakeHTTP([self.recording], self.archive))['status'],
                         'archive-version-mismatch')

    def test_recording_disambiguation_cannot_hide_an_edit(self):
        self.assertFalse(recording_match(self.track, dict(self.recording, disambiguation='radio edit')))
        self.assertTrue(recording_match(self.track, dict(self.recording, disambiguation='Club Mix')))
        self.assertFalse(recording_match(dict(self.track,title='Deliver Me'), dict(self.recording,title='Deliver Me',disambiguation='live')))

    def test_reviewed_album_requires_full_order_for_missing_duration(self):
        owned=[dict(id=str(i),title=f'Track {i}',artist='Artist',duration_ms=0) for i in range(4)]
        evidence=dict(album='Album',artist='Artist',url='https://getsongbpm.com/album/example',retrieved_at='2026-10-24',
                      tracks=[dict(title=t['title'],bpm=120,duration_ms=300000) for t in owned])
        album=dict(title='Album',artist='Artist')
        self.assertEqual(len(reviewed_matches(owned,evidence,album)),4)
        self.assertEqual(reviewed_matches(owned,evidence,dict(album,title='Other Album')),[])
        self.assertEqual(reviewed_matches(owned,dict(evidence,tracks=evidence['tracks'][::-1]),album),[])
        known=[dict(owned[0],duration_ms=300000)]
        self.assertEqual(len(reviewed_matches(known,evidence,album)),1)
        self.assertEqual(reviewed_matches([dict(known[0],title='Track 0 (Radio Edit)')],evidence,album),[])
        self.assertEqual(reviewed_matches([dict(known[0],duration_ms=180000)],evidence,album),[])

    def test_duplicate_transfer_requires_recording_evidence_and_no_conflict(self):
        target=dict(id='target',album_id='a',title='Song (Club Mix)',artist='Artist',duration_ms=360000)
        source=dict(target,id='source',bpm=123,bpm_source='local:librosa',bpm_provenance='{}')
        self.assertEqual(duplicate_matches(target,[source])['source_track_id'],'source')
        for change in [dict(album_id='b'),dict(title='Song (Radio Edit)'),dict(duration_ms=0),dict(duration_ms=300000)]:
            self.assertIsNone(duplicate_matches(dict(target,**change),[source]))
        self.assertIsNone(duplicate_matches(target,[source,dict(source,id='conflict',bpm=130)]))
        self.assertIsNone(duplicate_matches(target,[dict(source,bpm_source='manual')]))

    def test_duration_backfill_and_atomic_unknown_guard(self):
        db=sqlite3.connect(':memory:')
        db.executescript("CREATE TABLE tracks(id TEXT,bpm REAL,bpm_source TEXT,bpm_provenance TEXT,bpm_updated_at TEXT,duration_ms INTEGER);"
                         "INSERT INTO tracks VALUES('unknown',NULL,NULL,NULL,NULL,0);"
                         "INSERT INTO tracks VALUES('manual',NULL,'manual',NULL,NULL,0);"
                         "INSERT INTO tracks VALUES('known',120,'external:acousticbrainz',NULL,NULL,0);")
        result=dict(bpm=123,recording_duration_ms=300000)
        self.assertEqual(write_bpm(db,'unknown',result,'external:getsongbpm',only_unknown=True),1)
        self.assertEqual(write_bpm(db,'manual',result,'external:getsongbpm',only_unknown=True),0)
        self.assertEqual(write_bpm(db,'known',result,'external:getsongbpm',only_unknown=True),0)
        self.assertEqual(db.execute("SELECT duration_ms FROM tracks WHERE id='unknown'").fetchone()[0],300000)
        self.assertEqual(db.execute("SELECT duration_ms FROM tracks WHERE id='manual'").fetchone()[0],0)

    def test_http_retry_is_bounded_and_access_failures_stop(self):
        with tempfile.TemporaryDirectory() as work:
            http=HTTP(Path(work),Budget(120))
            with patch.object(http,'_get',side_effect=[OSError('503'),{'ok':True}]) as get, patch('enrich_bpm.time.sleep'):
                self.assertEqual(http.get('https://example.test/track'),{'ok':True})
                self.assertEqual(get.call_count,2)
            with patch.object(http,'_get',side_effect=OSError('503')) as get, patch('enrich_bpm.time.sleep'):
                with self.assertRaises(OSError):http.get('https://example.test/track')
                self.assertEqual(get.call_count,3)
            with patch.object(http,'_get',side_effect=RuntimeError('403; stop')) as get:
                with self.assertRaises(RuntimeError):http.get('https://example.test/track')
                self.assertEqual(get.call_count,1)
            url='https://example.test/cached'
            (Path(work)/(hashlib.sha256(url.encode()).hexdigest()+'.json')).write_text('{"cached": true}')
            with patch('enrich_bpm.fcntl.flock',side_effect=AssertionError('cached reads must not wait on locks')):
                self.assertEqual(http.get(url),{'cached':True})
            http.cached_only=True
            with patch('enrich_bpm.urllib.request.urlopen') as network:
                with self.assertRaises(CacheMiss):http.get('https://example.test/uncached')
                network.assert_not_called()

    def test_archive_slot_prefix_needs_actual_slot_album_and_full_version(self):
        track=dict(self.track,track_number='A1',album_title='Album',duration_ms=0)
        self.archive['metadata']['tags'].update(album=['Album'],title=['A1. Song (Club Mix)'])
        self.assertEqual(external_result(track,FakeHTTP([self.recording],self.archive))['status'],'ok')
        for title,album in [('A2. Song (Club Mix)','Album'),('A1. Song (Radio Edit)','Album'),('A1. Song (Club Mix)','Other')]:
            self.archive['metadata']['tags'].update(album=[album],title=[title])
            self.assertEqual(external_result(track,FakeHTTP([self.recording],self.archive))['status'],'archive-version-mismatch')

    def test_manual_null_lock_and_local_priority(self):
        db = sqlite3.connect(':memory:')
        db.executescript('CREATE TABLE tracks(id TEXT,bpm REAL,bpm_source TEXT,bpm_provenance TEXT,bpm_updated_at TEXT);'
                         "INSERT INTO tracks VALUES('manual',NULL,'manual',NULL,NULL);"
                         "INSERT INTO tracks VALUES('auto',120,'local:librosa',NULL,NULL);")
        self.assertEqual(write_bpm(db, 'manual', {'bpm': 125}, 'local:librosa'), 0)
        self.assertEqual(write_bpm(db, 'auto', {'bpm': 125}, 'external:acousticbrainz'), 0)
        self.assertEqual(write_bpm(db, 'auto', {'bpm': 125}, 'local:librosa'), 1)
        self.assertIsNone(db.execute("SELECT bpm FROM tracks WHERE id='manual'").fetchone()[0])

    def test_tracklist_idempotent_preserves_versions(self):
        db = sqlite3.connect(':memory:')
        db.executescript((Path(__file__).parents[1] / 'schema.sql').read_text())
        db.execute("INSERT INTO albums(id,title,artist) VALUES('album','Album','Artist')")
        db.execute("INSERT INTO release_versions(id,album_id,title,artist,discogs_release_id,source) VALUES('pressing','album','Album','Artist',123,'collection')")
        db.commit()
        release = dict(id='pressing', album_id='album', artist='Artist', discogs_release_id=123)
        data = dict(id=123, artists=[{'name': 'Artist (2)'}], tracklist=[
            dict(type_='heading', title='Side A'),
            dict(type_='track', title='Song (Club Mix)', position='A1', duration='6:00'),
            dict(type_='track', title='Song (Radio Edit)', position='B1', duration='3:00')])
        self.assertEqual(import_tracklist(db, release, data), 2)
        self.assertEqual(import_tracklist(db, release, data), 0)
        rows = db.execute('SELECT artist,duration_ms FROM tracks ORDER BY track_number').fetchall()
        self.assertEqual(rows, [('Artist (2)', 360000), ('Artist (2)', 180000)])

    def test_resume_passes_cached_misses_without_spending_item_budget(self):
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            db = sqlite3.connect(root / 'music.db')
            db.executescript((Path(__file__).parents[1] / 'schema.sql').read_text())
            cache = root / 'cache'
            cache.mkdir()
            jobs = sqlite3.connect(cache / 'jobs.sqlite3')
            jobs.execute('CREATE TABLE jobs(key TEXT PRIMARY KEY,result TEXT NOT NULL)')
            for i in range(3):
                path = root / f'{i}.wav'
                path.write_bytes(b'cached fixture; no decoding expected')
                db.execute("INSERT INTO tracks(id,title) VALUES(?,?)", (str(i), 'Track'))
                db.execute("INSERT INTO audio_files(id,track_id,relpath,kind) VALUES(?,?,?,'track')", (str(i), str(i), path.name))
                st = path.stat()
                identity = [ALGORITHM, str(path.resolve()), st.st_size, st.st_mtime_ns]
                key = hashlib.sha256(json.dumps(identity).encode()).hexdigest()
                jobs.execute('INSERT INTO jobs VALUES(?,?)', (key, json.dumps({'status': 'inconsistent-windows'})))
            jobs.commit(); jobs.close(); db.commit(); db.close()
            args = SimpleNamespace(mode='local', max_seconds=50, cache=str(cache), db=str(root / 'music.db'),
                sample_dance=False, probe=False, refresh=False, limit=1, music_root=str(root),
                python='must-not-run', report=str(root / 'report.json'))
            old = {s: signal.getsignal(s) for s in (signal.SIGTERM, signal.SIGINT)}
            try:
                with redirect_stdout(StringIO()):
                    self.assertEqual(run(args), 0)
            finally:
                for sig, handler in old.items():
                    signal.signal(sig, handler)
            stats = json.loads((root / 'report.json').read_text())['stats']
            self.assertEqual(stats['processed'], 3)
            self.assertEqual(stats['cached'], 3)
            self.assertEqual(stats.get('attempted', 0), 0)

    def test_cached_success_applied_before_new_network_failure(self):
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            db = sqlite3.connect(root / 'music.db')
            db.row_factory = sqlite3.Row
            db.executescript((Path(__file__).parents[1] / 'schema.sql').read_text())
            db.execute("INSERT INTO albums(id,title,artist,in_collection) VALUES('a','Album','Artist',1)")
            db.execute("INSERT INTO release_versions(id,album_id,title,artist,source) VALUES('r','a','Album','Artist','collection')")
            for tid,title,number in [('pending','Uncached','1'),('cached','Cached','2')]:
                db.execute("INSERT INTO tracks(id,album_id,release_id,title,artist,track_number,duration_ms) VALUES(?,'a','r',?,'Artist',?,300000)", (tid,title,number))
            track = db.execute("SELECT * FROM tracks WHERE id='cached'").fetchone()
            db.commit(); db.close()
            cache = root / 'cache'; cache.mkdir()
            jobs = sqlite3.connect(cache / 'jobs.sqlite3')
            jobs.execute('CREATE TABLE jobs(key TEXT PRIMARY KEY,result TEXT NOT NULL)')
            jobs.execute('INSERT INTO jobs VALUES(?,?)', (external_job_key(track), json.dumps({'status':'ok','bpm':123.4,'url':'test fixture'})))
            jobs.commit(); jobs.close()
            args = SimpleNamespace(mode='acousticbrainz', max_seconds=80, cache=str(cache), db=str(root / 'music.db'),
                sample_dance=False, probe=False, refresh=False, limit=1, report=str(root / 'report.json'))
            old = {sig: signal.getsignal(sig) for sig in (signal.SIGTERM,signal.SIGINT)}
            try:
                with redirect_stdout(StringIO()), patch('enrich_bpm.HTTP.get', side_effect=OSError('offline')):
                    self.assertEqual(run(args), 1)
            finally:
                for sig,handler in old.items(): signal.signal(sig,handler)
            with sqlite3.connect(root / 'music.db') as db:
                bpm,source = db.execute("SELECT bpm,bpm_source FROM tracks WHERE id='cached'").fetchone()
            self.assertEqual((bpm,source),(123.4,'external:acousticbrainz'))
            stats = json.loads((root / 'report.json').read_text())['stats']
            self.assertEqual(stats['written'],1)
            self.assertEqual(stats['errors'],1)


if __name__ == '__main__':
    unittest.main()
