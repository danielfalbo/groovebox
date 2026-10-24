#!/usr/bin/env node
// Groovebox E2E runner.
//
// Spins up an ISOLATED server instance (random port, snapshot copy of
// music.db) so tests never touch the live :3000 service, runs the Playwright
// UI regression suite against it, then tears everything down.
//
// Usage: sh e2e/run.sh            (defaults: repo binary + snapshot DB)
//        sh e2e/run.sh --live     run against an already-running server (BASE_URL)
//        BASE_URL=http://localhost:3000 sh e2e/run.sh --live
//
// Needs the global `playwright` install (see e2e/README.md).

import {runMobile} from './mobile.mjs';
import { spawn, execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { mkdtempSync, rmSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { createServer } from 'node:net';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// Resolve the globally-installed playwright (CJS honors NODE_PATH, ESM doesn't).
const { chromium } = createRequire(import.meta.url)('playwright');

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');

const DB = join(ROOT, 'music.db');

const LARGE = 45000;

function log(msg) { console.log(`[e2e] ${msg}`); }

function snapshotDB(dstDir) {
  // sqlite3's backup() produces a consistent snapshot even while the live
  // server (WAL mode) is running. python3 only — no sqlite3 CLI needed.
  execFileSync('python3', ['-c', `
import sqlite3, sys
src, dst = sys.argv[1], sys.argv[2]
out = sqlite3.connect(dst)
sqlite3.connect(src).backup(out)
out.close()
`, DB, join(dstDir, 'music.db')], { stdio: 'inherit', timeout: 20000 });
}

function addTempoFixtures(work) {
  // Fixture writes are confined to the isolated snapshot, never --live.
  execFileSync('python3', ['-c', `
import sqlite3,sys
c=sqlite3.connect(sys.argv[1])
# Older snapshots are migrated by the server; add these fixture columns here too.
cols={r[1] for r in c.execute('pragma table_info(tracks)')}
for name,typ in [('bpm','REAL'),('bpm_source','TEXT'),('bpm_provenance','TEXT'),('bpm_updated_at','TEXT')]:
 if name not in cols:c.execute('ALTER TABLE tracks ADD COLUMN '+name+' '+typ)
for id,title in [('bpm-match','__BPM_TEST__ Match'),('bpm-average','__BPM_TEST__ False average'),('bpm-null','__BPM_TEST__ Unknown only'),('bpm-cd','__BPM_TEST__ CD'),('bpm-edge','__BPM_EDGE__ Boundaries')]:
 c.execute("INSERT INTO albums(id,title,artist,in_collection,has_vinyl) VALUES(?,?, 'Tempo fixtures',1,1)",(id,title))
 c.execute("INSERT INTO release_versions(id,album_id,title,artist,source,has_vinyl,format_description) VALUES(?,?,?,'Tempo fixtures','collection',1,'Vinyl')",(id+'-release',id,title))
for id,album,title,bpm in [('bpm-hit','bpm-match','Matching club mix',121.4),('bpm-unknown','bpm-match','Unknown mix',None),('bpm-slow','bpm-average','Slow',90),('bpm-fast','bpm-average','Fast',150)]:
 c.execute("INSERT INTO tracks(id,album_id,release_id,title,artist,track_number,duration_ms,bpm,bpm_source) VALUES(?,?,?,?,'Tempo fixtures','A1',300000,?,?)",(id,album,album+'-release',title,bpm,'external:acousticbrainz' if bpm else None))
c.execute("INSERT INTO tracks(id,album_id,title,artist,duration_ms,bpm) VALUES('bpm-catalog','bpm-average','Unowned radio edit','Tempo fixtures',180000,122)")
c.execute("UPDATE release_versions SET has_vinyl=0,format_description='CD' WHERE id='bpm-cd-release'")
c.execute("UPDATE albums SET has_vinyl=0 WHERE id='bpm-cd'")
for id,album,bpm in [('bpm-null-track','bpm-null',None),('bpm-cd-track','bpm-cd',121.4)]:
 c.execute("INSERT INTO tracks(id,album_id,release_id,title,artist,bpm) VALUES(?,?,?,?,'Tempo fixtures',?)",(id,album,album+'-release',id,bpm))
# IEEE-754 predecessors exercise values above .99 and the exact band edges.
for i,bpm in enumerate([20,99.99999999999999,100,109.99999999999999,110,119.99999999999999,120,129.99999999999997,130,139.99999999999997,140,400,None]):
 c.execute("INSERT INTO tracks(id,album_id,release_id,title,artist,bpm) VALUES(?,'bpm-edge','bpm-edge-release',?,'Tempo fixtures',?)",('edge-'+str(i),'Edge '+str(i),bpm))
c.execute("INSERT INTO search_fts(target_type,target_id,title,artist) SELECT 'track',id,title,artist FROM tracks WHERE id='bpm-hit'")
c.execute("UPDATE release_versions SET discogs_release_id='1',label='Fixture pressing label',catalog_number='MOBILE-TEST' WHERE id='bpm-match-release'")
c.commit()
`, join(work, 'music.db')], {stdio: 'inherit', timeout: 20000});
}

function addTracklistFixtures(work) {
  execFileSync('python3', ['-c', `
import sqlite3,sys
from datetime import datetime,timezone
sys.path.insert(0,'scripts')
from tracklists import install_schema,reconcile
c=sqlite3.connect(sys.argv[1]);c.row_factory=sqlite3.Row;install_schema(c)
c.execute("INSERT INTO albums(id,title,artist,in_collection) VALUES('owned-list','__TRACKLIST__ Full Record','Fixture Artist',1)")
c.execute("INSERT INTO tracks(id,album_id,title,artist,bpm) VALUES('owned-catalog','owned-list','Opening (Radio Edit)','Fixture Artist',200)")
for id,rid,fmt in [('owned-vinyl',9876001,'Vinyl'),('owned-cd',9876002,'CD')]:
 c.execute("INSERT INTO release_versions(id,album_id,discogs_release_id,title,artist,source,has_vinyl,format_description,catalog_number) VALUES(?,'owned-list',?,'Full Record','Fixture Artist','collection',?,?,?)",(id,rid,int(fmt=='Vinyl'),fmt,fmt+'-001'))
 c.commit()
 tracks=[dict(type_='track',position='A1',title='Opening (Club Mix)',duration='3:00'),dict(type_='heading',position='',title='Side A — original mixes'),dict(type_='index',position='',title='Complete Suite (Extended)',sub_tracks=[dict(type_='track',position='A2.1',title='Part 1',duration='2:00'),dict(type_='track',position='A2.2',title='Part 2',duration='2:10')]),dict(type_='track',position='A10',title='Finale',duration='4:00')] if fmt=='Vinyl' else [dict(type_='track',position='1-01',title='Opening (Radio Edit)',duration='2:00')]
 reconcile(c,dict(id=id,album_id='owned-list',discogs_release_id=rid,artist='Fixture Artist'),dict(id=rid,artists=[dict(name='Fixture Artist')],tracklist=tracks),datetime.now(timezone.utc).isoformat(timespec='seconds'))
c.commit();c.close()
`, join(work,'music.db')], {cwd:ROOT,stdio:'inherit',timeout:20000});
}

function freePort() {
  return new Promise((res) => {
    const s = createServer();
    s.listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => res(p)); });
  });
}

