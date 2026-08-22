// ---- Local Library + Now-Playing (server-driven mirror) ----

function esc(s) { return String(s==null?'':s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;'); }
function fmtDur(ms) {
  if (!ms || ms < 0) return '0:00';
  const s = Math.floor(ms / 1000);
  return Math.floor(s/60) + ':' + String(s%60).padStart(2,'0');
}
function srcLabel(src) {
  switch (src) { case 'cd': return 'CD'; case 'vinyl': return 'VINYL'; case 'playback': return 'PLAYBACK'; default: return (src||'').toUpperCase(); }
}

function showLocalFlow() {
  ['grid-container','table-container','album-detail-container','artist-detail-container'].forEach(id=>{const el=document.getElementById(id); if(el) el.style.display='none';});
  if (typeof hideSectionFilter === 'function') hideSectionFilter();
  const cont = document.getElementById('local-container');
  cont.style.display = 'block';
  return cont;
}

function clearNavActive() {
  document.querySelectorAll('#sidebar-panel .nav-item-btn').forEach(n => n.classList.remove('bp5-active'));
}

async function showLocalAlbums() {
  clearNavActive();
  const navL = document.getElementById('nav-local'); if (navL) navL.classList.add('bp5-active');
  const cont = showLocalFlow();
  cont.innerHTML = '<div class="bp5-spinner bp5-intent-primary"><div class="bp5-spinner-head"></div></div>';
  let albums = [];
  try { albums = await (await fetch('/api/local/albums')).json(); } catch(e){}
  if (!albums || !albums.length) {
    cont.innerHTML = '<div class="bp5-text-muted" style="padding:24px;">No local albums yet. Drop files into <code>~/syncthing/archive/music</code> then click “Sync Local Library”.</div><button class="bp5-button bp5-intent-primary" onclick="triggerLocalSync()">Sync Now</button>';
    return;
  }
  const cards = albums.map(alb => {
    const cover = alb.cover_image_url || fallbackCover;
    const raws = alb.raw_count ? ` • <span style="color:#ffb366">${alb.raw_count} raw</span>` : '';
    return `<div class="grid-card" onclick="openLocalAlbum('${alb.id}')">
      <div class="grid-card-art-wrap">
        <img class="grid-card-art" src="${esc(cover)}" onerror="this.onerror=null;this.src='${fallbackCover}'">
        <span class="bp5-tag bp5-intent-success bp5-round album-badge">📁 Local</span>
      </div>
      <div class="grid-card-title">${esc(alb.title)}</div>
      <div class="grid-card-subtitle">${esc(alb.artist)}${alb.release_year?' • '+alb.release_year:''}</div>
      <div class="grid-card-version-count">${alb.track_count} tracks${raws}</div>
      <button class="bp5-button bp5-small bp5-intent-primary card-play-btn" onclick="event.stopPropagation();playAlbumNow('${alb.id}')">▶ Play</button>
    </div>`;
  }).join('');
  cont.innerHTML = `<div class="bp5-text-muted local-hdr"><strong>Local Library</strong> — ${albums.length} group(s) with local audio</div>
    <div class="grid-container" style="display:grid">${cards}</div>`;
}

async function openLocalAlbum(albumId) {
  clearNavActive();
  const cont = showLocalFlow();
  cont.innerHTML = '<div class="bp5-spinner bp5-intent-primary"><div class="bp5-spinner-head"></div></div>';
  let files = [];
  try { files = await (await fetch('/api/local/albums/'+albumId)).json(); } catch(e){}
  if (!Array.isArray(files) || !files.length) { cont.innerHTML = '<div class="bp5-text-muted">No files</div>'; return; }
  const first = files[0];
  const rows = files.map((f,i) => `
    <tr>
      <td style="width:36px">${i+1}</td>
      <td><span class="bp5-tag ${f.kind==='raw'?'bp5-intent-warning':'bp5-intent-success'} bp5-round">${f.kind==='raw'?'RAW':'TRACK'}</span> <strong>${esc(f.title)}</strong>${f.artist&&f.artist!==first.artist?'<div class="bp5-text-muted">'+esc(f.artist)+'</div>':''}</td>
      <td><span class="bp5-tag bp5-minimal">${esc(f.format)} ${f.sample_rate?'-'+f.sample_rate:''}</span> <span class="bp5-tag bp5-minimal">${srcLabel(f.source)}</span></td>
      <td>${fmtDur(f.duration_ms)}</td>
      <td style="text-align:right"><button class="bp5-button bp5-small bp5-intent-primary" onclick="playFileNow('${f.id}')">▶</button></td>
    </tr>`).join('');
  const nTracks = files.filter(f=>f.kind==='track').length;
  const nRaw = files.length - nTracks;
  cont.innerHTML = `
    <div class="album-detail-hero">
      <img class="album-detail-art" src="${esc(first.cover_image_url||fallbackCover)}" onerror="this.onerror=null;this.src='${fallbackCover}'">
      <div>
        <div class="bp5-text-muted">LOCAL GROUP</div>
        <h2 style="margin:6px 0">${esc(first.artist)} — ${esc(first.album)}</h2>
        <div class="bp5-text-muted">${nTracks} tracks${nRaw?' + '+nRaw+' raw sides':''}</div>
        <button class="bp5-button bp5-intent-primary" onclick="playAlbumNow('${first.album_id}')">▶ Play album</button>
      </div>
    </div>
    <table class="bp5-html-table bp5-html-table-striped bp5-interactive bp5-compact full-width-table">
      <thead><tr><th>#</th><th>File</th><th>Format</th><th>Duration</th><th style="text-align:right">Play</th></tr></thead>
      <tbody>${rows}</tbody>
    </table>`;
  document.title = first.album + ' — Groovebox';
}

// ---- playback actions ----
async function playAlbumNow(albumId) {
  try { await fetch('/api/local/play', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({album_id:albumId})}); } catch(e){}
  npRefresh();
}
async function playFileNow(fileId) {
  try { await fetch('/api/local/play-file', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({file_id:fileId})}); } catch(e){}
  npRefresh();
}
async function triggerLocalSync() {
  const el = document.getElementById('local-sync-last-time'); if (el) el.textContent = 'Syncing…';
  try {
    const res = await fetch('/api/sync/local', {method:'POST'});
    const d = await res.json();
    if (el) el.textContent = d.message || 'Sync started';
  } catch(e) { if(el) el.textContent = 'Failed'; }
  // re-poll after a moment
  setTimeout(() => { showLocalAlbums(); }, 2000);
}
async function stopEverything() {
  await fetch('/api/playback/stop', {method:'POST'});
  npRefresh();
}

