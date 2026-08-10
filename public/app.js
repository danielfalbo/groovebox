let currentPlaylistSort = localStorage.getItem('playlist-sort') || 'date_desc';

document.addEventListener('DOMContentLoaded', () => {
  initSidebarState();
  initSortState();
  loadPlaylists();
});

async function loadPlaylists() {
  try {
    const res = await fetch(`/api/playlists?sort=${currentPlaylistSort}`);
    const playlists = await res.json();
    const container = document.getElementById('sidebar-playlists');
    container.innerHTML = '';

    playlists.forEach(p => {
      const a = document.createElement('a');
      a.className = 'bp5-menu-item bp5-popover-dismiss playlist-item-btn';
      a.onclick = () => selectPlaylist(p.id, p.name, p.description, a);
      const formattedDate = formatDate(p.created_at);

      // Render 2x2 grid cover images (filling with placeholders if < 4)
      const urls = p.cover_art_urls || [];
      let coverHTML = '';
      if (urls.length > 0) {
        const gridImgs = [];
        for (let i = 0; i < 4; i++) {
          const imgUrl = urls[i % urls.length] || 'https://via.placeholder.com/38';
          gridImgs.push(`<img class="playlist-cover-img" src="${imgUrl}" alt="art" onerror="this.src='https://via.placeholder.com/38'">`);
        }
        coverHTML = `<div class="playlist-cover-grid">${gridImgs.join('')}</div>`;
      } else {
        coverHTML = `<div class="playlist-cover-fallback"><span class="bp5-icon-standard bp5-icon-music"></span></div>`;
      }

      a.innerHTML = `
        <div class="playlist-item-left">
          ${coverHTML}
          <div class="playlist-info">
            <span class="bp5-text-overflow-ellipsis playlist-name-text">${p.name}</span>
            ${formattedDate ? `<span class="playlist-date-text">${formattedDate}</span>` : ''}
          </div>
        </div>
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

function clearNavActive() {
  document.querySelectorAll('.playlist-item-btn, .nav-item-btn').forEach(el => el.classList.remove('bp5-active'));
}

async function showAllSongs() {
  clearNavActive();
  document.getElementById('nav-all-songs').classList.add('bp5-active');
  document.getElementById('view-title').textContent = 'All Songs';
  document.getElementById('view-subtitle').textContent = 'Complete music library tracks';
  
  document.getElementById('grid-container').style.display = 'none';
  document.getElementById('table-container').style.display = 'block';

  try {
    const res = await fetch('/api/tracks');
    const tracks = await res.json();
    renderTracks(tracks);
  } catch (err) {
    console.error('Failed to load tracks:', err);
  }
}

async function showArtists() {
  clearNavActive();
  document.getElementById('nav-artists').classList.add('bp5-active');
  document.getElementById('view-title').textContent = 'Artists';
  document.getElementById('view-subtitle').textContent = 'Browse library by artist';
  
  document.getElementById('table-container').style.display = 'none';
  const grid = document.getElementById('grid-container');
  grid.style.display = 'grid';
  grid.innerHTML = '';

  try {
    const res = await fetch('/api/artists');
    const artists = await res.json();
    
    if (!artists || artists.length === 0) {
      grid.innerHTML = '<div class="bp5-text-muted">No artists found</div>';
      return;
    }

    artists.forEach(a => {
      const card = document.createElement('div');
      card.className = 'grid-card';
      card.onclick = () => {
        const searchInput = document.getElementById('search-input');
        searchInput.value = a.name;
        handleSearch(a.name);
      };
      card.innerHTML = `
        <div class="grid-card-icon">
          <span class="bp5-icon-standard bp5-icon-user"></span>
        </div>
        <div class="grid-card-title">${a.name}</div>
        <div class="grid-card-subtitle">${a.track_count} ${a.track_count === 1 ? 'track' : 'tracks'}</div>
      `;
      grid.appendChild(card);
    });
  } catch (err) {
    console.error('Failed to load artists:', err);
  }
}

async function showAlbums() {
  clearNavActive();
  document.getElementById('nav-albums').classList.add('bp5-active');
  document.getElementById('view-title').textContent = 'Albums & Releases';
  document.getElementById('view-subtitle').textContent = 'Browse releases, vinyl collection, and wantlist';
  
  document.getElementById('table-container').style.display = 'none';
  const grid = document.getElementById('grid-container');
  grid.style.display = 'grid';
  grid.innerHTML = '';

  try {
    const res = await fetch('/api/albums');
    const albums = await res.json();
    
    if (!albums || albums.length === 0) {
      grid.innerHTML = '<div class="bp5-text-muted">No albums found</div>';
      return;
    }

    albums.forEach(alb => {
      const card = document.createElement('div');
      card.className = 'grid-card';
      card.onclick = () => {
        const searchInput = document.getElementById('search-input');
        searchInput.value = alb.title;
        handleSearch(alb.title);
      };
      const coverUrl = alb.cover_image_url || 'https://via.placeholder.com/180';
      
      let badgeHTML = '';
      if (alb.has_vinyl) {
        badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">📀 Vinyl</span>`;
      } else if (alb.streaming_notes === 'Discogs Wantlist') {
        badgeHTML = `<span class="bp5-tag bp5-intent-primary bp5-round album-badge">🎯 Wantlist</span>`;
      }

      card.innerHTML = `
        <div class="grid-card-art-wrap">
          <img class="grid-card-art" src="${coverUrl}" alt="cover" onerror="this.src='https://via.placeholder.com/180'">
          ${badgeHTML}
        </div>
        <div class="grid-card-title">${alb.title}</div>
        <div class="grid-card-subtitle">${alb.artist}${alb.release_year ? ' • ' + alb.release_year : ''}</div>
      `;
      grid.appendChild(card);
    });
  } catch (err) {
    console.error('Failed to load albums:', err);
  }
}

async function triggerDiscogsSync() {
  const btn = document.getElementById('discogs-sync-btn');
  if (!btn) return;
  
  const originalText = btn.textContent;
  btn.disabled = true;
  btn.textContent = 'Syncing Discogs...';

  try {
    const res = await fetch('/api/sync/discogs', { method: 'POST' });
    const data = await res.json();
    if (res.ok) {
      alert('Discogs sync complete!');
      showAlbums();
    } else {
      alert('Discogs sync error: ' + (data.error || 'Failed to sync'));
    }
  } catch (err) {
    alert('Failed to connect to Discogs sync endpoint: ' + err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = originalText;
  }
}

async function selectPlaylist(id, name, description, element) {
  clearNavActive();
  document.getElementById('view-title').textContent = name;
  document.getElementById('view-subtitle').textContent = description || 'Playlist tracks';
  
  document.getElementById('grid-container').style.display = 'none';
  document.getElementById('table-container').style.display = 'block';

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
    clearNavActive();
    document.getElementById('view-title').textContent = `Search: "${query}"`;
    document.getElementById('view-subtitle').textContent = 'FTS Full-Text Search Results';
    
    document.getElementById('grid-container').style.display = 'none';
    document.getElementById('table-container').style.display = 'block';

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

function formatDate(dateStr) {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}
