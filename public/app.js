let currentPlaylistSort = localStorage.getItem('playlist-sort') || 'date_desc';
const fallbackCover = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='180' height='180'%3E%3Crect width='180' height='180' fill='%23252a31' rx='6'/%3E%3Cpath fill='%23738091' d='M70 120v-40l40-10v42.5a10 10 0 1 1-10-9.5V85l-20 5v32.5A10 10 0 1 1 70 120z'/%3E%3C/svg%3E`;

document.addEventListener('DOMContentLoaded', () => {
  initSidebarState();
  initSortState();
  loadPlaylists();
  startSyncPolling();
  showAlbums('collection');
});

function toggleSidebar() {
  const sidebar = document.getElementById('sidebar-panel');
  const isCollapsed = sidebar.classList.toggle('collapsed');
  localStorage.setItem('sidebar-collapsed', isCollapsed ? 'true' : 'false');
}

function initSidebarState() {
  if (localStorage.getItem('sidebar-collapsed') === 'true') {
    const sidebar = document.getElementById('sidebar-panel');
    sidebar.classList.add('collapsed');
  }
}

function initSortState() {
  const select = document.getElementById('playlist-sort-select');
  if (select) {
    select.value = currentPlaylistSort;
  }
}

function handleSortChange(sortValue) {
  currentPlaylistSort = sortValue;
  localStorage.setItem('playlist-sort', sortValue);
  loadPlaylists();
}

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

      const urls = p.cover_art_urls || [];
      let coverHTML = '';
      if (urls.length > 0) {
        const gridImgs = [];
        for (let i = 0; i < 4; i++) {
          const imgUrl = urls[i % urls.length] || fallbackCover;
          gridImgs.push(`<img class="playlist-cover-img" src="${imgUrl}" alt="art" onerror="this.onerror=null;this.src='${fallbackCover}'">`);
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
  } catch (err) {
    console.error('Failed to load playlists:', err);
  }
}

let currentSectionView = ''; // 'songs', 'artists', 'albums', 'playlist'
let rawSectionData = [];

function clearNavActive() {
  document.querySelectorAll('.playlist-item-btn, .nav-item-btn').forEach(el => el.classList.remove('bp5-active'));
  document.getElementById('album-detail-container').style.display = 'none';
  const artistContainer = document.getElementById('artist-detail-container');
  if (artistContainer) artistContainer.style.display = 'none';
  
  const filterInput = document.getElementById('section-filter-input');
  if (filterInput) filterInput.value = '';
}

let activeAlbumFilter = 'collection';

function showSectionFilter(placeholder, showPills = false) {
  const wrap = document.getElementById('section-filter-wrap');
  const input = document.getElementById('section-filter-input');
  const pills = document.getElementById('album-filter-pills');
  if (wrap && input) {
    wrap.style.display = 'block';
    input.placeholder = placeholder;
    input.value = '';
  }
  if (pills) {
    pills.style.display = showPills ? 'flex' : 'none';
  }
}

function hideSectionFilter() {
  const wrap = document.getElementById('section-filter-wrap');
  if (wrap) wrap.style.display = 'none';
}

async function updateAlbumCounts() {
  try {
    const res = await fetch('/api/albums/counts');
    if (res.ok) {
      const data = await res.json();
      document.getElementById('count-all').innerText = data.all.toLocaleString();
      document.getElementById('count-collection').innerText = data.collection.toLocaleString();
      document.getElementById('count-wantlist').innerText = data.wantlist.toLocaleString();
    }
  } catch (err) {
    console.error('Failed to update album counts:', err);
  }
}

async function setAlbumFilter(filter) {
  activeAlbumFilter = filter;
  document.querySelectorAll('#album-filter-pills .bp5-button').forEach(btn => btn.classList.remove('bp5-active'));
  document.getElementById(`pill-${filter}`).classList.add('bp5-active');

  const grid = document.getElementById('grid-container');
  grid.style.display = 'grid';
  grid.innerHTML = `
    <div style="grid-column: 1 / -1; text-align: center; padding: 48px;">
      <div class="bp5-spinner bp5-intent-primary" style="margin: 0 auto;">
        <div class="bp5-spinner-head"></div>
      </div>
      <div class="bp5-text-muted" style="margin-top: 12px; font-size: 13px;">Loading ${filter}...</div>
    </div>
  `;

  try {
    const filterInput = document.getElementById('section-filter-input');
    const q = filterInput ? filterInput.value.trim() : '';

    let params = new URLSearchParams();
    if (filter !== 'all') params.append('filter', filter);
    if (q) params.append('q', q);

    const url = `/api/albums${params.toString() ? '?' + params.toString() : ''}`;
    const res = await fetch(url);
    rawSectionData = await res.json();
    renderAlbumCards(rawSectionData);
  } catch (err) {
    console.error('Failed to load filtered albums:', err);
  }
}

let albumSearchDebounceTimer = null;

function handleSectionFilter(query) {
  const q = (query || '').toLowerCase().trim();

  if (currentSectionView === 'songs') {
    if (!q) {
      renderTracks(rawSectionData);
      return;
    }
    const filtered = rawSectionData.filter(t => 
      (t.title && t.title.toLowerCase().includes(q)) ||
      (t.artist && t.artist.toLowerCase().includes(q)) ||
      (t.album_title && t.album_title.toLowerCase().includes(q))
    );
    renderTracks(filtered);
  } else if (currentSectionView === 'artists') {
    const grid = document.getElementById('grid-container');
    grid.innerHTML = '';
    const filtered = !q ? rawSectionData : rawSectionData.filter(a => a.name && a.name.toLowerCase().includes(q));
    
    if (filtered.length === 0) {
      grid.innerHTML = '<div class="bp5-text-muted">No matching artists</div>';
      return;
    }
    renderArtistCards(filtered);
  } else if (currentSectionView === 'albums') {
    if (albumSearchDebounceTimer) clearTimeout(albumSearchDebounceTimer);
    
    albumSearchDebounceTimer = setTimeout(async () => {
      const grid = document.getElementById('grid-container');
      try {
        let params = new URLSearchParams();
        if (activeAlbumFilter !== 'all') params.append('filter', activeAlbumFilter);
        if (q) params.append('q', q);

        const url = `/api/albums${params.toString() ? '?' + params.toString() : ''}`;
        const res = await fetch(url);
        const albums = await res.json();

        if (!albums || albums.length === 0) {
          grid.innerHTML = '<div class="bp5-text-muted" style="grid-column: 1 / -1; text-align: center; padding: 24px;">No matching albums</div>';
          return;
        }
        renderAlbumCards(albums);
      } catch (err) {
        console.error('Failed to search albums:', err);
      }
    }, 200);
  }
}

async function showAllSongs() {
  clearNavActive();
  currentSectionView = 'songs';
  showSectionFilter('Filter songs by title, artist, album...');
  document.getElementById('nav-all-songs').classList.add('bp5-active');
  
  document.getElementById('grid-container').style.display = 'none';
  document.getElementById('table-container').style.display = 'block';

  const tbody = document.getElementById('tracklist-body');
  tbody.innerHTML = `
    <tr>
      <td colspan="5" style="text-align: center; padding: 32px;">
        <div class="bp5-spinner bp5-intent-primary bp5-small" style="margin: 0 auto;">
          <div class="bp5-spinner-head"></div>
        </div>
        <div class="bp5-text-muted" style="margin-top: 8px; font-size: 12px;">Loading songs...</div>
      </td>
    </tr>
  `;

  try {
    const res = await fetch('/api/tracks');
    rawSectionData = await res.json();
    renderTracks(rawSectionData);
  } catch (err) {
    console.error('Failed to load tracks:', err);
  }
}

async function showArtists() {
  clearNavActive();
  currentSectionView = 'artists';
  showSectionFilter('Filter artists by name...');
  document.getElementById('nav-artists').classList.add('bp5-active');
  
  document.getElementById('table-container').style.display = 'none';
  const grid = document.getElementById('grid-container');
  grid.style.display = 'grid';
  grid.innerHTML = `
    <div style="grid-column: 1 / -1; text-align: center; padding: 48px;">
      <div class="bp5-spinner bp5-intent-primary" style="margin: 0 auto;">
        <div class="bp5-spinner-head"></div>
      </div>
      <div class="bp5-text-muted" style="margin-top: 12px; font-size: 13px;">Loading artists...</div>
    </div>
  `;

  try {
    const res = await fetch('/api/artists');
    rawSectionData = await res.json();
    renderArtistCards(rawSectionData);
  } catch (err) {
    console.error('Failed to load artists:', err);
  }
}

function renderArtistCards(artists) {
  const grid = document.getElementById('grid-container');
  grid.innerHTML = '';
  if (!artists || artists.length === 0) {
    grid.innerHTML = '<div class="bp5-text-muted" style="grid-column: 1 / -1; text-align: center; padding: 24px;">No artists found</div>';
    return;
  }

  artists.forEach(a => {
    const card = document.createElement('div');
    card.className = 'grid-card';
    card.onclick = () => openArtistPage(a.name);
    let parts = [];
    if (a.album_count > 0) parts.push(`${a.album_count} ${a.album_count === 1 ? 'album' : 'albums'}`);
    if (a.track_count > 0) parts.push(`${a.track_count} ${a.track_count === 1 ? 'track' : 'tracks'}`);
    const subtitle = parts.length > 0 ? parts.join(' • ') : '0 items';

    let avatarHTML = '';
    if (a.image_url) {
      avatarHTML = `<img class="grid-card-artist-avatar" src="${a.image_url}" alt="${a.name}" onerror="this.outerHTML='<div class=\\'grid-card-icon\\'><span class=\\'bp5-icon-standard bp5-icon-user\\'></span></div>'">`;
    } else {
      avatarHTML = `<div class="grid-card-icon"><span class="bp5-icon-standard bp5-icon-user"></span></div>`;
    }

    card.innerHTML = `
      ${avatarHTML}
      <div class="grid-card-title">${a.name}</div>
      <div class="grid-card-subtitle">${subtitle}</div>
    `;
    grid.appendChild(card);
  });
}

async function showAlbums(targetFilter) {
  clearNavActive();
  currentSectionView = 'albums';
  showSectionFilter('Filter albums by title, artist...', true);
  document.getElementById('nav-albums').classList.add('bp5-active');
  
  document.getElementById('table-container').style.display = 'none';
  
  updateAlbumCounts();
  setAlbumFilter(targetFilter || activeAlbumFilter);
}

function renderAlbumCards(albums) {
  const grid = document.getElementById('grid-container');
  grid.innerHTML = '';
  if (!albums || albums.length === 0) {
    grid.innerHTML = '<div class="bp5-text-muted">No albums found</div>';
    return;
  }

  albums.forEach(alb => {
    const card = document.createElement('div');
    card.className = 'grid-card';
    card.onclick = () => openAlbumPage(alb.id);

    const coverUrl = alb.cover_image_url || fallbackCover;
    
    let badgeHTML = '';
    const fmt = (alb.primary_format || '').toLowerCase();
    
    if (alb.has_vinyl || fmt.includes('vinyl')) {
      badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">📀 Vinyl</span>`;
    } else if (fmt.includes('file') || fmt.includes('mp3') || fmt.includes('wav') || fmt.includes('flac')) {
      badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">📁 Files</span>`;
    } else if (fmt.includes('sacd')) {
      badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">💿 SACD</span>`;
    } else if (fmt.includes('cd')) {
      badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">💿 CD</span>`;
    } else if (fmt.includes('cassette')) {
      badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">📻 Cassette</span>`;
    } else if (fmt.includes('flexi')) {
      badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">💿 Flexi</span>`;
    } else if (alb.in_collection) {
      badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">📀 Collection</span>`;
    } else if (alb.in_wantlist) {
      badgeHTML = `<span class="bp5-tag bp5-intent-primary bp5-round album-badge">🎯 Wantlist</span>`;
    }

    let versionLabel = `${alb.version_count} ${alb.version_count === 1 ? 'version' : 'versions'}`;
    if (alb.version_count === 0) {
      versionLabel = `${alb.track_count} tracks`;
    }

    card.innerHTML = `
      <div class="grid-card-art-wrap">
        <img class="grid-card-art" src="${coverUrl}" alt="cover" onerror="this.onerror=null;this.src='${fallbackCover}'">
        ${badgeHTML}
      </div>
      <div class="grid-card-title">${alb.title}</div>
      <div class="grid-card-subtitle">${alb.artist}${alb.release_year ? ' • ' + alb.release_year : ''}</div>
      <div class="grid-card-version-count">${versionLabel}</div>
    `;
    grid.appendChild(card);
  });
}

