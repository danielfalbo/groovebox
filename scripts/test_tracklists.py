import copy
import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from tracklists import (ROOT, contextual_recording, current_inventory, duration_ms, flatten,
                        install_schema, reconcile, restore_owned_membership, run)


def track(title='Song (Club Mix)', position='A1', **kw):
    return dict(type_='track', title=title, position=position, duration='6:00', **kw)


class TracklistsTests(unittest.TestCase):
    def setUp(self):
        self.db=sqlite3.connect(':memory:')
        self.db.row_factory=sqlite3.Row
        self.db.executescript((ROOT/'schema.sql').read_text())
        install_schema(self.db)
        self.db.execute("INSERT INTO albums(id,title,artist,in_collection) VALUES('a','Album','Artist',1)")
        self.db.execute("INSERT INTO release_versions(id,album_id,discogs_release_id,title,artist,source) VALUES('r','a',123,'Album','Artist','collection')")
        self.db.commit()
        self.release=dict(id='r',album_id='a',discogs_release_id=123,artist='Artist')
        self.data=dict(id=123,artists=[dict(name='Artist')],tracklist=[track()])
        self.addCleanup(self.db.close)

    def apply(self,data=None):
        return reconcile(self.db,self.release,data or self.data,'2026-10-24T12:00:00Z')

    def add(self,tid='old',**kwargs):
        row=dict(id=tid,album_id='a',release_id='r',title='Song (Club Mix)',artist='Artist',track_number='A1',duration_ms=0)
        row.update(kwargs)
        self.db.execute('INSERT INTO tracks('+','.join(row)+') VALUES('+','.join('?' for _ in row)+')',list(row.values()))
        self.db.commit()

    def test_tree_headings_nested_medley_artists_and_source_order(self):
        data=dict(id=123,artists=[dict(name='Release Artist')],tracklist=[
            dict(type_='heading',title='Disc Two',position=''),
            dict(type_='index',title='Medley (Extended)',position='',artists=[dict(name='Full Artist (2)',anv='Full Artist',join='&'),dict(name='Guest')],sub_tracks=[
                track('Part 1','2-01.1'),dict(type_='index',title='Inner Suite',position='',sub_tracks=[track('Part 2','2-01.2')])]),
            track('After','2-02',artists=[dict(name='Different Artist')])])
        entries,issues=flatten(data)
        self.assertEqual(issues,[])
        self.assertEqual([e['title'] for e in entries],['Disc Two','Medley (Extended)','Part 1','Inner Suite','Part 2','After'])
        self.assertEqual([e['artist'] for e in entries if e['is_track']],['Full Artist & Guest','Full Artist & Guest','Different Artist'])
        r=self.apply(data)
        self.assertEqual((r['status'],r['expected_tracks'],r['mapped_tracks']),('complete',3,3))
        child=self.db.execute("SELECT * FROM tracks WHERE title='Part 2'").fetchone()
        self.assertEqual(contextual_recording(self.db,child)['title'],'Medley (Extended) / Inner Suite / Part 2')
        self.assertEqual(json.loads(self.db.execute('SELECT metadata_json FROM discogs_tracklists').fetchone()[0]),data)

    def test_repeated_numbers_on_two_explicit_discs_idempotent(self):
        self.data['tracklist']=[dict(type_='heading',title='Disc 1'),track('Same','1'),dict(type_='heading',title='Disc 2'),track('Same','1')]
        self.assertEqual(self.apply()['inserted'],2)
        self.assertEqual(self.apply()['inserted'],0)
        self.assertEqual(self.apply()['status'],'complete')
        self.assertEqual(self.db.execute('SELECT count(*) FROM tracks').fetchone()[0],2)

    def test_partial_upgrade_preserves_ids_bpm_locks_audio_playlist_and_streaming(self):
        self.add(bpm=123.4,bpm_source='local:librosa',bpm_provenance='{"evidence":"keep"}',bpm_updated_at='old-time',spotify_id='spotify',tidal_id='tidal',isrc='GB1234567890')
        self.add('manual',title='Manual',track_number='B1',bpm=None,bpm_source='manual',bpm_provenance='manual-null')
        self.add('number',title='Override',track_number='B2',bpm=129,bpm_source='manual')
        self.db.execute("INSERT INTO playlists(id,name) VALUES('p','Keep')")
        self.db.execute("INSERT INTO playlist_tracks(playlist_id,track_id,position) VALUES('p','old',7)")
        self.db.execute("INSERT INTO audio_files(id,track_id,release_id,relpath) VALUES('audio','old','r','keep.flac')")
        self.db.commit()
        before={t:[tuple(r) for r in self.db.execute('SELECT * FROM '+t)] for t in ('audio_files','playlists','playlist_tracks')}
        preserved=tuple(self.db.execute("SELECT bpm,bpm_source,bpm_provenance,bpm_updated_at,isrc,spotify_id,tidal_id FROM tracks WHERE id='old'").fetchone())
        self.data['tracklist'] += [track('Manual','B1'),track('Override','B2'),track('Missing','C1')]
        r=self.apply();self.assertEqual((r['status'],r['inserted']),('complete',1))
        self.assertEqual(tuple(self.db.execute("SELECT bpm,bpm_source,bpm_provenance,bpm_updated_at,isrc,spotify_id,tidal_id FROM tracks WHERE id='old'").fetchone()),preserved)
        self.assertEqual(tuple(self.db.execute("SELECT bpm,bpm_source FROM tracks WHERE id='manual'").fetchone()),(None,'manual'))
        self.assertEqual(tuple(self.db.execute("SELECT bpm,bpm_source FROM tracks WHERE id='number'").fetchone()),(129,'manual'))
        for table,rows in before.items():self.assertEqual([tuple(r) for r in self.db.execute('SELECT * FROM '+table)],rows)
        self.assertEqual(self.apply()['inserted'],0)
        self.assertEqual(self.db.execute('PRAGMA integrity_check').fetchone()[0],'ok')
        self.assertEqual(self.db.execute('PRAGMA foreign_key_check').fetchall(),[])

    def test_position_duration_and_artist_credit_correction_preserves_id(self):
        self.add(track_number='wrong',artist='Artist')
        self.db.execute("INSERT INTO search_fts(target_type,target_id,title,artist,isrc,catalog_number) VALUES('track','old','Song (Club Mix)','Artist','KEEP-ISRC','KEEP-CAT')")
        self.db.commit()
        self.data['artists']=[dict(name='Artist (2)',anv='Artist',join='')]
        r=self.apply()
        self.assertEqual((r['inserted'],r['updated'],r['status']),(0,1,'complete'))
        row=self.db.execute("SELECT track_number,duration_ms FROM tracks WHERE id='old'").fetchone()
        self.assertEqual(tuple(row),('A1',360000))
        self.assertEqual(self.apply()['updated'],0)
        self.assertEqual(tuple(self.db.execute("SELECT isrc,catalog_number FROM search_fts WHERE target_id='old'").fetchone()),('KEEP-ISRC','KEEP-CAT'))

    def test_collection_pagination_retry_and_actual_copy_count(self):
        import urllib.error
        from enrich_bpm import HTTP,Budget
        class Response:
            def __init__(self,data):self.data=data
            def __enter__(self):return self
            def __exit__(self,*_):return False
            def read(self,*_):return json.dumps(self.data).encode()
        with tempfile.TemporaryDirectory() as work:
            h=HTTP(Path(work),Budget(120))
            pages=[urllib.error.HTTPError('https://api.discogs.com/oauth/identity',503,'transient',{},None),
                Response(dict(username='fixture')),
                Response(dict(pagination=dict(pages=2,items=3),releases=[dict(id=1),dict(id=2)])),
                Response(dict(pagination=dict(pages=2,items=3),releases=[dict(id=1)]))]
            with patch('tracklists.read_token',return_value='test-only'),patch('urllib.request.urlopen',side_effect=pages) as get,patch('tracklists.time.sleep'):
                inv=current_inventory(h)
                self.assertEqual((inv['items'],len(inv['releases'])),(3,3))
                self.assertEqual(get.call_count,4)
                self.assertEqual(len({x['id'] for x in inv['releases']}),2)

    def test_different_mix_or_artist_never_reuses_bpm(self):
        self.add(bpm=128,bpm_source='manual')
        for change in [dict(title='Song (Radio Edit)'),dict(artists=[dict(name='Other Artist')])]:
            data=copy.deepcopy(self.data);data['tracklist'][0].update(change)
            result=self.apply(data)
            self.assertEqual((result['status'],result['inserted'],result['mapped_tracks']),('conflict',0,0))
            self.assertEqual(tuple(self.db.execute("SELECT title,bpm,bpm_source FROM tracks WHERE id='old'").fetchone()),('Song (Club Mix)',128,'manual'))

    def test_changed_medley_context_never_reuses_bpm(self):
        self.data['tracklist']=[dict(type_='index',title='Medley (Club Mix)',sub_tracks=[track('Part 1','A1.1')])]
        self.apply()
        self.db.execute("UPDATE tracks SET bpm=128,bpm_source='manual'");self.db.commit()
        self.data['tracklist'][0]['title']='Medley (Radio Edit)'
        r=self.apply()
        self.assertEqual(r['status'],'conflict');self.assertEqual(r['mapped_tracks'],0)
        self.assertIn('recording-context-changed',[c['reason'] for c in r['conflicts']])
        self.assertEqual(self.db.execute('SELECT bpm FROM tracks').fetchone()[0],128)

    def test_duplicate_existing_slot_reported_without_deletion_or_insertion(self):
        self.add('one');self.add('two')
        r=self.apply()
        self.assertEqual((r['status'],r['inserted'],r['mapped_tracks']),('conflict',0,0))
        self.assertEqual(len(r['retained_unmapped']),2)
        self.assertEqual(self.db.execute('SELECT count(*) FROM tracks').fetchone()[0],2)

    def test_duplicate_source_entries_fail_closed_without_multiplying_slots(self):
        self.data['tracklist'] *= 2
        r=self.apply()
        self.assertEqual(r['status'],'conflict');self.assertEqual(r['inserted'],0)
        self.assertEqual(self.apply()['inserted'],0)

    def test_no_catalog_or_other_pressing_merge(self):
        self.add('catalog',release_id=None,bpm=120)
        r=self.apply();self.assertEqual(r['inserted'],1)
        self.assertIsNone(self.db.execute("SELECT release_id FROM tracks WHERE id='catalog'").fetchone()[0])
        self.assertEqual(self.db.execute("SELECT bpm FROM tracks WHERE release_id='r'").fetchone()[0],None)

    def test_removed_source_track_kept_as_explicit_conflict(self):
        self.add('curated',title='Legitimate extra mix',track_number='Z1',bpm_source='manual')
        r=self.apply();self.assertEqual(r['status'],'conflict');self.assertEqual(r['inserted'],1)
        self.assertEqual(r['retained_unmapped'][0]['id'],'curated')
        self.assertIsNotNone(self.db.execute("SELECT id FROM tracks WHERE id='curated'").fetchone())

    def test_missing_positions_not_skipped_and_never_claimed_verified(self):
        self.data['tracklist']=[track('Component','')]
        r=self.apply();self.assertEqual((r['expected_tracks'],r['inserted'],r['status']),(1,1,'conflict'))
        self.assertEqual(self.apply()['inserted'],0)

    def test_missing_duration_retained_and_conflicting_protected_duration_reported(self):
        self.add(duration_ms=350000,bpm=128,bpm_source='manual')
        r=self.apply();self.assertEqual(r['status'],'conflict')
        self.assertEqual(self.db.execute("SELECT duration_ms FROM tracks WHERE id='old'").fetchone()[0],350000)
        self.data['tracklist'][0]['duration']=''
        self.assertEqual(self.apply()['status'],'complete')
        self.assertEqual(self.db.execute('SELECT duration_ms FROM tracks').fetchone()[0],350000)

    def test_missing_id_empty_invalid_and_wrong_release_never_write(self):
        for data in [dict(self.data,id=999),dict(self.data,tracklist=[]),dict(self.data,tracklist=[dict(type_='track',title='Bad',position='A1',duration='4:99')])]:
            with self.assertRaises(ValueError):self.apply(data)
        self.assertEqual(self.db.execute('SELECT count(*) FROM tracks').fetchone()[0],0)
        self.assertEqual(duration_ms('1:02:03'),3723000)

    def test_inventory_promotes_owned_wantlist_and_preserves_copy_count(self):
        self.db.execute("UPDATE release_versions SET source='wantlist'");self.db.commit()
        inv=dict(items=2,verified_at='now',releases=[dict(id=123),dict(id=123)])
        result=restore_owned_membership(self.db,inv)
        self.assertEqual((result['items'],result['unique_releases']),(2,1))
        self.assertEqual(result['restored_collection_source'],[123])
        self.assertEqual(restore_owned_membership(self.db,inv)['restored_collection_source'],[])
        self.assertEqual(self.db.execute('SELECT source FROM release_versions').fetchone()[0],'collection')

    def test_full_batch_reports_missing_ids_and_provider_error(self):
        with tempfile.TemporaryDirectory() as work:
            path=Path(work)/'db';out=sqlite3.connect(path);self.db.backup(out);out.close()
            out=sqlite3.connect(path)
            out.execute("INSERT INTO release_versions(id,album_id,title,artist,source) VALUES('no-id','a','Unknown','Artist','collection')");out.commit();out.close()
            args=SimpleNamespace(db=str(path),cache=work,max_seconds=60,report=str(Path(work)/'report.json'),refresh=False,cached_only=False,verify_collection=False,inventory=None)
            with patch('tracklists.HTTP.get',side_effect=OSError('HTTP 503')):
                r=run(args)
            self.assertFalse(r['complete']);self.assertEqual(r['totals']['error'],2)
            self.assertEqual({x['release_id'] for x in r['records']},{'r','no-id'})
            self.assertTrue(Path(args.report).exists())


if __name__=='__main__':unittest.main()
