# Claude Review

Review of the Photostore design documents and implementation as of branch
`codex/ui-thumbnails` (2026-07-03). Scope: all `*.md` design docs, the Go
backend (`internal/photostore`, `cmd/photostore`), the SvelteKit frontend
(`web/src`), and the E2E infrastructure. `go test ./...` passes.

## Verdict

This is an unusually disciplined MVP. The document chain
(VISION → DESIGN_OVERVIEW → INGESTION_DESIGN → MVP_IMPLEMENTATION_PLAN →
MVP_UI_DESIGN / E2E_GUIDE / EVENT_LOG_POSITION_DESIGN / PHOTO_TIME_VIEWS_DESIGN)
is coherent, and the implementation genuinely honors its core commitments:
append-only JSONL events, byte-offset projection cursors, events that record
operations rather than reducer conclusions, verify-before-delete deduplication,
and pixel-exact E2E tests.

There is, however, **one direct contradiction between the code and the design
documents' most emphatic rule** (capture-time fallback), one designed safety
mechanism that exists only as dead code (metadata mismatch detection), and a
handful of correctness and security issues worth fixing before the store holds
data you care about.

## What Is Strong

- **Event-sourcing discipline.** Events record configuration, copies,
  materializations, and lifecycle; duplicate groups, inventory matches, and
  assets are projections, exactly as MVP_IMPLEMENTATION_PLAN demands.
  `events_applied` is an idempotence guard, `projection_state.next_offset` is
  the replay cursor, and cursor advancement shares the SQLite transaction with
  reducer writes — a faithful implementation of EVENT_LOG_POSITION_DESIGN,
  including the crash-outcome matrix.
- **Crash-tolerant byte handling.** `copyHash` streams into a `.tmp` file and
  renames; CAS materialization is a hard link from the retained acquired
  object; acquired files are chmod'd `0400`. `VerifyAndDeduplicate` rehashes
  both files and does a byte-for-byte comparison before touching anything,
  matching the plan's "deletion is never based only on the incoming hash."
- **Cross-process safety.** `flock` on `events/events.lock` around append +
  projection apply, WAL-mode SQLite with a busy timeout, and startup replay
  from the cursor mean the CLI and `serve` can share a store. This is better
  than the design strictly required.
- **Versioned derivation surfaces.** CAS namespace `cas/sha256/v1`, thumbnail
  namespace `thumbnails/jpeg/240/orient-v2`, extractor version 2, dedup
  strategy version 3 — the "fix logic, bump version, regenerate" recovery story
  is real, and dedup/capture-time projections are actually rebuilt from the
  log on open.
- **E2E rigor.** Zero-pixel screenshot tolerance, no `waitForTimeout`, a
  `check:e2e-rules` script that enforces the 2000 ms rule, generated scenario
  READMEs, deterministic IDs/clock via env hooks, and CI running the whole
  stack on macOS.
- **MVP_USAGE.md is kept current** (it documents hard-link materialization and
  the worker env vars), which is rare for a fast-moving repo.

## Critical Findings

### 1. Capture-time reducer silently falls back to CreateDate/ModifyDate

`captureTimeFromFields` (internal/photostore/capture_time.go:146) tries
`datetime_original`, then falls back to `create_date`, then `modify_date`.

PHOTO_TIME_VIEWS_DESIGN.md could not be more explicit that this must not
happen: "`CreateDate`, as a separate extracted field, **not as a fallback**",
"Silent fallback from one timestamp source to another" is excluded from scope,
and the reducer selection order ends at "No effective capture time." AGENTS.md
repeats the rule repo-wide ("Do not add … fallback behavior"). A photo whose
EXIF has only `ModifyDate` (e.g., an edited export) will be filed on its edit
date with nothing in the projection marking it as lower-quality evidence —
exactly the unexplainable-date situation the design was written to prevent.

Related smaller drift in the same reducer:

- `precision` is hardcoded to `"second"`; the design defines a precision
  vocabulary.
- `photo_capture_times.source_kind` stores the *occurrence* kind
  (`source_root`), not the design's provenance vocabulary
  (`user_correction | exif_datetime_original | none`), so the UI cannot show
  "why this date" as the design requires.
- `subsec_time_original` is extracted but unused; offset is carried only as a
  raw string (acceptable for MVP, but undocumented).

**Recommendation:** remove the two fallback branches (photos land in the
already-working Undated view), or, if you decide date-by-CreateDate is a
feature, promote it to an explicit, labeled reducer source in the design doc
and the projection's `source_kind` so it is never silent.

### 2. Metadata mismatch detection is designed, tabled, and dead

The scan-time workflow in PHOTO_TIME_VIEWS_DESIGN (steps 6–8: on duplicate
content, re-extract, compare against the projection, emit
`PhotoMetadataObservationMismatchDetected` on disagreement) is not implemented.
`recordMetadataForCandidate` returns `"skipped"` as soon as a success row
exists — it never re-extracts or compares. `appendMetadataMismatch`
(metadata.go:222) has no callers; the `metadata_issues` table and its
projection case in `applyEventAt` can never be populated. The event log gains
a projection handler for an event that nothing emits.

