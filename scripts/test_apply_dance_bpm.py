import copy
import json
from pathlib import Path
import sqlite3
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parent))
from apply_dance_bpm import apply_entry, matches


class DanceImportTests(unittest.TestCase):
    def setUp(self):
        self.db = sqlite3.connect(':memory:')
        self.db.row_factory = sqlite3.Row
        self.db.executescript((Path(__file__).parents[1]/'schema.sql').read_text())
        self.db.execute("INSERT INTO albums(id,title,artist) VALUES('a','Song','Artist')")
        self.db.execute("INSERT INTO release_versions(id,album_id,title,artist,source,has_vinyl,discogs_release_id) VALUES('r','a','Song','Artist','collection',1,123)")
        self.db.execute("INSERT INTO tracks(id,release_id,title,artist,track_number,duration_ms) VALUES('t','r','Song (Club Mix)','Artist','A1',300000)")
        self.db.commit()
        self.entry = dict(expected=dict(id='t',release_id='r',title='Song (Club Mix)',artist='Artist',track_number='A1',duration_ms=300000),
            discogs_release_id=123,bpm=123,url='https://www.decks.de/track/example/code',source_title='Song (Club Mix)',
            source_artist='Artist',source_release_title='Song',
            catalog_number='CAT123',identity_review='fixture: exact pressing and mix',retrieved_at='2026-10-24')

    def tearDown(self):
        self.db.close()

    def track(self):
        return dict(self.db.execute('SELECT t.*,rv.discogs_release_id,rv.source,rv.has_vinyl FROM tracks t JOIN release_versions rv ON rv.id=t.release_id').fetchone())

    def test_full_identity_and_actual_owned_vinyl_required(self):
        t = self.track()
        self.assertTrue(matches(t,self.entry))
        for k,v in [('title','Song (Radio Edit)'),('artist','Other'),('release_id','other'),('track_number','B1'),
                    ('duration_ms',0),('discogs_release_id',456),('source','wantlist'),('has_vinyl',0)]:
            self.assertFalse(matches(dict(t,**{k:v}),self.entry),k)
        for bpm in [True,float('nan'),float('inf'),0,401]:
            self.assertFalse(matches(t,dict(self.entry,bpm=bpm)))
        self.assertFalse(matches(t,dict(self.entry,url='https://other.invalid/track')))
        e=copy.deepcopy(self.entry);del e['expected']['duration_ms']
        self.assertFalse(matches(t,e))

    def test_probe_import_provenance_and_resume(self):
        self.assertEqual(apply_entry(self.db,self.entry,True),1)
        self.assertIsNone(self.track()['bpm'])
        self.assertEqual(apply_entry(self.db,self.entry),1)
        t=self.track()
        self.assertEqual((t['bpm'],t['bpm_source'],t['duration_ms']),(123,'external:decks',300000))
        self.assertEqual(json.loads(t['bpm_provenance'])['source_title'],'Song (Club Mix)')
        self.assertEqual(apply_entry(self.db,self.entry),0)

    def test_manual_null_manual_number_local_and_existing_estimates_survive(self):
        for bpm,source in [(None,'manual'),(125,'manual'),(None,'local:librosa'),(125,'local:librosa'),(125,'external:acousticbrainz')]:
            self.db.execute('UPDATE tracks SET bpm=?,bpm_source=?',(bpm,source));self.db.commit()
            self.assertEqual(apply_entry(self.db,self.entry),0)
            self.assertEqual((self.track()['bpm'],self.track()['bpm_source']),(bpm,source))


if __name__ == '__main__':
    unittest.main()
