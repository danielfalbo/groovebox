-- Official release structure is separate from curated/canonical recordings.
CREATE TABLE IF NOT EXISTS discogs_tracklists (
    release_id TEXT PRIMARY KEY REFERENCES release_versions(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    checked_at TEXT NOT NULL,
    fetched_at TEXT,
    source_sha256 TEXT,
    expected_tracks INTEGER NOT NULL DEFAULT 0,
    mapped_tracks INTEGER NOT NULL DEFAULT 0,
    report_json TEXT NOT NULL,
    metadata_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS discogs_tracklist_entries (
    release_id TEXT NOT NULL REFERENCES release_versions(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    parent_path TEXT NOT NULL,
    depth INTEGER NOT NULL,
    entry_type TEXT NOT NULL,
    position TEXT NOT NULL,
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    duration_ms INTEGER,
    is_track INTEGER NOT NULL,
    track_id TEXT REFERENCES tracks(id) ON DELETE SET NULL,
    raw_json TEXT NOT NULL,
    PRIMARY KEY (release_id,path)
);
CREATE INDEX IF NOT EXISTS idx_discogs_entries_track ON discogs_tracklist_entries(track_id);
