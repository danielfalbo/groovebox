import json
from contextlib import redirect_stdout
from io import StringIO
import hashlib
import signal
import tempfile
from types import SimpleNamespace
from unittest.mock import patch
from pathlib import Path
import sqlite3
import sys
import unittest
sys.path.insert(0, str(Path(__file__).parent))
from enrich_bpm import external_result, import_tracklist, norm, recording_match, write_bpm, run, ALGORITHM, external_job_key


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
        self.assertEqual(external_result(dict(self.track, duration_ms=0), h)['status'], 'missing-duration')

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
        db.executescript('CREATE TABLE tracks(id TEXT PRIMARY KEY,album_id TEXT,release_id TEXT,title TEXT,artist TEXT,track_number TEXT,duration_ms INTEGER);'
                         'CREATE TABLE search_fts(target_type TEXT,target_id TEXT,title TEXT,artist TEXT);')
        release = dict(id='pressing', album_id='album', artist='Artist', discogs_release_id=123)
        data = dict(artists=[{'name': 'Artist (2)'}], tracklist=[
            dict(type_='heading', title='Side A'),
            dict(type_='track', title='Song (Club Mix)', position='A1', duration='6:00'),
            dict(type_='track', title='Song (Radio Edit)', position='B1', duration='3:00')])
        self.assertEqual(import_tracklist(db, release, data), 2)
        self.assertEqual(import_tracklist(db, release, data), 0)
        rows = db.execute('SELECT artist,duration_ms FROM tracks ORDER BY track_number').fetchall()
        self.assertEqual(rows, [('Artist', 360000), ('Artist', 180000)])

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