async function openArtistPage(artistName) {
  clearNavActive();
  hideSectionFilter();
  document.getElementById('grid-container').style.display = 'none';
  document.getElementById('table-container').style.display = 'none';

  const container = document.getElementById('artist-detail-container');
  container.style.display = 'block';
  container.innerHTML = '<div class="bp5-spinner bp5-intent-primary"><div class="bp5-spinner-head"></div></div>';

  try {
    const res = await fetch(`/api/artists/${encodeURIComponent(artistName)}`);
    if (!res.ok) throw new Error('Artist not found');
    const artistData = await res.json();

    let albumsGridHTML = '';
    if (artistData.albums && artistData.albums.length > 0) {
      const cards = artistData.albums.map(alb => {
        const coverUrl = alb.cover_image_url || fallbackCover;
        let badgeHTML = '';
        if (alb.has_vinyl) {
          badgeHTML = `<span class="bp5-tag bp5-intent-warning bp5-round album-badge">📀 Vinyl</span>`;
        } else if (alb.in_wantlist) {
          badgeHTML = `<span class="bp5-tag bp5-intent-primary bp5-round album-badge">🎯 Wantlist</span>`;
        }
        let versionLabel = `${alb.version_count} ${alb.version_count === 1 ? 'version' : 'versions'}`;
        if (alb.version_count === 0) {
          versionLabel = `${alb.track_count} tracks`;
        }

        return `
          <div class="grid-card" onclick="openAlbumPage('${alb.id}')">
            <div class="grid-card-art-wrap">
              <img class="grid-card-art" src="${coverUrl}" alt="cover" onerror="this.onerror=null;this.src='${fallbackCover}'">
              ${badgeHTML}
            </div>
            <div class="grid-card-title">${alb.title}</div>
            <div class="grid-card-subtitle">${alb.release_year ? alb.release_year : 'Master Release'}</div>
            <div class="grid-card-version-count">${versionLabel}</div>
          </div>
        `;
      }).join('');

      albumsGridHTML = `
        <div class="album-section">
          <h4 class="bp5-heading section-heading"><span class="bp5-icon-standard bp5-icon-record"></span> Albums & Releases (${artistData.albums.length})</h4>
          <div class="grid-container" style="display: grid;">${cards}</div>
        </div>
      `;
    }

    let tracksHTML = '';
    if (artistData.tracks && artistData.tracks.length > 0) {
      const tRows = artistData.tracks.map((t, idx) => {
        const isClickable = Boolean(t.album_id);
        const trAttrs = isClickable 
          ? `class="clickable-track-row" title="View album: ${t.album_title || 'Album details'}" onclick="if(!event.target.closest('a, button, input, svg')) openAlbumPage('${t.album_id}')"`
          : '';
        return `
        <tr ${trAttrs}>
          <td>${idx + 1}</td>
          <td>
            <div class="track-title-text">${t.title}</div>
            <div class="track-artist-text">${t.album_title || '-'}</div>
          </td>
          <td>${formatDuration(t.duration_ms)}</td>
          <td style="text-align: right; white-space: nowrap;">
            <a href="https://www.youtube.com/results?search_query=${encodeURIComponent((t.artist || '') + ' ' + t.title)}" target="_blank" class="bp5-button bp5-minimal bp5-small youtube-icon-btn" title="Search on YouTube"><svg class="youtube-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z"/></svg></a>
            ${t.spotify_id ? `<a href="https://open.spotify.com/track/${t.spotify_id}" target="_blank" class="bp5-button bp5-minimal bp5-small spotify-icon-btn" title="Open in Spotify"><svg class="spotify-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm5.521 17.34c-.24.359-.66.48-1.021.24-2.82-1.74-6.36-2.101-10.561-1.141-.418.122-.779-.179-.899-.539-.12-.421.18-.78.54-.9 4.56-1.021 8.52-.6 11.64 1.32.42.18.479.659.301 1.02zm1.48-3.3c-.301.42-.841.6-1.262.3-3.239-1.98-8.159-2.58-11.939-1.38-.479.12-1.02-.12-1.14-.6-.12-.48.12-1.021.6-1.141 4.38-1.38 9.841-.72 13.44 1.5.42.301.6.841.301 1.32zm.12-3.36C15.24 8.4 8.82 8.16 5.16 9.301c-.6.179-1.2-.18-1.38-.72-.18-.6.18-1.2.72-1.38 4.26-1.26 11.28-1.02 15.72 1.62.539.301.719 1.02.419 1.56-.299.421-1.02.599-1.559.3z"/></svg></a>` : ''}
          </td>
        </tr>
      `;
      }).join('');

      tracksHTML = `
        <div class="album-section">
          <h4 class="bp5-heading section-heading"><span class="bp5-icon-standard bp5-icon-music"></span> Individual Tracks (${artistData.tracks.length})</h4>
          <table class="bp5-html-table bp5-html-table-striped bp5-compact full-width-table">
            <thead>
              <tr>
                <th style="width: 50px;">#</th>
                <th>Track / Album</th>
                <th style="width: 100px;">Duration</th>
                <th style="width: 100px; text-align: right;">Stream</th>
              </tr>
            </thead>
            <tbody>${tRows}</tbody>
          </table>
        </div>
      `;
    }

    let artistHeaderAvatar = '';
    const firstAlbumCover = (artistData.albums && artistData.albums.length > 0) ? artistData.albums[0].cover_image_url : '';
    if (firstAlbumCover) {
      artistHeaderAvatar = `<img class="artist-header-avatar" src="${firstAlbumCover}" alt="${artistData.name}" onerror="this.outerHTML='<div class=\\'grid-card-icon\\' style=\\'width: 96px; height: 96px; font-size: 36px; margin: 0;\\'><span class=\\'bp5-icon-standard bp5-icon-user\\'></span></div>'">`;
    } else {
      artistHeaderAvatar = `<div class="grid-card-icon" style="width: 96px; height: 96px; font-size: 36px; margin: 0;"><span class="bp5-icon-standard bp5-icon-user"></span></div>`;
    }

    container.innerHTML = `
      <button class="bp5-button bp5-minimal bp5-icon-arrow-left back-btn" onclick="showArtists()">Back to Artists</button>
      <div class="album-header-card bp5-card bp5-elevation-1">
        ${artistHeaderAvatar}
        <div class="album-header-info">
          <h1 class="bp5-heading album-header-title">${artistData.name}</h1>
          <div class="album-header-meta">
            <span>${artistData.albums ? artistData.albums.length : 0} Albums</span>
            <span>${artistData.tracks ? artistData.tracks.length : 0} Tracks</span>
          </div>
        </div>
      </div>
      ${albumsGridHTML}
      ${tracksHTML}
    `;
  } catch (err) {
    console.error('Failed to load artist page:', err);
    container.innerHTML = '<div class="bp5-callout bp5-intent-danger">Failed to load artist details.</div>';
  }
}

