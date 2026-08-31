# Groovebox testing

## Unit tests (Go)
Pure logic lives in `main.go` (`NormalizeAlbumTitle` and friends). Run:

```sh
go test ./...            # or: npm test

## E2E (Playwright, no npm deps)
The UI regression suite lives in `e2e/run.mjs` and covers the bugs that have
bitten before:

- Back from an album detail hides the hero (`album-detail-container` display:none)
- ctrl/cmd-click and middle-click open an album in a new tab (plain click still
  SPA-navigates)
- no reserved 84px bottom padding for the now-playing bar while idle; padding
  applies only when `body.np-active` (playback running)
- album grid renders, wantlist pill switches view, artist-page album cards
  open new tabs, too

### How it works
`e2e/run.sh` runs Go unit tests, then `e2e/run.mjs`:

1. snapshots the real `music.db` via python3 `sqlite3.backup()` (consistent
   even with WAL) into a temp dir — the live service/DB is never touched
2. boots an isolated `groovebox` on a random port against the snapshot
3. drives a real Chromium (global Playwright) through the scenarios
4. kills the server, deletes the temp dir, exits non-zero on any failure

Requires the global playwright install and its chromium browser:

```sh
npm root -g          # → e.g. /home/me/.npm-global/lib/node_modules
npm ls -g playwright
~/pie/node_modules/.bin/playwright install chromium   # once, if browsers missing
```

### Run against the live server instead
```sh
BASE_URL=http://100.110.199.94:3000 sh e2e/run.sh --live
```
(avoid unless you know the live DB is safe to read from — tests never write
to the snapshot but the live instance is yours.)