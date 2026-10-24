// Real touch journeys. All writes use the runner's disposable DB snapshot.
import {createRequire} from 'node:module';
import {mkdirSync} from 'node:fs';
import {join} from 'node:path';
const {chromium, webkit} = createRequire(import.meta.url)('playwright');
const artifacts = join(import.meta.dirname, 'artifacts');
const assert = (ok, message) => { if (!ok) throw new Error(message); };

async function bounds(page, selector, {target = false} = {}) {
  const el = typeof selector === 'string' ? page.locator(selector).first() : selector;
  await el.scrollIntoViewIfNeeded();
  const b = await el.boundingBox();
  const viewport = page.viewportSize();
  assert(b && b.x >= -1 && b.y >= -1 && b.x + b.width <= viewport.width + 1 && b.y + b.height <= viewport.height + 1,
    `${typeof selector === 'string' ? selector : 'control'} outside viewport: ${JSON.stringify(b)}`);
  if (target) {
    assert(b.width >= 43 && b.height >= 43, `small touch target: ${JSON.stringify(b)}`);
    assert(await el.evaluate(e => {
      const b = e.getBoundingClientRect();
      return e.contains(document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2));
    }), 'control covered at its tap point');
  }
  return el;
}
async function tap(page, selector) { const el = await bounds(page, selector, {target:true}); await el.tap(); }
async function layout(page) {
  const bad = await page.evaluate(() => [...document.querySelectorAll('.main-panel, #grid-container, #local-container, .album-section, .album-header-card, .custom-dialog')]
    .filter(e => e.getBoundingClientRect().width && getComputedStyle(e).visibility !== 'hidden')
    .filter(e => e.scrollWidth > e.clientWidth + 2)
    .map(e => `${e.id || e.className}: ${e.clientWidth}/${e.scrollWidth}`));
  assert(!bad.length, 'content overflows outside table scroller: ' + bad.join(', '));
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'document overflows');
}
async function shot(page, name) { await page.screenshot({path:join(artifacts, name + '.png')}); }
async function openSidebar(page) {
  await tap(page, '#sidebar-toggle-btn');
  await page.waitForFunction(() => Math.abs(document.querySelector('#sidebar-panel').getBoundingClientRect().left) < 1);
}
async function nav(page, selector) {
  await openSidebar(page);
  await tap(page, selector);
  await page.waitForFunction(() => document.querySelector('#sidebar-panel').classList.contains('collapsed'));
}
async function closeDialog(page, id) { await tap(page, `#${id} [aria-label="Close dialog"]`); }
async function trackRows(page) { await page.locator('#tracklist-body .track-title-text').first().waitFor(); }

