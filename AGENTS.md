# AGENTS.md: Developer & Agent Guidelines

## 🚀 Overview & Repository Structure
`my-music-lib` is a self-hosted, local-first music archival and curation engine written in Go and SQLite with a vanilla HTML5/CSS3/JS Web UI.

- `main.go`: Entry point, CLI flag handlers (`-port`, `-sync-discogs`, `-dedupe-albums`, `-seed-stars`), REST API routes (`/api/albums`, `/api/artists`, `/api/sync/discogs`, `/api/sync/status`, `/api/albums/dedupe`, `/api/tidal/*`), and `DedupeAlbums`/`NormalizeAlbumTitle` merge logic. `SeedStars` stars a curated must-have collector album list (matched against existing albums, skips missing).
- `db.go`: SQLite connection, WAL mode initialization, schema migration execution (`ensureColumn` helper for safe ALTER TABLE).
- `schema.sql`: DDL for 1-to-1 canonical `albums`, `release_versions` (Discogs collection/wantlist pressings), `tracks`, `playlists`, `playlist_tracks`, and `search_fts` (FTS5 table). `playlists` and `tracks` carry `spotify_id` / `apple_music_id` columns (historical import data; no code writes them anymore). `tracks.tidal_id` and `playlists.tidal_*` columns are live (Tidal sync).
- `discogs.go`: Discogs collection (93 items) and wantlist (2,073 items — audited 2026-08-31: deduped every-pressing bloat down from ~5.2k to max ~8 pressings/album, vinyl+SACD+audiophile-philosophy wins) client with thread-safe live progress streaming (`GetSyncProgress`).
- `tidal.go`: Tidal two-way playlist sync — OAuth device flow, playlist CRUD, and a safe 3-way merge reconcile engine. Uses hardcoded tidalapi device-flow client credentials (public) with `.env` / `TIDAL_DEVICE_*` overrides. OAuth tokens persisted in `.tidal-session.json` (gitignored, never in music.db).
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

3b. **Spotify Account Import (one-time, completed 2026-08-10, code removed 2026-08-10):**
   - A one-time OAuth Authorization Code import (`spotify.go`, since deleted) brought in 76 playlists, 14,517 unique tracks, and 19,607 playlist memberships. No refresh tokens were stored and the code never ran again after that import, so it was deleted along with the Apple Music (`apple_music.go`) and Spotify/Notion CSV (`importer.go`) one-off importers. The data they imported remains in `music.db`.

