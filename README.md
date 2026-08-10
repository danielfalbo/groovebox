# 🎵 Music Vault & Archival System

A self-hosted, local-first music archival engine that consolidates physical media (Discogs), streaming catalogs (Spotify, Apple Music), Shazam tags, and custom playlists into a single portable SQLite database with instant full-text search and a sleek mobile-first web interface.

---

## 🎯 Architecture Overview

- **Backend:** Go (`net/http`, pure standard library server)
- **Database:** SQLite via `modernc.org/sqlite` (Pure Go, CGO-free, portable single binary)
- **Search Engine:** SQLite FTS5 (Porter stemmer + unicode61 tokenizer for instant full-text search)
- **Frontend:** Vanilla HTML5 + CSS3 (Glassmorphism dark mode) + Vanilla JavaScript
- **Features:** Collapsible sidebar, creation date playlist sorting, 2x2 grid collage playlist covers, Library view switching (All Songs, Artists, Albums grid)
- **Networking:** Localhost & Tailscale VPN ready

---

## 🚀 Running Locally

### 1. Build & Run
```bash
# Build binary
go build -o music-vault .

# Run Web Server (defaults to port 8080)
./music-vault -port 8080
```

### 2. Import Spotify / Notion CSV Playlists
```bash
./music-vault -import-spotify /path/to/spotify_csv_folder
```

Access the UI locally at `http://localhost:8080`.

---

## 🔌 API Endpoints

- `GET /api/stats` - Total tracks, releases, and playlists count.
- `GET /api/playlists?sort=[date_desc|date_asc|name_asc|name_desc]` - List playlists with track counts, creation dates, and 4 cover art URLs.
- `GET /api/playlists/:id` - Get tracks inside a specific playlist.
- `GET /api/tracks` - Browse all songs in the library.
- `GET /api/artists` - Browse all artists with track counts.
- `GET /api/albums` - Browse all album releases with cover art.
- `GET /api/search?q=:query` - Instant FTS5 full-text search across songs, artists, and releases.

---

## 🌐 Deploying to Home Server over Tailscale

To access your music archive securely from your phone or laptop anywhere in the world without exposing ports to the public internet, deploy `music-vault` to your Home Server via [Tailscale](https://tailscale.com).

### Step 1: Build for Linux (if running on a Linux home server / Raspberry Pi)
Cross-compile a CGO-free static binary from your Mac:

```bash
# For Linux x86_64 server
GOOS=linux GOARCH=amd64 go build -o music-vault-linux .

# For ARM64 server / Raspberry Pi 4/5
GOOS=linux GOARCH=arm64 go build -o music-vault-arm64 .
```

### Step 2: Copy Files to Home Server
Copy the executable, database, and public web assets to your home server:

```bash
scp music-vault-linux user@homeserver:/opt/music-vault/music-vault
scp music.db user@homeserver:/opt/music-vault/music.db
scp -r public user@homeserver:/opt/music-vault/public
```

### Step 3: Run as a systemd Service (Recommended)
On your home server, create `/etc/systemd/system/music-vault.service`:

```ini
[Unit]
Description=Music Vault Archival System
After=network.target

[Service]
Type=simple
User=daniel
WorkingDirectory=/opt/music-vault
ExecStart=/opt/music-vault/music-vault -port 8080 -db /opt/music-vault/music.db
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start the service:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now music-vault
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

- **`releases`**: Albums, vinyl records, digital releases (title, artist, release year, Discogs ID, cover art).
- **`tracks`**: Individual songs (title, artist, duration, Spotify ID, Shazam ID, ISRC).
- **`playlists`**: Internal playlists.
- **`playlist_tracks`**: Ordered track mappings.
- **`search_fts`**: FTS5 virtual table for fast searching.
