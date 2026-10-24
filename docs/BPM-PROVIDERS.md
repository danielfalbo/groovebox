# BPM provider expansion — 2026-10-24

Actual metadata enrichment completed in a bounded session, without audio downloads,
new accounts, credentials, purchases, playback actions or service restarts.
[Machine-readable counts](bpm-provider-results.json), [23-slot comparison](bpm-provider-sample.json),
and [reviewed provider evidence](bpm-reviewed-metadata.json) are tracked. Runtime
DB, consistent before snapshot, HTTP/job caches and detailed logs remain ignored.

| Measure | Before | After |
| --- | ---: | ---: |
| All stored BPM rows | 135 | 432 |
| Owned pressing slots with BPM | 77 / 1,039 | 362 / 1,039 |
| Distinct owned vinyl slots with BPM | 60 / 936 (6.4%) | 296 / 936 (31.6%) |
| Collection albums with any eligible BPM | 19 / 125 | 38 / 125 |
| Owned slots missing duration | 419 | 287 |
| Local analysis rows | 48 | 48 |
| AcousticBrainz rows | 87 | 114 |
| GetSongBPM rows | 0 | 227 |
| Evidenced recording transfers | 0 | 43 |

**297 additional values**, including 236 additional vinyl slots; 132 missing
owned durations filled. Slots are distinct `tracks.id` tied to actual owned
pressings. Multiple pressings of a song remain separate slots. None of the 135
pre-existing BPM numbers changed; all 48 local-analysis rows (including their
provenance) remained byte-for-byte equivalent. There were no manual rows at the
start; tests also exercise protected manual numbers and explicit NULLs.

## Providers and access

