# 🚀 Groovebox Plan & Task Overview

## 🌍 Big Picture

**Single Source of Truth:** Groovebox is a self-hosted, local-first music archival engine consolidating physical media (Discogs), digital files, streaming catalogs (Spotify, Apple Music), Shazam tags, and custom playlists into one searchable library.

**V1 Scope:** Archival, indexing, metadata linkage, and mobile curation over Tailscale. No direct audio streaming or playback (see [Explicit Non-Goals](#-explicit-non-goals)).

## 🎯 Architecture Summary
`groovebox` is a self-hosted, local-first music archival engine written in Go and SQLite with a modern HTML5/CSS3/JS Web UI.

### Key Components
- **`main.go`**: HTTP router & API handlers (`/api/albums`, `/api/albums/counts`, `/api/albums/:id`, `/api/artists`, `/api/artists/:name`, `/api/tracks`, `/api/sync/discogs`, `/api/sync/status`).
- **`discogs.go`**: Discogs API sync (Collection: 71 items, Wantlist: 5,478 items) with thread-safe live progress streaming (`GetSyncProgress`).
- **`importer.go`**: Spotify CSV Notion export parser with historical playlist date resolution.
- **`schema.sql`**: SQLite DDL (`albums`, `release_versions`, `tracks`, `playlists`, `playlist_tracks`, `search_fts`).
- **`public/`**: Static Web UI (`index.html`, `style.css`, `app.js`).

---

## 🛠️ Accomplished Progress

- [x] **Canonical 1-to-1 Albums Schema**: Replaced flat release table with 1-to-1 master release `albums` table and separate `release_versions` table for pressings/wantlist entries.
- [x] **Dedicated Views**: Built dedicated **Album Detail Page** (pressings table & tracklist) and **Artist Detail Page** (albums grid & tracks).
- [x] **Live Discogs Sync Progress**: Added async sync execution with `GET /api/sync/status` API and live navigation bar progress pill with timestamp tracking.
- [x] **UI & Micro-Interactions**:
  - Rebranded application to **Groovebox**.
  - Section-specific local search/filter bar with loading spinners.
  - One-click **YouTube Search Icon** button & **Spotify green SVG button** on every track.
  - Symmetrical Blueprint navbar & custom animated Vinyl SVG sidebar icon.
  - B-tree indexing for instant `/api/artists` queries.
  - Circular 1:1 ratio artist avatars.
- [x] **Discogs Collection & Wantlist Filter Pills**: Segmented toggle bar on Albums grid (`All Albums`, `📀 Collection`, `🎯 Wantlist`) with live count badges and fast query filtering (`/api/albums?filter=collection|wantlist` & `/api/albums/counts`). Filter state persists on re-entry.
- [x] **Favicon**: SVG vinyl record favicon (`public/favicon.svg`) + hi-res JPG fallback (`public/favicon.jpg`) with Blueprint dark theme-matching `#2b95d6` center badge.
- [x] **Navbar Brand Icon**: Replaced default music note icon with a custom animated vinyl SVG icon that rotates 90° on hover.
- [x] **Enhanced Album Detail Page**:
  - Improved loading spinner with contextual message.
  - Pressing thumbnails (32×32px strictly constrained) in the Discogs pressings table.
  - Source badges per pressing: `📀 Collection`, `🎯 Wantlist`, `Spotify`.
  - Empty-state message when no pressings are linked yet.
  - Discogs release link replaced with inline SVG Discogs logo icon button (`discogs-icon-btn` in `app.js`).
  - Cleaner header status tags: `📀 In Collection` / `🎯 On Wantlist`.
  - Discogs icon button styling (`.discogs-icon-btn`, `.discogs-svg-icon` in `public/style.css`) matching the Spotify/YouTube icon buttons.

---

## 🗺️ Unfinished Roadmap (carried over from earlier plan, not yet done)

These were part of the original V1 scope and were never completed — they dropped out of the plan during a docs rewrite rather than being finished or deliberately cut:

- [ ] **Apple Music Import**: Parser for `Library.xml` (`POST /api/import/apple-music` or `-import-apple-music <path>` CLI flag) to ingest tracks, artists, album releases, ISRCs, play counts, and playlists. Target file: `/Users/daniel/apple-music-library/Library.xml` (~7.4 MB).
- [ ] **Shazam Ingestion**: `POST /api/shazam` webhook for instant tag capture (designed for iOS Shortcuts integration, offline queuing/batch flush over Tailscale) plus a Shazam CSV importer for historical tags.
- [ ] **Playlist Management CRUD**: Full create/edit/delete/reorder endpoints and UI/modal for internal playlists (currently only Spotify-imported playlists exist; no way to manage playlists from the app itself).
- [ ] **Mobile Polish & Tailscale Deployment**: Final mobile usability pass and documented Tailscale home-server deployment instructions.
- [ ] **Live Spotify Playlist Sync**: New capability (not previously scoped) to pull the user's *current* Spotify account playlists directly via the official Spotify Web API — distinct from the existing `importer.go`, which only parses a static historical Notion/CSV export. Requires:
  - OAuth Authorization Code flow (`playlist-read-private`, `playlist-read-collaborative` scopes) using a Client ID/Secret registered at the Spotify Developer Dashboard, with a refresh token stored in `.env` (same pattern as `DISCOGS_TOKEN`).
  - New `spotify.go` client mirroring `discogs.go`'s thread-safe live-progress-streaming sync pattern, paginating `GET /me/playlists` and `GET /playlists/{id}/tracks`, mapped into the existing `playlists`/`playlist_tracks`/`tracks` schema.
  - New `POST /api/sync/spotify` endpoint + `-sync-spotify` CLI flag, following the same conventions as the Discogs sync trigger.
  - Decided against Playwright/Puppeteer scraping of `open.spotify.com` for this — fragile against DOM changes, against Spotify's ToS, and the official Web API is free for personal-use apps and returns clean structured data with stable IDs.

---

## 🚫 Explicit Non-Goals

- **Local audio playback / streaming**: not part of Groovebox's scope — this is an archival/indexing tool, not a player.
- **Discogs OAuth login UI**: no in-app Discogs authentication flow; sync continues to use a token from `.env`.

---

## 📋 Recommended Future Tasks

1. **Format & Genre Sub-Filters**: Add additional pill filters for media format (Vinyl LP, CD, Digital) and master genres within the Collection/Wantlist views.
2. **Album Page: Discogs Master Link**: Surface Discogs master release URL in album header (`discogs_master_id` already returned by API — link to `https://www.discogs.com/master/:id`).