async function openAlbumPage(albumId) {
  clearNavActive();
  hideSectionFilter();
  const titleCard = document.getElementById('view-title-card');
  if (titleCard) titleCard.style.display = 'none';
  document.getElementById('grid-container').style.display = 'none';
  document.getElementById('table-container').style.display = 'none';

  const container = document.getElementById('album-detail-container');
  container.style.display = 'block';
  container.innerHTML = `
    <div style="text-align: center; padding: 48px;">
      <div class="bp5-spinner bp5-intent-primary" style="margin: 0 auto;">
        <div class="bp5-spinner-head"></div>
      </div>
      <div class="bp5-text-muted" style="margin-top: 12px; font-size: 13px;">Loading album details & pressings...</div>
    </div>
  `;

  try {
    const res = await fetch(`/api/albums/${albumId}`);
    if (!res.ok) throw new Error('Album not found');
    const album = await res.json();

    const coverUrl = album.cover_image_url || fallbackCover;
    let badgesHTML = '';
    if (album.has_vinyl) {
      badgesHTML += `<span class="bp5-tag bp5-intent-warning bp5-round album-detail-badge">📀 In Collection</span>`;
    }
    if (album.in_wantlist) {
      badgesHTML += `<span class="bp5-tag bp5-intent-primary bp5-round album-detail-badge">🎯 On Wantlist</span>`;
    }

    const discogsSvg = `<svg class="discogs-svg-icon" viewBox="0 0 24 24" fill="currentColor" style="display: inline-block; vertical-align: text-bottom; margin-right: 4px;"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm0 4.2a7.8 7.8 0 1 1 0 15.6 7.8 7.8 0 0 1 0-15.6zm0 4.2a3.6 3.6 0 1 0 0 7.2 3.6 3.6 0 0 0 0-7.2zm0 2.1a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3z"/></svg>`;
    const qobuzSvg = `<svg viewBox="0 0 24 24" fill="currentColor" style="width: 16px; height: 16px; display: inline-block; vertical-align: text-bottom; margin-right: 4px;"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 15c-2.76 0-5-2.24-5-5s2.24-5 5-5 5 2.24 5 5-2.24 5-5 5zm2.5-5a2.5 2.5 0 1 1-5 0 2.5 2.5 0 0 1 5 0z"/></svg>`;

    const searchQuery = encodeURIComponent(`${album.artist} ${album.title}`);
    const discogsSearchUrl = `https://www.discogs.com/search/?q=${searchQuery}&type=master`;
    const qobuzSearchUrl = `https://www.qobuz.com/gb-en/search/albums/${searchQuery}`;
    
    let discogsMasterBtn = '';
    if (album.discogs_master_id) {
      discogsMasterBtn = `<a href="https://www.discogs.com/master/${album.discogs_master_id}" target="_blank" class="bp5-button bp5-outlined bp5-small">${discogsSvg} Open Master #${album.discogs_master_id} ↗</a>`;
    }

    const discogsSearchBtn = `<a href="${discogsSearchUrl}" target="_blank" class="bp5-button bp5-outlined bp5-small">${discogsSvg} Search Discogs ↗</a>`;
    const qobuzSearchBtn = `<a href="${qobuzSearchUrl}" target="_blank" class="bp5-button bp5-outlined bp5-small">${qobuzSvg} Search Qobuz Store ↗</a>`;

    const pressingsActionBtns = `<div style="display: flex; align-items: center; gap: 8px;">${discogsMasterBtn}${discogsSearchBtn}${qobuzSearchBtn}</div>`;

    // Versions Table (Pressings & Wants)
    let versionsHTML = '';
    if (album.versions && album.versions.length > 0) {
      const vRows = album.versions.map(v => {
        let sourceBadge = '';
        if (v.source === 'collection' || v.has_vinyl) {
          sourceBadge = '<span class="bp5-tag bp5-intent-warning bp5-minimal bp5-round">📀 Collection</span>';
        } else if (v.source === 'wantlist') {
          sourceBadge = '<span class="bp5-tag bp5-intent-primary bp5-minimal bp5-round">🎯 Wantlist</span>';
        } else if (v.source === 'spotify') {
          sourceBadge = '<span class="bp5-tag bp5-intent-success bp5-minimal bp5-round">Spotify</span>';
        }

        const pressingArt = v.cover_image_url || coverUrl;

        return `
          <tr>
            <td style="width: 44px;">
              <img src="${pressingArt}" alt="pressing art" class="version-pressing-art" onerror="this.onerror=null;this.src='${fallbackCover}'">
            </td>
            <td>
              <div class="version-item-title">
                ${v.format_description ? `<span class="bp5-tag bp5-minimal">${v.format_description}</span>` : ''}
                <strong>${v.label || 'Discogs Pressing'}</strong> ${v.catalog_number ? `(${v.catalog_number})` : ''}
              </div>
            </td>
            <td>${v.release_year ? v.release_year : 'N/A'}</td>
            <td>${sourceBadge}</td>
            <td style="text-align: right;">
              ${v.discogs_release_id ? `<a href="https://www.discogs.com/release/${v.discogs_release_id}" target="_blank" class="bp5-button bp5-minimal bp5-small discogs-icon-btn" title="View on Discogs"><svg class="discogs-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm0 4.2a7.8 7.8 0 1 1 0 15.6 7.8 7.8 0 0 1 0-15.6zm0 4.2a3.6 3.6 0 1 0 0 7.2 3.6 3.6 0 0 0 0-7.2zm0 2.1a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3z"/></svg></a>` : ''}
            </td>
          </tr>
        `;
      }).join('');

      versionsHTML = `
        <div class="album-section">
          <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px;">
            <h4 class="bp5-heading section-heading" style="margin: 0;"><span class="bp5-icon-standard bp5-icon-layers"></span> Discogs Collection & Wantlist Pressings (${album.versions.length})</h4>
            ${pressingsActionBtns}
          </div>
          <table class="bp5-html-table bp5-html-table-striped bp5-compact full-width-table">
            <thead>
              <tr>
                <th style="width: 44px;"></th>
                <th>Format & Label / Cat#</th>
                <th>Year</th>
                <th>Source Status</th>
                <th style="text-align: right;">Link</th>
              </tr>
            </thead>
            <tbody>${vRows}</tbody>
          </table>
        </div>
      `;
    } else {
      versionsHTML = `
        <div class="album-section">
          <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px;">
            <h4 class="bp5-heading section-heading" style="margin: 0;"><span class="bp5-icon-standard bp5-icon-layers"></span> Pressings</h4>
            ${pressingsActionBtns}
          </div>
          <div class="bp5-text-muted" style="font-size: 13px;">No specific Discogs physical release pressings linked to this canonical album yet.</div>
        </div>
      `;
    }

    // Tracks Table
    let tracksHTML = '';
    if (album.tracks && album.tracks.length > 0) {
      const tRows = album.tracks.map((t, idx) => `
        <tr>
          <td style="width: 50px;">${idx + 1}</td>
          <td>
            <div class="track-title-text">${t.title}</div>
            <div class="track-artist-text">${t.artist || album.artist}</div>
          </td>
          <td style="width: 100px;">${formatDuration(t.duration_ms)}</td>
          <td style="width: 100px; text-align: right; white-space: nowrap;">
            <a href="https://www.youtube.com/results?search_query=${encodeURIComponent((t.artist || album.artist || '') + ' ' + t.title)}" target="_blank" class="bp5-button bp5-minimal bp5-small youtube-icon-btn" title="Search on YouTube"><svg class="youtube-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z"/></svg></a>
            ${t.spotify_id ? `<a href="https://open.spotify.com/track/${t.spotify_id}" target="_blank" class="bp5-button bp5-minimal bp5-small spotify-icon-btn" title="Open in Spotify"><svg class="spotify-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm5.521 17.34c-.24.359-.66.48-1.021.24-2.82-1.74-6.36-2.101-10.561-1.141-.418.122-.779-.179-.899-.539-.12-.421.18-.78.54-.9 4.56-1.021 8.52-.6 11.64 1.32.42.18.479.659.301 1.02zm1.48-3.3c-.301.42-.841.6-1.262.3-3.239-1.98-8.159-2.58-11.939-1.38-.479.12-1.02-.12-1.14-.6-.12-.48.12-1.021.6-1.141 4.38-1.38 9.841-.72 13.44 1.5.42.301.6.841.301 1.32zm.12-3.36C15.24 8.4 8.82 8.16 5.16 9.301c-.6.179-1.2-.18-1.38-.72-.18-.6.18-1.2.72-1.38 4.26-1.26 11.28-1.02 15.72 1.62.539.301.719 1.02.419 1.56-.299.421-1.02.599-1.559.3z"/></svg></a>` : ''}
          </td>
        </tr>
      `).join('');

      tracksHTML = `
        <div class="album-section">
          <h4 class="bp5-heading section-heading"><span class="bp5-icon-standard bp5-icon-music"></span> Tracklist (${album.tracks.length})</h4>
          <table class="bp5-html-table bp5-html-table-striped bp5-compact full-width-table">
            <thead>
              <tr>
                <th style="width: 50px;">#</th>
                <th>Title</th>
                <th style="width: 100px;">Duration</th>
                <th style="width: 100px; text-align: right;">Stream</th>
              </tr>
            </thead>
            <tbody>${tRows}</tbody>
          </table>
        </div>
      `;
    }

    container.innerHTML = `
      <button class="bp5-button bp5-minimal bp5-icon-arrow-left back-btn" onclick="showAlbums()">Back to Albums</button>
      <div class="album-header-card bp5-card bp5-elevation-1">
        <img class="album-header-art" src="${coverUrl}" alt="Album Art" onerror="this.onerror=null;this.src='${fallbackCover}'">
        <div class="album-header-info">
          <div class="album-header-badges">${badgesHTML}</div>
          <h1 class="bp5-heading album-header-title">${album.title}</h1>
          <h3 class="bp5-heading album-header-artist">${album.artist}</h3>
          <div class="album-header-meta">
            ${album.release_year ? `<span>Release Year: ${album.release_year}</span>` : ''}
            ${album.discogs_master_id ? `<span>Discogs Master ID: #${album.discogs_master_id}</span>` : ''}
          </div>
        </div>
      </div>
      ${versionsHTML}
      ${tracksHTML}
    `;
  } catch (err) {
    console.error('Failed to load album page:', err);
    container.innerHTML = '<div class="bp5-callout bp5-intent-danger">Failed to load album details.</div>';
  }
}

function toggleSettingsMenu(event) {
  event.stopPropagation();
  const menu = document.getElementById('settings-dropdown-menu');
  if (menu) {
    menu.style.display = menu.style.display === 'none' ? 'block' : 'none';
  }
}

async function triggerDedupeAlbums() {
  const menu = document.getElementById('settings-dropdown-menu');
  if (menu) menu.style.display = 'none';

  if (!confirm('Merge duplicate albums based on normalized titles and track evidence?')) {
    return;
  }

  try {
    const res = await fetch('/api/albums/dedupe', { method: 'POST' });
    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Deduplication failed');
    }
    checkSyncStatus();
  } catch (err) {
    alert('Failed to merge duplicate albums: ' + err.message);
  }
}

document.addEventListener('click', (e) => {
  const menu = document.getElementById('settings-dropdown-menu');
  const gearBtn = document.getElementById('settings-gear-btn');
  if (menu && gearBtn && !menu.contains(e.target) && !gearBtn.contains(e.target)) {
    menu.style.display = 'none';
  }
});

let syncPollInterval = null;



function startSyncPolling() {
  checkSyncStatus();
  if (!syncPollInterval) {
    syncPollInterval = setInterval(checkSyncStatus, 2000);
  }
}

async function checkSyncStatus() {
  try {
    const res = await fetch('/api/sync/status');
    if (!res.ok) return;
    const data = await res.json();

    const bannerWrap = document.getElementById('sync-banner-wrap');
    const statusText = document.getElementById('sync-status-text');
    const lastTimeDiv = document.getElementById('sync-last-time');
    const dedupeTimeDiv = document.getElementById('dedupe-last-time');
    const dedupeBtn = document.getElementById('dedupe-albums-btn');

    if (lastTimeDiv && data.last_synced_at) {
      lastTimeDiv.textContent = `Last synced Discogs: ${data.last_synced_at}`;
    }
    if (dedupeTimeDiv && data.last_deduped_at) {
      dedupeTimeDiv.textContent = `Last merged: ${data.last_deduped_at}`;
    }

    if (data.is_syncing) {
      if (bannerWrap) bannerWrap.style.display = 'inline-flex';
      if (statusText) statusText.textContent = data.message || 'Operation in Progress...';

      if (data.stage === 'deduping') {
        if (dedupeBtn) {
          dedupeBtn.textContent = 'Merging Duplicates...';
          dedupeBtn.classList.add('bp5-disabled');
        }
        if (syncBtn) {
          syncBtn.classList.add('bp5-disabled');
        }
      } else {
        if (syncBtn) {
          syncBtn.textContent = 'Sync in Progress...';
          syncBtn.classList.add('bp5-disabled');
        }
        if (dedupeBtn) {
          dedupeBtn.classList.add('bp5-disabled');
        }
      }
    } else {
      const wasSyncing = bannerWrap && bannerWrap.style.display !== 'none';
      if (bannerWrap) bannerWrap.style.display = 'none';
      if (syncBtn) {
        syncBtn.textContent = 'Sync Discogs (Collection & Wantlist)';
        syncBtn.classList.remove('bp5-disabled');
      }
      if (dedupeBtn) {
        dedupeBtn.textContent = 'Merge Duplicate Albums';
        dedupeBtn.classList.remove('bp5-disabled');
      }
      if (wasSyncing && currentSectionView === 'albums') {
        showAlbums();
      }
    }
  } catch (err) {
    console.error('Failed to fetch sync status:', err);
  }
}

async function triggerDiscogsSync() {
  const menu = document.getElementById('settings-dropdown-menu');
  if (menu) menu.style.display = 'none';

  const btn = document.getElementById('discogs-sync-btn');
  btn.textContent = 'Starting Sync...';
  btn.classList.add('bp5-disabled');

  try {
    const res = await fetch('/api/sync/discogs', { method: 'POST' });
    const data = await res.json();
    if (res.ok) {
      startSyncPolling();
    } else {
      alert(`Sync failed: ${data.error || 'Unknown error'}`);
      btn.textContent = 'Sync Discogs (Collection & Wantlist)';
      btn.classList.remove('bp5-disabled');
    }
  } catch (err) {
    alert(`Sync failed: ${err.message}`);
    btn.textContent = 'Sync Discogs (Collection & Wantlist)';
    btn.classList.remove('bp5-disabled');
  }
}

let activePlaylistID = null;
let activePlaylistName = '';
let activePlaylistDesc = '';
let editingPlaylistID = null;
let selectedTrackForPlaylist = null;
let allPlaylistsCache = [];

async function loadPlaylists() {
  try {
    const res = await fetch(`/api/playlists?sort=${currentPlaylistSort}`);
    const playlists = await res.json();
    allPlaylistsCache = playlists || [];
    const container = document.getElementById('sidebar-playlists');
    container.innerHTML = '';

    allPlaylistsCache.forEach(p => {
      const a = document.createElement('a');
      a.className = 'bp5-menu-item bp5-popover-dismiss playlist-item-btn';
      if (activePlaylistID === p.id && currentSectionView === 'playlist') {
        a.classList.add('bp5-active');
      }
      a.onclick = () => selectPlaylist(p.id, p.name, p.description, a);
      const formattedDate = formatDate(p.created_at);

      const urls = p.cover_art_urls || [];
      let coverHTML = '';
      if (urls.length > 0) {
        const gridImgs = [];
        for (let i = 0; i < 4; i++) {
          const imgUrl = urls[i % urls.length] || fallbackCover;
          gridImgs.push(`<img class="playlist-cover-img" src="${imgUrl}" alt="art" onerror="this.onerror=null;this.src='${fallbackCover}'">`);
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
  } catch (err) {
    console.error('Failed to load playlists:', err);
  }
}

// Modal logic for Create / Edit Playlist
function openCreatePlaylistModal() {
  editingPlaylistID = null;
  document.getElementById('playlist-modal-title').innerText = 'Create New Playlist';
  document.getElementById('playlist-name-input').value = '';
  document.getElementById('playlist-desc-input').value = '';
  document.getElementById('playlist-modal-submit-btn').innerText = 'Create Playlist';
  document.getElementById('playlist-modal').style.display = 'flex';
  setTimeout(() => document.getElementById('playlist-name-input').focus(), 100);
}

function openEditPlaylistModal(id, name, desc) {
  editingPlaylistID = id;
  document.getElementById('playlist-modal-title').innerText = 'Edit Playlist Details';
  document.getElementById('playlist-name-input').value = name || '';
  document.getElementById('playlist-desc-input').value = desc || '';
  document.getElementById('playlist-modal-submit-btn').innerText = 'Save Changes';
  document.getElementById('playlist-modal').style.display = 'flex';
  setTimeout(() => document.getElementById('playlist-name-input').focus(), 100);
}

let autoDebounceTimer = null;

function openAddTrackModal() {
  document.getElementById('track-title-input').value = '';
  document.getElementById('track-artist-input').value = '';
  document.getElementById('track-album-input').value = '';
  document.getElementById('track-duration-input').value = '';
  document.getElementById('track-spotify-input').value = '';
  document.getElementById('track-cover-input').value = '';
  const suggestions = document.getElementById('track-title-suggestions');
  if (suggestions) suggestions.style.display = 'none';
  document.getElementById('add-track-modal').style.display = 'flex';
  setTimeout(() => document.getElementById('track-title-input').focus(), 100);
}

function closeAddTrackModal() {
  document.getElementById('add-track-modal').style.display = 'none';
  const suggestions = document.getElementById('track-title-suggestions');
  if (suggestions) suggestions.style.display = 'none';
}

function handleTrackTitleAutocomplete(query) {
  const container = document.getElementById('track-title-suggestions');
  if (!container) return;

  const q = (query || '').trim();
  if (q.length < 2) {
    container.style.display = 'none';
    return;
  }

  if (autoDebounceTimer) clearTimeout(autoDebounceTimer);
  autoDebounceTimer = setTimeout(async () => {
    try {
      const [localRes, onlineRes] = await Promise.all([
        fetch(`/api/autocomplete?q=${encodeURIComponent(q)}`),
        fetch(`/api/autocomplete/online?q=${encodeURIComponent(q)}`)
      ]);

      const localItems = (await localRes.json()) || [];
      const onlineItems = (await onlineRes.json()) || [];

      if (localItems.length === 0 && onlineItems.length === 0) {
        container.style.display = 'none';
        return;
      }

      container.innerHTML = '';

      if (localItems.length > 0) {
        const header = document.createElement('div');
        header.className = 'auto-meta-text';
        header.style.padding = '6px 12px 2px 12px';
        header.style.fontWeight = 'bold';
        header.style.color = '#2b95d6';
        header.innerText = 'IN YOUR LIBRARY';
        container.appendChild(header);

        localItems.slice(0, 4).forEach(item => {
          const div = document.createElement('div');
          div.className = 'autocomplete-item';
          div.onclick = () => {
            document.getElementById('track-title-input').value = item.title;
            if (item.artist) document.getElementById('track-artist-input').value = item.artist;
            if (item.album_title) document.getElementById('track-album-input').value = item.album_title;
            container.style.display = 'none';
          };
          div.innerHTML = `
            <div class="auto-title-text">${item.title}</div>
            <div class="auto-meta-text">${item.artist}${item.album_title ? ' • ' + item.album_title : ''}</div>
          `;
          container.appendChild(div);
        });
      }

      if (onlineItems.length > 0) {
        const header = document.createElement('div');
        header.className = 'auto-meta-text';
        header.style.padding = '6px 12px 2px 12px';
        header.style.fontWeight = 'bold';
        header.style.color = '#15b371';
        header.innerText = 'ONLINE MUSIC DATABASE (ITUNES)';
        container.appendChild(header);

        onlineItems.slice(0, 8).forEach(item => {
          const div = document.createElement('div');
          div.className = 'autocomplete-item';
          div.onclick = () => {
            document.getElementById('track-title-input').value = item.title;
            if (item.artist) document.getElementById('track-artist-input').value = item.artist;
            if (item.album_title) document.getElementById('track-album-input').value = item.album_title;
            if (item.duration_ms) document.getElementById('track-duration-input').value = formatDuration(item.duration_ms);
            if (item.cover_image_url) document.getElementById('track-cover-input').value = item.cover_image_url;
            container.style.display = 'none';
          };
          const coverImg = item.cover_image_url ? `<img src="${item.cover_image_url}" style="width: 24px; height: 24px; border-radius: 3px; object-fit: cover;">` : '';
          div.innerHTML = `
            <div style="display: flex; align-items: center; gap: 8px;">
              ${coverImg}
              <div>
                <div class="auto-title-text">${item.title}</div>
                <div class="auto-meta-text">${item.artist}${item.album_title ? ' • ' + item.album_title : ''}</div>
              </div>
            </div>
          `;
          container.appendChild(div);
        });
      }

      container.style.display = 'block';
    } catch (err) {
      console.error('Autocomplete failed:', err);
    }
  }, 200);
}

async function submitAddTrackForm() {
  const title = document.getElementById('track-title-input').value.trim();
  const artist = document.getElementById('track-artist-input').value.trim();
  const album_title = document.getElementById('track-album-input').value.trim();
  const durationStr = document.getElementById('track-duration-input').value.trim();
  const spotify_id = document.getElementById('track-spotify-input').value.trim();
  const cover_image_url = document.getElementById('track-cover-input').value.trim();

  if (!title) {
    alert('Please enter a song title.');
    return;
  }

  let duration_ms = 0;
  if (durationStr) {
    const parts = durationStr.split(':');
    if (parts.length === 2) {
      duration_ms = (parseInt(parts[0], 10) * 60 + parseInt(parts[1], 10)) * 1000;
    } else if (parts.length === 1 && !isNaN(parts[0])) {
      duration_ms = parseInt(parts[0], 10) * 1000;
    }
  }

  try {
    const res = await fetch('/api/tracks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title,
        artist,
        album_title,
        duration_ms,
        spotify_id,
        cover_image_url
      })
    });

    if (!res.ok) {
      const errText = await res.text();
      throw new Error(errText || 'Failed to add song');
    }

    closeAddTrackModal();
    showAllSongs();
  } catch (err) {
    alert(err.message);
  }
}

function closePlaylistModal() {
  document.getElementById('playlist-modal').style.display = 'none';
}

async function submitPlaylistForm() {
  const name = document.getElementById('playlist-name-input').value.trim();
  const description = document.getElementById('playlist-desc-input').value.trim();

  if (!name) {
    alert('Please enter a playlist name.');
    return;
  }

  try {
    if (editingPlaylistID) {
      const res = await fetch(`/api/playlists/${editingPlaylistID}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, description })
      });
      if (!res.ok) throw new Error('Failed to update playlist');
      closePlaylistModal();
      await loadPlaylists();
      if (activePlaylistID === editingPlaylistID) {
        selectPlaylist(editingPlaylistID, name, description);
      }
    } else {
      const res = await fetch('/api/playlists', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, description })
      });
      if (!res.ok) throw new Error('Failed to create playlist');
      const newPl = await res.json();
      closePlaylistModal();
      await loadPlaylists();
      selectPlaylist(newPl.id, newPl.name, newPl.description);
    }
  } catch (err) {
    alert(err.message);
  }
}

