#!/usr/bin/env python3
"""Read-only BPM coverage; count distinct owned pressing slots, not albums."""
import argparse
import json
from pathlib import Path
import sqlite3


def coverage(path):
    db=sqlite3.connect(f'file:{Path(path).resolve()}?mode=ro',uri=True,timeout=5)
    owned="SELECT DISTINCT t.id,t.bpm,rv.has_vinyl,t.duration_ms,t.album_id FROM tracks t JOIN release_versions rv ON rv.id=t.release_id WHERE rv.source='collection'"
    def slots(vinyl=False):
        return dict(zip(('known','slots'),db.execute(f'SELECT sum(bpm IS NOT NULL),count(*) FROM ({owned})'+(' WHERE has_vinyl=1' if vinyl else '')).fetchone()))
    report={'bpm_rows':db.execute('SELECT count(*) FROM tracks WHERE bpm IS NOT NULL').fetchone()[0],
            'sources':dict(db.execute('SELECT bpm_source,count(*) FROM tracks WHERE bpm IS NOT NULL GROUP BY bpm_source')),
            'owned_slots':slots(),'owned_vinyl_slots':slots(True),
            'owned_missing_durations':db.execute(f'SELECT count(*) FROM ({owned}) WHERE COALESCE(duration_ms,0)=0').fetchone()[0],
            'collection_albums_with_bpm':db.execute("""SELECT count(DISTINCT a.id) FROM albums a JOIN tracks t ON t.album_id=a.id WHERE a.in_collection=1 AND t.bpm IS NOT NULL AND
                (EXISTS (SELECT 1 FROM release_versions rv WHERE rv.id=t.release_id AND rv.source='collection') OR EXISTS
                 (SELECT 1 FROM audio_files f WHERE f.track_id=t.id AND f.kind='track'))""").fetchone()[0]}
    db.close();return report

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--db',default='music.db');a=p.parse_args()
    print(json.dumps(coverage(a.db),indent=2))
