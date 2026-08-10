# 🎵 Groovebox

A self-hosted, local-first music archival engine that consolidates physical media (Discogs collection & wantlist), streaming catalogs (Spotify, Apple Music), Shazam tags, and custom playlists into a single portable SQLite database with instant full-text search and a sleek mobile-first web interface.

---

## 🎯 Architecture Overview

- **Backend:** Go (`net/http`, pure standard library server)
- **Database:** SQLite via `modernc.org/sqlite` (Pure Go, CGO-free, portable single binary)
- **Search Engine:** SQLite FTS5 (Porter stemmer + unicode61 tokenizer for instant full-text search)
- **Frontend:** Vanilla HTML5 + CSS3 (Glassmorphism dark mode with Blueprint BP5) + Vanilla JavaScript
- **Features:** Collapsible sidebar, creation date playlist sorting, 2x2 grid collage playlist covers, Library view switching (All Songs, Artists with avatars, Albums grid, Dedicated Album & Artist pages)
- **Networking:** Localhost & Tailscale VPN ready

---

## 🚀 Running Locally

### 1. Build & Run
```bash
# Build binary
go build -o groovebox .

# Run Web Server (defaults to port 8080)
./groovebox -port 8080
```

### 2. Sync Discogs Collection & Wantlist
```bash
./groovebox -sync-discogs
```

### 3. Import Spotify / Notion CSV Playlists
```bash
./groovebox -import-spotify /path/to/spotify_csv_folder
```

Access the UI locally at `http://localhost:8080`.

---

## 🔌 API Endpoints

- `GET /api/stats` - Total tracks, canonical albums, and playlists count.
- `GET /api/playlists?sort=[date_desc|date_asc|name_asc|name_desc]` - List playlists with track counts, creation dates, and 4 cover art URLs.
- `GET /api/playlists/:id` - Get tracks inside a specific playlist.
- `GET /api/tracks` - Browse all songs in the library.
- `GET /api/artists` - Browse all artists (aggregating albums & tracks) with cover art avatars.
- `GET /api/artists/:name` - Get dedicated artist detail view with albums grid & tracks.
- `GET /api/albums?filter=[collection|wantlist]` - Browse canonical master albums. Optional filter by `collection` (has_vinyl=1) or `wantlist` (in_wantlist=1).
- `GET /api/albums/counts` - Get `{all, collection, wantlist}` album counts for UI badge display.
- `GET /api/albums/:id` - Get dedicated album detail view (Discogs pressings table with thumbnails & tracklist).
- `GET /api/search?q=:query` - Instant FTS5 full-text search across songs, artists, and releases.
- `POST /api/sync/discogs` - Trigger async Discogs collection & wantlist sync.
- `GET /api/sync/status` - Thread-safe live progress streaming (*stage*, *current_page*, *total_pages*, *items_fetched*, *last_synced_at*).

---

## 🌐 Deploying to Home Server over Tailscale

To access your music archive securely from your phone or laptop anywhere in the world without exposing ports to the public internet, deploy `groovebox` to your Home Server via [Tailscale](https://tailscale.com).

### Step 1: Build for Linux (if running on a Linux home server / Raspberry Pi)
Cross-compile a CGO-free static binary from your Mac:

```bash
# For Linux x86_64 server
GOOS=linux GOARCH=amd64 go build -o groovebox-linux .

# For ARM64 server / Raspberry Pi 4/5
GOOS=linux GOARCH=arm64 go build -o groovebox-arm64 .
```

### Step 2: Copy Files to Home Server
Copy the executable, database, and public web assets to your home server:

```bash
scp groovebox-linux user@homeserver:/opt/groovebox/groovebox
scp music.db user@homeserver:/opt/groovebox/music.db
scp -r public user@homeserver:/opt/groovebox/public
```

### Step 3: Run as a systemd Service (Recommended)
On your home server, create `/etc/systemd/system/groovebox.service`:

```ini
[Unit]
Description=Groovebox Archival System
After=network.target

[Service]
Type=simple
User=daniel
WorkingDirectory=/opt/groovebox
ExecStart=/opt/groovebox/groovebox -port 8080 -db /opt/groovebox/music.db
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start the service:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now groovebox
```

### Step 4: Access via Tailscale

1. Ensure **Tailscale** is running on your home server:
   ```bash
   tailscale status
   ```
2. Note your home server's Tailscale hostname or IP (e.g. `homeserver.tail1234.ts.net` or `100.x.y.z`).
3. Open `http://homeserver.tail1234.ts.net:8080` or `http://100.x.y.z:8080` from any device on your Tailnet (iPhone, Mac, iPad).

*(Optional)* Serve over HTTPS with standard port 443 using **Tailscale Serve**:
```bash
sudo tailscale serve --bg 8080
```
Now access securely at `https://homeserver.tail1234.ts.net` on any device!

### 🧪 E2E UI & Visual Testing (Playwright)
To visually inspect UI changes or capture layout screenshots end-to-end:
```bash
npx -y playwright screenshot http://localhost:8080 screenshot.png
```

---

## 🗂️ Database Schema

- **`albums`**: 1-to-1 canonical master release entities (`discogs_master_id`, title, artist, release year, cover image, collection vinyl & wantlist flags).
- **`release_versions`**: Specific Discogs physical pressings & digital entries (`album_id`, `discogs_release_id`, label, cat#, format, source).
- **`tracks`**: Individual songs linked to canonical albums (title, artist, duration, Spotify ID, Shazam ID, ISRC).
- **`playlists`**: Internal playlists.
- **`playlist_tracks`**: Ordered track mappings.
- **`search_fts`**: FTS5 virtual table for fast searching.