async function deletePlaylistConfirm(id, name) {
  if (!confirm(`Are you sure you want to delete the playlist "${name}"? This action cannot be undone.`)) {
    return;
  }
  try {
    const res = await fetch(`/api/playlists/${id}`, { method: 'DELETE' });
    if (!res.ok) throw new Error('Failed to delete playlist');
    if (activePlaylistID === id) {
      activePlaylistID = null;
      showAllSongs();
    }
    await loadPlaylists();
  } catch (err) {
    alert(err.message);
  }
}

// Add Track to Playlist Modal
function openAddToPlaylistModal(track) {
  selectedTrackForPlaylist = track;
  document.getElementById('add-to-playlist-track-info').innerText = `Add "${track.title}" by ${track.artist || 'Unknown Artist'}`;
  
  const listElem = document.getElementById('add-to-playlist-list');
  listElem.innerHTML = '';

  if (allPlaylistsCache.length === 0) {
    listElem.innerHTML = '<div class="bp5-text-muted" style="padding: 12px; text-align: center;">No playlists exist yet. Create one first!</div>';
  } else {
    allPlaylistsCache.forEach(p => {
      const div = document.createElement('div');
      div.className = 'playlist-select-item';
      div.onclick = () => addTrackToPlaylist(p.id, track.id, p.name);
      div.innerHTML = `
        <div style="font-weight: 500; font-size: 13px; color: #f5f8fa;">${p.name}</div>
        <span class="bp5-tag bp5-minimal bp5-round">${p.track_count} tracks</span>
      `;
      listElem.appendChild(div);
    });
  }

  document.getElementById('add-to-playlist-modal').style.display = 'flex';
}

