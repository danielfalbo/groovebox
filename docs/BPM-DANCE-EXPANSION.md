# Dance pressing metadata follow-up — 2026-10-24

**26 additional unknown BPM estimates stored: 21 from decks.de, five from
GetSongBPM.** All 432 existing numbers and their provenance were preserved.
No local/manual estimate was replaced; no octave correction was applied.

| Measure | Before | After |
| --- | ---: | ---: |
| Stored BPM rows | 432 | 458 |
| Distinct owned vinyl slots known | 296 / 936 | 322 / 936 (34.4%) |
| All owned pressing slots known | 362 / 1,039 | 388 / 1,039 |
| Original fixed dance sample | 6 / 23 | 13 / 23 |
| Collection albums with eligible BPM | 38 | 46 |
| Owned slots missing duration | 287 | 282 |

These are track rows, including separate pressing slots, not deduplicated songs.
The 21 reviewed house slots were selected because their evidence qualified;
21/21 is an import result, **not an unbiased provider-coverage measurement**.
The original 23-slot sample was retained without replacing difficult songs.
It still has ten unknown slots. Overall, **614 owned vinyl slots remain NULL**.

## Accepted recording evidence

[Finite store evidence](bpm-dance-reviewed.json), [before/after verification](bpm-dance-results.json)
and [independent known-estimate comparison](bpm-dance-crosscheck.json) are tracked.
Each store entry retains its actual source title/artist, catalog number, source
slot, URL/date, expected local identity/duration, and explicit identity review.

| Owned record / exact mix | Store BPM |
| --- | --- |
| Shakedown, DFTD050X — Peggy Gou's Acid Journey / Tiger & Woods / Purple Disco Machine **Extended** / Galactic Boogie | 124 / 127 / 122 / 127 |
| Robin S., CHAMPC1204 — Stonebridge Club / Todd Edwards / Diesel / Kerri Chandler | 123 / 125 / 126 / 123 |
| Camelphat & Elderbrook, DFTD523 — Cola / Dario D'Attis / Franky Rizardo / Mousse T.'s Glitterbox | 122 / 122 / 121 / 122 |
| Stardust, BEC5543668 — Music Sounds Better With You, original long version, **6:55** | 124 |
| Recondite, HFT046 — Phalanx / Warg | 121 / 122 |
| &Me, KM046 — The Rapture Pt.II / Solaris | 120 / 160 |
| &Me & Black Coffee, KM066 — The Rapture Pt.III / &Me — L.I.F.E | 120 / 119 |
| Paul Hill / Nikki-O, MM-33 — Need Me Some U / Music | 108 / 120 |

Store product identifiers and the complete ordered named mixes anchor missing
durations. This is **metadata evidence of recording identity**, not an audio
fingerprint or proof that a repress has identical mastering. No missing store
duration was invented. Stardust additionally matches barcode and 415,000 ms;
the separately indexed 6:43 collector edition was rejected. Robin's store page
abbreviates titles to mix names under the song heading and calls side AA “B”;
only the four real vinyl mixes were mapped. Its empty B3 placeholder and six
owned CD bonus slots received nothing. KM046's shortened source title “The
Rapture” was reconciled explicitly using the product's Pt.II title/catalog and
paired Solaris; it was not confused with another part. MM033 versus MM-33 is a
finite reviewed catalog spelling, corroborated by both split-side artist/title
pairs; there is no automatic catalog-number aliasing.

