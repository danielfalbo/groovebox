# Track tempo and owned-record browsing

Current dance follow-up: **458 BPM rows; 322/936 owned vinyl slots**. See
[dance evidence, exact mixes and source disagreements](BPM-DANCE-EXPANSION.md)
for 26 additional attributed estimates and their validation/limitations.

Previous provider expansion: **432 BPM rows; 296/936 owned vinyl slots**, up from
135 rows and 60/936. See [provider results and matching rules](BPM-PROVIDERS.md)
for the completed multi-provider research, 23-slot sample, 297 actual additions,
132 duration backfills, safeguards and remaining unknowns. The rollout numbers
and provider restrictions below record the initial run; the expansion document
supersedes them where indicated.

Collection → BPM from/to → Find matching tracks. Bounds are inclusive; Vinyl
only requires the actual owned vinyl release. Cards list matching track names,
positions and pressing details. Unknown values do not match a range. There is
no album-average BPM and no crate tagging. The collection count badge remains
the full collection count; the tempo status reports matching records.

Album tracklists have editable BPM buttons, with source, timestamp and evidence
in their tooltip. A manual value, including an explicit unknown/NULL, is protected
from enrichment. **Auto** removes that override and clears the value so the next
bulk run can enrich it. Local, songs, artists and playlist track views show BPM.
Browser navigation and shareable URLs preserve bounds, search and vinyl mode.

## Data and recording identity

`tracks.bpm` is nullable REAL (20–400); `bpm_source`, `bpm_provenance` (JSON text)
and `bpm_updated_at` record evidence. `ensureColumn` adds each field safely on old
databases; the schema includes the same columns for new databases. Unknown stays
NULL. Sources are `manual`, `local:librosa`, `external:acousticbrainz`,
`external:getsongbpm`, `external:decks`, and `external:recording-copy`.
Updates atomically check the source so a manual correction made during analysis
wins. External results cannot replace local analysis.

`PUT /api/tracks/:id/bpm {"bpm":123.4}` sets a manual override; a null BPM locks
unknown; `DELETE` clears the override. `/api/albums?filter=collection` accepts
`bpm_min`, `bpm_max`, `vinyl=1`, and `q`. It returns coverage and `matching_tracks`
for an active range. Invalid/nonfinite/inverted bounds return 400.

Discogs collection sync historically imported release summaries, without
tracklists. The bulk tool fetches official release metadata to add tracks tied
to each **owned pressing**, with Discogs positions and per-track artists. IDs
are deterministic; reruns don't create duplicates or change existing tracks.
Vinyl mixes aren't inferred from Spotify/catalog tracks sharing an album. Raw
whole-side files aren't assigned a track BPM. Collection coverage counts owned
release tracks plus indexed local recordings, so multiple pressings/recordings
may count separately. Vinyl mode counts only owned vinyl release tracks.

## Bounded, resumable enrichment

Run from this repo. No audio is uploaded, downloaded or modified. Only the
local analyzer needs the isolated Python environment. Setup and commands:

```sh
timeout 60s python3 -m venv .venv-bpm
timeout 180s .venv-bpm/bin/pip install -r scripts/bpm-requirements.txt
timeout 90s go build -o .bpm-cache/groovebox-next .
timeout 30s .bpm-cache/groovebox-next -migrate-only -db music.db
# Read owned Discogs metadata (no token required, at most 24 requests/minute).
timeout 620s python3 scripts/enrich_bpm.py tracklists --limit 150 --max-seconds 600
# Analyze indexed local files with one bounded child at a time.
timeout 920s python3 scripts/enrich_bpm.py local --limit 100 --max-seconds 900
# Optional, archived external metadata; no login or credentials.
timeout 620s python3 scripts/enrich_bpm.py acousticbrainz --limit 300 --max-seconds 600
# Research before adopting a provider; never writes BPM.
timeout 320s python3 scripts/enrich_bpm.py acousticbrainz --sample-dance --probe --max-seconds 300 --report .bpm-cache/probe.json
```

Create `.bpm-cache/` before the build if it doesn't exist. The tool defaults to
this directory for HTTP responses, per-input outcomes and progress, all ignored
by git. `--db`, `--music-root`, `--python`, `--cache`, `--limit`, `--max-seconds`,
`--report`, and `--refresh` are configurable. No generic arbitrary-provider or
unverified API stub is installed. Each processed item and the final counters
print as JSON; redirect output to an ignored log to monitor a batch with `tail`.

