# Groovebox testing

Run all Go, desktop and mobile UI checks with:

```sh
timeout 360s sh e2e/run.sh
timeout 30s python3 -m unittest discover -s scripts -p 'test_*.py'
```

The runner builds current Go sources, snapshots `music.db` using SQLite backup,
adds synthetic tempo/search fixtures to that disposable copy, and starts a server
on a random port. It removes the server and snapshot afterward. It never writes
to live `music.db` or uses live port 3000. No npm dependencies are added; it uses
the globally installed Playwright and browsers in `~/.cache/ms-playwright`.

`run.mjs` covers desktop navigation, back, new tabs, filter pills, polling errors,
BPM range semantics and protected manual overrides. Remote image bytes are made
invalid deliberately to exercise fallback covers without relying on a CDN.

`mobile.mjs` runs complete touch journeys in Chromium and WebKit at **390×844
and 375×844**. Missing browser binaries are explicitly reported as skipped;
an installed browser that cannot launch fails the run. Each journey covers:

- Collection, invalid/corrected BPM ranges, vinyl filtering, reload/back/forward,
  album details, pressing links and track BPM correction.
- Real horizontal touch swipes in Chromium and scrolling-to/tapping the pressing
  link in both engines; readable track widths and bounded table scrollers.
- Sidebar destinations, songs and artist filtering/reload, artist album links,
  local details/reload/back and leaving Local Library.
- Playlist headers, edit/search/selection dialogs, global search, settings, and
  creating/adding/removing/deleting a disposable playlist through real taps.
- Add Song dialogs at 844px and **500px height**, header/footer hit testing, and
  a mocked paused playback state, without playing audio.

Layout checks include viewport bounds, unintended content overflow, at least
44px control hit targets, `elementFromPoint` visibility, title widths, and
non-overlapping player controls. Screenshots at each view are written to
`e2e/artifacts/mobile-{engine}-{width}-*.png` (gitignored). These complement actual
navigation/taps and request/JS error checks; screenshot files are not a pixel
baseline, and must also be viewed after layout changes.

All external requests are made unavailable during mobile journeys. Styles and
icon fonts must load from local assets; corrupt external images must fall back
without JS errors. Playback, library sync and Tidal mutation requests are
blocked and fail the tests. Playlist/BPM writes only reach the isolated snapshot.

`BASE_URL=http://localhost:3000 sh e2e/run.sh --live` runs read-only desktop
checks; the BPM write checks and full mobile mutation journeys are explicitly
skipped. For live mobile inspection use a fresh touch context with a route guard
that aborts every method except GET/HEAD. Never tap live playback/sync controls.

See [the mobile audit](../docs/MOBILE-AUDIT.md) for reproduced bugs, evidence and
limits of headless verification.