4. **Albums API Filtering & Sorting:**
   - `GET /api/albums` supports `?filter=collection` (in_collection=1) and `?filter=wantlist` (in_wantlist=1).
   - **Starred (must-have) feature:** `albums.starred` flag (migration in db.go). `PUT /api/albums/:id/star` with `{"starred":bool}` toggles it (also POST). Wantlist view sorts `starred DESC, title ASC` so must-haves float to top; collection view order unchanged. UI: ★/☆ button on album cards + detail-page "☆ Star as must-have" toggle. Seed via `./groovebox -seed-stars -db music.db` (idempotent, sets → never unsets).
   - `?filter=collection` sorts by `albums.collection_added_at` (Discogs' `date_added`, captured per collection item during sync) descending, newest first; albums without a recorded date sort last. Other filters keep alphabetical (`title ASC`) order.
   - `GET /api/albums/counts` returns `{all, collection, wantlist}` counts for pill badge display.
   - Limit raised to 5000 records (wantlist is ~2,073 items).
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

7. **Historical Shazam Ingestion & Track Cleanups (one-time, completed 2026-08-10):**
   - Imported 30 historical Shazam tracks directly into monthly playlists (`2026-08`, `2026-07`, `2026-06`) via an ad-hoc script never checked into this repo.
   - Deduplicated 602 redundant track records across identical albums and backfilled missing track durations via iTunes API.
   - The `tracks.shazam_id` column and its index were dead code (never read/written by any code in this repo) and have been removed from `schema.sql` and `music.db`.

8. **Album Deduplication (`DedupeAlbums`):**
   - Candidate pairs (same artist) qualify via matching `discogs_master_id` OR equal `NormalizeAlbumTitle` output — normalized title equality alone is sufficient merge evidence (do not additionally require track overlap; duplicate albums can have complementary, non-overlapping tracklists, e.g. a collection entry with only side-A tracks vs. a digital entry with only side-B tracks).
   - Merges reassign the secondary album's `release_versions` and `tracks` onto the canonical album; never insert a placeholder `release_versions` row for the deleted secondary album — the Discogs Pressings table (`public/app.js`) renders every `release_versions` row as a real pressing, so synthetic rows show up as fake "Discogs Pressing" noise.

9. **Client-Side URL Routing:**
   - `public/app.js` implements a small router (`pushURL`/`replaceURL`/`renderFromLocation`) over the History API — every view (`/albums`, `/albums/:id`, `/artists`, `/artists/:name`, `/songs`, `/playlists/:id`, `/search`) maps to a shareable URL, and the browser back/forward buttons work via a `popstate` listener.
   - `main.go`'s `/` handler falls back to serving `public/index.html` for any non-`/api/` path that isn't a real static file, so deep links and page refreshes on client-side routes work.
   - Live-typing filters/search use `replaceState` (no history spam per keystroke); genuine navigations (clicking an album, artist, or playlist) use `pushState`.

## 🚫 Non-Goals

- ~~Local audio playback / streaming~~ — **FLIPPED 2026-08-22**: Groovebox is now also
  a local music streamer (see “🎧 Local Streamer” below).
- **Discogs OAuth login UI**: no in-app Discogs authentication flow; sync continues to use a token from `.env` / `../discogs-albums/.env`.

---

## 🎧 Local Streamer (BUILT 2026-08-22)

Groovebox **plays local audio** and syncs the local music library **one-way**
(filesystem → DB). Layout contract: `~/syncthing/archive/music/LIBRARY.md`.

### Scanner / library sync (`library.go`, `local_api.go`)
- One-way sync: filesystem is truth of **existence**; DB is truth of
  **identity/curation**. `POST /api/sync/local` walks `GROOVEBOX_MUSIC_ROOT`
  (default = the const `musicLibrary` in `library.go` =
  `/home/me/syncthing/archive/music`). Manual “Sync Local Library” button in the
  settings menu, Discogs/Tidal style.
- Creates/joins canonical albums (title+artist match), a `release_versions` row
  per source dir (so vinyl/raw pressings have a stable id to link to), per-track
  rows, and an `audio_files` index row per file.
- Local art (`folder.jpg`/`large_cover.jpg`/`cover.jpg`) served via
  `/api/local/cover?rel=...`; used as a fallback when the album has no remote cover.
- Deletions/renames on disk are detected (absent rows pruned). Empty dirs ignored.
- Raw vinyl sides (`A.wav`/`B.wav`, un-split whole sides) are `kind='raw'`,
  `track_id NULL`; they surface as continuous playable items and can be manually
  linked to a `release_versions` row via `POST /api/local/link`.

### Playback (`playback.go`)
- Server-side player: Groovebox **owns ALSA hw:0** (single writer) via an
  `ffmpeg -> aplay` pipeline (decode/resample to S16LE 44.1k stereo). No MPD.
- State is authoritative server-side; a browser tab only mirrors it by polling
  `/api/playback/state`.
- API: `GET /api/playback/state`; `POST /api/playback/{pause|resume|toggle|stop|clear|next|prev|seek|volume}`.
- Queue: `POST /api/local/play` (whole album) and `POST /api/local/play-file`
  (track or raw) both enqueue the full album starting at the chosen slot.

### Data model
- `audio_files` table (`schema.sql`): `{id, album_id, track_id NULL, release_id,
  relpath UNIQUE, kind(track|raw), source(cd|vinyl|playback), format, bit_depth,
  sample_rate, size_bytes, mtime, sha256, duration_ms}`.
- Albums that matched an existing Discogs/catalog row keep remote art; otherwise
  local art (a `/api/local/cover` path) is stored.

### Web UI (`public/local.js`)  
- “Local Library” sidebar item → grid of local albums; per-album detail lists
  tracks + raw sides with per-file ▶ play.
- Bottom now-playing bar mirrors server state (title/artist/seek/volume) and is
  best-effort: it re-reads `/api/playback/state` every 2s even across tab reloads.

### Playback learnings (verified 2026-08-23)
- **ALSA hw:0 single-writer race on relaunch**: killing the old pipeline and
  IMMEDIATELY starting a new `aplay` fails with `Device or resource busy` (the
  previous aplay still holds the PCM). `stopProcessLocked` must wait for the old
  process group to fully die before a seek/skip relaunch. Empirically an
  immediate reopen fails; a ~0.4s delay opens clean.
- **Any `launch()` that supersedes the last process MUST spawn a watcher.**
  `SeekTo` historically relaunched without `go p.watch(p.sess)`, so a seek-
  relaunched process that exited was never reaped -> the server stayed frozen at
  `"playing"` with an advancing wall-clock position but dead audio. Keep launch
  funneled through `startLocked` (which spawns the watcher) or add the watch.
- **Volume must be synced with real ALSA, not an in-memory default.**
  `Player.volume` used to be a hardcoded 80 that launch/startLocked never
  applied — if the hardware `Master` was muted/zeroed, playback ran dead-silent
  while the bar showed 80% + advancing time. Fix: `init()` seeds `player.volume`
  from `readAlsaVolume()` at startup, and `startLocked` calls `applyVolumeLocked()`
  before each launch so output always matches the displayed level.
- **UI seek slider:** commit on `pointerup` (not `onchange`) with a `npSeeking`
  flag so the every-2s `npRefresh` poll doesn't yank the thumb mid-gesture; on
  the first click over the range track some browsers don't fire `change` and the
  poll steals the committed value.
- **Bar pinning:** static assets are served with only `Last-Modified` by
  default — add `Cache-Control: no-cache` + cache-busting `?v=` query strings
  when changing the now-playing bar, and/or pin the bar via inline styles in
  `npRefresh()` so a stale/overridden stylesheet can never drop it into page
  flow.

## 💡 Future Ideas (not started)

- **Format & Genre Sub-Filters**: additional pill filters for media format (Vinyl LP, CD, Digital) and master genres within the Collection/Wantlist views.


## 🛠️ Essential Commands

```bash
# Start Web Server
go run . -port 8080

# Re-sync Discogs collection & wantlist into SQLite
go run . -sync-discogs

# Playwright Visual Regression Screenshot
npx -y playwright screenshot http://localhost:8080 screenshot.png

# TESTS — always run before shipping UI/UX changes:
sh e2e/run.sh            # Go unit tests + isolated e2e (snapshot DB, never touches live :3000)
sh e2e/run.sh --live     # e2e against an already-running server (BASE_URL override)
```

## 🧪 Testing

- **Unit (`main_test.go`)**: pure logic such as `NormalizeAlbumTitle` (dedupe-safe properties). `go test ./...`.
- **E2E (`e2e/run.mjs` + global Playwright)**: boots an isolated `groovebox` on a random port against a python3 `sqlite3.backup()` snapshot of `music.db` (never touches the live service/DB), then drives Chromium through the UI regressions: Back-from-album hides the hero (`album-detail-container`
  `display:none` — guards against `clearNavActive` being shadowed by later scripts), cmd/ctrl + middle-click open album cards in new tabs, idle playback bar reserves no bottom padding (`body.np-active` toggles it), wantlist pill, artist-page album cards, and a zero-console-error check while the `/api/sync/status` poll runs (caught the missing `syncBtn` lookup — it threw on every 2s tick as a caught console.error, so the harness watches `console` errors AND `pageerror`s). No npm deps needed (uses the global `playwright` install; browsers via `~/.cache/ms-playwright`).
- Keep cache-buster query (`?v=N` in index.html) bumped whenever `public/*` JS/CSS change so browsers pick up the served files, and re-run `sh e2e/run.sh` before commit.

## 🌊 Tidal Two-Way Playlist Sync (`tidal.go`)

Two-way playlist sync between Groovebox and Tidal, using a safe 3-way merge per connected playlist:

- **Auto-connect**: `POST /api/tidal/sync` pulls all Tidal playlists, creates local counterparts for any not yet connected, and reconciles every connection.
- **Manual connect**: `POST /api/tidal/connect/:playlistID` creates a Tidal playlist for an existing groovebox playlist and links them.
- **Per-playlist sync**: `POST /api/tidal/sync/:playlistID` reconciles a single connection.
- **Disconnect**: `DELETE /api/tidal/connect/:playlistID` unlinks without deleting either side.
- **Auth**: `POST /api/tidal/auth` starts device login (returns a link); `GET /api/tidal/auth` polls / reflects auth state.

**Sync algorithm** (snapshot-based 3-way merge, `tidal_snap_local` / `tidal_snap_tidal` columns on `playlists`):
- Adds: tracks present on one side but not the other are added to the other (ISRC join, fallback to normalized title+artist).
- Deletes: a track is only removed if it was in the last-known snapshot (we knew about it) and vanished from the source side, and was not freshly added on the target (adds win over staggered deletes).
- Order: append-only, never fight reorders between the two sides.

**Track matching**: `tracks.tidal_id` is the bidirectional link. When pushing local → Tidal, the Tidal ID is resolved via v1 search + ISRC match (`ResolveTidalID`). Tidal API requires an `If-None-Match` etag header for playlist mutations (fetched via `fetchETag`).

**Credentials**: hardcoded tidalapi device-flow client credentials (public, from the open-source Python library). Override via `.env` with `TIDAL_DEVICE_CLIENT_ID` / `TIDAL_DEVICE_SECRET`. OAuth tokens are stored in `.tidal-session.json` (gitignored).