async function collection(page, url, prefix) {
  await page.goto(url + '/albums?filter=collection&q=__BPM_TEST__');
  await page.locator('#grid-container .grid-card').nth(1).waitFor();
  await layout(page);
  await page.evaluate(() => document.fonts.ready);
  assert(await page.evaluate(() => [...document.fonts].some(f => f.family.includes('blueprint-icons-16') && f.status === 'loaded')), 'navigation icon font did not load locally');
  assert(await page.evaluate(() => [...document.querySelectorAll('link[rel=stylesheet]')].every(e => new URL(e.href).origin === location.origin)), 'layout still depends on an external stylesheet');
  await bounds(page, '#bpm_min', {target:true});
  assert(await page.locator('#bpm_min').evaluate(e => parseFloat(getComputedStyle(e).fontSize)) >= 16, 'BPM input triggers iOS zoom');
  await shot(page, prefix + '-collection');
  await page.locator('#bpm_min').fill('130');
  await page.locator('#bpm_max').fill('120');
  await tap(page, '#tempo-filter button[type=submit]');
  assert(await page.locator('#bpm_max').evaluate(e => !e.validity.valid), 'inverted range accepted');
  // Correcting an invalid range must unblock native form submission.
  await page.locator('#bpm_min').fill('120');
  await page.locator('#bpm_max').fill('125');
  await page.locator('#bpm-vinyl').check();
  await tap(page, '#tempo-filter button[type=submit]');
  await page.locator('.bpm-match').waitFor();
  assert(await page.locator('#grid-container .grid-card').count() === 1, 'BPM selection wrong');
  await shot(page, prefix + '-bpm');
  await page.reload();
  await page.locator('.bpm-match').waitFor();
  assert(await page.locator('#bpm_min').inputValue() === '120', 'BPM range lost on reload');
  await page.locator('.grid-card-title').first().tap();
  await page.locator('#album-detail-container .pressings-table').waitFor();
  await layout(page);
  await shot(page, prefix + '-album');
  const region = page.getByRole('region', {name:'Discogs pressings'});
  await region.scrollIntoViewIfNeeded();
  if (page.context().browser().browserType().name() === 'chromium') {
    const b = await region.boundingBox();
    const cdp = await page.context().newCDPSession(page);
    const y = Math.min(b.y + b.height / 2, page.viewportSize().height - 24);
    await cdp.send('Input.dispatchTouchEvent', {type:'touchStart',touchPoints:[{x:b.x+b.width-20,y}]});
    for(let step=1;step<=8;step++) {
      await cdp.send('Input.dispatchTouchEvent', {type:'touchMove',touchPoints:[{x:b.x+b.width-20-(b.width-40)*step/8,y}]});
      await new Promise(r=>setTimeout(r,16));
    }
    await cdp.send('Input.dispatchTouchEvent', {type:'touchEnd',touchPoints:[]});
    await cdp.detach();
    await page.waitForFunction(() => document.querySelector('[aria-label="Discogs pressings"]').scrollLeft > 20);
  }
  const link = region.locator('a').first();
  // Browser scrolls the overflow region to the final column, then a real tap
  // must reach the link (a clipped table without a scroller fails this).
  await bounds(page, link, {target:true});
  assert(await region.evaluate(e => e.scrollLeft > 0), 'pressing link never scrolled into reach');
  await shot(page, prefix + '-pressings-scrolled');
  const popupPromise = page.waitForEvent('popup');
  await link.tap();
  const popup = await popupPromise;
  assert(popup.url().includes('discogs.com/release/'), 'wrong pressing destination');
  await popup.close();
  const bpm = page.locator('[data-track-bpm="bpm-hit"]');
  await bounds(page, bpm, {target:true});
  const title = await page.locator('#album-detail-container .track-title-text').first().boundingBox();
  assert(title.width >= 180, 'album title squeezed by table columns');
  await shot(page, prefix + '-tracks');
  const refreshed = page.waitForResponse(r => r.url().endsWith('/api/albums/bpm-match'));
  page.once('dialog', d => d.accept('121.4'));
  await bpm.tap(); // Snapshot only; same value keeps desktop fixture stable.
  await refreshed;
  await page.waitForFunction(() => document.querySelector('[data-track-bpm="bpm-hit"]')?.title.includes('Manual override'));
  await tap(page, '#album-detail-container .back-btn');
  await page.locator('.bpm-match').waitFor();
  assert(await page.locator('#bpm_min').inputValue() === '120', 'Back lost range');
  await page.goBack();
  await page.locator('#album-detail-container .album-header-title').waitFor();
  await page.goForward();
  await page.locator('.bpm-match').waitFor();
  await tap(page, '#pill-wantlist');
  await page.waitForFunction(() => !location.search.includes('filter=collection'));
  assert(!await page.locator('#tempo-filter').isVisible(), 'BPM leaks into wantlist');
  await tap(page, '#pill-all');
  await page.locator('#grid-container .grid-card').first().waitFor();
}

async function views(page, url, prefix) {
  await nav(page, '#nav-all-songs');
  await trackRows(page);
  await page.locator('#section-filter-input').fill('Matching club mix');
  await page.locator('#tracklist-body .track-title-text').filter({hasText:'Matching club mix'}).waitFor();
  await page.reload();
  await trackRows(page);
  assert((await page.locator('#tracklist-body').innerText()).includes('Matching club mix'), 'songs reload lost filter');
  const row = page.locator('#tracklist-body tr').first();
  const title = await row.locator('.track-title-text').boundingBox();
  assert(title.width >= 180, 'song title squeezed by actions');
  await bounds(page, row.locator('[title="Add to Playlist"]'), {target:true});
  await layout(page);
  await shot(page, prefix + '-songs');
  await row.locator('[title="Add to Playlist"]').tap();
  await layout(page);
  await shot(page, prefix + '-choose-playlist');
  await closeDialog(page, 'add-to-playlist-modal');
  await row.locator('.track-title-text').tap();
  await page.locator('#album-detail-container .album-header-title').waitFor();
  await page.goBack();
  await trackRows(page);
  await nav(page, '#nav-artists');
  const artists = await (await page.request.get(url + '/api/artists')).json();
  const artistName = (artists.find(a => a.name === 'Radiohead') || artists.find(a => a.album_count > 0 && a.track_count > 0)).name;
  await page.locator('#section-filter-input').fill(artistName);
  await page.locator('#grid-container').getByText(artistName, {exact:true}).waitFor();
  await page.reload();
  await page.locator('#grid-container').getByText(artistName, {exact:true}).waitFor();
  assert(await page.locator('#section-filter-input').inputValue() === artistName.toLowerCase(), 'artists reload lost query');
  await page.locator('#grid-container').getByText(artistName, {exact:true}).tap();
  await page.locator('#artist-detail-container .album-header-title').waitFor();
  await layout(page);
  await shot(page, prefix + '-artist');
  await page.locator('#artist-detail-container .grid-card-title').first().tap();
  await page.locator('#album-detail-container .album-header-title').waitFor();
  await page.goBack();
  await page.locator('#artist-detail-container .album-header-title').waitFor();

  await nav(page, '#nav-local');
  await page.locator('#local-container .grid-card').first().waitFor();
  await layout(page);
  await shot(page, prefix + '-local');
  await page.locator('#local-container .grid-card-title').first().tap();
  await page.waitForURL(/\/local\/.+/);
  await page.locator('.album-detail-hero').waitFor();
  const localURL = page.url();
  await page.reload();
  await page.locator('.album-detail-hero').waitFor();
  assert(page.url() === localURL, 'local detail URL changed on reload');
  await layout(page);
  const play = page.getByRole('region', {name:'Local audio files'}).locator('button').first();
  await bounds(page, play, {target:true}); // Never tap playback.
  await shot(page, prefix + '-local-detail');
  await tap(page, '#local-container .back-btn');
  await page.locator('#local-container .grid-card').first().waitFor();
  await page.goBack();
  await page.locator('.album-detail-hero').waitFor();
  for (const destination of ['#nav-albums', '#nav-all-songs', '#nav-artists']) {
    await nav(page, destination);
    assert(!await page.locator('#local-container').isVisible(), 'local view remains on another destination');
    await page.locator(destination === '#nav-all-songs' ? '.track-meta' : '#grid-container .grid-card').first().waitFor();
  }
}

