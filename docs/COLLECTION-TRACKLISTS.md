# Collection tracklists — 2026-10-24

**All 132 current owned Discogs releases match their complete official release
trees: 1,098 track/component entries, covering 136 physical collection copies.**
This is completeness against Discogs metadata, not an independent examination of
the records or a claim of 1,098 distinct songs. Medley components count as leaves;
their enclosing titles remain visible. There are **zero unresolved release IDs,
source ambiguities, request failures or reconciliation conflicts** in this audit.

[Per-release coverage and source hashes](collection-tracklists-report.json)
records every pressing, before counts, inserted/updated counts, source URL and
retrieval timestamp, expected/matched entries, conflicts and preserved extras.
The collection inventory was freshly read with the existing account token at
21:28 UTC; no credentials or account configuration changed. Full release metadata
uses the anonymous official API. The older cache responses were retrieved earlier
the same day; six missing releases were fetched successfully during validation.
The live repair reused all 132 official responses, without new metadata requests.

| Measure | Before | After |
| --- | ---: | ---: |
| Actual current collection copies / distinct release IDs | 136 / 132 | 136 / 132 |
| DB release versions marked collection | 126 | 132 |
| Collection album flags | 125 | 128 |
| Owned track/component rows | 1,039 | 1,098 |
| Complete owned release trees, including headings/indexes | Not stored | 132 |
| Stored tree nodes | 0 | 1,111: 1,098 leaves, 10 headings, 3 indexes |
| Missing stored owned-track durations | 282 | 311 |
| BPM values | 458 | 458 |
| Known owned vinyl BPM slots | 322 / 936 | 322 / 995 |

**59 missing tracks added, 216 existing artist credits corrected** to the
official ANV/join rendering. No existing track title, position, duration, ID,
BPM/provenance/timestamp or streaming identifier changed in production.
All 19,429 pre-existing track IDs survived. Playlist tables, memberships and
audio-file records were compared with the consistent pre-repair snapshot and
are identical. New track IDs are stable per pressing/slot; no existing ID was
replaced. No playlist sync, speaker playback, audio acquisition or analysis ran.

## Why the lists were wrong

1. **Normal Discogs sync only read summaries.** `DiscogsBasicInfo` has no
   tracklist and `processDiscogsItem` never created tracks. Old Spotify/local
   imports supplied incomplete or different canonical album lists. The later BPM
   importer repaired only releases already marked `source='collection'`.
2. **Wantlist upserts overwrote ownership.** A release can be both owned and
   wanted, but `release_versions.source` is one string. The wantlist pass ran
   second and changed that string to `wantlist`. The live inventory identified
   three such releases: Sakamoto's *Illustrated Musical Encyclopedia* (416723,
   eight tracks), Timberlake's *The 20/20 Experience* (4383047, ten), and
   Sakamoto's *Coda* (31961633, sixteen). The first two were the previously
   documented albums flagged owned without an owned release row.
3. **The DB inventory was stale.** Three actual collection releases were absent
   altogether: Led Zeppelin *Untitled* (6224514, eight tracks), Olivia Dean
   *The Art Of Loving* (36979755, twelve), and *Growth* (38023572, five). This
   accounts for the other 25 missing tracks. Counting only the old eligible
   subset could never establish completeness of the current collection.
4. **Album details flattened unrelated recordings.** Daft Punk *Discovery*
   had eight old catalog rows plus fourteen complete owned rows. *The Bends*
   had twelve catalog plus twelve owned rows. *Wish You Were Here* had six
   catalog rows plus twelve owned components. The old flat UI mixed them
   together, so its length was not a pressing track count or completeness test.
   Timberlake's thirteen old catalog rows were not its ten-track owned LP.