This was the design's mechanism for detecting nondeterministic extraction,
corrupt stored bytes, and wrong content refs. Either wire the comparison into
the duplicate-content path or delete the dead code and note the deferral in
the design doc; the current state misleads a reader into thinking the check
exists.

### 3. Local web server is exposed to cross-site requests

MVP_UI_DESIGN says "Validate all filesystem paths server-side" and the threat
model is local-first, but `serve` has no defense against the standard
localhost attack set:

- **CSRF on command endpoints.** `decodeJSON` ignores `Content-Type`, so any
  web page you visit can fire no-preflight `text/plain` POSTs at
  `http://127.0.0.1:8080/api/sources` (register *any* readable path, e.g.
  `~/.ssh`), `/api/scans` (copy those files into the store), and then the
  bytes are readable via `/api/objects/{id}/bytes`.
- **DNS rebinding.** No `Host` header check, so the loopback-only bind does
  not actually restrict which origins can read API responses.
- **WebSocket hijacking.** `/api/events/ws` performs no `Origin` check, so any
  site can subscribe to job/progress events.

**Recommendation:** reject requests whose `Host` is not the configured listen
address, require `Content-Type: application/json` on mutating routes, and
check `Origin` on the WebSocket upgrade. ~30 lines total, closes all three.

## Correctness and Robustness

### 4. CAS materialize race across processes aborts the scan

`materialize` (store.go:1133) does stat-then-`os.Link`. In-process it is
serialized by `contentMu`, but a concurrent CLI scan against the same store
can create the CAS path between the stat and the link; `os.Link` then returns
`EEXIST`, which propagates as a fatal error and cancels the whole scan. Treat
`EEXIST` as success (the content is there; optionally skip the event, matching
the "already existed" branch).

### 5. Dedup relink is not crash-atomic

`verifyAndDeduplicateCandidate` does `os.Remove(duplicatePath)` then
`os.Link(canonicalPath, duplicatePath)`. A crash between the two leaves the
acquired object path dangling with no event recorded; every subsequent dedup
run reports a verification error for it forever, and the "retained acquired
object" durability story is broken for that occurrence. Link to a temp name in
the same directory and `os.Rename` over the duplicate instead — rename is
atomic and the failure mode becomes a leftover temp file.

### 6. `verifyDuplicateFiles` assumes lockstep short reads

The comparison loop treats `nA != nB` from two independent `Read` calls as
"bytes differ." `Read` is allowed to return short counts at different
boundaries even for identical content (rare on local regular files, real on
network filesystems). Use `io.ReadFull`/`io.ReadAtLeast` per buffer, or
compare via two `bufio.Reader`s, so a spurious short read cannot mark
identical duplicates as different (which permanently blocks their dedup, since
the byte-compare gate fails).

### 7. `BytesReleased` is overstated

`duplicateCandidates` intentionally selects *all* retained links (strategy v3
relinks everything), but the summary adds `result.Size` to `BytesReleased` for
every candidate — including first acquisitions whose acquired object is
already the same inode as CAS, where relinking releases zero bytes. The
dashboard's `retained_duplicate_bytes` (projections.go:155) gets this right by
filtering `cas_existed_at_ingest = 1`; the dedup summary should count released
bytes the same way (or check `st_nlink`/inode identity before counting).

### 8. The scan pipeline serializes almost everything behind `contentMu`

In `acquireSourceFile`, the global `contentMu` is held across the CAS
existence check, the `SourceFileAcquired` append, materialization, **and EXIF
extraction**. The worker pool therefore only parallelizes `copyHash`; JPEG
decode/EXIF parse — nontrivial CPU per file — is serialized. The lock needs to
cover only check-exists → append → link for a given content ref. Consider a
per-content-ref lock (keyed mutex) or narrowing the critical section and doing
extraction outside it.

### 9. `PhotoMetadataExtracted` records a false `extraction_context`

`recordMetadataForCandidate` hardcodes `"phase": "ingestion_scan"` and
`"source_kind": "source_root"` even when invoked from
`metadata refresh-missing` or for inventory-resolved files. These are durable
event facts that are simply wrong for those paths. Thread the actual context
through (the design shows `extraction_context` precisely so provenance is
truthful).

## Smaller Issues

- **`IngestionScanFailed` is never emitted.** A fatal scan error returns
  through the job runner but appends nothing; the `scans` row stays `started`
  forever and is surfaced as "interrupted." That works with resume, but the
  event vocabulary in both design docs promises a failure event; either emit
  it or update the docs.
- **`IngestionScanResumeRequested` is an undocumented event type** with no
  projection handler. Add it to MVP_IMPLEMENTATION_PLAN's exact-event-types
  list (which claims "The event log contains only the event types defined in
  this plan" — no longer true; the dedup and metadata event families are also
  missing from that doc).
