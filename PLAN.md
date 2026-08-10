# 🚀 Groovebox Plan & Task Overview

## 🌍 Big Picture

**Single Source of Truth:** Groovebox is a self-hosted, local-first music archival engine consolidating physical media (Discogs), digital files, streaming catalogs (Spotify, Apple Music), Shazam tags, and custom playlists into one searchable library.

**V1 Scope:** Archival, indexing, metadata linkage, and mobile curation over Tailscale. No direct audio streaming or playback (see [Explicit Non-Goals](#-explicit-non-goals)).

## 🎯 Architecture Summary
`groovebox` is a self-hosted, local-first music archival engine written in Go and SQLite with a modern HTML5/CSS3/JS Web UI.

### Key Components
- **`main.go`**: HTTP router & API handlers (`/api/albums`, `/api/albums/counts`, `/api/albums/:id`, `/api/artists`, `/api/artists/:name`, `/api/tracks`, `/api/sync/discogs`, `/api/sync/status`).
- **`discogs.go`**: Discogs API sync (Collection: 71 items, Wantlist: 5,478 items) with thread-safe live progress streaming (`GetSyncProgress`).
- **`importer.go`**: Spotify CSV/Notion export parser with historical playlist date resolution.
- **`spotify.go`**: One-time Spotify account importer via OAuth Authorization Code flow; fetches all owned playlists, tracks, and album metadata; uses earliest `added_at` timestamp as playlist `created_at` proxy.
- **`schema.sql`**: SQLite DDL (`albums`, `release_versions`, `tracks`, `playlists`, `playlist_tracks`, `search_fts`). `playlists` and `tracks` carry `spotify_id`; `playlist_tracks` carries `added_at`.
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
- [x] **Discogs Collection & Wantlist Filter Pills**: Segmented toggle bar on Albums grid (`All Albums`, `📀 Collection`, `🎯 Wantlist`) with live count badges and fast query filtering (`/api/albums?filter=collection|wantlist` & `/api/albums/counts`). Application default homepage view set to **`📀 Collection`** in Albums view.
- [x] **Lossless Master Album Deduplication**: Evidence-backed deduplication algorithm (`DedupeAlbums`) merging duplicate digital/streaming releases into 1-to-1 master albums with title normalization (`NormalizeAlbumTitle`) and track title overlap matching. Triggerable via CLI flag (`go run . -dedupe-albums`) or UI Settings dropdown menu.
- [x] **Favicon**: SVG vinyl record favicon (`public/favicon.svg`) + hi-res JPG fallback (`public/favicon.jpg`) with Blueprint dark theme-matching `#2b95d6` center badge.
- [x] **Navbar Brand Icon**: Replaced default music note icon with a custom animated vinyl SVG icon that rotates 90° on hover.
- [x] **Enhanced Album Detail Page**:
  - Improved loading spinner with contextual message.
  - Pressing thumbnails (32×32px strictly constrained) in the Discogs pressings table.
  - Source badges per pressing: `📀 Collection`, `🎯 Wantlist`, `Spotify`.
  - Empty-state message when no pressings are linked yet.
  - Discogs release link replaced with inline SVG Discogs logo icon button (`discogs-icon-btn` in `app.js`).
  - Cleaner header status tags: `📀 In Collection` / `🎯 On Wantlist`.
- [x] **Track Row Navigation to Album Details**: Returned `album_id` in track API responses (`TrackDetail` struct) and added clickable row handlers (`.clickable-track-row`) across tracklists to navigate directly to the track's album detail view.
- [x] **Unified SVG Fallback Cover Art**: Integrated standard inline SVG vinyl fallback image (`fallbackCover`) across album cards, playlist collages, and pressing thumbnails with `onerror` safety to eliminate broken image icons and layout flicker.
- [x] **Discogs & Qobuz Search Buttons**: Added action buttons to the Pressings section header on Album Detail pages to open Discogs Master releases (`https://www.discogs.com/master/:id`), search Discogs (`https://www.discogs.com/search/?q=...&type=master`), and search the Qobuz Download Store (`https://www.qobuz.com/gb-en/search/albums/...`).
- [x] **One-time Spotify Account Import** *(completed 2026-08-10)*: Imported 76 owned playlists (19,607 track memberships, 14,517 unique tracks) directly from Spotify Web API via OAuth Authorization Code flow (`-import-spotify-account` CLI flag). Earliest track `added_at` used as playlist `created_at` proxy for sort ordering. Followed/external playlists skipped per Spotify API rules. No refresh-token storage or recurring sync.
- [x] **Playlist Management CRUD & Curation**:
  - Full REST endpoints (`POST/PUT/DELETE /api/playlists`, `POST/DELETE /api/playlists/:id/tracks`, `POST /api/playlists/:id/tracks/reorder`).
  - Interactive UI modal dialogs to create/edit playlists and select target playlists for adding tracks.
  - Track-level curation controls (Add to playlist `+` button across tracklists, Move Up `▲` / Move Down `▼` reordering, and Remove `✕` buttons in playlist view).
  - Sidebar `+` button to create new internal playlists and header edit/delete banner actions.

---

## 🗺️ Completed Roadmap Items

- [x] **Apple Music Import** *(completed 2026-08-10)*: Integrated XML parser (`apple_music.go`) and CLI flag `-import-apple-music <path>`. Imported 3,556 tracks, 1,888 new albums, and 35 playlists from `/Users/daniel/apple-music-library/Library.xml` into SQLite. Track ISRCs and titles matched against existing catalog.
- [x] **Historical Shazam Screenshot Import** *(completed 2026-08-10)*: OCR/extracted 30 recent Shazam history screenshots from `/Users/daniel/Downloads/shazam` and auto-populated corresponding monthly playlists (`2026-08`, `2026-07`, `2026-06`). Real album titles re-linked and deduplicated against canonical master albums.
- [x] **Track Deduplication & Duration Cleanup** *(completed 2026-08-10)*: Deduplicated 602 redundant track records across identical albums and reassigned 897 playlist memberships. Cleaned 6 empty title tracks and populated missing track durations via iTunes API.
- [x] **Batch High-Res Cover Art Fetch** *(completed 2026-08-10)*: Populated high-res 600x600 artwork for missing albums via iTunes Search API with fallback SVG cover handling.

---

## 🚫 Explicit Non-Goals

- **Local audio playback / streaming**: not part of Groovebox's scope — this is an archival/indexing tool, not a player.
- **Discogs OAuth login UI**: no in-app Discogs authentication flow; sync continues to use a token from `.env`.

---

## 📋 Recommended Future Tasks

1. **Format & Genre Sub-Filters**: Add additional pill filters for media format (Vinyl LP, CD, Digital) and master genres within the Collection/Wantlist views.
