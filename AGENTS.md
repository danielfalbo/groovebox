# AGENTS.md: Developer & Agent Guidelines

## 🚀 Overview & Repository Structure
`my-music-lib` is a self-hosted, local-first music archival and curation engine written in Go and SQLite with a vanilla HTML5/CSS3/JS Web UI.

- `main.go`: Entry point, CLI flag handlers (`-port`, `-import-spotify`, `-sync-discogs`), REST API routes (`/api/albums`, `/api/artists`, `/api/sync/discogs`, `/api/sync/status`).
- `db.go`: SQLite connection, WAL mode initialization, schema migration execution.
- `schema.sql`: DDL for 1-to-1 canonical `albums`, `release_versions` (Discogs collection/wantlist pressings), `tracks`, `playlists`, `playlist_tracks`, and `search_fts` (FTS5 table).
- `importer.go`: Spotify CSV import parser linking imported tracks to canonical albums.
- `discogs.go`: Discogs collection (71 items) and wantlist (5,478 items) client with thread-safe live progress streaming (`GetSyncProgress`).
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

3b. **Planned: Live Spotify Playlist Sync (not yet implemented):**
   - `importer.go` today only parses a static historical Notion/CSV export — it has no live connection to a Spotify account.
   - Pulling a user's *current* Spotify playlists requires the official Spotify Web API via OAuth (Authorization Code flow, `playlist-read-private`/`playlist-read-collaborative` scopes), with a Client ID/Secret from the Spotify Developer Dashboard and a refresh token stored in `.env` (mirroring `DISCOGS_TOKEN`).
   - Planned implementation: new `spotify.go` mirroring `discogs.go`'s async sync + live-progress pattern, triggered via `POST /api/sync/spotify` / `-sync-spotify` CLI flag.
   - Explicitly decided against Playwright/Puppeteer scraping of `open.spotify.com` for this — fragile, against ToS; the official API is free for personal use and preferred. See `PLAN.md` roadmap.

4. **Albums API Filtering:**
   - `GET /api/albums` supports `?filter=collection` (has_vinyl=1) and `?filter=wantlist` (in_wantlist=1).
   - `GET /api/albums/counts` returns `{all, collection, wantlist}` counts for pill badge display.
   - Limit raised to 5000 records (wantlist is ~715 items).

5. **Web UI & Aesthetics (Groovebox):**
   - Uses Blueprint dark theme CSS (`bp5-dark`) with custom dark mode tweaks (`#111418` background).
   - Favicon: `public/favicon.svg` (vector vinyl SVG) + `public/favicon.jpg` (hi-res fallback).
   - Navbar brand: custom animated vinyl SVG icon (rotates 90° on hover via `.custom-navbar-logo-icon`).
   - Dedicated views for **Album Details** (pressings table with 32×32px thumbnails, source badges, Discogs logo icon button) and **Artist Pages** (albums grid & tracks).
   - Segmented filter pills on Albums grid: `All Albums` / `📀 Collection` / `🎯 Wantlist` with live count badges.
   - Section-specific local search filter bar with loading spinners.
   - Micro-interaction buttons (YouTube direct search & Spotify links on every track; Discogs SVG logo icon on every pressing row).
   - Sidebar collapse state stored in `localStorage` (`sidebar-collapsed`).
   - **Icon button pattern**: `.spotify-icon-btn`, `.youtube-icon-btn`, `.discogs-icon-btn` — all 28px circle buttons with brand-colored SVG icons and hover background. See `style.css` for `.spotify-icon-btn` / `.youtube-icon-btn` as the reference to follow for adding new icon buttons.

---

## 🛠️ Essential Commands

```bash
# Start Web Server
go run . -port 8080

# Re-sync Discogs collection & wantlist into SQLite
go run . -sync-discogs

# Re-run Spotify CSV importer (updates historical dates)
go run . -import-spotify /tmp/spotify_playlists

# Playwright Visual Regression Screenshot
npx -y playwright screenshot http://localhost:8080 screenshot.png
```