Repeat a bounded batch to resume. Completed outcomes are reused and applied before new network work, so a provider
outage cannot starve a cached success later in album order. Network/file failures
remain retryable. The item limit counts new attempts, so cached misses
don't trap subsequent runs at the first batch. HTTP caches expire after 30 days;
`--refresh` retries cached outcomes and refetches metadata. Known local values
are skipped normally; use refresh after replacing an indexed audio file. SIGINT
or SIGTERM ends after the current bounded operation and writes the report.

Local analysis uses maintained [librosa 0.11.0](https://librosa.org/doc/0.11.0/generated/librosa.beat.beat_track.html)
(ISC license). FFmpeg decodes three interior windows of up to 45 seconds at 22.05 kHz mono.
Beat timing regression reduces tempo-bin quantization. All three estimates must
agree within 4%, with at least 16 beats per window, or BPM remains unknown.
Tracks under 45 seconds/over 20 minutes and silent signals are skipped. It is an
estimate, not a verified beat grid: variable-tempo tracks and half/double tempo
ambiguity still need listening/manual correction. Evidence includes every
window estimate, offsets, algorithm/library versions and file identity. Each
worker has a 90-second timeout, FFmpeg/ffprobe have 20/15-second timeouts, and
numeric libraries use one thread. Only one audio-analysis worker runs per batch.

External HTTP calls have a 15-second timeout, response-size cap and on-disk cache.
Per-host file locks coordinate request rates across batches: MusicBrainz and
AcousticBrainz at most one request per 1.1 seconds; Discogs per 2.5 seconds.
401/403/429 stop the batch; three consecutive transient failures also stop it.
Successful partial writes are committed per item/release. No long DB transaction
is held during analysis or HTTP calls. A time budget leaves room for the next
operation; the enclosing shell timeout is an additional bound.

## External sources researched on 2026-10-24

- [AcousticBrainz](https://acousticbrainz.org/) says collection stopped in 2022,
  but its documented [read API](https://acousticbrainz.readthedocs.io/api.html)
  still answered real requests. Its CC0 data is indexed by MusicBrainz recording
  ID. The tool requires a **unique exact title, full artist credit and duration**
  (within max(3 seconds, 1%)), retaining all remix/edit/live words. Existing ISRCs
  narrow the search but don't bypass these checks. An archive submission must
  independently agree on title, artist and audio duration. Ambiguity, missing
  durations, missing archive data or conflicting versions leave BPM unknown.
  Evidence records recording MBID, source URL, matching method and durations.
  [MusicBrainz search](https://musicbrainz.org/doc/Development/XML_Web_Service/Version_2/Search)
  and [rate limits](https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting)
  were checked. MusicBrainz returned intermittent HTTP 503s during the run.
- [GetSongBPM's documented API](https://getsongbpm.com/api) requires registration,
  a key, email and mandatory backlink, even for private apps. No account/key was
  created or requested. Its coverage therefore remains untested; no integration
  claims or guesses about its dance coverage are made.
- [Spotify removed audio-feature access for new/development apps](https://developer.spotify.com/blog/2024-11-27-changes-to-the-web-api).
  No new app, subscription or credentials were obtained.
- Deezer's public metadata endpoints were probed without requesting any audio.
  The official [track reference](https://developers.deezer.com/api/track) and
  [search reference](https://developers.deezer.com/api/search) redirected to login
  from this server. The live track metadata control reported BPM=0. A 12-record
  dance probe found no safely matched nonzero BPM (see JSON evidence). No Deezer
  production integration was added without a usable verified reference/coverage.

The repeatable AcousticBrainz dance sample uses 12 owned records: Peggy Gou,
Demuja, Inner City, Stardust, Robin S., Shakedown, Aly-Us, Camelphat/Elderbrook,
Daft Punk, Chic, The Rhythm Slaves, and Paul Woolford. It tests their actual
owned-release tracklists, not an arbitrary popular-song sample. See
[bpm-dance-probe.json](bpm-dance-probe.json) and
[bpm-deezer-probe.json](bpm-deezer-probe.json) for measured outcomes. The archive
integration is optional and cannot fill the vinyl-only dance collection reliably.
The broader collection scan did find exact matches outside this initial sample,
including seven tracks on the owned Bicep pressing (Discogs release 25801321).

## Initial rollout results and operation (before provider expansion)

Measured final database state (2026-10-24):

| Item | Result |
| --- | ---: |
| Albums flagged in collection | 125 |
| Owned pressings with fetched tracklists | 126 across 123 albums |
| Imported owned-pressing track slots | 1,039 (57 sample + 982 remaining) |
| Local song files / distinct linked tracks | 94 / 93 |
| Local tracks with accepted BPM estimates | 48 / 93 |
| Local tracks left unknown | 44 inconsistent windows + 1 insufficient beats |
| Unsplit raw files excluded | 3 |
| Stored external BPM track rows | 87 |
| Stored BPM rows total (local + external) | 135 |
| Owned-pressing slots with BPM | 77 / 1,039 |
| Owned-vinyl slots with BPM | 60 / 936 |
| Collection albums with any known track BPM | 19 / 125 |
| Owned-release tracks missing durations | 419 |
| Pending external outcomes or transient failures | 315 |

These are track rows/pressing slots, not deduplicated songs. Coverage is partial,
particularly for vinyl-only dance mixes. The original 57-track dance probe
accepted **0** results: 33 missing durations, 9 no exact recording, 6 ambiguous
recordings, 3 broad/ambiguous searches, 3 missing archive BPM, 1 archive version
mismatch, and 2 HTTP errors. This does not prove that no external BPM exists.
The broader pass found seven Bicep tracks, including Orca 122.94, Opal 129.95 and
Rain 123.01, matched to owned Discogs release 25801321. Archive values are estimates.

All 93 local track identities were evaluated in the successful bulk run. A
second local run reused all 45 unsuccessful cached outcomes, skipped 48 already
analyzed tracks, and spawned no analyzer processes. The real analyzer also
returned 120.00 BPM on a generated 120 BPM percussion test signal.

External enrichment is **not exhausted**. The first pass had an 18-minute budget
and processed 922 of the 923 candidates in its initial snapshot (tracklist import
was still finishing). It made 512 MusicBrainz and 111 AcousticBrainz requests and
recorded 160 transient failures. The bounded resume made 10 + 5 requests and
stopped after three consecutive provider failures (six failures total, mostly
15-second read timeouts plus 503s). A final pass applied four successful cached
outcomes before attempting one new lookup. There are no enrichment workers left
running. Re-run the documented bounded external command when the provider is
healthy; cached negative outcomes cost no new requests. Use `--refresh` only to
revisit completed negative matches or changed metadata. Some pending rows have
never been queried; others need a retry. Missing duration and ambiguous version
matches will continue to stay unknown rather than receiving a guessed tempo.

The 1,115 owned-release/local candidates currently divide into: 40 local BPM,
87 external BPM, 350 cached missing-duration outcomes, 126 no exact recording,
82 broad searches, 75 ambiguous recordings, 32 missing archive BPM, 8 archive
version mismatches, and the 315 pending/failing outcomes. Eight additional local
BPMs belong to local albums outside the Discogs collection. Two albums flagged
as owned (Justin Timberlake's The 20/20 Experience and Ryuichi Sakamoto's
Illustrated Musical Encyclopedia) have only wantlist release rows in the existing
DB; no owned pressing/tracklist was inferred for them.

`music.db` is ignored, not tracked; the established backup policy remains in
place. A consistent pre-migration SQLite backup is kept locally at
`.bpm-cache/pre-bpm.db`. No database, cache, tokens or audio is committed.
The backend was built and atomically installed, then only `groovebox.service`
was restarted after playback and both syncs were confirmed idle. The service
is active, the installed binary matches the built candidate, and real collection,
track and BPM-filter APIs passed health checks. Playback remained idle. SQLite
`quick_check` returned `ok`. Static asset cache versions are 7. The final live vinyl-only 120–130 BPM check
returned 7 records with 12 matching tracks.

Verification passed: `sh e2e/run.sh` (10/10), standalone `go test ./...`, and
6 Python tests. A real mobile Chromium smoke check at 390px displayed matching
vinyl tracks, with no page overflow or JavaScript errors. Test fixtures never
entered the live database. Full item logs and pre-feature backups stay in ignored
`.bpm-cache/`; only aggregate results and the metadata-only dance probes are
tracked here.

Validation: Go tests cover migration preservation/idempotence, bounds, actual
owned-release matching versus averages/unowned edits, and manual API behavior.
Python tests cover conservative recording/version matching, archive evidence,
manual NULL protection/local priority idempotent tracklist import, resuming beyond cached misses and applying cached
successes before provider failures. Playwright
covers matching-track display, bounds across reload/Back, unknown exclusion,
manual edits and reset using fixtures confined to the isolated DB snapshot.
The e2e runner builds current Go sources into its temporary directory; it no
longer silently tests a stale live binary. `--live` skips fixture/write tests.