async function dialogsAndPlaylist(page, url, prefix) {
  await nav(page, '.playlist-item-btn');
  await page.locator('#playlist-view-header .album-header-title').waitFor();
  await layout(page);
  await shot(page, prefix + '-playlist');
  let busy = true;
  await page.route('**/api/sync/status', r => r.fulfill({json:busy ? {is_syncing:true, message:'Syncing collection and checking a long playlist title for changes'} : {is_syncing:false}}));
  await page.evaluate(() => checkSyncStatus());
  await bounds(page, '.navbar-custom');
  await bounds(page, '#settings-gear-btn', {target:true});
  await shot(page, prefix + '-sync-banner');
  busy = false;
  await page.evaluate(() => checkSyncStatus());
  await page.unroute('**/api/sync/status');
  await tap(page, 'button[onclick="openAddSongsToPlaylistModal()"]');
  await page.locator('#add-songs-search-input').fill('Matching club mix');
  await page.locator('.add-song-row').first().waitFor();
  await layout(page);
  await bounds(page, '.add-song-btn', {target:true});
  await shot(page, prefix + '-playlist-search');
  await closeDialog(page, 'add-songs-modal');
  await tap(page, '.playlist-header-actions button[onclick^="openEditPlaylistModal"]');
  await layout(page);
  await closeDialog(page, 'playlist-modal');
  await page.locator('#search-input').fill('Matching club mix');
  await page.locator('#tracklist-body .track-title-text').filter({hasText:'Matching club mix'}).waitFor();
  assert(!await page.locator('[title="Remove from Playlist"]').count(), 'playlist remove actions leaked into search');
  await page.reload();
  await trackRows(page);
  await layout(page);
  await shot(page, prefix + '-search');
  await tap(page, '#settings-gear-btn');
  await bounds(page, '#settings-dropdown-menu');
  await bounds(page, '#local-sync-btn', {target:true}); // No sync mutations.
  await shot(page, prefix + '-settings');
  await tap(page, '#settings-gear-btn');
  await tap(page, 'button[onclick="openAddTrackModal()"]');
  await layout(page);
  await shot(page, prefix + '-add-song');
  await page.setViewportSize({width:page.viewportSize().width,height:500});
  await bounds(page, '#add-track-modal [aria-label="Close dialog"]', {target:true});
  await bounds(page, '#add-track-modal .bp5-dialog-footer button:last-child', {target:true});
  await page.locator('#track-spotify-input').scrollIntoViewIfNeeded();
  await shot(page, prefix + '-add-song-short');
  await closeDialog(page, 'add-track-modal');
  await page.setViewportSize({width:page.viewportSize().width,height:844});
  await openSidebar(page);
  await tap(page, 'button[onclick="openCreatePlaylistModal()"]');
  await bounds(page, '#playlist-name-input', {target:true});
  await shot(page, prefix + '-create-playlist');
  const name = `Mobile journey ${prefix}`;
  await page.locator('#playlist-name-input').fill(name);
  await tap(page, '#playlist-modal-submit-btn');
  await page.locator('#playlist-modal').waitFor({state:'hidden'});
  // Creation navigates after refreshing the sidebar; await that completion
  // before issuing another gesture.
  await page.locator('#playlist-view-header').getByText(name,{exact:true}).waitFor();
  await tap(page, 'button[onclick="openAddSongsToPlaylistModal()"]');
  await page.locator('#add-songs-search-input').fill('Matching club mix');
  await page.locator('.add-song-btn').first().waitFor();
  await tap(page, '.add-song-btn');
  await page.waitForFunction(() => document.querySelector('.add-song-btn')?.disabled);
  await closeDialog(page, 'add-songs-modal');
  await trackRows(page);
  await tap(page, '#tracklist-body [title="Remove from Playlist"]');
  await page.getByText('No tracks found',{exact:true}).waitFor();
  page.once('dialog', d => d.accept());
  await tap(page, '.playlist-header-actions button[onclick^="deletePlaylistConfirm"]');
  await page.waitForURL(/\/songs$/);
  assert(await page.locator('#search-input').inputValue() === '', 'stale search query on songs navigation');
}