// ---- now-playing bar (mirrors server state) ----
async function npAction(action) {
  await fetch('/api/playback/'+action, {method:'POST'});
  npRefresh();
}
let npDurMs = 0;
async function npSeekCommit() {
  const v = document.getElementById('np-seek').value;
  const ms = Math.round((v / 1000) * npDurMs);
  await fetch('/api/playback/seek', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({position_ms: ms})});
  npRefresh();
}
function npSeekPreview(input) { /* live thumb callout optional */ }
async function npVolume(v) {
  await fetch('/api/playback/volume', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({volume: parseInt(v,10)})});
}
async function npRefresh() {
  let st; try { st = await (await fetch('/api/playback/state')).json(); } catch(e){ return; }
  const bar = document.getElementById('now-playing-bar');
  if (!st || st.status === 'idle' || !st.current) {
    if (bar) bar.style.display = 'none';
    return;
  }
  if (bar) bar.style.display = 'flex';
  const cur = st.current;
  document.getElementById('np-cover').src = cur.cover_url || fallbackCover;
  document.getElementById('np-title').textContent = cur.title || '—';
  document.getElementById('np-artist').textContent = cur.artist ? cur.artist + (cur.album ? ' — ' + cur.album : '') : '—';
  document.getElementById('np-time').textContent = fmtDur(st.position_ms);
  document.getElementById('np-dur').textContent = fmtDur(cur.duration_ms);
  npDurMs = cur.duration_ms || 0;
  const dur = cur.duration_ms || 1;
  const pct = Math.min(1000, Math.max(0, Math.floor(st.position_ms / dur * 1000)));
  const seek = document.getElementById('np-seek');
  if (document.activeElement !== seek) seek.value = pct;
  const toggle = document.getElementById('np-toggle');
  toggle.textContent = st.status === 'paused' ? '▶' : '⏸';
}

function initNavLocalRouter() {
  const orig = window.renderFromLocation || function(){};
  window.renderFromLocation = function() {
    const parts = location.pathname.split('/').filter(Boolean);
    if (parts[0] === 'local') {
      const id = parts[1] ? decodeURIComponent(parts[1]) : null;
      if (id) openLocalAlbum(id); else showLocalAlbums();
      return;
    }
    return orig.apply(this, arguments);
  };
}
function navShowLocal() { history.pushState({},'', '/local'); window.renderFromLocation(); }

// poll every 2s (server owns state; tab just reflects it)
setInterval(npRefresh, 2000);

initNavLocalRouter();
// Re-evaluate once in case the page deep-loaded straight to /local before we
// replaced the router (the original app.js render already ran).
window.renderFromLocation();
npRefresh();