function closeAddToPlaylistModal() {
  document.getElementById('add-to-playlist-modal').style.display = 'none';
  selectedTrackForPlaylist = null;
}

async function addTrackToPlaylist(playlistID, trackID, playlistName) {
  try {
    const res = await fetch(`/api/playlists/${playlistID}/tracks`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ track_id: trackID })
    });
    if (!res.ok) throw new Error('Failed to add track');
    closeAddToPlaylistModal();
    await loadPlaylists();
    if (activePlaylistID === playlistID) {
      selectPlaylist(playlistID, activePlaylistName, activePlaylistDesc);
    }
  } catch (err) {
    alert(err.message);
  }
}

async function removeTrackFromPlaylist(playlistID, trackID, position) {
  try {
    const res = await fetch(`/api/playlists/${playlistID}/tracks?position=${position}`, {
      method: 'DELETE'
    });
    if (!res.ok) throw new Error('Failed to remove track');
    await loadPlaylists();
    selectPlaylist(playlistID, activePlaylistName, activePlaylistDesc);
  } catch (err) {
    alert(err.message);
  }
}

async function moveTrackInPlaylist(playlistID, currentIdx, direction, tracks) {
  const newIdx = currentIdx + direction;
  if (newIdx < 0 || newIdx >= tracks.length) return;

  const trackIDs = tracks.map(t => t.id);
  // Swap positions
  const temp = trackIDs[currentIdx];
  trackIDs[currentIdx] = trackIDs[newIdx];
  trackIDs[newIdx] = temp;

  try {
    const res = await fetch(`/api/playlists/${playlistID}/tracks/reorder`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ track_ids: trackIDs })
    });
    if (!res.ok) throw new Error('Failed to reorder playlist');
    selectPlaylist(playlistID, activePlaylistName, activePlaylistDesc);
  } catch (err) {
    alert(err.message);
  }
}