5. **Hierarchy was thrown away.** The old BPM importer already recursed into
   positioned subtracks; it did not miss those actual leaves. It discarded
   heading/index nodes and inherited context, however. Release 8814446's
   “Shine On You Crazy Diamond (1-5)” and “(6-9)” became anonymous “Part 1”…
   “Part 9” rows. ELO release 601218 lost “Concerto For A Rainy Day” above C1–C4.
   Grand Theft Auto V release 6367579 lost its three volume headings above its
   59 entries. Bonus-CD/bonus-track headings also disappeared.
6. **Another deletion hazard existed in album deduplication.** It collapsed
   same-title tracks across different pressings and rewrote playlist references.
   That could destroy version identity and BPM/audio links. It now only moves
   track album associations, preserving every recording ID. No dedupe was run
   on production; no historical execution evidence establishes this as the cause
   of a particular missing row.

All 126 previously collection-marked releases had usable, same-day cached full
responses and matching old leaf counts. No missing cache, failed release request
or skipped positioned nested leaf explained their current counts. The problems
were inventory/ownership, missing source structure, and presentation. Future
unpositioned leaves, malformed data, duplicate slots and provider failures are
explicit blockers rather than silently skipped successes.

## Maintained reconciliation

`scripts/tracklists.py` is the shared implementation used after **every normal
Discogs sync**, the refresh CLI, and the legacy BPM tracklist importer. Sync
passes its fully fetched collection inventory, including duplicate copies, after
committing summary updates. Wantlist membership no longer downgrades an owned
pressing; the independent album wantlist flag is still retained. Refresh failures
appear in sync status and the report; summary success cannot masquerade as
tracklist success.

```sh
# Fresh collection membership; cached official releases up to 30 days old.
timeout 1220s ./groovebox -refresh-tracklists -db music.db
# Force new release metadata too, with a bounded 20-minute run.
timeout 1220s python3 scripts/tracklists.py --verify-collection --refresh --max-seconds 1200
# Offline revalidation against the last captured inventory, whose date is retained.
timeout 60s python3 scripts/tracklists.py --cached-only --inventory .bpm-cache/collection-inventory.json --max-seconds 45
```

The Go CLI/normal sync writes `.bpm-cache/sync-tracklists.json`; direct Python
defaults to `.bpm-cache/collection-tracklists.json`. Nonzero exit means coverage
is incomplete. The inventory capture and complete HTTP responses remain ignored.
With no verified inventory, a per-release success never becomes a whole-collection
completeness claim. New collection summaries can be inserted or promoted from
the exact inventory. Missing/stale IDs are reported; stale owned rows are not
silently deleted by the reconciler.

