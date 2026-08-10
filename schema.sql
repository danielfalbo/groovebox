-- Core Releases / Albums Table
CREATE TABLE IF NOT EXISTS releases (
    id TEXT PRIMARY KEY, -- UUID v4 stored as TEXT
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    label TEXT,
    catalog_number TEXT,
    release_year INTEGER,
    discogs_id INTEGER UNIQUE,
    upc TEXT,
    cover_image_url TEXT,
    
    -- Ownership & Availability Flags
    has_vinyl INTEGER DEFAULT 0, -- Boolean (0 = false, 1 = true)
    has_files INTEGER DEFAULT 0, -- Boolean (0 = false, 1 = true)
    streaming_notes TEXT,       -- e.g., "Available on Spotify / Apple Music" or custom text fallback
    
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Core Tracks Table
CREATE TABLE IF NOT EXISTS tracks (
    id TEXT PRIMARY KEY, -- UUID v4 stored as TEXT
    release_id TEXT REFERENCES releases(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    artist TEXT,
    track_number TEXT,
    duration_ms INTEGER,
    isrc TEXT,
    shazam_id TEXT,
    spotify_id TEXT,
    apple_music_id TEXT,
    
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Playlists Table (Strictly internal to local DB)
CREATE TABLE IF NOT EXISTS playlists (
    id TEXT PRIMARY KEY, -- UUID v4 stored as TEXT
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
CREATE INDEX IF NOT EXISTS idx_tracks_shazam ON tracks(shazam_id);
CREATE INDEX IF NOT EXISTS idx_tracks_release ON tracks(release_id);
CREATE INDEX IF NOT EXISTS idx_releases_discogs ON releases(discogs_id);

-- Full Text Search Table (FTS5) for fast fuzzy/prefix search
CREATE VIRTUAL TABLE IF NOT EXISTS search_fts USING fts5(
    target_type, -- 'release' or 'track'
    target_id UNINDEXED,
    title,
    artist,
    catalog_number,
    isrc,
    tokenize = 'porter unicode61'
);
