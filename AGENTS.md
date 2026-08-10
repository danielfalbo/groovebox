# AGENTS.md: Developer & Agent Guidelines

## 🚀 Overview & Repository Structure
`my-music-lib` is a self-hosted, local-first music archival and curation engine written in Go and SQLite with a vanilla HTML5/CSS3/JS Web UI.

- `main.go`: Entry point, CLI flag handlers (`-port`, `-import-spotify`, `-sync-discogs`), and HTTP routing handlers.
- `db.go`: SQLite connection, WAL mode initialization, schema migration execution.
- `schema.sql`: DDL for `releases`, `tracks`, `playlists`, `playlist_tracks`, and `search_fts` (FTS5 table).
- `importer.go`: Spotify CSV import parser with historical `AddedAt` date resolution.
- `discogs.go`: Discogs collection (71 items) and wantlist (5,478 items) sync client.
- `public/`: Static Web UI (`index.html`, `style.css`, `app.js`).

---

## 🔑 Key Conventions & Architecture Decisions

1. **Pure Go SQLite Driver:**
   - Always use `modernc.org/sqlite` (pure Go, CGO-free).
   - Database file is located at root `music.db`. WAL mode is enabled (`_pragma=journal_mode(WAL)`).

2. **Discogs Credentials & Rate Limits:**
   - `DISCOGS_TOKEN` is automatically read from `../discogs-albums/.env`.
   - Discogs API requests use `per_page=100` and handle rate limits (`429`) with exponential backoff retries.

3. **Search Engine:**
   - Full-text search is powered by `search_fts` (SQLite FTS5 virtual table).
   - Whenever tracks are inserted/updated, `search_fts` should be updated accordingly.

4. **Web UI & Aesthetics:**
   - Uses Blueprint dark theme CSS (`bp5-dark`) with custom dark mode tweaks (`#111418` background).
   - Sidebar collapse state is stored in `localStorage` (`sidebar-collapsed`).
   - Playlist sort preference is stored in `localStorage` (`playlist-sort`).
   - Action dropdowns (such as "Sync Discogs") are kept inside the header settings gear menu (`⚙️`).

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