| Provider | Actual findings | Production outcome |
| --- | --- | --- |
| GetSongBPM | Public indexed album pages contain track-level BPM and duration. The [provider describes its database as CC BY 4.0](https://getsongbpm.com/about). | 227 new values from reviewed, attributed metadata; no authenticated API integration. |
| MusicBrainz + AcousticBrainz | Unique recording identification and archived tempo metadata still work; intermittent 503s/timeouts persist. | 27 new values across bulk, sample and cached passes. |
| Deezer | 23 actual owned dance slots searched; 12 exact-title/artist/duration-where-known candidates inspected, all with `bpm=0`. | No BPM imported. Control track 3135556 also returned 0. |
| SongBPM | Real public pages exist (e.g. Starry Night, 123 BPM, 6:39), but [current terms prohibit automated access/collection](https://songbpm.com/legal/terms/). | Stopped after discovery/policy checks; no bulk requests or imported values. |
| Tunebat | Search index surfaced relevant Info pages, but [terms §7(a)(ix) prohibit automated collection](https://tunebat.com/documents/tunebat-terms-of-service-new.pdf). Robots separately allow indexing and AI grounding but do not supersede terms. | Discovery links only; no imported values. |
| Musicstax | Indexed remix metadata exists; [terms §5 prohibit extraction without consent](https://musicstax.com/terms/). | Discovery/policy review only; no imported values. |
| SongData.io | Indexed metadata exists; direct robots/terms requests returned 403. | Stopped; no bypass or imports. |
| Beatport | Relevant exact remix/track pages appeared in search; a real track fetch returned 403 despite permissive robots. | Stopped; no bypass or imports. |

GetSongBPM's API still requires [registration, an API key and a backlink](https://getsongbpm.com/api).
Direct requests here returned 403, so no site scraper or retry/bypass was used.
The independently available search-indexed **CC BY metadata** was reviewed and
transcribed into the evidence file, with page URLs, dates, attribution and license.
No account/key was needed for that path. Dates are retrieval dates; an index may
represent an older page snapshot. Provider data can itself be wrong.

The reviewed file is a finite metadata input, not an arbitrary-source importer or
an invented API integration. It includes rejected/unmatched evidence too. Album
average BPM is never used. Clear counterexamples were rejected: GetSongBPM's
[Chic page](https://getsongbpm.com/album/chic/YvVAK) actually lists C'est Chic songs;
its [To Believe page](https://getsongbpm.com/album/to-believe/Yn0mM) lists remixes,
not the owned original album. A Good Life album version at 4:04 does not identify
the owned Magic Juan/Mayday mixes at 8:28/6:12. Unrelated covers and similarly
named artists returned by search were never accepted.

## Measured dance comparison

The fixed 23-slot sample takes the first two owned vinyl tracks per artist from
the original 12 dance artists (Stardust has one slot), ordered by release ID and
physical position. It includes Peggy Gou, Demuja, Inner City, Stardust, Robin S.,
Shakedown, Aly-Us, Camelphat/Elderbrook, Daft Punk, Chic, The Rhythm Slaves and
Paul Woolford. Failed searches were not replaced with easier songs.

Initially all 23 slots were unknown. **Six now have metadata-supported BPMs**:

| Owned slot | BPM | Evidence |
| --- | ---: | --- |
| Peggy Gou — Starry Night | 122.99 | Unique exact MB recording; archive title/artist and durations agree |
| Peggy Gou — Han Pan | 126.01 | Same rule |
| Demuja — Do You Want My Love | 123.91 | Same rule plus exact A1 prefix/album corroboration |
| Demuja — Get Up | 122.01 | Same rule plus exact A2 prefix/album corroboration |
| Daft Punk — One More Time | 122 | GetSongBPM Discovery; complete ordered original album tracklist |
| Daft Punk — Aerodynamic | 123 | Same album/tracklist evidence |

Final cached archive recheck: 4 accepted, 7 no exact recording, 6 ambiguous
recordings, 4 searches too broad to prove uniqueness, 2 missing archived BPM.
Deezer accepted 0/23 nonzero BPM. GetSongBPM supplied 2/23 accepted slots in this
bounded review; this is **not** proof its remaining database lacks those songs.
Search-result domain counts in the JSON measure discovery only: related songs,
wrong edits and spurious search results are included and are not coverage claims.
Other providers' policy/access blocks are not counted as negative music matches.

All six accepted identities were reviewed against their evidence; no known
version mismatch was knowingly accepted. **No independent beat-grid accuracy
percentage can be claimed**: no audio for these records was analyzed in this task.
Both external sources supply estimated tempos. On 102 matched existing rows in
the wider reviewed set (including separate pressing/local rows), GetSongBPM agreed
within 2 BPM with 75 retained estimates; 27 disagreed more. This is an estimate
comparison, not ground truth or 102 independent recordings. Examples: Bicep Ayr
79 vs archive 161.5, Vespa 167 vs 135.43, Moby Rushing 170 vs local 84.99. Half/double
ambiguity, variable tempo, poor beats and occasional non-integer-ratio disagreement
remain. Existing values were retained; no averaging, octave correction or
provider consensus value was invented.

## Matching improvements and safeguards

- Missing local duration no longer ends lookup immediately. A **unique exact full
  artist/title recording** can qualify if no conflicting MusicBrainz
  disambiguation remains and its duration independently agrees with archive audio
  length. Multiple exact recording IDs remain ambiguous. ISRC search never skips
  identity validation. Remix/edit/live/version words are preserved.
- Archive tags may contain a physical slot, e.g. `A1. Do You Want My Love`.
  Only this actual track's exact slot prefix followed by punctuation may be
  removed, and only with an independently matching archive album. The remaining
  full title, artist and MB/archive duration must still agree. Arbitrary title
  numbers, another slot or a different mix cannot pass. Submitted tags are kept
  in provenance. Cache identity is now `acousticbrainz/exact-v3`.
- Reviewed GetSongBPM rows require exact artist, album and full track title plus
  corroborating duration. Without a local duration, the **complete ordered owned
  core album tracklist** must match (at least four tracks); provider bonus tracks
  may follow it. Conflicting or repeated same-title candidates remain unknown.
  One explicit Florence +/and the Machine alias has linked provider evidence;
  there is no general artist-word deletion or fuzzy matching.
- Successful MB/archive or reviewed album evidence can backfill only a missing
  duration. Provenance retains source duration, URL and matching method.
- Duplicate transfers require the same canonical album, exact full title/artist
  and agreeing nonzero durations. Conflicting known estimates (>1 BPM), unknown
  durations and manual sources do not transfer. Copies preserve the original
  evidence/source track and are not recursively used as authorities. These are
  metadata-evidenced recording transfers, not fingerprint-verified audio copies.
- Updates atomically protect manual values/NULLs and local analysis. Reviewed
  and transfer writes additionally require BPM still NULL at write time.

Prior errors were inspected: 161 historical MusicBrainz 503s, five read timeouts,
350 cached missing-duration owned-slot outcomes, broad/ambiguous searches,
missing archive data and version/tag mismatches. HTTP now retries transient
failures up to three attempts with bounded backoff; 401/403/429 still stop access
immediately. MusicBrainz/AcousticBrainz stay at <=1 request/1.1s; no parallel
requests to a host bypass that rate. Cached reads no longer wait behind another
batch's slow network call. `--cached-only` sweeps all available evidence without
network requests, leaving cache misses retryable instead of recording false misses.

The main 520-second batch processed 153 candidates and wrote 18 values with
117 MB + 40 archive requests; seven item failures remained after bounded retries.
The 180-second resume processed 329 (289 cached), wrote five values, and made
47 MB + 12 archive requests, with one item failure. Sample/probe and cached
passes supplied the other four archive values. Cached sweeps traversed remaining
owned/local candidates; the final sweep made zero requests and left 379 HTTP
cache misses. This is partial coverage, not exhaustion of every provider or all
936 vinyl slots. Unknowns remain explicit. No 14k streaming-catalog crawl ran.

## Reproduce and validate

```sh
timeout 20s python3 scripts/bpm_coverage.py
timeout 320s python3 scripts/probe_bpm_providers.py --max-seconds 300
# Apply only the finite reviewed evidence and corroborated duplicate transfers:
timeout 30s python3 scripts/apply_reviewed_bpm.py --probe
timeout 30s python3 scripts/apply_reviewed_bpm.py
# Resume owned vinyl first, then other owned/indexed collection recordings:
timeout 200s python3 scripts/enrich_bpm.py acousticbrainz --limit 1200 --max-seconds 180
# Reuse cached metadata without touching providers:
timeout 45s python3 scripts/enrich_bpm.py acousticbrainz --cached-only --limit 1500 --max-seconds 40
```

Python tests cover identity/version rejection, missing-duration corroboration,
slot/album prefix validation, full ordered album matching, duplicate conflict
rejection, manual NULL/local priority, atomic unknown guards and duration writes,
retry limits, access-error stopping, offline cache behavior and resuming past
cached negatives. All 13 Python tests and Go tests passed. All 27 newly stored archive rows
also independently revalidated against cached recording/archive metadata. No UI/Go changes or deployment were
needed; the running service reads the enriched DB directly. Playback was idle
on final read-only inspection, SQLite quick_check was `ok`, and track/audio-file/
playlist-membership counts were unchanged. A before snapshot is retained at
`.bpm-cache/pre-provider-expansion.db`.