async function selectPlaylist(id, name, description, elem) {
  clearNavActive();
  currentSectionView = 'playlist';
  activePlaylistID = id;
  activePlaylistName = name;
  activePlaylistDesc = description || '';
  hideSectionFilter();

  if (!elem) {
    const sidebarItems = document.querySelectorAll('#sidebar-playlists .playlist-item-btn');
    sidebarItems.forEach(el => {
      if (el.onclick && el.onclick.toString().includes(id)) {
        el.classList.add('bp5-active');
      }
    });
  } else {
    elem.classList.add('bp5-active');
  }

  document.getElementById('grid-container').style.display = 'none';
  document.getElementById('table-container').style.display = 'block';

  const tbody = document.getElementById('tracklist-body');
  tbody.innerHTML = `
    <tr>
      <td colspan="5" style="text-align: center; padding: 32px;">
        <div class="bp5-spinner bp5-intent-primary bp5-small" style="margin: 0 auto;">
          <div class="bp5-spinner-head"></div>
        </div>
        <div class="bp5-text-muted" style="margin-top: 8px; font-size: 12px;">Loading playlist...</div>
      </td>
    </tr>
  `;

  try {
    const res = await fetch(`/api/playlists/${id}`);
    const tracks = await res.json();
    rawSectionData = tracks || [];
    renderPlaylistView(id, name, activePlaylistDesc, rawSectionData);
  } catch (err) {
    console.error('Failed to load playlist tracks:', err);
    tbody.innerHTML = '<tr><td colspan="5" class="bp5-text-muted" style="text-align: center; padding: 24px;">Failed to load playlist</td></tr>';
  }
}

