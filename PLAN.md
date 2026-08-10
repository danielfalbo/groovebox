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

### Step 3: Web UI Visualization & Search Engine (v0 & v1 UI) ✅
- [x] **HTTP Server (`main.go`):** Serves `index.html` static assets and JSON API endpoints (`/api/stats`, `/api/playlists`, `/api/tracks`, `/api/artists`, `/api/albums`, `/api/search`).
- [x] **FTS5 Search Engine:** Enabled instant full-text search across titles, artists, and releases.
- [x] **Sleek UI (`public/`):** Mobile-first glassmorphism Web UI built with HTML5, CSS3, and vanilla JS.
- [x] **Collapsible Sidebar:** Smooth width animation toggle with `localStorage` state persistence.
- [x] **Playlist Date & Sorting:** Displays formatted creation dates and supports sorting by Name (A-Z, Z-A) and Date (Newest/Oldest).
- [x] **2x2 Grid Collage Covers:** Dynamic 2x2 collage generated for each playlist using up to 4 album artwork images.
- [x] **Library Browsing:** Full view switching for All Songs (table), Artists (grid cards), and Albums (grid cards).

### Step 4: Apple Music & Other One-Off Imports ⬜
- [ ] Apple Music `Library.xml` parser (`POST /api/import/apple-music`). Target file: `/Users/daniel/apple-music-library/Library.xml` (~7.4 MB).
- [ ] Shazam CSV / Webhook importer.

### Step 5: Discogs Live Background Sync Engine ✅
- [x] **Native Go Discogs Client (`discogs.go`):** Reads credentials from `../discogs-albums/.env` (`DISCOGS_TOKEN`) and authenticates user identity.
- [x] **Collection & Wantlist Ingestion:** Syncs collection releases (`has_vinyl = 1`) and wantlist releases (`has_vinyl = 0`, `streaming_notes = 'Discogs Wantlist'`) into SQLite `releases` table.
- [x] **CLI & API Endpoint:** Supports `-sync-discogs` CLI flag and `POST /api/sync/discogs` endpoint with live progress button in top navbar.
- [x] **UI Visual Badges:** Displays `📀 Vinyl` badge for owned vinyl records and `🎯 Wantlist` badge for saved wantlist releases in Albums grid view.

### Step 6: Playlist Management & Mobile Polish ⬜
- [ ] Complete internal playlist CRUD endpoints & UI modal/screens.
- [ ] Final mobile usability testing & Tailscale deployment instructions.

---

## 📌 Current Status & Handoff Notes for Next Agent

### **Completed So Far:**
- [x] **Step 1:** Core Go SQLite foundation initialized with pure Go driver (`modernc.org/sqlite`) & WAL mode in [`db.go`](file:///Users/daniel/my-music-lib/db.go). Full-Text Search table `search_fts` set up in [`schema.sql`](file:///Users/daniel/my-music-lib/schema.sql).
- [x] **Step 2:** Notion Spotify CSV Importer built in [`importer.go`](file:///Users/daniel/my-music-lib/importer.go). Downloaded 19 playlist CSV files from `danielfalbo/notion` via `gh api`. Parsed earliest `Added At` timestamps to set accurate historical creation dates (ranging back to 2015).
- [x] **Step 3:** Blueprint Dark Mode Web UI (`bp5-dark`) in [`public/`](file:///Users/daniel/my-music-lib/public/) with collapsible sidebar, creation date sorting, 2x2 grid collage playlist covers, Library browsing by All Songs / Artists / Albums, FTS5 search (`GET /api/search`), and header gear dropdown menu (`⚙️`).
- [x] **Step 4:** Discogs Sync Engine ([`discogs.go`](file:///Users/daniel/my-music-lib/discogs.go)) for collection & wantlist items with live gear dropdown sync button (`POST /api/sync/discogs`) and UI vinyl/wantlist tags.
- [x] **Step 5 (Docs):** [`README.md`](file:///Users/daniel/my-music-lib/README.md) written with local quickstart, Playwright screenshot test CLI command, and Tailscale Home Server deployment guide.

---

### **🚀 Next Immediate Task for Next Agent: Step 4 (Apple Music Import)**
- **Target File:** `/Users/daniel/apple-music-library/Library.xml` (~7.4 MB, present on local machine).
- **Goal:** Implement `POST /api/import/apple-music` (or CLI flag `-import-apple-music <path>`) in Go (`apple_music.go`).
- **Details:** Parse tracks, artists, album releases, ISRCs, play counts, and playlists from Apple Music `Library.xml` and upsert into SQLite (`tracks`, `releases`, `playlists`, and `playlist_tracks`).
- **Subsequent Steps:** Shazam webhook (`POST /api/shazam`), Shazam CSV importer, and playlist management CRUD.

### **🛠️ Useful Commands for Next Agent:**
```bash
# Run server locally
go run . -port 8080

# Trigger Discogs sync
go run . -sync-discogs

# Re-run Spotify importer if needed (updates accurate dates)
go run . -import-spotify /tmp/spotify_playlists

# Run Playwright visual test
npx -y playwright screenshot http://localhost:8080 screenshot.png
```
