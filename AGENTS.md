# AGENTS.md: Developer & Agent Guidelines

## 🚀 Overview & Repository Structure
`my-music-lib` is a self-hosted, local-first music archival and curation engine written in Go and SQLite with a vanilla HTML5/CSS3/JS Web UI.

- `main.go`: Entry point, CLI flag handlers (`-port`, `-import-spotify`, `-import-spotify-account`, `-import-apple-music`, `-sync-discogs`, `-dedupe-albums`), REST API routes (`/api/albums`, `/api/artists`, `/api/sync/discogs`, `/api/sync/status`, `/api/albums/dedupe`), and `DedupeAlbums`/`NormalizeAlbumTitle` merge logic.
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

4. **Albums API Filtering & Sorting:**
   - `GET /api/albums` supports `?filter=collection` (in_collection=1) and `?filter=wantlist` (in_wantlist=1).
   - `?filter=collection` sorts by `albums.collection_added_at` (Discogs' `date_added`, captured per collection item during sync) descending, newest first; albums without a recorded date sort last. Other filters keep alphabetical (`title ASC`) order.
   - `GET /api/albums/counts` returns `{all, collection, wantlist}` counts for pill badge display.
   - Limit raised to 5000 records (wantlist is ~5,491 items).
   - `has_vinyl` (on both `albums` and `release_versions`) means "owned as physical vinyl" — it must only be set when `source == "collection"`. Do not set it for wantlist-sourced vinyl formats; that previously caused wantlist-only albums to show a "Collection"/vinyl badge instead of "Wantlist" (fixed in `discogs.go`'s `processDiscogsItem`, with a self-healing recompute in `db.go`'s `initDB`).

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
   - Full REST endpoints in `main.go`: `POST/PUT/DELETE /api/playlists`, `POST/DELETE /api/playlists/:id/tracks`, `POST /api/tracks`, `GET /api/autocomplete`, `GET /api/autocomplete/online`.
   - Track curation controls across views (`+` Add to playlist modal, `✕` track removal). Manual positional re-ordering was intentionally removed — not a supported feature.
   - Live autocompletion combines local `search_fts` / `tracks` table lookup (`/api/autocomplete`) with Apple Music's free iTunes Search API (`/api/autocomplete/online`) to auto-fill title, artist, album, duration, and 300x300 high-res cover art.

7. **Historical Shazam Ingestion & Track Cleanups:**
   - Imported 30 historical Shazam tracks directly into monthly playlists (`2026-08`, `2026-07`, `2026-06`).
   - Deduplicated 602 redundant track records across identical albums and backfilled missing track durations via iTunes API.

8. **Album Deduplication (`DedupeAlbums`):**
   - Candidate pairs (same artist) qualify via matching `discogs_master_id` OR equal `NormalizeAlbumTitle` output — normalized title equality alone is sufficient merge evidence (do not additionally require track overlap; duplicate albums can have complementary, non-overlapping tracklists, e.g. a collection entry with only side-A tracks vs. a digital entry with only side-B tracks).
   - Merges reassign the secondary album's `release_versions` and `tracks` onto the canonical album; never insert a placeholder `release_versions` row for the deleted secondary album — the Discogs Pressings table (`public/app.js`) renders every `release_versions` row as a real pressing, so synthetic rows show up as fake "Discogs Pressing" noise.

9. **Client-Side URL Routing:**
   - `public/app.js` implements a small router (`pushURL`/`replaceURL`/`renderFromLocation`) over the History API — every view (`/albums`, `/albums/:id`, `/artists`, `/artists/:name`, `/songs`, `/playlists/:id`, `/search`) maps to a shareable URL, and the browser back/forward buttons work via a `popstate` listener.
   - `main.go`'s `/` handler falls back to serving `public/index.html` for any non-`/api/` path that isn't a real static file, so deep links and page refreshes on client-side routes work.
   - Live-typing filters/search use `replaceState` (no history spam per keystroke); genuine navigations (clicking an album, artist, or playlist) use `pushState`.

## 🚫 Non-Goals

- **Local audio playback / streaming**: out of scope — this is an archival/indexing tool, not a player. Do not add playback UI.
- **Discogs OAuth login UI**: no in-app Discogs authentication flow; sync continues to use a token from `.env` / `../discogs-albums/.env`.

## 💡 Future Ideas (not started)

- **Format & Genre Sub-Filters**: additional pill filters for media format (Vinyl LP, CD, Digital) and master genres within the Collection/Wantlist views.


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
