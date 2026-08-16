-- Canonical Albums (1-to-1 master release entity)
CREATE TABLE IF NOT EXISTS albums (
    id TEXT PRIMARY KEY, -- UUID v4 stored as TEXT
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    release_year INTEGER,
    discogs_master_id INTEGER UNIQUE,
    cover_image_url TEXT,
    has_vinyl INTEGER DEFAULT 0,    -- 1 if owned as physical vinyl
    in_collection INTEGER DEFAULT 0, -- 1 if owned in Discogs collection
    in_wantlist INTEGER DEFAULT 0,   -- 1 if in wantlist
    collection_added_at DATETIME, -- Discogs collection date_added, for "recently added" sorting
    streaming_notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Specific Physical Pressings & Digital Release Versions
CREATE TABLE IF NOT EXISTS release_versions (
    id TEXT PRIMARY KEY, -- UUID v4 stored as TEXT
    album_id TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    discogs_release_id INTEGER UNIQUE,
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    label TEXT,
    catalog_number TEXT,
    release_year INTEGER,
    cover_image_url TEXT,
    format_description TEXT, -- e.g. "Vinyl, LP, Album", "CD", "Digital"
    source TEXT NOT NULL,    -- 'collection', 'wantlist', 'spotify'
    has_vinyl INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Core Tracks Table (Linked to Canonical Album)
CREATE TABLE IF NOT EXISTS tracks (
    id TEXT PRIMARY KEY, -- UUID v4 stored as TEXT
    album_id TEXT REFERENCES albums(id) ON DELETE CASCADE,
    release_id TEXT REFERENCES release_versions(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    artist TEXT,
    track_number TEXT,
    duration_ms INTEGER,
    isrc TEXT,
    spotify_id TEXT,
    apple_music_id TEXT,
    tidal_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Playlists Table (Internal)
CREATE TABLE IF NOT EXISTS playlists (
    id TEXT PRIMARY KEY, -- UUID v4 stored as TEXT
    spotify_id TEXT UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Playlist Tracks Junction Table (Ordered list)
CREATE TABLE IF NOT EXISTS playlist_tracks (
    playlist_id TEXT REFERENCES playlists(id) ON DELETE CASCADE,
    track_id TEXT REFERENCES tracks(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (playlist_id, track_id, position)
);

-- Indexes for Fast Search & Matching
CREATE INDEX IF NOT EXISTS idx_tracks_isrc ON tracks(isrc);
CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks(album_id);
CREATE INDEX IF NOT EXISTS idx_versions_album ON release_versions(album_id);
CREATE INDEX IF NOT EXISTS idx_albums_discogs_master ON albums(discogs_master_id);
CREATE INDEX IF NOT EXISTS idx_albums_artist ON albums(artist);
CREATE INDEX IF NOT EXISTS idx_tracks_artist ON tracks(artist);

-- Full Text Search Table (FTS5) for fast fuzzy/prefix search
CREATE VIRTUAL TABLE IF NOT EXISTS search_fts USING fts5(
    target_type, -- 'album' or 'track'
    target_id UNINDEXED,
    title,
    artist,
    catalog_number,
    isrc,
    tokenize = 'porter unicode61'
);