async function waitReady(url, portProc, deadlineMs = 30000) {
  const deadline = Date.now() + deadlineMs;
  while (Date.now() < deadline) {
    if (portProc.exitCode != null) throw new Error('server exited early');
    try { const r = await fetch(`${url}/api/albums/counts`, {signal:AbortSignal.timeout(3000)}); if (r.ok) return; } catch {}
    await new Promise(r => setTimeout(r, 400));
  }
  throw new Error(`server at ${url} never became ready`);
}

async function startServer() {
  const work = mkdtempSync(join(tmpdir(), 'groovebox-e2e-'));
  const BIN = join(work, 'groovebox');
  execFileSync('go', ['build', '-o', BIN, '.'], {cwd: ROOT, stdio: 'inherit', timeout: 90000});
  snapshotDB(work);
  addTempoFixtures(work);
  addTracklistFixtures(work);
  const port = await freePort();
  const url = `http://127.0.0.1:${port}`;
  const child = spawn(BIN, ['-port', String(port), '-db', join(work, 'music.db')],
    { cwd: ROOT, env: { ...process.env, DISCOGS_TOKEN: '' }, stdio: ['ignore', 'pipe', 'pipe'] });
  child.stdout.on('data', d => process.stdout.write(`[server] ${d}`));
  child.stderr.on('data', d => process.stderr.write(`[server] ${d}`));
  await waitReady(url, child);
  log(`isolated server ready on ${url} (db snapshot in ${work})`);
  return { url, work, child };
}

