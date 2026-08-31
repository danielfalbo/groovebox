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

import { spawn, execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { mkdtempSync, rmSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { createServer } from 'node:net';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// Resolve the globally-installed playwright (CJS honors NODE_PATH, ESM doesn't).
const { chromium } = createRequire(import.meta.url)('playwright');

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');
const BIN = join(ROOT, 'groovebox');
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
`, DB, join(dstDir, 'music.db')], { stdio: 'inherit' });
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
    try { const r = await fetch(`${url}/api/albums/counts`); if (r.ok) return; } catch {}
    await new Promise(r => setTimeout(r, 400));
  }
  throw new Error(`server at ${url} never became ready`);
}

async function startServer() {
  const work = mkdtempSync(join(tmpdir(), 'groovebox-e2e-'));
  if (!existsSync(BIN)) throw new Error(`binary missing: ${BIN} — run \`go build -o groovebox .\` first`);
  snapshotDB(work);
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
  await page.locator('.grid-card').first().click();
  await page.waitForFunction(() => location.pathname.startsWith('/artists/'), null, { timeout: 15000 });
  const hasAlbums = await page.locator('#artist-detail-container .grid-card').count();
  if (hasAlbums === 0) { log('artist has no album cards — skipping'); return; }
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