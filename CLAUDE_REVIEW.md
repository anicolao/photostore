# Claude Review

Review of the Photostore design documents and implementation, originally
performed on branch `codex/ui-thumbnails` (2026-07-03). Updated the same day
after assessing the remediation series on `codex/claude-review-notes`
(`870069d`..`adfd9a1`, thirteen commits following the review notes commit).

Verification for this update: every remediation diff was read against the
original finding, and the full check suite was run — `go test ./...`,
`bun run check:precommit` (E2E rules + svelte-check), and the Playwright E2E
suite. All pass.

## Verdict

The remediation series addresses **every critical finding, every
correctness/robustness finding, and all but a few deliberate deferrals** from
the original review — and the deferrals are now recorded in the design docs
rather than left implicit. Several fixes went beyond the recommendation
(capture-time provenance, scan-failure projection, SSE conversion). The
original review's assessment stands and is now stronger: the document chain is
coherent, the implementation honors it, and the doc/code contradictions that
existed have been resolved on the code side, the doc side, or both —
deliberately, per finding.

Two small new issues were introduced by the server hardening (see "New
Observations"); both are edge-case usability problems, not security holes.

## What Is Strong

- **Event-sourcing discipline.** Events record configuration, copies,
  materializations, and lifecycle; duplicate groups, inventory matches, and
  assets are projections. `events_applied` is an idempotence guard,
  `projection_state.next_offset` is the replay cursor, and cursor advancement
  shares the SQLite transaction with reducer writes.
- **Crash-tolerant byte handling**, now stronger than at review time: temp-file
  + rename for acquisition, temp-link + rename for dedup relink, `EEXIST`
  tolerated in CAS materialization, `io.ReadFull`-based byte comparison.
- **Cross-process safety.** `flock` around append + projection apply, WAL-mode
  SQLite, startup replay from the cursor.
- **Versioned derivation surfaces.** CAS `cas/sha256/v1`, thumbnails now
  `thumbnails/jpeg/240/orient-v3` (bumped with the CatmullRom upgrade, exactly
  as the recovery story prescribes), extractor v2, dedup strategy v3.
- **E2E rigor.** Zero-pixel screenshots, no `waitForTimeout`, enforced 2000 ms
  rule, deterministic IDs/clock, macOS CI.
- **Docs are now current**: README status, MVP_IMPLEMENTATION_PLAN event
  catalog and hard-link rationale, DESIGN_OVERVIEW decisions, E2E_GUIDE,
  PHOTO_TIME_VIEWS implementation-status note, MVP_USAGE.

## Resolution of Original Findings

### Critical

1. **Capture-time fallback to CreateDate/ModifyDate — FIXED** (`870069d`).
   Both fallback branches removed; only `datetime_original` produces a capture
   time, so affected photos land in the Undated view as designed. The fix also
   resolved two of the related provenance drifts: `source_kind` now records
   `exif_datetime_original` (the design's vocabulary, not the occurrence kind)
   and `source_event_id` now points at the extraction event. A companion UI
   fix (`adfd9a1`) removed the same fallback chain from the image-details
   page — a spot the original review missed — which now shows
   "No DateTimeOriginal" instead of a silently substituted date.
   *Residual:* `precision` is still hardcoded `"second"`, but since the parser
   only accepts full `YYYY:MM:DD HH:MM:SS` strings, that value is currently
   truthful; the precision vocabulary matters only when partial-precision
   dates are implemented. `subsec_time_original` remains extracted-but-unused.

2. **Metadata mismatch detection dead — FIXED** (`80ce022`, refined in
   `4a03eb3`). `recordMetadataForCandidate` now re-extracts when a success row
   already exists for the content, deep-compares against the projection, and
   emits `PhotoMetadataObservationMismatchDetected` on divergence — including
   the case where re-extraction *fails* after a prior success. This covers
   both the scan-time duplicate-content path (design steps 6–8) and
   refresh-missing. `extraction_context` is now threaded through truthfully
   (finding 9 below). Tests cover match, mismatch, and failure-after-success.

3. **Server exposed to cross-site requests — FIXED** (`dbdb809`). Requests
   whose `Host` does not match the configured listen address get 403 (closes
   DNS rebinding); mutating `/api/` routes require `Content-Type:
   application/json` parsed via `mime.ParseMediaType` (closes no-preflight
   CSRF); the event stream checks `Origin` against the request host. The
   later SSE conversion (`f6e11c2`) carried the origin check over. See "New
   Observations" for two edge cases the Host check introduced.

### Correctness and Robustness

4. **CAS materialize `EEXIST` race — FIXED** (`2252319`). `os.Link` returning
   `os.ErrExist` is now treated as success, matching the already-existed
   branch.

5. **Dedup relink not crash-atomic — FIXED** (`2252319`). The relink now hard
   links the canonical file to a unique temp name in the same directory,
   chmods it, and `os.Rename`s over the duplicate. The event payload's
   verification block was updated (`atomic_relink: true` replaces
   `delete_duplicate_before_relink`), keeping the durable record truthful.

6. **Lockstep short-read assumption — FIXED** (`2252319`).
   `verifyDuplicateFiles` uses `io.ReadFull` per buffer and treats
   `io.ErrUnexpectedEOF` as EOF, so differing short-read boundaries can no
   longer mark identical files as different.

7. **`BytesReleased` overstated — FIXED** (`2252319`). The summary now counts
   released bytes only for candidates with `cas_existed_at_ingest = 1`,
   matching the dashboard's `retained_duplicate_bytes` definition.

8. **Scan serialized behind `contentMu` — FIXED** (`4a03eb3`). EXIF extraction
   now runs outside the lock; the metadata recorder re-reads the projection
   after extraction, and the `PhotoMetadataExtracted` reducer only fires when
   the `insert or ignore` actually inserted a row, so concurrent extraction of
   the same content is benign. *Residual:* `contentMu` is still one global
   mutex over check-exists → append → link rather than per-content-ref, but
   with the CPU-heavy work moved out, what remains is mostly the event append,
   which a single JSONL log serializes anyway.

9. **False `extraction_context` — FIXED** (`80ce022`). Phase
   (`ingestion_scan` / `metadata_refresh_missing`) and the occurrence's real
   `source_kind` are threaded through to the event payload.

### Smaller Issues

- **`IngestionScanFailed` never emitted — FIXED** (`85ae269`). Both scan and
  resume paths append it on failure, with an error payload and stats snapshot;
  a projection handler marks the `scans` row `failed`. The
  `IngestionScanCompleted`/`Failed` reducers now also preserve
  `started_at_ms`, fixing a latent projection quirk the review hadn't flagged.
- **Undocumented event types — FIXED** (`2161742`). The plan's "Exact MVP
  Event Types" catalog now includes all 21 event families actually emitted,
  including `IngestionScanFailed`, `IngestionScanResumeRequested`, the
  metadata family (with `PhotoMetadataObservationMismatchDetected`), and the
  dedup family — making the plan the canonical payload reference the review
  asked for.
- **Init re-emits `StoreInitialized` — FIXED** (`85ae269`). `Init` checks
  `events_applied` and returns the opened store without appending a second
  event.
- **Actor missing `hostname` — FIXED** (`85ae269`).
- **`serveBuildFile` probing CWD — FIXED** (`85ae269`). The filesystem UI is
  served only when an explicit `--build-dir` flag is given; otherwise the
  embedded UI is used. No CWD probing remains.
- **Hand-rolled WebSocket + unbounded jobs map — FIXED** (`f6e11c2`,
  `fd50f47`). The event channel is now SSE (`GET /api/events/stream`) as
  MVP_UI_DESIGN originally recommended; all custom frame/upgrade code is
  deleted and the client uses `EventSource` with automatic reconnect.
  Completed jobs are pruned beyond a retention limit of 100.
- **`PHOTOSTORE_DETERMINISTIC_IDS` in production paths — MITIGATED**
  (`b70c056`). Deterministic IDs now require a second opt-in variable
  (`PHOTOSTORE_ALLOW_DETERMINISTIC_IDS`), set only by the E2E test server.
  Still env-gated rather than injected, but accidental production activation
  now requires two deliberate settings.
- **Hash-only inventory lines dropped — DOCUMENTED AS DEFERRED** (`206a715`).
  INGESTION_DESIGN, the plan, and MVP_USAGE now state that `.lookup`/`all`
  hash-only entries are skipped in the MVP.
- **Nearest-neighbor thumbnails — FIXED** (`4f0e4b9`). Scaling now uses
  `golang.org/x/image/draw` CatmullRom through an orientation-mapping adapter,
  and the renderer namespace was bumped `orient-v2` → `orient-v3`, exercising
  the versioned-derivation recovery story for real.

### Design Document Observations

- **README "Current Status" — FIXED** (`2161742`): describes the runnable
  ingestion MVP, the CLI/server/UI layout, and the test infrastructure.
- **Plan said "APFS-clone" — FIXED** (`2161742`): the plan now documents the
  hard-link decision with the rationale the review asked for (a first
  acquisition has already copied the bytes; a clone would just create a second
  logical file for dedup to collapse), and is explicit that acquired object
  and CAS entry share an inode.
- **DESIGN_OVERVIEW open questions — FIXED** (`2161742`): resolved questions
  moved into a "Current MVP Decisions" section; the remaining open list only
  contains genuinely open items.
- **E2E_GUIDE vs. single scenario — RECONCILED AS DOCS** (`2161742`): the
  guide now describes the actual monolithic `001-dashboard` scenario and
  records the intent to split future features into smaller scenarios
  (historical inventory first). The scenario itself was not split — a
  deliberate, now-documented choice; the baseline-churn coupling noted in the
  original review still exists until a split happens.
- **PHOTO_TIME_VIEWS command APIs — DOCUMENTED** (`2161742`): the doc now
  lists `POST/DELETE .../capture-time`, `CaptureTimeCorrected`, and
  `CaptureTimeCleared` as not implemented.

## New Observations (introduced by the fixes)

1. **The Host allowlist breaks non-canonical hostnames.** `allowedHost` is the
   literal `--addr` value, so with the default `127.0.0.1:8080` a user who
   types `http://localhost:8080` gets 403 on every request. Worse, with
   `--allow-public-bind --addr 0.0.0.0:8080`, no real request ever carries
   `Host: 0.0.0.0:8080`, so the public-bind escape hatch is now entirely
   non-functional. Suggest: when the bind IP is loopback, also accept
   `localhost` (and the other loopback literals); when it is unspecified
   (`0.0.0.0`/`::`), match on port only or require an explicit
   `--allowed-host`. The dev proxy is unaffected (`changeOrigin: true`).
2. **SSE drops events for slow consumers silently.** `broadcast` uses a
   non-blocking send into a 128-slot channel; a stalled client loses events
   with no `job_snapshot` resync until it reconnects. Fine for progress
   chatter, but worth a comment or a dropped-event counter so a future
   consumer doesn't assume the stream is lossless.

## Remaining Items (carried forward, all minor)

- Precision vocabulary and `subsec_time_original`/offset normalization —
  blocked on partial-precision date support; current `"second"` is truthful.
- `contentMu` is global rather than per-content-ref (cheap critical section
  now; the JSONL append serializes regardless).
- One monolithic E2E scenario; split documented as future work in E2E_GUIDE.
- Deterministic IDs are double-env-gated rather than injected.
- Capture-time correction commands unimplemented (documented as such).

## Priority Recommendations

1. Fix the Host allowlist for `localhost` and `--allow-public-bind` — the
   latter is currently a broken flag.
2. When partial-precision capture dates are implemented, adopt the design's
   precision vocabulary at the same time (the projection column already
   exists).
3. Split the historical-inventory E2E flow out of `001-dashboard` before the
   next significant UI change, per the updated E2E_GUIDE.