function renderPlaylistView(playlistID, name, description, tracks) {
  const tableContainer = document.getElementById('table-container');
  
  // Custom Playlist Header Banner inserted before table
  let headerDiv = document.getElementById('playlist-view-header');
  if (!headerDiv) {
    headerDiv = document.createElement('div');
    headerDiv.id = 'playlist-view-header';
    tableContainer.parentNode.insertBefore(headerDiv, tableContainer);
  }
  headerDiv.style.display = 'block';

  headerDiv.innerHTML = `
    <div class="album-header-card bp5-card bp5-elevation-1" style="margin-bottom: 16px;">
      <div class="grid-card-icon" style="width: 80px; height: 80px; font-size: 32px; margin: 0; border-radius: 8px; flex-shrink: 0; background: #252a31;">
        <span class="bp5-icon-standard bp5-icon-music" style="color: #2b95d6;"></span>
      </div>
      <div class="album-header-info" style="flex: 1;">
        <div style="display: flex; align-items: center; justify-content: space-between;">
          <h1 class="bp5-heading album-header-title" style="margin: 0;">${name}</h1>
          <div style="display: flex; gap: 6px;">
            <button class="bp5-button bp5-outlined bp5-small bp5-icon-edit" onclick="openEditPlaylistModal('${playlistID}', '${name.replace(/'/g, "\\'")}', '${(description||'').replace(/'/g, "\\'")}')">Edit</button>
            <button class="bp5-button bp5-outlined bp5-intent-danger bp5-small bp5-icon-trash" onclick="deletePlaylistConfirm('${playlistID}', '${name.replace(/'/g, "\\'")}')">Delete</button>
          </div>
        </div>
        ${description ? `<div class="bp5-text-muted" style="font-size: 13px; margin-top: 4px;">${description}</div>` : ''}
        <div class="album-header-meta" style="margin-top: 8px;">
          <span>${tracks ? tracks.length : 0} Tracks</span>
        </div>
      </div>
    </div>
  `;

  renderTracks(tracks);
}

