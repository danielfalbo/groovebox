# AGENTS.md: Developer & Agent Guidelines

## 🚀 Overview & Repository Structure
`my-music-lib` is a self-hosted, local-first music archival and curation engine written in Go and SQLite with a vanilla HTML5/CSS3/JS Web UI.

- `main.go`: Entry point, CLI flag handlers (`-port`, `-import-spotify`, `-import-spotify-account`, `-import-apple-music`, `-sync-discogs`), REST API routes (`/api/albums`, `/api/artists`, `/api/sync/discogs`, `/api/sync/status`).
- `db.go`: SQLite connection, WAL mode initialization, schema migration execution (`ensureColumn` helper for safe ALTER TABLE).
- `schema.sql`: DDL for 1-to-1 canonical `albums`, `release_versions` (Discogs collection/wantlist pressings), `tracks`, `playlists`, `playlist_tracks`, and `search_fts` (FTS5 table). `playlists` and `tracks` carry `spotify_id` / `apple_music_id`; `playlist_tracks` carries `added_at`.
- `importer.go`: Spotify CSV/Notion export parser linking imported tracks to canonical albums.
- `discogs.go`: Discogs collection (71 items) and wantlist (5,478 items) client with thread-safe live progress streaming (`GetSyncProgress`).
- `spotify.go`: One-time Spotify account importer — OAuth Authorization Code flow, owned playlist + track pagination, album upsert, and `created_at` inference from earliest track `added_at`.
- `apple_music.go`: Apple Music `Library.xml` export parser — XML stream decoder ingesting tracks, albums, ISRCs, and 37 playlists with YYYY-MM creation date inference.
- `public/`: Static Web UI (`index.html`, `style.css`, `app.js`) branded as **Groovebox**.

---

## 🔑 Key Conventions & Architecture Decisions

1. **Pure Go SQLite Driver:**
   - Always use `modernc.org/sqlite` (pure Go, CGO-free).
   - Database file is located at root `music.db`. WAL mode is enabled (`_pragma=journal_mode(WAL)`).

2. **Canonical Albums & Release Versions Architecture:**
   - Albums are 1-to-1 master release entities (`discogs_master_id` or normalized `title` + `artist`).
   - Specific physical pressings & digital entries are linked in `release_versions` under their parent `album_id`.
   - Artist endpoints (`/api/artists`) query distinct artists across both `albums` and `tracks` backed by `idx_albums_artist` and `idx_tracks_artist` B-tree indexes.

3. **Discogs Sync & Live Progress API:**
   - `DISCOGS_TOKEN` is automatically read from `../discogs-albums/.env`.
   - `POST /api/sync/discogs` starts background syncs asynchronously.
   - `GET /api/sync/status` returns thread-safe progress metrics (*stage*, *current_page*, *total_pages*, *items_fetched*, *last_synced_at*). UI polls status continuously.

3b. **Spotify Account Import (one-time, completed 2026-08-10):**
   - `spotify.go` implements a single-run OAuth Authorization Code flow: spins up a local callback server on `http://127.0.0.1:8787/callback`, prints the authorize URL, exchanges the code for an access token, then paginates all owned playlists + tracks.
   - Requires `SPOTIFY_CLIENT_ID` and `SPOTIFY_CLIENT_SECRET` (or legacy `SPOTIFY_TOKEN`) in environment. Redirect URI must be registered in Spotify Developer Dashboard.
   - Followed/collaborative playlists not owned by the account are skipped (Spotify API 403).
   - Playlist `created_at` is inferred from the earliest track `added_at` timestamp — Spotify does not expose playlist creation dates.
   - **Result:** 76 playlists, 14,517 unique tracks, 19,607 playlist memberships imported. No refresh-token storage; command exits after single run.
   - Explicitly decided against Playwright/Puppeteer scraping of `open.spotify.com` — fragile, against ToS; official API preferred.

4. **Albums API Filtering:**
   - `GET /api/albums` supports `?filter=collection` (has_vinyl=1) and `?filter=wantlist` (in_wantlist=1).
   - `GET /api/albums/counts` returns `{all, collection, wantlist}` counts for pill badge display.
   - Limit raised to 5000 records (wantlist is ~715 items).

5. **Web UI & Aesthetics (Groovebox):**
   - Uses Blueprint dark theme CSS (`bp5-dark`) with custom dark mode tweaks (`#111418` background).
   - Favicon: `public/favicon.svg` (vector vinyl SVG) + `public/favicon.jpg` (hi-res fallback).
   - Navbar brand: custom animated vinyl SVG icon (rotates 90° on hover via `.custom-navbar-logo-icon`).
   - Dedicated views for **Album Details** (pressings table with 32×32px thumbnails, source badges, Discogs & Qobuz action buttons) and **Artist Pages** (albums grid & tracks).
   - Segmented filter pills on Albums grid: `All Albums` / `📀 Collection` / `🎯 Wantlist` with live count badges.
   - Section-specific local search filter bar with loading spinners.
   - Micro-interaction buttons (YouTube direct search & Spotify links on every track; Discogs SVG logo icon on every pressing row).
   - Track rows are clickable (`.clickable-track-row`) across all tracklists, automatically navigating to the track's canonical Album Details page.
   - Solid SVG fallback cover art (`fallbackCover`) used consistently across all album cards, playlist thumbnails, and pressing images to eliminate image broken state/flicker.
   - Pressings section header includes action buttons to open/search master releases on Discogs (`https://www.discogs.com/search/?q=...&type=master`) and Qobuz Download Store (`https://www.qobuz.com/gb-en/search/albums/...`).
   - Sidebar collapse state stored in `localStorage` (`sidebar-collapsed`).
   - **Icon button pattern**: `.spotify-icon-btn`, `.youtube-icon-btn`, `.discogs-icon-btn`, `.playlist-act-btn` — all 28px circle buttons with brand-colored SVG icons and hover background. See `style.css` for reference.

6. **Playlist CRUD & Curation + Live Global Autocomplete:**
   - Full REST endpoints in `main.go`: `POST/PUT/DELETE /api/playlists`, `POST/DELETE /api/playlists/:id/tracks`, `POST /api/playlists/:id/tracks/reorder`, `POST /api/tracks`, `GET /api/autocomplete`, `GET /api/autocomplete/online`.
   - Track curation controls across views (`+` Add to playlist modal, `▲`/`▼` positional re-ordering, `✕` track removal).
   - Live autocompletion combines local `search_fts` / `tracks` table lookup (`/api/autocomplete`) with Apple Music's free iTunes Search API (`/api/autocomplete/online`) to auto-fill title, artist, album, duration, and 300x300 high-res cover art.

7. **Historical Shazam Ingestion & Track Cleanups:**
   - Imported 30 historical Shazam tracks directly into monthly playlists (`2026-08`, `2026-07`, `2026-06`).
   - Deduplicated 602 redundant track records across identical albums and backfilled missing track durations via iTunes API.


## 🛠️ Essential Commands

```bash
# Start Web Server
go run . -port 8080

# Re-sync Discogs collection & wantlist into SQLite
go run . -sync-discogs

# Re-run Spotify CSV importer (updates historical dates)
go run . -import-spotify /tmp/spotify_playlists

# One-time Spotify account import (OAuth, requires SPOTIFY_CLIENT_ID + SPOTIFY_CLIENT_SECRET)
# Register http://127.0.0.1:8787/callback in Spotify Developer Dashboard first
go run . -import-spotify-account

# Playwright Visual Regression Screenshot
npx -y playwright screenshot http://localhost:8080 screenshot.png
```