The API's published documentation is at
[Discogs Developers](https://www.discogs.com/developers/); that documentation page
returned 403 during this session, while official release endpoints worked and
advertised an anonymous limit of **25/minute** in response headers. The client
uses a shared per-host lock at **24/minute**, 15-second request timeouts, a 3 MB
response limit and at most three transient attempts. Access/rate failures stop
new release network requests while allowing usable cached releases to finish.
The collection is private, so inventory reads reuse the existing credential
through `/oauth/identity` and both paginated collection pages; release reads
remain anonymous. No token enters URLs, command arguments, caches or reports.

`tracklists.sql` stores the **entire original release JSON**, full artists/ANVs/
join strings/extra credits/formats, ordered tree entries and per-release audit
state. `schema.sql`/`tracks` continue to hold curated recording identities.
Headings/indexes/medley parents are displayed as groups, not invented recordings.
Leaves retain source positions, including sides, disc prefixes and nested parts.
Repeated numbers under distinct explicit disc headings are separate slots;
duplicate positions within one context are conflicts. Positionless leaves remain
visible and are flagged rather than declared verified physical slots.

Existing rows match only on the same pressing and full title/artist identity;
the previous importer's exact artist rendering of the same official credit is
recognised. No mix/edit/live words are removed. Unique identity can correct a
wrong position without replacing its ID. Conflicting occupied slots, ambiguous
duplicates or changed medley/disc context are retained and reported. Unknown
official durations preserve measured/provider durations; substantial changes to
durations with BPM, audio or curated links require review. Writes take short
`BEGIN IMMEDIATE` transactions and revalidate ownership under the lock. BPM
fields—including a manual NULL—are never written. Catalog tracks never stand in
for an owned pressing, and legitimate extra tracks are never deleted.

External BPM queries now retain index/medley context instead of searching for a
generic “Part 1”. Existing BPM estimates remain unchanged. The larger 995-slot
vinyl denominator reflects recovered tracks, not lower quality or new BPM data.

Album details show each owned pressing separately in official source order,
including headings and nested titles, its source link/date and coverage status.
The first pressing opens initially; other pressings and catalog/local recordings
can be expanded independently. Original BPM and streaming actions remain.
Relinked/deleted track mappings and source snapshots older than 30 days cannot
keep a misleading “complete” badge.

## Validation, deployment and limits

A consistent SQLite backup was taken **before any live repair** at
`.bpm-cache/pre-tracklist-20261024T211534Z.db`. The complete repair first ran on
a copy; its next fresh-inventory run inserted/updated **zero** tracks. Production
repair completed in eight seconds using the validated caches. `integrity_check`
returned `ok`, with zero `foreign_key_check` violations. Runtime DBs, backups,
caches and screenshots are not committed.

Go tests cover normal sync's owned/wanted collision and refresh integration,
ordered hierarchical API output, invalidated completeness and dedupe preservation.
Python tests cover nested medleys, headings, multi-disc repeated numbers,
duplicates/conflicts, partial upgrades, ID/duration/artist corrections, idempotence,
full recording context, protected BPM numbers/manual NULLs, audio/playlists,
streaming IDs/search metadata, missing IDs and bounded failed requests. Mobile
fixtures live only in the isolated test snapshot.

Final checks passed: **32 Python tests**, Go tests and `go vet`, and **16/16
Playwright journeys** (12 desktop plus Chromium/WebKit touch at both 375/390px).
The grouped-tracklist fixtures exercise source ordering, nested titles, durations,
separate pressings and collapsed catalog rows. Initial mobile runs caught and
fixed a real group-row overflow; the final full run has no overflow or JS errors.
Read-only playback GETs are mocked during mobile navigation to avoid WebKit's
cancelled-poll network diagnostic; playback and sync mutations are blocked.

Only `groovebox.service` was restarted, after idle playback and Discogs/local/Tidal
sync checks, using an atomic binary replacement. The running executable's hash
matches the build. All **128 live collection album APIs** were inspected: **132
complete pressing lists / 1,098 mapped leaves**. The real refresh CLI then read a
fresh 136-copy/132-release inventory at **21:37 UTC** and inserted/updated **zero**
tracks. Live API and browser reads left playback and all syncs idle.

Actual live Pink Floyd, Robin S. (including its Bonus CD) and Cosmo pages were
also checked read-only in both engines at both widths: **12 album/viewport cases**,
zero overflow, JS errors or mutation attempts. Representative screenshots were
opened and visually inspected, including:

- `e2e/artifacts/tracklists-live-webkit-375-8814446-tracks.png`
- `e2e/artifacts/tracklists-live-chromium-390-626105-tracks.png`
- `e2e/artifacts/tracklists-live-webkit-390-16057265-tracks.png`
- `e2e/artifacts/mobile-{engine}-{width}-owned-{pressing,medley,second-pressing}.png`

The full suite log is `.bpm-cache/tracklists-e2e-final.log`. Live API, CLI,
preservation and visual-check JSON/logs remain in `.bpm-cache/tracklists-*`.
Static cache version: **11**. These are headless browser checks, not physical
iPhone/Safari or actual audio verification.

**448 official entries have no duration.** Existing independent durations were
retained for 137 of them; **311 remain unknown**. No duration or tempo was guessed.
Discogs itself is community-maintained and may omit information about a physical
copy; this audit proves equality with the full official release metadata available
on the recorded dates, not independent physical/audio verification. Multiple
copies of one release share its one release tree.