const results = [];
function test(name, fn) { results.push({ name, fn }); }

function assert(cond, msg) { if (!cond) throw new Error(msg); }

// ---------------------------------------------------------------------------
// UI regression tests (the ones that bit us before).
// ---------------------------------------------------------------------------

test('albums grid renders cards + filter pills', async ({ page, url }) => {
  await page.goto(url + '/albums', { waitUntil: 'networkidle' });
  await page.waitForSelector('.grid-card', { timeout: 15000 });
  assert(await page.locator('.grid-card').count() > 0, 'no album cards rendered');
  assert(await page.locator('#album-filter-pills').isVisible(), 'filter pills not visible');
  const coll = await page.locator('#count-collection').innerText();
  assert(parseInt(coll, 10) > 0, 'collection count badge not numeric');
});

test('plain click SPA-navigates to album, Back hides album hero (regression)', async ({ page, url }) => {
  await page.goto(`${url}/albums`, { waitUntil: 'networkidle' });
  await page.waitForSelector('.grid-card', { timeout: 15000 });
  await page.locator('.grid-card').first().click();
  await page.waitForFunction(() => location.pathname.startsWith('/albums/'), null, { timeout: 15000 });
  assert(await page.locator('#album-detail-container').isVisible(), 'detail container not shown');
  const cardOnPage = page.locator('.grid-card');
  assert((await cardOnPage.count()) === 0 || !(await cardOnPage.first().isVisible()), 'grid still visible on detail page');

  const back = page.locator('.back-btn');
  assert(await back.count() >= 1, 'no Back button on detail page');
  await back.first().click();
  await page.waitForURL(/\/albums(\?|$)/, { timeout: 15000 });
  await page.waitForTimeout(600);
  const disp = await page.locator('#album-detail-container').evaluate(el => el.style.display);
  assert(disp === 'none', `BROKEN: album-detail-container still display:${disp} after Back (shadowed clearNavActive)`);
  assert((await page.locator('.grid-card').count()) > 0, 'grid did not re-render after Back');
});

