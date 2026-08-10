#!/usr/bin/env python3
import sys
import sqlite3
from contextlib import closing

def die(msg):
    print(msg, file=sys.stderr)
    sys.exit(1)

def get_db_tables(db):
    q = "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '%_fts%' AND name NOT LIKE '%_idx%'"
    return {row[0] for row in db.execute(q)}

def dump_db(db_path):
    with closing(sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)) as db:
        db.row_factory = sqlite3.Row
        tables = get_db_tables(db)
        for table in sorted(tables):
            print(f"=== TABLE: {table} ===")
            rows = db.execute(f"SELECT * FROM {table}").fetchall()
            for row in rows:
                identifier = dict(row).get('id', dict(row).get('title', dict(row).get('name', '_')))
                print(f"--- ENTRY: {identifier} ---")
                for key in row.keys():
                    val = row[key]
                    if val is None:
                        continue
                    print(f"{key}: {val}")
                print("")

def main():
    if len(sys.argv) < 2:
        die(f"Usage: {sys.argv[0]} <db_path>")
    dump_db(sys.argv[1])

if __name__ == '__main__':
    try:
        main()
    except BrokenPipeError:
        sys.stderr.close()
        sys.exit(0)