let searchDebounceTimer = null;

async function handleSearch(query) {
  if (!query || query.trim() === '') {
    showAllSongs();
    return;
  }

  clearNavActive();
  hideSectionFilter();
  const headerDiv = document.getElementById('playlist-view-header');
  if (headerDiv) headerDiv.style.display = 'none';
  
  document.getElementById('grid-container').style.display = 'none';
  document.getElementById('table-container').style.display = 'block';

  const tbody = document.getElementById('tracklist-body');
  tbody.innerHTML = `
    <tr>
      <td colspan="5" style="text-align: center; padding: 32px;">
        <div class="bp5-spinner bp5-intent-primary bp5-small" style="margin: 0 auto;">
          <div class="bp5-spinner-head"></div>
        </div>
        <div class="bp5-text-muted" style="margin-top: 8px; font-size: 12px;">Searching library...</div>
      </td>
    </tr>
  `;

  if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(async () => {
    try {
      const res = await fetch(`/api/search?q=${encodeURIComponent(query)}`);
      const tracks = await res.json();
      renderTracks(tracks);
    } catch (err) {
      console.error('Search failed:', err);
      tbody.innerHTML = '<tr><td colspan="5" class="bp5-text-muted" style="text-align: center; padding: 24px;">Search error</td></tr>';
    }
  }, 200);
}

function clearNavActive() {
  document.querySelectorAll('.playlist-item-btn, .nav-item-btn').forEach(el => el.classList.remove('bp5-active'));
  document.getElementById('album-detail-container').style.display = 'none';
  const artistContainer = document.getElementById('artist-detail-container');
  if (artistContainer) artistContainer.style.display = 'none';
  const playlistHeader = document.getElementById('playlist-view-header');
  if (playlistHeader) playlistHeader.style.display = 'none';
  
  const filterInput = document.getElementById('section-filter-input');
  if (filterInput) filterInput.value = '';
}

function renderTracks(tracks) {
  const tbody = document.getElementById('tracklist-body');
  tbody.innerHTML = '';

  if (!tracks || tracks.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" class="bp5-text-muted" style="text-align: center; padding: 24px;">No tracks found</td></tr>';
    return;
  }

  const isPlaylistView = (currentSectionView === 'playlist');

  tracks.forEach((t, i) => {
    const tr = document.createElement('tr');
    if (t.album_id) {
      tr.classList.add('clickable-track-row');
      tr.title = `View album: ${t.album_title || 'Album details'}`;
      tr.onclick = (e) => {
        if (e.target.closest('a, button, input, svg')) return;
        openAlbumPage(t.album_id);
      };
    }

    const coverUrl = t.cover_image_url || fallbackCover;
    const duration = formatDuration(t.duration_ms);
    const ytBtn = `<a href="https://www.youtube.com/results?search_query=${encodeURIComponent((t.artist || '') + ' ' + t.title)}" target="_blank" class="bp5-button bp5-minimal bp5-small youtube-icon-btn" title="Search on YouTube"><svg class="youtube-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z"/></svg></a>`;
    const spotifyBtn = t.spotify_id 
      ? `<a href="https://open.spotify.com/track/${t.spotify_id}" target="_blank" class="bp5-button bp5-minimal bp5-small spotify-icon-btn" title="Open in Spotify"><svg class="spotify-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm5.521 17.34c-.24.359-.66.48-1.021.24-2.82-1.74-6.36-2.101-10.561-1.141-.418.122-.779-.179-.899-.539-.12-.421.18-.78.54-.9 4.56-1.021 8.52-.6 11.64 1.32.42.18.479.659.301 1.02zm1.48-3.3c-.301.42-.841.6-1.262.3-3.239-1.98-8.159-2.58-11.939-1.38-.479.12-1.02-.12-1.14-.6-.12-.48.12-1.021.6-1.141 4.38-1.38 9.841-.72 13.44 1.5.42.301.6.841.301 1.32zm.12-3.36C15.24 8.4 8.82 8.16 5.16 9.301c-.6.179-1.2-.18-1.38-.72-.18-.6.18-1.2.72-1.38 4.26-1.26 11.28-1.02 15.72 1.62.539.301.719 1.02.419 1.56-.299.421-1.02.599-1.559.3z"/></svg></a>`
      : '';

    const trackJSON = JSON.stringify({ id: t.id, title: t.title, artist: t.artist }).replace(/"/g, '&quot;');
    const addToPlaylistBtn = `<button class="bp5-button bp5-minimal bp5-small playlist-act-btn" onclick="openAddToPlaylistModal(${trackJSON})" title="Add to Playlist"><span class="bp5-icon-standard bp5-icon-plus"></span></button>`;

    let playlistCurateBtns = '';
    if (isPlaylistView && activePlaylistID) {
      const isFirst = (i === 0);
      const isLast = (i === tracks.length - 1);
      const upBtn = `<button class="bp5-button bp5-minimal bp5-small playlist-act-btn ${isFirst ? 'bp5-disabled' : ''}" onclick="moveTrackInPlaylist('${activePlaylistID}', ${i}, -1, rawSectionData)" title="Move Up"><span class="bp5-icon-standard bp5-icon-chevron-up"></span></button>`;
      const downBtn = `<button class="bp5-button bp5-minimal bp5-small playlist-act-btn ${isLast ? 'bp5-disabled' : ''}" onclick="moveTrackInPlaylist('${activePlaylistID}', ${i}, 1, rawSectionData)" title="Move Down"><span class="bp5-icon-standard bp5-icon-chevron-down"></span></button>`;
      const removeBtn = `<button class="bp5-button bp5-minimal bp5-small playlist-act-btn danger" onclick="removeTrackFromPlaylist('${activePlaylistID}', '${t.id}', ${t.position || i + 1})" title="Remove from Playlist"><span class="bp5-icon-standard bp5-icon-cross"></span></button>`;
      playlistCurateBtns = `${upBtn}${downBtn}${removeBtn}`;
    }

    tr.innerHTML = `
      <td>${i + 1}</td>
      <td>
        <div class="track-meta">
          <img class="cover-art-small" src="${coverUrl}" alt="art" onerror="this.onerror=null;this.src='${fallbackCover}'">
          <div>
            <div class="track-title-text">${t.title}</div>
            <div class="track-artist-text">${t.artist}</div>
          </div>
        </div>
      </td>
      <td class="bp5-text-muted">${t.album_title || '-'}</td>
      <td class="bp5-text-muted">${duration}</td>
      <td style="text-align: right; white-space: nowrap;">${playlistCurateBtns}${addToPlaylistBtn}${ytBtn}${spotifyBtn}</td>
    `;
    tbody.appendChild(tr);
  });
}

function formatDuration(ms) {
  if (!ms) return '-';
  const totalSeconds = Math.floor(ms / 1000);
  const mins = Math.floor(totalSeconds / 60);
  const secs = totalSeconds % 60;
  return `${mins}:${secs < 10 ? '0' : ''}${secs}`;
}

function formatDate(dateStr) {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}