async function player(page, prefix) {
  await page.route('**/api/playback/state', r => r.fulfill({json:{status:'paused', current:{title:'Paused layout preview', artist:'No audio',duration_ms:180000},position_ms:40000,volume:69}}));
  await page.evaluate(() => npRefresh());
  await bounds(page, '#now-playing-bar');
  for (const selector of ['#np-toggle', '.np-btn-stop', '#np-volume', '#np-seek']) await bounds(page,selector,{target:true});
  const boxes = await page.locator('#np-toggle, #np-volume, #np-seek, .np-now').evaluateAll(els => els.map(e => {const b=e.getBoundingClientRect();return {x:b.x,y:b.y,r:b.right,b:b.bottom};}));
  for(let i=0;i<boxes.length;i++) for(let j=i+1;j<boxes.length;j++) {
    const a=boxes[i], b=boxes[j]; assert(a.r<=b.x||b.r<=a.x||a.b<=b.y||b.b<=a.y,'playback controls overlap');
  }
  await shot(page, prefix + '-player');
  await tap(page, 'button[onclick="openAddTrackModal()"]');
  await bounds(page, '#add-track-modal .bp5-dialog-footer button:last-child', {target:true});
  await closeDialog(page, 'add-track-modal');
}

export async function runMobile(url) {
  mkdirSync(artifacts, {recursive:true});
  const results=[];
  for (const [engine, type] of [['chromium',chromium],['webkit',webkit]]) {
    let browser;
    // An installed but broken browser is a failure, not a silent skip.
    if (!createRequire(import.meta.url)('node:fs').existsSync(type.executablePath())) {
      console.log(`[mobile] SKIP ${engine}: browser is not installed`); continue;
    }
    try { browser=await type.launch(); }
    catch(e) { results.push({name:engine+' launch',passed:false,error:e.message}); continue; }
    try {
      for (const width of [390,375]) {
        const prefix=`mobile-${engine}-${width}`;
        const context=await browser.newContext({viewport:{width,height:844},isMobile:true,hasTouch:true});
        const page=await context.newPage(); page.setDefaultTimeout(12000);
        const errors=[];const unsafe=[];const localFailures=[];
        page.on('pageerror',e=>errors.push(e.message));
        page.on('console',m=>{if(m.type()==='error')errors.push(m.text())});
        page.on('requestfailed',r=>{if(r.url().startsWith(url))localFailures.push(r.failure()?.errorText+' '+r.url())});
        page.on('response',r=>{if(r.url().startsWith(url)&&r.status()>=400)localFailures.push(`${r.status()} ${r.url()}`)});
        await context.route('**/*',r=>{
          const req=r.request();const path=new URL(req.url()).pathname;
          if (!['GET','HEAD'].includes(req.method()) && /^\/api\/(playback|local\/play|sync|tidal)/.test(path)) {
            unsafe.push(req.method()+' '+path);return r.abort();
          }
          // No external network, including unpkg. Invalid image bytes exercise
          // actual image error events and fallback rendering without HTTP errors.
          if (!req.url().startsWith(url) && !req.url().startsWith('data:')) return r.fulfill({status:200,contentType:'text/plain',body:'external network unavailable'});
          return r.continue();
        });
        console.log(`[mobile] ${prefix}: touch navigation, dialogs, tables; external network unavailable`);
        try {
          await collection(page,url,prefix);
          await views(page,url,prefix);
          await dialogsAndPlaylist(page,url,prefix);
          await player(page,prefix);
          assert(!unsafe.length,'attempted playback/sync: '+unsafe.join(', '));
          assert(!errors.length,'JS/console errors: '+[...new Set(errors)].join('\n'));
          assert(!localFailures.length,'local request failures: '+localFailures.join('\n'));
          results.push({name:prefix+' touch journey (external network unavailable)',passed:true});
        } catch(e) {
          await shot(page,prefix+'-failure').catch(()=>{});
          console.error(`[mobile] ${prefix}: ${e.message}`);
          results.push({name:prefix,passed:false,error:e.stack});
        } finally { await context.close(); }
      }
    } finally { await browser.close(); }
  }
  return results;
}