- **Init on an existing store re-emits `StoreInitialized`.** `photostore init`
  twice appends a second event. Harmless but noisy; check for prior init.
- **Event `actor` omits `hostname`** (plan's envelope includes it). Cheap to
  add, useful once multiple machines touch a store.
- **`serveBuildFile` reads `web/build` relative to CWD.** A binary run from an
  arbitrary directory silently falls back to the embedded UI, or — worse — a
  different `web/build` in the CWD would be served. Resolve relative to an
  explicit dev flag rather than probing the CWD.
- **Jobs map is unbounded and in-memory only**, and hand-rolled WebSocket
  server never reads frames, so client close/ping frames are unnoticed until a
  write fails; subscriber goroutines linger. MVP_UI_DESIGN recommended SSE,
  which would eliminate the custom protocol code (`writeWebSocketText`,
  upgrade handshake, etc.) entirely. Recommend switching to SSE or a
  maintained WS library.
- **`PHOTOSTORE_DETERMINISTIC_IDS` in production code paths.** If set in a
  real environment, two processes would mint colliding event/object IDs from
  independent counters. Consider gating it more defensively (e.g., refuse in
  `serve`) or moving ID generation behind an injected interface in tests.
- **`parseInventory` drops hash-only lines** (`len(fields) < 2`), so `.lookup`
  and `all` files are effectively unsupported even though the manifest design
  and `hash_only_entries_allowed` filter field anticipate them. Fine for MVP;
  document it.
- **Thumbnail scaling is nearest-neighbor** (`thumb.Set` with integer source
  mapping). For a photo product, thumbnails will look visibly aliased;
  `golang.org/x/image/draw` (CatmullRom/ApproxBiLinear) is a drop-in upgrade
  and keeps the renderer-version namespace story (bump `orient-v2` → `v3`).

## Design Document Observations

- **README.md "Current Status" is stale.** It says the repository is in the
  design phase and "contains design documents" only. There is a working
  ingestion MVP, web UI, and CI. Update it — it is the first thing a
  collaborator reads.
- **MVP_IMPLEMENTATION_PLAN still says "APFS-clone"** throughout, while the
  implementation (and MVP_USAGE) settled on hard links. The tradeoff is real:
  a clone gives an independent physical copy (protects against single-copy
  corruption); a hard link means the acquired object and CAS entry are one
  inode, so the "retained acquired object as recovery input" property is
  weaker than the plan implies for *first* acquisitions. Worth an explicit
  paragraph in the plan recording the decision and its rationale.
- **DESIGN_OVERVIEW's open questions are now implicitly answered** (JSONL log,
  SQLite projections, single-process ordering, local sharing, JPEG-only) but
  the section is unchanged. Converting resolved questions into recorded
  decisions would keep the doc trustworthy.
- **E2E_GUIDE plans three scenario directories** (dashboard, source-scan,
  historical-inventory) and a `fixtures.ts` helper; the implementation has one
  monolithic `001-dashboard` scenario (325 lines covering scan, dedup,
  metadata, reload-recovery) and no fixtures module. The single scenario works
  but couples unrelated flows to one screenshot sequence — any UI change
  invalidates a long chain of baselines. Splitting per the guide would reduce
  churn.
- **PHOTO_TIME_VIEWS_DESIGN's command APIs are unimplemented**
  (`POST/DELETE /api/objects/{id}/capture-time`, `CaptureTimeCorrected`/
  `CaptureTimeCleared`). The doc labels itself review-only, so this is fine —
  but since the read-side shipped, mark the correction workflow as "not yet
  implemented" to keep doc/state alignment.
- The event-payload examples in the docs and the actual payloads have drifted
  in small ways (extractor fields shape, `warnings` optionality, `label`/
  `group` added to `HistoricalInventoryFileAcquired`). Since events are
  forever, consider making one doc — probably MVP_IMPLEMENTATION_PLAN — the
  canonical payload reference and updating it with each new event family.

## Priority Recommendations

1. Remove the capture-time fallback (or make it an explicit labeled source) —
   it violates the repo's own top rule and misfiles photos. *(Small change,
   plus projection rebuild.)*
2. Add Host/Origin/Content-Type checks to `serve`. *(Small, closes the
   cross-site hole.)*
3. Wire up or delete the metadata mismatch path; make `extraction_context`
   truthful.
4. Fix the dedup relink atomicity, the `EEXIST` race in `materialize`, and the
   short-read assumption in `verifyDuplicateFiles` — all three protect the
   bytes the whole system exists to protect.
5. Update README status, MVP_IMPLEMENTATION_PLAN (hard links, full event-type
   list), and DESIGN_OVERVIEW open questions.
6. Then quality-of-life: narrow `contentMu`, SSE instead of hand-rolled
   WebSocket, better thumbnail filtering, split E2E scenarios.
