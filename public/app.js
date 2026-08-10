let currentPlaylistSort = localStorage.getItem('playlist-sort') || 'date_desc';

document.addEventListener('DOMContentLoaded', () => {
  initSidebarState();
  initSortState();
  loadPlaylists();
  showAllSongs();
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

let activeAlbumFilter = 'all';

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
    const url = filter === 'all' ? '/api/albums' : `/api/albums?filter=${filter}`;
    const res = await fetch(url);
    rawSectionData = await res.json();
    renderAlbumCards(rawSectionData);
  } catch (err) {
    console.error('Failed to load filtered albums:', err);
  }
}

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
    const grid = document.getElementById('grid-container');
    grid.innerHTML = '';
    const filtered = !q ? rawSectionData : rawSectionData.filter(alb => 
      (alb.title && alb.title.toLowerCase().includes(q)) ||
      (alb.artist && alb.artist.toLowerCase().includes(q))
    );
    
    if (filtered.length === 0) {
      grid.innerHTML = '<div class="bp5-text-muted">No matching albums</div>';
      return;
    }
    renderAlbumCards(filtered);
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

async function showAlbums() {
  clearNavActive();
  currentSectionView = 'albums';
  showSectionFilter('Filter albums by title, artist...', true);
  document.getElementById('nav-albums').classList.add('bp5-active');
  
  document.getElementById('table-container').style.display = 'none';
  
  updateAlbumCounts();
  setAlbumFilter(activeAlbumFilter);
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

    const coverUrl = alb.cover_image_url || 'https://via.placeholder.com/180';
    
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

    card.innerHTML = `
      <div class="grid-card-art-wrap">
        <img class="grid-card-art" src="${coverUrl}" alt="cover" onerror="this.src='https://via.placeholder.com/180'">
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
        const coverUrl = alb.cover_image_url || 'https://via.placeholder.com/180';
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
              <img class="grid-card-art" src="${coverUrl}" alt="cover" onerror="this.src='https://via.placeholder.com/180'">
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
      const tRows = artistData.tracks.map((t, idx) => `
        <tr>
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
      `).join('');

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

    const coverUrl = album.cover_image_url || 'https://via.placeholder.com/220';
    let badgesHTML = '';
    if (album.has_vinyl) {
      badgesHTML += `<span class="bp5-tag bp5-intent-warning bp5-round album-detail-badge">📀 In Collection</span>`;
    }
    if (album.in_wantlist) {
      badgesHTML += `<span class="bp5-tag bp5-intent-primary bp5-round album-detail-badge">🎯 On Wantlist</span>`;
    }

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
              <img src="${pressingArt}" alt="pressing art" class="version-pressing-art" onerror="this.src='https://via.placeholder.com/40'">
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
          <h4 class="bp5-heading section-heading"><span class="bp5-icon-standard bp5-icon-layers"></span> Discogs Collection & Wantlist Pressings (${album.versions.length})</h4>
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
          <h4 class="bp5-heading section-heading"><span class="bp5-icon-standard bp5-icon-layers"></span> Pressings</h4>
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
        <img class="album-header-art" src="${coverUrl}" alt="Album Art" onerror="this.src='https://via.placeholder.com/220'">
        <div class="album-header-info">
          <div class="album-header-badges">${badgesHTML}</div>
          <h1 class="bp5-heading album-header-title">${album.title}</h1>
          <h3 class="bp5-heading album-header-artist">${album.artist}</h3>
          <div class="album-header-meta">
            ${album.release_year ? `<span>Release Year: ${album.release_year}</span>` : ''}
            ${album.discogs_master_id ? `<span>Discogs Master ID: <a href="https://www.discogs.com/master/${album.discogs_master_id}" target="_blank">#${album.discogs_master_id} ↗</a></span>` : ''}
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

document.addEventListener('click', (e) => {
  const menu = document.getElementById('settings-dropdown-menu');
  const gearBtn = document.getElementById('settings-gear-btn');
  if (menu && gearBtn && !menu.contains(e.target) && !gearBtn.contains(e.target)) {
    menu.style.display = 'none';
  }
});

let syncPollInterval = null;

document.addEventListener('DOMContentLoaded', () => {
  initSidebarState();
  initSortState();
  loadPlaylists();
  showAllSongs();
  startSyncPolling();
});

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
    const syncBtn = document.getElementById('discogs-sync-btn');
    const lastTimeDiv = document.getElementById('sync-last-time');

    if (lastTimeDiv && data.last_synced_at) {
      lastTimeDiv.textContent = `Last synced: ${data.last_synced_at}`;
    }

    if (data.is_syncing) {
      if (bannerWrap) bannerWrap.style.display = 'inline-flex';
      if (statusText) statusText.textContent = data.message || 'Syncing Discogs...';
      if (syncBtn) {
        syncBtn.textContent = 'Sync in Progress...';
        syncBtn.classList.add('bp5-disabled');
      }
    } else {
      if (bannerWrap) bannerWrap.style.display = 'none';
      if (syncBtn) {
        syncBtn.textContent = 'Sync Discogs (Collection & Wantlist)';
        syncBtn.classList.remove('bp5-disabled');
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

async function selectPlaylist(id, name, description, elem) {
  clearNavActive();
  if (elem) elem.classList.add('bp5-active');

  document.getElementById('view-title').textContent = name;
  document.getElementById('view-subtitle').textContent = description || 'Playlist tracks';
  
  document.getElementById('grid-container').style.display = 'none';
  document.getElementById('table-container').style.display = 'block';

  try {
    const res = await fetch(`/api/playlists/${id}`);
    const tracks = await res.json();
    renderTracks(tracks);
  } catch (err) {
    console.error('Failed to load playlist tracks:', err);
  }
}

let searchDebounceTimer = null;

async function handleSearch(query) {
  if (!query || query.trim() === '') {
    showAllSongs();
    return;
  }

  clearNavActive();
  hideSectionFilter();
  
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

function renderTracks(tracks) {
  const tbody = document.getElementById('tracklist-body');
  tbody.innerHTML = '';

  if (!tracks || tracks.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" class="bp5-text-muted" style="text-align: center; padding: 24px;">No tracks found</td></tr>';
    return;
  }

  tracks.forEach((t, i) => {
    const tr = document.createElement('tr');
    const coverUrl = t.cover_image_url || 'https://via.placeholder.com/36';
    const duration = formatDuration(t.duration_ms);
    const ytBtn = `<a href="https://www.youtube.com/results?search_query=${encodeURIComponent((t.artist || '') + ' ' + t.title)}" target="_blank" class="bp5-button bp5-minimal bp5-small youtube-icon-btn" title="Search on YouTube"><svg class="youtube-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z"/></svg></a>`;
    const spotifyBtn = t.spotify_id 
      ? `<a href="https://open.spotify.com/track/${t.spotify_id}" target="_blank" class="bp5-button bp5-minimal bp5-small spotify-icon-btn" title="Open in Spotify"><svg class="spotify-svg-icon" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm5.521 17.34c-.24.359-.66.48-1.021.24-2.82-1.74-6.36-2.101-10.561-1.141-.418.122-.779-.179-.899-.539-.12-.421.18-.78.54-.9 4.56-1.021 8.52-.6 11.64 1.32.42.18.479.659.301 1.02zm1.48-3.3c-.301.42-.841.6-1.262.3-3.239-1.98-8.159-2.58-11.939-1.38-.479.12-1.02-.12-1.14-.6-.12-.48.12-1.021.6-1.141 4.38-1.38 9.841-.72 13.44 1.5.42.301.6.841.301 1.32zm.12-3.36C15.24 8.4 8.82 8.16 5.16 9.301c-.6.179-1.2-.18-1.38-.72-.18-.6.18-1.2.72-1.38 4.26-1.26 11.28-1.02 15.72 1.62.539.301.719 1.02.419 1.56-.299.421-1.02.599-1.559.3z"/></svg></a>`
      : '';

    tr.innerHTML = `
      <td>${i + 1}</td>
      <td>
        <div class="track-meta">
          <img class="cover-art-small" src="${coverUrl}" alt="art" onerror="this.src='https://via.placeholder.com/36'">
          <div>
            <div class="track-title-text">${t.title}</div>
            <div class="track-artist-text">${t.artist}</div>
          </div>
        </div>
      </td>
      <td class="bp5-text-muted">${t.album_title || '-'}</td>
      <td class="bp5-text-muted">${duration}</td>
      <td style="text-align: right; white-space: nowrap;">${ytBtn}${spotifyBtn}</td>
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
