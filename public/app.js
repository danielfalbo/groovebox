document.addEventListener('DOMContentLoaded', () => {
  loadStats();
  loadPlaylists();
});

async function loadStats() {
  try {
    const res = await fetch('/api/stats');
    const data = await res.json();
    document.getElementById('stat-tracks').textContent = data.total_tracks.toLocaleString();
    document.getElementById('stat-releases').textContent = data.total_releases.toLocaleString();
    document.getElementById('stat-playlists').textContent = data.total_playlists.toLocaleString();
  } catch (err) {
    console.error('Failed to load stats:', err);
  }
}

async function loadPlaylists() {
  try {
    const res = await fetch('/api/playlists');
    const playlists = await res.json();
    const container = document.getElementById('sidebar-playlists');
    container.innerHTML = '';

    playlists.forEach(p => {
      const a = document.createElement('a');
      a.className = 'bp5-menu-item bp5-popover-dismiss playlist-item-btn';
      a.onclick = () => selectPlaylist(p.id, p.name, p.description, a);
      a.innerHTML = `
        <span class="bp5-text-overflow-ellipsis">${p.name}</span>
        <span class="bp5-tag bp5-minimal bp5-round">${p.track_count}</span>
      `;
      container.appendChild(a);
    });

    if (playlists.length > 0) {
      const firstBtn = container.querySelector('.playlist-item-btn');
      selectPlaylist(playlists[0].id, playlists[0].name, playlists[0].description, firstBtn);
    }
  } catch (err) {
    console.error('Failed to load playlists:', err);
  }
}

async function selectPlaylist(id, name, description, element) {
  document.getElementById('view-title').textContent = name;
  document.getElementById('view-subtitle').textContent = description || 'Playlist tracks';
  
  document.querySelectorAll('.playlist-item-btn').forEach(el => el.classList.remove('bp5-active'));
  if (element) {
    element.classList.add('bp5-active');
  }
  
  try {
    const res = await fetch(`/api/playlists/${id}`);
    const tracks = await res.json();
    renderTracks(tracks);
  } catch (err) {
    console.error('Failed to load playlist tracks:', err);
  }
}

let searchDebounce = null;
function handleSearch(query) {
  clearTimeout(searchDebounce);
  if (!query.trim()) {
    return;
  }
  
  searchDebounce = setTimeout(async () => {
    document.getElementById('view-title').textContent = `Search: "${query}"`;
    document.getElementById('view-subtitle').textContent = 'FTS Full-Text Search Results';
    document.querySelectorAll('.playlist-item-btn').forEach(el => el.classList.remove('bp5-active'));
    
    try {
      const res = await fetch(`/api/search?q=${encodeURIComponent(query)}`);
      const tracks = await res.json();
      renderTracks(tracks);
    } catch (err) {
      console.error('Search failed:', err);
    }
  }, 250);
}

function showDashboard() {
  loadPlaylists();
}

function renderTracks(tracks) {
  const tbody = document.getElementById('tracklist-body');
  tbody.innerHTML = '';

  if (!tracks || tracks.length === 0) {
    tbody.innerHTML = `<tr><td colspan="5" style="text-align: center; padding: 24px;" class="bp5-text-muted">No tracks found</td></tr>`;
    return;
  }

  tracks.forEach((t, i) => {
    const tr = document.createElement('tr');
    const duration = formatDuration(t.duration_ms);
    const coverUrl = t.cover_image_url || 'https://via.placeholder.com/36';

    tr.innerHTML = `
      <td>${t.position || i + 1}</td>
      <td>
        <div class="track-meta">
          <img class="cover-art-small" src="${coverUrl}" alt="cover" onerror="this.src='https://via.placeholder.com/36'">
          <div>
            <div class="track-title-text">${t.title}</div>
            <div class="track-artist-text">${t.artist}</div>
          </div>
        </div>
      </td>
      <td>${t.album_title || '-'}</td>
      <td>${duration}</td>
      <td style="text-align: right;">
        ${t.spotify_id ? `<a class="bp5-button bp5-minimal bp5-intent-success bp5-small" href="https://open.spotify.com/track/${t.spotify_id}" target="_blank">Spotify ↗</a>` : '-'}
      </td>
    `;
    tbody.appendChild(tr);
  });
}

function formatDuration(ms) {
  if (!ms) return '0:00';
  const totalSec = Math.floor(ms / 1000);
  const min = Math.floor(totalSec / 60);
  const sec = totalSec % 60;
  return `${min}:${sec < 10 ? '0' : ''}${sec}`;
}