test('ctrl/cmd-click opens album in a new tab', async ({ page, url }) => {
  await page.goto(`${url}/albums`, { waitUntil: 'networkidle' });
  await page.waitForSelector('.grid-card', { timeout: 15000 });
  const [tab] = await Promise.all([
    page.waitForEvent('popup', { timeout: 10000 }),
    page.locator('.grid-card').nth(1).click({ modifiers: ['Control'] }),
  ]);
  await tab.waitForLoadState('domcontentloaded');
  assert(/\/albums\/[^/?#]+/.test(tab.url()), `popup URL not an album page: ${tab.url()}`);
  await tab.close();
});

test('middle-click opens album in a new tab (auxclick)', async ({ page, url }) => {
  await page.goto(`${url}/albums`, { waitUntil: 'networkidle' });
  await page.waitForSelector('.grid-card', { timeout: 15000 });
  const card = page.locator('.grid-card').nth(2);
  const box = await card.boundingBox();
  assert(box, 'card has no bounding box');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down({ button: 'middle' });
  await page.mouse.up({ button: 'middle' });
  const popup = await page.waitForEvent('popup', { timeout: 10000 }).catch(() => null);
  assert(popup, 'middle-click did not open a tab');
  await popup.waitForLoadState('domcontentloaded');
  assert(/\/albums\/[^/?#]+/.test(popup.url()), `popup URL wrong album page: ${popup.url()}`);
  await popup.close();
});

test('no reserved playback-bar padding when idle (regression)', async ({ page, url }) => {
  await page.goto(`${url}/albums`, { waitUntil: 'networkidle' });
  await page.waitForSelector('.grid-card', { timeout: 15000 });
  const pad = await page.evaluate(() => {
    const mp = document.querySelector('.main-panel');
    const cs = getComputedStyle(mp);
    return { bottom: cs.paddingBottom, barDisplay: getComputedStyle(document.getElementById('now-playing-bar')).display, npActive: document.body.classList.contains('np-active') };
  });
  assert(!pad.npActive, 'np-active class set while idle');
  assert(pad.barDisplay === 'none', `now-playing-bar visible while idle: ${pad.barDisplay}`);
  assert(pad.bottom !== '84px', `BROKEN: 84px bottom padding reserved while idle (got ${pad.bottom})`);
  // and the bar-companion rule still exists when playback IS active
  const activePad = await page.evaluate(() => {
    document.body.classList.add('np-active');
    return getComputedStyle(document.querySelector('.main-panel')).paddingBottom;
  });
  assert(activePad === '84px', `np-active padding ${activePad}, expected 84px`);
});

test('wantlist pill switches view', async ({ page, url }) => {
  await page.goto(`${url}/albums`, { waitUntil: 'networkidle' });
  await page.waitForSelector('.grid-card', { timeout: 15000 });
  await page.locator('#pill-wantlist').click();
  await page.waitForFunction(() => new URLSearchParams(location.search).get('filter') === 'wantlist', null, { timeout: 10000 });
  await page.waitForTimeout(800);
  assert((await page.locator('.grid-card').count()) > 0, 'wantlist view rendered no cards');
});

test('artist page album cards open new tab on ctrl-click', async ({ page, url }) => {
  await page.goto(`${url}/artists`, { waitUntil: 'networkidle' });
  await page.waitForSelector('.grid-card', { timeout: 15000 });
  if (await page.locator('.grid-card').count() === 0) { log('no artists — skipping'); return; }
  await page.locator('.grid-card').filter({hasText:/[0-9]+ albums?/}).first().click();
  await page.waitForFunction(() => location.pathname.startsWith('/artists/'), null, { timeout: 15000 });
  await page.locator('#artist-detail-container .grid-card').first().waitFor();
  const hasAlbums = await page.locator('#artist-detail-container .grid-card').count();
  assert(hasAlbums > 0, 'artist album cards missing');
  const [tab] = await Promise.all([
    page.waitForEvent('popup', { timeout: 10000 }),
    page.locator('#artist-detail-container .grid-card').first().click({ modifiers: ['Control'] }),
  ]);
  assert(/\/albums\/[^/?#]+/.test(tab.url()), `artist album card did not open album tab: ${tab.url()}`);
  await tab.close();
});

test('no page/console errors while sync-status polling runs (regression: syncBtn ReferenceError)', async ({ page, url }) => {
  // The settings/sync poll hits /api/sync/status every 2s. The bug (a missing
  // syncBtn lookup) threw inside the function — caught by its try/catch, so it
  // surfaced as a console.error, NOT an uncaught pageerror. Watch both.
  const errors = [];
  const onPageErr = e => errors.push(`pageerror: ${e.message} @ ${e.filename}:${e.lineno}`);
  const onConsole = m => { if (m.type() === 'error') errors.push(`console: ${m.text()}`); };
  page.on('pageerror', onPageErr);
  page.on('console', onConsole);
  await page.goto(`${url}/albums`, { waitUntil: 'networkidle' });
  await page.waitForSelector('.grid-card', { timeout: 15000 });
  await page.waitForTimeout(5000); // ~2+ poll cycles
  page.off('pageerror', onPageErr);
  page.off('console', onConsole);
  assert(errors.length === 0, `JS errors during polling:\n${errors.join('\n')}`);
});


test('one-click tempo presets preserve search/vinyl and restore on reload/back', async ({page, url}) => {
  if (process.argv.includes('--live')) return;
  mkdirSync(join(__dirname, 'artifacts'), {recursive:true});
  await page.goto(`${url}/albums?filter=collection&q=__BPM_TEST__`, {waitUntil: 'networkidle'});
  assert(await page.locator('.grid-card').count() === 4, 'All tempos omitted unknown/CD fixtures');
  assert(!await page.locator('#tempo-custom').isVisible(), 'custom form visible by default');
  await page.locator('#bpm-vinyl').check();
  await page.locator('[data-tempo="120"]').click();
  await page.waitForFunction(() => document.querySelectorAll('.grid-card').length === 1);
  assert((await page.locator('.bpm-match').innerText()).includes('Matching club mix'), 'actual matching title missing');
  assert((await page.locator('.bpm-match').innerText()).includes('121.4 BPM'), 'matching BPM missing');
  assert(!(await page.locator('#grid-container').innerText()).includes('Unknown mix'), 'unknown matched range');
  assert(new URL(page.url()).searchParams.get('q') === '__BPM_TEST__', 'preset lost search');
  assert(await page.locator('[data-tempo="120"]').getAttribute('aria-pressed') === 'true', 'preset not selected');
  await page.screenshot({path:join(__dirname,'artifacts','tempo-desktop-presets.png')});
  await page.reload({waitUntil: 'networkidle'});
  assert(await page.locator('[data-tempo="120"]').getAttribute('aria-pressed') === 'true', 'reload lost preset');
  assert(!await page.locator('#tempo-custom').isVisible(), 'preset reload opened custom');
  assert(await page.locator('#bpm-vinyl').isChecked(), 'reload lost vinyl');
  await page.locator('.grid-card').click();
  assert(!await page.locator('#tempo-filter').isVisible(), 'tempo controls visible on detail');
  await page.goBack();
  await page.waitForSelector('.bpm-match');
  assert(await page.locator('[data-tempo="120"]').getAttribute('aria-pressed') === 'true', 'Back lost preset');
  await page.locator('#pill-wantlist').click();
  assert(!await page.locator('#tempo-filter').isVisible(), 'tempo visible in wantlist');
  await page.locator('#pill-collection').click();
  await page.waitForSelector('.bpm-match');
  assert(await page.locator('[data-tempo="120"]').getAttribute('aria-pressed') === 'true', 'filter switch lost preset');
  await page.locator('[data-tempo="all"]').click();
  await page.waitForFunction(() => document.querySelectorAll('.grid-card').length === 3);
  const params = new URL(page.url()).searchParams;
  assert(!params.has('bpm_min') && !params.has('bpm_max'), 'All tempos retained bounds');
  assert(params.get('vinyl') === '1' && params.get('q') === '__BPM_TEST__', 'All tempos reset unrelated filters');
  assert((await page.locator('#grid-container').innerText()).includes('Unknown only'), 'All tempos omitted unknown-only record');
});

test('custom tempo reveal, validation, exact inclusive bounds and bookmarks', async ({page, url}) => {
  if (process.argv.includes('--live')) return;
  await page.goto(`${url}/albums?filter=collection&q=__BPM_TEST__&vinyl=1`, {waitUntil:'networkidle'});
  const originalURL = page.url();
  await page.locator('[data-tempo="custom"]').click();
  assert(page.url() === originalURL, 'revealing Custom changed applied range');
  assert(await page.locator('[data-tempo="custom"]').getAttribute('aria-expanded') === 'true', 'Custom expansion missing');
  await page.locator('#bpm_min').fill('130');
  await page.locator('#bpm_max').fill('120');
  await page.locator('#tempo-custom button').click();
  assert(await page.locator('#bpm_max').evaluate(e => !e.validity.valid), 'inverted range accepted');
  assert(page.url() === originalURL, 'invalid custom range changed URL');
  await page.locator('#bpm_min').fill('19');
  await page.locator('#tempo-custom button').click();
  assert(await page.locator('#bpm_min').evaluate(e => !e.validity.valid), 'out of bounds accepted');
  await page.locator('#bpm_min').fill('121.4');
  await page.locator('#bpm_max').fill('121.4');
  await page.locator('#tempo-custom button').click();
  await page.waitForSelector('.bpm-match');
  assert(await page.locator('[data-tempo="custom"]').getAttribute('aria-pressed') === 'true', 'custom range not selected');
  assert(await page.locator('.grid-card').count() === 1, 'inclusive equal bounds failed');
  await page.screenshot({path:join(__dirname,'artifacts','tempo-desktop-custom.png')});
  await page.reload({waitUntil:'networkidle'});
  assert(await page.locator('#tempo-custom').isVisible(), 'bookmarked custom range hidden');
  assert(await page.locator('#bpm_max').inputValue() === '121.4', 'bookmark changed custom precision');
  await page.locator('.grid-card').click();
  await page.locator('.back-btn').first().click();
  await page.waitForSelector('.bpm-match');
  assert(await page.locator('[data-tempo="custom"]').getAttribute('aria-pressed') === 'true', 'Back lost Custom');
  // Draft inputs must not leak into the independent vinyl control or search.
  await page.locator('#bpm_min').fill('130');
  await page.locator('#bpm-vinyl').uncheck();
  await page.waitForFunction(() => document.querySelectorAll('.grid-card').length === 2);
  assert(new URL(page.url()).searchParams.get('bpm_min') === '121.4', 'vinyl applied unsubmitted draft');
  await page.locator('[data-tempo="all"]').click();
  assert(!await page.locator('#tempo-custom').isVisible(), 'preset did not close Custom');
  const bad = await fetch(`${url}/api/albums?filter=collection&bpm_min=130&bpm_max=120`);
  assert(bad.status === 400, 'inverted API range accepted');
});

test('tempo bands include exact float boundaries once and ignore stale requests', async ({page, url}) => {
  if (process.argv.includes('--live')) return;
  await page.goto(`${url}/albums?filter=collection&q=__BPM_EDGE__`, {waitUntil:'networkidle'});
  for (const [key, indices] of [['slow',[0,1]],['100',[2,3]],['110',[4,5]],['120',[6,7]],['130',[8,9]],['fast',[10,11]]]) {
    await page.locator(`[data-tempo="${key}"]`).click();
    await page.waitForFunction(expected => [...document.querySelectorAll('.bpm-match strong')].map(e => e.textContent.trim()).join('|') === expected, indices.map(i => 'Edge '+i).join('|'));
    assert(await page.locator('.bpm-match').count() === 2, 'overlap/unknown included in '+key);
  }
  // Existing API max=109.99 remains inclusive and is not silently widened.
  await page.goto(`${url}/albums?filter=collection&q=__BPM_EDGE__&bpm_min=100&bpm_max=109.99`, {waitUntil:'networkidle'});
  assert(await page.locator('[data-tempo="custom"]').getAttribute('aria-pressed') === 'true', 'non-preset bookmark mislabeled');
  assert(await page.locator('.bpm-match').count() === 1, 'custom max was widened');
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  const delayed = '**/api/albums?*';
  let entered;
  const started = new Promise(resolve => { entered = resolve; });
  await page.route(delayed, async route => {
    if (new URL(route.request().url()).searchParams.get('bpm_min') !== '100') return route.continue();
    const response = await route.fetch();
    entered();
    await gate;
    await route.fulfill({response});
  });
  await page.locator('[data-tempo="100"]').click();
  await started;
  await page.locator('[data-tempo="120"]').click();
  await page.waitForFunction(() => document.querySelector('.bpm-match strong')?.textContent.trim() === 'Edge 6');
  const stale = page.waitForResponse(r => new URL(r.url()).searchParams.get('bpm_min') === '100');
  release();
  await stale;
  await page.waitForTimeout(100);
  assert((await page.locator('.bpm-match strong').first().innerText()).trim() === 'Edge 6', 'stale response replaced latest range');
  await page.unroute(delayed);
});

test('manual BPM correction persists, unknown locks and Auto resets', async ({page, url}) => {
  if (process.argv.includes('--live')) return;
  await page.goto(`${url}/albums/bpm-match`, {waitUntil: 'networkidle'});
  const button = page.locator('[data-track-bpm="bpm-hit"]');
  await button.waitFor();
  page.once('dialog', d => d.accept('124.5'));
  await button.click();
  await page.waitForFunction(() => document.querySelector('[data-track-bpm="bpm-hit"]')?.textContent.includes('124.5'));
  await page.reload({waitUntil:'networkidle'});
  assert((await button.getAttribute('title')).includes('Manual override'), 'source missing after reload');
  page.once('dialog', d => d.accept(''));
  await button.click();
  await page.waitForFunction(() => document.querySelector('[data-track-bpm="bpm-hit"]')?.textContent.includes('unknown'));
  let track = (await (await fetch(`${url}/api/albums/bpm-match`)).json()).tracks.find(t => t.id === 'bpm-hit');
  assert(track.bpm === null && track.bpm_source === 'manual', 'unknown manual lock missing');
  await page.getByRole('button', {name:'Auto', exact:true}).click();
  await page.waitForFunction(() => !document.querySelector('[data-track-bpm="bpm-hit"]')?.title.includes('Manual override'));
  track = (await (await fetch(`${url}/api/albums/bpm-match`)).json()).tracks.find(t => t.id === 'bpm-hit');
  assert(track.bpm === null && track.bpm_source === '', 'auto reset failed');
});

// ---------------------------------------------------------------------------
async function main() {
  const isLive = process.argv.includes('--live');
  let server = null;
  let url = null;
  const browser = await chromium.launch();
  try {
    if (isLive) {
      url = process.env.BASE_URL || 'http://127.0.0.1:3000';
      log(`running against live server ${url}`);
    } else {
      server = await startServer();
      url = server.url;
    }
    const page = await browser.newPage();
    // Render image failure fallbacks deterministically; no remote album-art
    // availability should decide whether local UI regression tests pass.
    await page.route('**/*', r => {
      if (r.request().resourceType() === 'image' && !r.request().url().startsWith(url) && !r.request().url().startsWith('data:'))
        return r.fulfill({contentType:'text/plain', body:'image unavailable'});
      return r.continue();
    });
    page.setDefaultTimeout(LARGE);
    for (const t of results) {
      try {
        await t.fn({ page, url });
        t.passed = true;
        console.log(`  ✔ ${t.name}`);
      } catch (e) {
        t.passed = false;
        console.error(`  ✘ ${t.name}\n    ${e.message}`);
        process.exitCode = 1;
      }
    }
    await page.close();
    if (!isLive) {
      // The desktop manual-override test deliberately clears this tempo.
      execFileSync('python3', ['-c', "import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.execute(\"UPDATE tracks SET bpm=121.4,bpm_source='external:acousticbrainz' WHERE id='bpm-hit'\"); c.commit()", join(server.work,'music.db')], {timeout:20000});
      for (const result of await runMobile(url)) {
        results.push(result);
        console.log(`  ${result.passed ? '✔' : '✘'} ${result.name}${result.error ? '\n    '+result.error : ''}`);
      }
    } else log('mobile mutation journeys require the isolated snapshot; skipped in --live mode');
  } finally {
    await browser.close();
    if (server) {
      log('shutting down isolated server');
      server.child.kill('SIGKILL');
      rmSync(server.work, { recursive: true, force: true });
    }
  }
  const failed = results.filter(r => !r.passed).length;
  console.log(`\n[e2e] ${results.length - failed}/${results.length} passed`);
  if (failed > 0) process.exitCode = 1;
}

main().catch(e => { console.error(e); process.exit(1); });
