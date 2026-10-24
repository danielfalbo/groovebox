# Mobile regression audit — 2026-10-24

The original desktop pass did not establish mobile usability. This audit began
against the running localhost:3000 app, using Playwright touch Chromium at
390×844 and WebKit at 375×844. Every non-GET/HEAD request was blocked. The
original live layout and failures were captured **before edits** and screenshots
were opened with the image viewer. Additional checks replayed the original
committed assets through Playwright routing, without changing the live server.

## Reproduced issues and fixes

| Reproduction | Evidence in `e2e/artifacts/` | Fix |
| --- | --- | --- |
| Open The Bends → pressings on a phone. The table was about 440px wide at x=27 in a 390px viewport; source/link columns were clipped. Track columns squeezed names and version metadata into narrow strips. | `before-chromium-pressings.png`, `before-chromium-tracks.png` | Pressings get a labeled horizontal scroll region and swipe hint; action links wrap. Mobile track rows give titles a full line and actions a separate line. Local file tables also get their own scroller. |
| Open Local Library → local album → Albums. The local hero/play controls remain above collection cards. Tapping a local card leaves the URL at `/local`, so reload loses the detail. | `before-chromium-after-local.png`, `before-rest.log` | Shared view cleanup hides local content. Local detail navigation pushes `/local/:id`; the central router handles reload/back/forward and a visible Back button. |
| Open Create Playlist from the mobile drawer. The dialog is underneath the drawer/backdrop (modal z=1000, drawer z=1200). | `before-create-over-drawer.png` | Close the drawer when opening dialogs and put modal layers above navigation and playback. |
| Open Add Song, reduce viewport height to 500px. Header/close control extend above the screen. | `before-chromium-add-song-short.png` | Bound dialog height, keep header/footer visible and scroll only its body. |
| Inspect phone toolbar, track links, stars and local play buttons. Several targets are 28–32px; a local-file play button remained 28px wide in the first revised layout. | `before-local-detail.png`; early mobile test logs | Phone buttons/links, text and number inputs have at least 44px targets; checkbox labels remain tappable. Numeric BPM inputs use 16px text to avoid iOS focus zoom. |
| Show a paused playback response in a read-only browser mock. Seek/time/volume overlap and metadata disappears at 375px. | `before-player.png` | Mobile player uses separate metadata/transport, seek and volume rows with measured content clearance and large slider hit areas. No actual audio was played. |
| Block unpkg styles. Text becomes black on dark backgrounds and navigation/settings icons disappear. | `before-chromium-no-cdn-pressings.png` | Serve pinned Normalize, Blueprint CSS and icon fonts locally with upstream licenses. |
| Browse songs/artists with failed Discogs cover URLs. Hundreds of `Unexpected identifier 'http'` JS errors occur because the fallback SVG's quotes break inline error handlers. | `before-chromium-requests.json`, `before-probe.log` | Error handlers refer to the shared fallback value; artist and player fallbacks use it too. |
| Submit BPM 130→120, correct to 120→125, submit again with original assets. Custom validity still says “Maximum must be at least minimum”. | `before-edge.log` | Editing either bound clears custom validity, allowing the corrected form to submit. |

Cross-view review also removed the duplicate startup render/router wrapper and
shadowed playlist loader, guarded asynchronous view responses, reapplied
song/artist URL filters after data loads, and explicitly marks global search as
its own view so playlist removal controls cannot leak into search results.
Leaving search also clears its stale navbar query.

## Verification

The read-only live journeys completed after the fixes in Chromium 390px,
WebKit 375px and Chromium 375px with unpkg blocked: no JavaScript errors or
failed requests in those runs (`after-audit.log`). Both engines confirmed the
local detail URL and that local content is hidden after selecting Albums
(`after-rest.log`). Live playlists, database curation and speakers were untouched;
no backend source or service restart was needed.

The integrated runner now executes the existing 10 desktop checks plus four
mobile journeys (Chromium/WebKit × 390/375), with external networking unavailable
throughout mobile journeys. It exercises layout bounds and actual tap targets,
a Chromium touch swipe, real external-link popup navigation, URL reload/back,
modal scrolling, and playlist CRUD/BPM writes **only on a DB snapshot**. Remote
image corruption is intentional and tests the real fallback event path.

Representative after screenshots, all opened for visual inspection:

- `after-webkit-collection.png`, `after-webkit-pressings.png`,
  `after-webkit-after-local.png`
- `after-webkit-add-song-short.png`, `after-create-over-drawer.png`,
  `after-player.png`
- `mobile-chromium-390-pressings-scrolled.png` and the other
  `mobile-{engine}-{width}-*.png` journey captures

Final outcome: **14/14 passed** (10 desktop checks and all four mobile
journeys), Go tests passed, and **6 Python tests passed**. No browsers were
skipped. A fresh live check confirmed `/app.js?v=9` and `/local.js?v=9`, no
page errors and no document overflow in Chromium/WebKit (`final-live.log`).
Final live screenshots are `final-live-{chromium,webkit}-{collection,pressings,tracks}.png`;
the full-resolution WebKit track screenshot and viewport geometry confirm the
navbar remains at y=0 after scrolling. All three application asset cache versions
are 9.

Full result log: `e2e/artifacts/final-tests.log`. Python and independent Go test
results are recorded alongside it in `final-unit-tests.log`. Artifacts are local,
gitignored, and remain under `/home/me/groovebox/e2e/artifacts/`; the test code and
this report are versioned.

## Limits

Headless WebKit is available and was used, but this is not a physical iPhone or
Safari installation. The 500px viewport checks constrained-height layout, not
an actual iOS software keyboard or its focus/viewport behavior. Screenshots are
manually reviewed evidence, not pixel-difference baselines. Real playback and
Discogs/Tidal/local sync mutations were deliberately excluded. Remote artwork
can still be unavailable; its fallback is now usable. Existing API limits (500
songs/artists in those browse endpoints) remain; this was a client UI repair,
not a catalog/enrichment change. No new tempo enrichment was run.
