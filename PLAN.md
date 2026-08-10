# PLAN.md: Centralized Music Archival, Organization, & Indexing System (V1)

## 🎯 Architecture Overview & Big Picture
- **Single Source of Truth:** Self-hosted, local-first music archival engine consolidating physical media (Discogs), digital files, streaming catalogs (Spotify, Apple Music), Shazam tags, and custom playlists.
- **V1 Scope:** Archival, indexing, metadata linkage, mobile curation. **No direct audio streaming.**
- **Stack:** 
  - **Backend:** Go (`net/http`, standard library server)
  - **Database:** SQLite (`modernc.org/sqlite`, pure Go, CGO-free, single portable binary for macOS & Linux)
  - **Frontend:** Vanilla HTML5 + CSS3 + JS (responsive, dark mode, mobile-first over Tailscale)

---

## 🗂️ Data Ingestion & Integration Map

1. **Discogs (`[integration]` Live Background Sync)**
   - Credentials read from `../discogs-albums/.env` (`DISCOGS_TOKEN`, `DISCOGS_USERNAME`).
   - Syncs collection (`has_vinyl = true`) and wantlist items into `releases`.

2. **Shazam / Mobile Capture (`[integration/app]`)**
   - API Webhook endpoint (`POST /api/shazam`) for instant tag ingestion.
   - Designed for iOS Shortcuts integration (supports offline queuing & batch flushing over Tailscale).

3. **One-Off Imports (`[one-off]`)**
   - **Apple Music:** Parse `Library.xml` / CSV export for tracks, ISRCs, and playlists.
   - **Spotify:** Ingest exported JSON/CSV (saved tracks, playlists).
   - **GitHub / Markdown / CSV Playlists:** Ingest tracklists directly into local playlists.
   - **Shazam CSV Export:** Ingest historical Shazam tags.

4. **Mobile Web Curation (`[integration/app]`)**
   - Responsive web UI for fast FTS search, manual entry, tagging physical/digital availability, and playlist creation.

---

## 📋 Database Schema Design (SQLite)

- **`releases`**: `id` (UUID/TEXT), `title`, `artist`, `label`, `catalog_number`, `release_year`, `discogs_id` (UNIQUE), `upc`, `cover_image_url`, `has_vinyl`, `has_files`, `streaming_notes`, `created_at`, `updated_at`.
- **`tracks`**: `id` (UUID/TEXT), `release_id`, `title`, `artist`, `track_number`, `duration_ms`, `isrc`, `shazam_id`, `spotify_id`, `apple_music_id`, `created_at`.
- **`playlists`**: `id` (UUID/TEXT), `name`, `description`, `created_at`, `updated_at`.
- **`playlist_tracks`**: `playlist_id`, `track_id`, `position`, `added_at`.
- **`tracks_fts`**: FTS5 Virtual Table for fast searching across title, artist, catalog number, ISRC.

---

## 🚀 Execution Roadmap & TODOs

### Step 1: Core Foundation & SQLite Setup ✅
- [x] Initialize `go.mod` and add pure Go SQLite driver (`modernc.org/sqlite`).
- [x] Create `schema.sql` with table definitions, indexes, and FTS5 search table.
- [x] Write `db.go` to initialize database connection, execute schema migrations, and prepare helper queries.

### Step 2: Notion Spotify Playlists Importer ✅
- [x] Build Go CSV parser for Spotify exports (`Track URI`, `Track Name`, `Artist Name(s)`, `Album Name`, etc.).
- [x] Downloaded 19 unique playlist CSV files directly from `danielfalbo/notion` via `gh api`.
- [x] Ingested 2,780 tracks across 1,510 releases and 19 playlists into SQLite database `music.db`.

### Step 3: Web UI Visualization & Search Engine (v0) ✅
- [x] **HTTP Server (`main.go`):** Serves `index.html` static assets and JSON API endpoints (`/api/stats`, `/api/playlists`, `/api/search`).
- [x] **FTS5 Search Engine:** Enabled instant full-text search across titles, artists, and releases.
- [x] **Sleek UI (`public/`):** Mobile-first glassmorphism Web UI built with HTML5, CSS3, and vanilla JS.

### Step 4: Apple Music & Other One-Off Imports ⬜
- [ ] Apple Music `Library.xml` parser (`POST /api/import/apple-music`).
- [ ] Shazam CSV / Webhook importer.

### Step 5: Apple Music & Other One-Off Imports ⬜
- [ ] Apple Music `Library.xml` parser (`POST /api/import/apple-music`).
- [ ] Shazam CSV / Webhook importer.

### Step 6: Discogs Live Background Sync Engine (Deferred) ⬜
- [ ] Implement Discogs client in Go (handling rate limits / 429 retries).
- [ ] Add background ticker/worker to sync collection & wantlist into SQLite.

### Step 7: Playlist Management & Mobile Polish ⬜
- [ ] Complete internal playlist CRUD endpoints & UI modal/screens.
- [ ] Final mobile usability testing & Tailscale deployment instructions.

---

## 📌 Current Status
**Phase:** Step 4 Apple Music & One-Off Imports
**Completed:** Core SQLite DB, Notion Spotify CSV Importer (19 playlists / 2,780 tracks), Blueprint Dark Web UI v0 with sticky table scrolling, and Tailscale deployment docs.
**Next Immediate Action:** Build Apple Music `Library.xml` parser (`POST /api/import/apple-music`).