GetSongBPM already had all 15 Cosmotronic recording values, but its page
interleaves equal track numbers from two discs. Correcting the **evidence order**
to the original nine-plus-six sequence, independently corroborated by
[label-delivered album metadata](https://music.amazon.ca/tracks/B078Y8M722),
allowed the unchanged conservative full-album matcher to accept five missing
slots: Turbo 121, Sei la mia città 115, Quando ho incontrato te 119, Attraverso
lo specchio 121, La notte farà il resto 120. Existing ten values were retained.
Five missing durations were filled. Original titles, BPMs and durations were
not edited. Provenance now includes this ordering evidence.

## Independent estimate disagreement

Two Peggy Gou and twelve Bicep recordings were cross-checked against separate
store references. **Seven of fourteen differ by more than 2 BPM**:

| Bicep recording | Retained | decks.de |
| --- | ---: | ---: |
| Glue | 129 | 104 |
| Kites | 136 | 181 |
| Vespa | 135.43 | 164 |
| Ayaya | 109.02 | 144 |
| Drift | 166.42 | 127 |
| Ayr | 161.5 | 107 |
| Vale | 128.04 | 86 |

Peggy Gou agrees within 0.01 BPM; Bicep Spring, Opal, Rain and Aura within 0.05;
Orca differs by 1.06. This sample demonstrates substantial source disagreement,
including non-octave differences. It does not establish which estimate is right
or an accuracy percentage. The new store numbers are **attributed estimates**,
with unverified beat grids; exact mix identity does not guarantee tempo accuracy.
All existing estimates remain untouched. No audio or previews were requested.

## Sources and concrete limits

| Route | This session's outcome |
| --- | --- |
| decks.de | Ten finite product references fetched (nine initial products plus Paul Hill); metadata-only requests at least 1.5s apart. Seven initial unknown records plus the split record qualified; Peggy/Bicep were comparison controls. No site search endpoint or catalog crawl. |
| [Store terms](https://www.decks.de/decks/workfloor/info/agb.php), [robots](https://www.decks.de/robots.txt) | Reviewed sales terms had no automated metadata-collection clause. Product paths are allowed for the review client; signals specify search=yes, ai-train=no, use=reference. Finite personal factual references were used; this is not an open-data license or permission for a general scraper. |
| [GetSongBPM](https://getsongbpm.com/about) | CC BY 4.0 metadata reviewed through independently available search-indexed pages. No direct retry around its prior 403. Five Cosmo additions; most exact single/remix searches failed to surface eligible metadata. |
| [Beatport](https://support.beatport.com/hc/en-us/articles/4414997837716-Terms-and-Conditions) | Current September 28 terms, section G, prohibit metadata text/data mining. Prior product 403 remains respected; no new product fetch, API, alternate endpoint or index-based import. Permissive robots do not override terms. |
| [Traxsource](https://www.traxsource.com/terms-of-service) | Site-content copying restrictions do not provide this collection permission. Robots disallow /search and request 10s crawl delay. Policy review only; no song imports. |
| Juno Download | Direct robots lookup failed DNS resolution. [Distributor support](https://support.dittomusic.com/en/articles/8671981-can-i-release-music-to-juno-download) reports permanent closure. No archive/mirror workaround. |
| MusicBrainz / AcousticBrainz | Seven rate-limited URL-to-owned-release relation requests mapped Inner City, Robin and Cola; four other exact release links were absent. A subsequent web fetch returned 429; all further MusicBrainz access stopped. Cached-only inspection found zero pending archive requests with unique exact candidate identity in the focused dance set; no new archive numbers. Existing ambiguous mixes were not guessed. |
| [BPMDatabase](https://www.bpmdatabase.com/terms/), [Mixgraph](https://www.mixgraph.io/terms), [12NOTEZ](https://12notez.in/terms) | Explicit collection/copy restrictions; no imported values or subsequent product fetching. |
| [Whatpeopleplay](https://www.whatpeopleplay.com/) | Current home page says shop is inactive. No catalog crawl. |
| [Sonovault](https://sonovault.now/mcp) | Documented metadata access needs a new API key, including its free tier; excluded by task scope. No signup/key. |
| BeatStats | Discovery exposed a relevant mix; a terms URL and robots could not be verified through the web tool. No import without established permission. |
| SongBPM / Tunebat / Musicstax / SongData | Existing documented collection restrictions/access failures retained. No direct retries or imported search snippets. |

Concrete rejects include Inner City's indexed KMSV003 reissue (different mixes
from owned TENX249); Aly-Us SR1288W white repress versus owned SR1288, without
duration corroboration; Robin's album/extended version versus the owned named
mixes; and Crystal Waters' 7:31 Basement Boy album mix versus the owned 7-inch
edits/other versions. WestBam metadata exposed a title mismatch and competing
store/provider tempos without local durations; none imported. Satoshi Tomiie's
AA001/AA004 are not the owned AA014 three-version pressing. Focused searches
for Demuja's missing Don't Cry, Woolford NRK121, Rhythm Slaves UND-016, OOFT!,
Marek Hemmann FAT047, Crystal Waters, Martin Solveig DOTB-27, Hugel and Dames
Brown DFTD759 did not establish further eligible exact pressing evidence within
this session. Negative searches are not proof those sources lack the music.

The remaining ceiling is a combination of unavailable/permitted sources,
unproven mix identity, missing durations and unreliable estimates. This bounded
review exhausted the tested routes, not every public provider or the collection.
No broad streaming-catalog enrichment, new accounts/keys, purchases or audio
analysis/downloads ran. No frontend/backend changes or service restart occurred.

## Validation and reproduction

`scripts/apply_dance_bpm.py` is an offline finite-evidence importer. It checks
every reviewed local ID, artist, full title, position, release and duration plus
actual owned vinyl eligibility. Each identity check and guarded unknown-only
write occurs under a short BEGIN IMMEDIATE transaction. Existing/local/manual
values and explicit manual NULLs cannot be replaced; reruns are idempotent.
It does not fuzzy-match or fetch any website. No duplicate transfers qualified.

Before mutation, SQLite backup retained `.bpm-cache/pre-dance-expansion.db`.
Both import paths were exercised against a disposable copy first. All 16 Python
tests and Go tests passed. Snapshot and live integrity_check returned `ok`;
foreign_key_check returned zero violations. All 432 pre-existing BPM/provenance/
timestamp tuples and all 48 local rows were identical afterward. There were no
manual rows in production; tests exercise both protected manual cases. Track,
album, pressing, audio-file, playlist and membership counts were unchanged.

```sh
timeout 30s python3 -m unittest discover -s scripts -p 'test_*.py'
timeout 60s go test ./...
timeout 20s python3 scripts/apply_dance_bpm.py --probe
timeout 20s python3 scripts/apply_dance_bpm.py
timeout 20s python3 scripts/apply_reviewed_bpm.py --probe
timeout 20s python3 scripts/bpm_coverage.py
```

Source evidence, results and limitations are tracked; runtime DB, snapshots,
HTTP responses and scratch logs remain ignored. Current import probe returns
zero pending entries. The existing reviewed-album probe likewise has no pending
additions. No live playback, playlist or sync actions were performed.
