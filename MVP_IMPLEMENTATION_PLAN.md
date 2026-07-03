# MVP Implementation Plan

This document narrows [INGESTION_DESIGN.md](./INGESTION_DESIGN.md) into a concrete first implementation.

The MVP ingests only files with `.jpg` or `.jpeg` extensions, case-insensitively. It preserves the v0 design's core ingestion model: immutable stored bytes, append-only events, and rebuildable projections.

The most important constraint is that events should record durable inputs and operations, not reducer conclusions. Inventory matches, duplicate groups, asset catalogs, and "already known" decisions are projections.

## Scope

The MVP includes:

- Local directory source roots.
- Recursive directory traversal.
- Extension-only candidate selection for `.jpg` and `.jpeg`.
- Acquisition of candidate JPEG files into Photostore, with incoming SHA-256 computed during copy.
- Immutable acquisition of selected `../aidisks` inventory files.
- SHA-256 hashing while copying source bytes into Photostore.
- Content-addressed materialization for newly seen content.
- JSONL append-only event log.
- SQLite projections for operational queries and reducer output.
- A scan report generated from projections.

The MVP excludes:

- Tar or archive traversal.
- Google Drive handling.
- Kopia restore, mount, or API calls.
- HEIC, PNG, TIFF, RAW, video, XMP, or any non-JPEG media.
- Magic-byte or MIME sniffing.
- Thumbnail generation.
- EXIF extraction.
- Near-duplicate detection.
- RAW/JPEG pairing.
- Deletion, movement, or reorganization of source files.
- Emitting events for inventory matches, duplicate groups, or asset creation.

## Programming Language

Use Go 1.22 or newer.

Go is the better MVP choice because ingestion is naturally a bounded pipeline:

- A walker goroutine enumerates candidate paths.
- Acquisition workers copy external bytes into temporary Photostore-owned files while hashing the incoming stream.
- A content-address materializer APFS-clones newly seen content into CAS.
- A single event writer serializes events into append-only JSONL order.
- SQLite projection writes are serialized or batched behind the event writer.

Required external packages:

- `github.com/google/uuid`
- `github.com/mattn/go-sqlite3` or `modernc.org/sqlite`

Default worker count:

```text
workers = min(runtime.NumCPU(), 8)
```

The CLI should allow overriding it:

```text
photostore scan --store ./photostore-data --workers 8
```

## Command Shape

Initial commands:

```text
photostore init --store ./photostore-data
photostore inventory acquire --store ./photostore-data --path ../aidisks/Media.toc --label Media --group media_restore
photostore inventory scan --store ./photostore-data --inventory inv_... --type toc --ext .jpg --ext .jpeg --resolver-root /Volumes/RestoredMedia/Media
photostore source add --store ./photostore-data --path /Volumes/OldBackup --label OldBackup
photostore scan --store ./photostore-data
photostore report --store ./photostore-data --scan-id SCAN_ID
```

## Storage Layout

The MVP store is a local directory:

```text
photostore-data/
  events/
    events.jsonl
  objects/
    acquired/
      obj_...              # immutable acquisition objects, UUID-addressed
  cas/
    sha256/
      v1/
        ab/
          cd/
            abcdef...fullhash # content-addressed materializations
  tmp/
  projections.sqlite3
  reports/
    scan-SCAN_ID.json
    scan-SCAN_ID.txt
```

Two storage layers are intentional:

- `objects/acquired/*` is the durable input layer for retained acquired objects. These objects are addressed by stable ids, not by computed content.
- `cas/sha256/v1/*` is a materialized content-addressed view over copied byte streams. The `v1` namespace is part of the CAS layout, not event data.

The acquisition layer is the recovery mechanism for newly seen content and acquired inventory files. If a hash implementation, parser, or reducer is wrong, reducers and verifiers can operate again over retained acquired bytes without depending on the original external file still existing.

## Event Semantics

Events record:

- User or process configuration.
- External bytes copied into Photostore-owned immutable storage.
- Acquisition operations that copied source bytes and measured the incoming byte stream.
- CAS materialization operations.
- Scan lifecycle.
- Failures.

Events do not record:

- Inventory matches.
- Duplicate classification.
- Asset creation from unique content.
- "Already known" conclusions.
- Historical expected-hash-not-seen conclusions.

Those are projection outputs.

Computed values can appear in acquisition events when they are measurements made while copying bytes into Photostore. The incoming SHA-256 and byte count are recorded because they determine where the copied bytes should be materialized and whether a same-content CAS object appears to already exist. This is still not a duplicate, match, or asset conclusion.

Follow-up verification and deduplication are separate. A verifier can later re-read Photostore-owned storage, recompute hashes, and compare bytes before deleting or collapsing any duplicate acquired object. That second pass is how the system becomes confident that a candidate duplicate and its canonical CAS target are byte-for-byte identical, and it is also how the system recovers if the ingestion hasher was wrong.

## Identifiers

Use these identifiers:

- `event_id`: `evt_` plus UUID4 hex.
- `source_root_id`: `src_` plus UUID4 hex.
- `historical_inventory_id`: `inv_` plus UUID4 hex.
- `scan_id`: `scan_` plus UUID4 hex.
- `stored_object_id`: `obj_` plus UUID4 hex.
- `source_occurrence_id`: `occ_` plus UUID4 hex.
- `content_ref`: `sha256:<hash>:<size>`.
- `inventory_entry_id`: `ient_` plus SHA-256 of `historical_inventory_id + "\0" + hash + "\0" + historical_path_or_empty`.

Do not derive `stored_object_id` from content. It must remain valid even if a hash algorithm or verifier version is later rejected.

## Event Log Format

Each event is one JSON object per line.

Common envelope:

```json
{
  "event_id": "evt_7f0e6aeb9a54425dbd2a09f29ebd8e46",
  "event_type": "EventType",
  "schema_version": 1,
  "recorded_at_ms": 1782931320123,
  "actor": {
    "type": "process",
    "id": "photostore-cli",
    "hostname": "host.local",
    "pid": 12345
  },
  "causation_id": null,
  "correlation_id": "scan_...",
  "payload": {}
}
```

Event append rules:

- All events are serialized through one event writer goroutine.
- Write a complete JSON line in one append operation.
- Flush and fsync after each event for MVP correctness.
- Apply the event to SQLite projections after append.
- If projection update fails, keep the event and allow projection rebuild.

## Parallel Execution Model

The MVP should use bounded parallelism without allowing concurrent writes to corrupt the event log or projections.

Pipeline:

```text
source roots
    |
    v
walker goroutine
    |
    v
bounded candidate channel
    |
    v
acquisition worker pool
    |
    v
CAS materializer
    |
    v
event writer goroutine -> events.jsonl -> SQLite projections
```

Rules:

- Directory walking may run one goroutine per source root.
- Acquisition should use a bounded worker pool.
- Event append and projection updates must be single-writer.
- CAS materialization assumes APFS for the MVP and should use file cloning where possible.
- Report generation runs after `IngestionScanCompleted`.
- Cancellation should flow through `context.Context`.
- The scan should continue after recoverable per-file failures.

## Exact MVP Event Types

These are the only event types required for the MVP.

### StoreInitialized

Emitted once when creating a new store.

Payload:

```json
{
  "store_path": "/absolute/path/photostore-data",
  "event_log_path": "/absolute/path/photostore-data/events/events.jsonl",
  "acquired_object_root_path": "/absolute/path/photostore-data/objects/acquired",
  "cas_root_path": "/absolute/path/photostore-data/cas",
  "projection_path": "/absolute/path/photostore-data/projections.sqlite3",
  "implementation": {
    "name": "photostore",
    "language": "go",
    "language_version": "1.22",
    "mvp_plan": "MVP_IMPLEMENTATION_PLAN.md"
  }
}
```

### SourceRootRegistered

Emitted when a local directory root is added.

Payload:

```json
{
  "source_root_id": "src_...",
  "label": "OldBackup",
  "root_path": "/Volumes/OldBackup",
  "source_type": "local_directory",
  "scan_policy": {
    "recursive": true,
    "follow_symlinks": false,
    "candidate_extensions": [".jpg", ".jpeg"]
  }
}
```

### HistoricalInventoryFileAcquired

Emitted when a historical inventory file is copied into immutable acquisition storage.

Payload:

```json
{
  "historical_inventory_id": "inv_...",
  "stored_object_id": "obj_...",
  "purpose": "historical_inventory",
  "original_path": "/absolute/path/../aidisks/Media.toc",
  "acquired_storage_key": "objects/acquired/obj_...",
  "source_file": {
    "mtime_ns": 1719870000000000000
  },
  "copy_result": {
    "bytes_copied": 1234567,
    "hash_algorithm": "sha256",
    "hash": "111122223333444455556666777788889999aaaabbbbccccddddeeeeffff0000",
    "content_ref": "sha256:111122223333444455556666777788889999aaaabbbbccccddddeeeeffff0000:1234567"
  }
}
```

The original `.toc` path is not used for parsing after this point. Parsing reads from `stored_object_id`.

### HistoricalInventoryFileAcquireFailed

Emitted when an inventory file cannot be copied.

Payload:

```json
{
  "purpose": "historical_inventory",
  "original_path": "/absolute/path/../aidisks/Media.toc",
  "error": {
    "type": "file_not_found",
    "message": "No such file or directory",
    "retryable": true
  }
}
```

### StoredObjectHashVerified

Optional follow-up event emitted when a verifier computes a hash over Photostore-owned storage after acquisition.

Payload:

```json
{
  "stored_object_id": "obj_...",
  "purpose": "source_media",
  "acquired_storage_key": "objects/acquired/obj_...",
  "verifier": {
    "algorithm": "sha256",
    "implementation": "go-crypto-sha256",
    "version": 1
  },
  "result": {
    "hash": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
    "size": 3456789,
    "content_ref": "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789:3456789"
  }
}
```

This is not part of the inline MVP acquisition path. It is a follow-up integrity fact over durable bytes and is not a duplicate, match, or asset conclusion.

### StoredObjectHashVerificationFailed

Emitted when a stored object cannot be read or verified.

Payload:

```json
{
  "stored_object_id": "obj_...",
  "purpose": "source_media",
  "acquired_storage_key": "objects/acquired/obj_...",
  "verifier": {
    "algorithm": "sha256",
    "implementation": "go-crypto-sha256",
    "version": 1
  },
  "error": {
    "type": "read_failed",
    "message": "could not read acquired object",
    "retryable": true
  }
}
```

### ContentAddressMaterialized

Emitted when newly seen content is materialized at its content-addressed path. The target path is implied by `content_ref` and the store's configured CAS layout.

Payload:

```json
{
  "stored_object_id": "obj_...",
  "content_ref": "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789:3456789",
  "materialization": {
    "method": "hard_link",
    "created": true
  }
}
```

The CAS storage key is derived from `content_ref` and the configured CAS layout, so it is not recorded in the event. For the MVP, emit this event only when the CAS object did not already exist and was created by hard-linking the acquired object into the CAS path. If the CAS path already exists, acquisition records a reference to the existing `content_ref`; a later verification pass can confirm that existing CAS object is present and intact.

### HistoricalInventoryScanRequested

Emitted when the user requests a scan over an acquired historical inventory.

The purpose of this event is operational intent: parse `stored_object_id` as a historical inventory, select entries matching the requested extensions, resolve their historical paths to current readable files, and acquire those files into Photostore. A later inventory scan can use the same acquired `.toc` object with different filters, such as `.png`.

This event does not record parsed entries, path matches, inventory/content matches, or assets. Those are reducer output and scan work products.

Payload:

```json
{
  "scan_id": "scan_...",
  "historical_inventory_id": "inv_...",
  "label": "Media",
  "group": "media_restore",
  "inventory_type": "toc",
  "original_path": "/absolute/path/../aidisks/Media.toc",
  "stored_object_id": "obj_...",
  "parser": {
    "name": "hash_keyed_text",
    "version": 1
  },
  "filter": {
    "candidate_extensions": [".jpg", ".jpeg"],
    "hash_only_entries_allowed": false
  },
  "path_resolver": {
    "type": "root_relative",
    "resolver_root": "/Volumes/RestoredMedia/Media",
    "strip_prefixes": ["./", "Media/"],
    "case_sensitive": true
  },
  "requested_by": "cli"
}
```

The parsed entries are projection output from the stored object plus parser version and filter. Do not emit one event per parsed entry in the MVP.

### SourceRootScanRequested

Emitted at the start of a requested scan before traversal begins.

Payload:

```json
{
  "scan_id": "scan_...",
  "source_root_ids": ["src_..."],
  "candidate_extensions": [".jpg", ".jpeg"],
  "requested_by": "cli"
}
```

For all scan-produced events, `correlation_id` is the `scan_id`.

### IngestionScanStarted

Emitted when traversal begins.

Payload:

```json
{
  "scan_id": "scan_...",
  "started_at_ms": 1782931320123,
  "source_roots": [
    {
      "source_root_id": "src_...",
      "root_path": "/Volumes/OldBackup",
      "label": "OldBackup"
    }
  ],
  "policy": {
    "recursive": true,
    "follow_symlinks": false,
    "candidate_extensions": [".jpg", ".jpeg"]
  }
}
```

### SourceEntryObserved

Emitted for each regular file whose extension is `.jpg` or `.jpeg`, whether found by directory traversal or resolved from a historical inventory entry.

Payload:

```json
{
  "scan_id": "scan_...",
  "source_root_id": "src_...",
  "source_kind": "source_root",
  "path": "/Volumes/OldBackup/DCIM/100CANON/IMG_1234.JPG",
  "relative_path": "DCIM/100CANON/IMG_1234.JPG",
  "historical_inventory_id": null,
  "inventory_entry_id": null,
  "entry_type": "regular_file",
  "filesystem": {
    "size": 3456789,
    "mtime_ns": 1719870000000000000,
    "ctime_ns": 1719870000000000000,
    "inode": 123456,
    "device": 16777231
  },
  "candidate_reason": {
    "method": "extension",
    "extension": ".jpg"
  }
}
```

For a file resolved from a historical inventory, use:

```json
{
  "scan_id": "scan_...",
  "source_root_id": null,
  "source_kind": "historical_inventory_resolved_path",
  "path": "/Volumes/RestoredMedia/Media/Pictures/IMG_1234.JPG",
  "relative_path": "Pictures/IMG_1234.JPG",
  "historical_inventory_id": "inv_...",
  "inventory_entry_id": "ient_...",
  "entry_type": "regular_file",
  "filesystem": {
    "size": 3456789,
    "mtime_ns": 1719870000000000000,
    "ctime_ns": 1719870000000000000,
    "inode": 123456,
    "device": 16777231
  },
  "candidate_reason": {
    "method": "historical_inventory_extension",
    "extension": ".jpg"
  }
}
```

Do not emit this for every non-JPEG file. Count skipped non-candidates in projections and reports.

### SourceFileAcquired

Emitted when a candidate external JPEG file is copied into Photostore, hashed during the copy, and assigned to a content reference.

Payload:

```json
{
  "scan_id": "scan_...",
  "source_occurrence_id": "occ_...",
  "stored_object_id": "obj_...",
  "purpose": "source_media",
  "content_ref": "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789:3456789",
  "source_root_id": "src_...",
  "source_kind": "source_root",
  "path": "/Volumes/OldBackup/DCIM/100CANON/IMG_1234.JPG",
  "relative_path": "DCIM/100CANON/IMG_1234.JPG",
  "historical_inventory_id": null,
  "inventory_entry_id": null,
  "acquired_storage_key": "objects/acquired/obj_...",
  "source_file_before_copy": {
    "size": 3456789,
    "mtime_ns": 1719870000000000000,
    "ctime_ns": 1719870000000000000,
    "inode": 123456,
    "device": 16777231
  },
  "copy_result": {
    "bytes_copied": 3456789,
    "hash_algorithm": "sha256",
    "hash": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
  },
  "storage_disposition": {
    "cas_existed_at_ingest": false,
    "acquired_object_retained": true,
    "temporary_copy_discarded": false
  }
}
```

`causation_id` should reference `SourceEntryObserved`.

When `cas_existed_at_ingest` is `false`, the temporary copy is retained as `stored_object_id` and APFS-cloned into CAS. When `cas_existed_at_ingest` is `true`, the temporary copy is still retained in the MVP; the later deduplication job may delete it only after recomputing both hashes and performing a byte-for-byte comparison against the canonical CAS target.

### HistoricalInventoryOccurrenceLinked

Emitted when a historical inventory entry is trusted enough to link a source occurrence to an already seen `content_ref` without resolving and copying the file again.

Payload:

```json
{
  "scan_id": "scan_...",
  "source_occurrence_id": "occ_...",
  "historical_inventory_id": "inv_...",
  "inventory_entry_id": "ient_...",
  "content_ref": "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789:3456789",
  "link_basis": {
    "type": "trusted_historical_inventory_hash",
    "inventory_type": "toc",
    "parser": {
      "name": "hash_keyed_text",
      "version": 1
    }
  }
}
```

This event records that a historical inventory key/value pair was used to avoid reacquiring bytes. It should not repeat the historical path, raw line, or other large inventory-derived data; those live in projections keyed by `inventory_entry_id`.

### SourceFileAcquireFailed

Emitted if a candidate external file cannot be copied.

Payload:

```json
{
  "scan_id": "scan_...",
  "source_root_id": "src_...",
  "path": "/Volumes/OldBackup/DCIM/100CANON/BAD.JPG",
  "relative_path": "DCIM/100CANON/BAD.JPG",
  "error": {
    "type": "permission_denied",
    "message": "Permission denied",
    "retryable": true
  }
}
```

### IngestionScanCompleted

Emitted after all selected roots have been traversed and queued acquisition work has finished.

Payload:

```json
{
  "scan_id": "scan_...",
  "completed_at_ms": 1782933120123,
  "status": "completed",
  "stats": {
    "source_roots_scanned": 1,
    "directories_seen": 123,
    "regular_files_seen": 4567,
    "candidate_files_seen": 321,
    "source_files_acquired": 320,
    "source_file_acquire_failures": 1,
    "content_addresses_materialized": 200,
    "non_candidate_files_skipped": 4246
  },
  "report_paths": {
    "json": "/absolute/path/photostore-data/reports/scan-scan_....json",
    "text": "/absolute/path/photostore-data/reports/scan-scan_....txt"
  }
}
```

The `content_addresses_materialized` value is an operation count, not a duplicate conclusion.

### IngestionScanFailed

Emitted when a fatal scan failure prevents completion.

Payload:

```json
{
  "scan_id": "scan_...",
  "failed_at_ms": 1782933120123,
  "status": "failed",
  "error": {
    "type": "acquisition_store_unavailable",
    "message": "Acquisition object root is not writable",
    "retryable": true
  },
  "partial_stats": {
    "source_files_acquired": 17
  }
}
```

## Reducers And Projections

Use SQLite for MVP projections. Tables are rebuildable from `events.jsonl` plus immutable stored objects.

Minimum tables:

```text
events_applied(event_id primary key, event_type, recorded_at_ms)
source_roots(source_root_id primary key, root_path unique, label, source_type, policy_json)
stored_objects(stored_object_id primary key, purpose, acquired_storage_key, original_path, first_event_id)
source_content_links(source_occurrence_id primary key, stored_object_id, content_ref, cas_existed_at_ingest, acquired_object_retained, link_event_id)
verified_hashes(stored_object_id, algorithm, verifier_version, hash, size, content_ref, verified_event_id)
content_addresses(content_ref primary key, algorithm, hash, size, derived_cas_storage_key, first_materialized_event_id)
source_occurrences(source_occurrence_id primary key, stored_object_id, source_kind, source_root_id, path, relative_path, scan_id, historical_inventory_id, inventory_entry_id, source_event_id)
historical_inventories(historical_inventory_id primary key, stored_object_id, original_path, label, group_name, acquired_event_id)
historical_inventory_scans(scan_id primary key, historical_inventory_id, inventory_type, parser_json, filter_json, path_resolver_json, requested_event_id)
historical_inventory_entries(inventory_entry_id primary key, scan_id, historical_inventory_id, sha256, historical_path, resolved_path, extension, raw_line)
historical_matches(content_ref, stored_object_id, historical_inventory_id, inventory_entry_id)
historical_seen_links(source_occurrence_id primary key, historical_inventory_id, inventory_entry_id, content_ref, link_event_id)
asset_projection(asset_projection_id primary key, content_ref unique, representative_stored_object_id, asset_kind)
scans(scan_id primary key, status, started_at_ms, completed_at_ms, stats_json)
```

`derived_cas_storage_key` is projection state computed from `content_ref` and the configured CAS layout. It is intentionally not stored in `ContentAddressMaterialized`.

Reducer responsibilities:

- Parse historical inventory entries from acquired inventory objects when an inventory scan is requested.
- Use parsed historical hashes as trusted advisory keys for skip decisions during historical inventory scans.
- Resolve selected historical paths to current filesystem paths according to the scan's resolver configuration.
- Compute historical inventory matches by joining acquired source media content refs to parsed inventory entry hashes, preferring follow-up verification results if available.
- Compute duplicate groups by grouping acquired source media occurrences by `content_ref`, preferring follow-up verification results if available.
- Compute the MVP asset projection as one projected asset per unique JPEG `content_ref`.
- Compute scan reports.

Do not emit events for these reducer conclusions.

## Acquisition And Materialization Algorithms

For each candidate JPEG:

1. Observe a `.jpg` or `.jpeg` source entry and emit `SourceEntryObserved`.
2. Copy the external file into a temporary file under `tmp/`, computing SHA-256 and byte count while copying.
3. If the copy fails, delete the temporary file and emit `SourceFileAcquireFailed`.
4. Compute `content_ref` from the incoming hash and byte count.
5. If the CAS path for `content_ref` does not exist:
   - move or retain the temporary file as `objects/acquired/obj_...`
   - APFS-clone that acquired object to the derived CAS path
   - emit `SourceFileAcquired` with `cas_existed_at_ingest: false`
   - emit `ContentAddressMaterialized`
6. If the CAS path for `content_ref` already exists:
   - move or retain the temporary file as `objects/acquired/obj_...`
   - emit `SourceFileAcquired` with `cas_existed_at_ingest: true`, `acquired_object_retained: true`, and the `content_ref`
   - do not emit `ContentAddressMaterialized`
7. Let follow-up verification and deduplication confirm equality before deleting any retained acquired object.

For each historical inventory file:

1. Copy the inventory file into `objects/acquired/obj_...`, computing SHA-256 and byte count while copying.
2. Emit `HistoricalInventoryFileAcquired`, or `HistoricalInventoryFileAcquireFailed`.
3. If the CAS path for the inventory content does not exist, APFS-clone the acquired object into CAS and emit `ContentAddressMaterialized`.
4. If the CAS path already exists, keep the acquired inventory object anyway because it is small relative to media and is the durable input to future inventory scans.

For each historical inventory scan request:

1. Emit `HistoricalInventoryScanRequested`, referencing the acquired inventory object, parser, filter, and path resolver.
2. Parse matching entries from the acquired inventory object.
3. For each selected entry, compute the expected `content_ref` from the historical SHA-256 and size if size is available. If size is unavailable, use the historical SHA-256 to query the projection for any already seen content with that hash.
4. If the projection already contains matching content from a previous acquisition, emit `HistoricalInventoryOccurrenceLinked` and do not resolve or copy the file.
5. If the projection does not contain matching content, resolve the historical path to a current filesystem path.
6. For each resolved readable file, use the same acquisition and materialization flow as a normal source JPEG:
   - emit `SourceEntryObserved` with source type `historical_inventory_resolved_path`
   - emit `SourceFileAcquired` or `SourceFileAcquireFailed`
   - emit `ContentAddressMaterialized` only for newly materialized content
7. Record unresolved historical paths in projections and reports, not as per-entry events.

The MVP should not delete retained acquired objects during acquisition. Duplicate media occurrences remain available for later verification and deduplication.

## Verification And Deduplication

Verification and deduplication are follow-up processes, separate from acquisition.

Before deleting an acquired object as a duplicate of a canonical CAS object, the deduplication process should:

1. Read the acquired object from `objects/acquired/obj_...`.
2. Read the canonical target from the current CAS namespace derived from `content_ref`.
3. Recompute the configured hash for both byte streams.
4. Confirm both hashes equal the expected `content_ref`.
5. Perform a byte-for-byte comparison of the acquired object and canonical target.
6. Only after the byte comparison succeeds, replace or mark the acquired object according to the storage policy.

The MVP can defer the deletion/replacement policy. The important requirement is that deletion is never based only on the incoming hash computed during acquisition.

## Hash Algorithm Recovery

If the SHA-256 implementation, verifier version, or content-addressing logic is later found to be wrong:

1. Mark the bad verifier version as untrusted in reducer configuration or a later administrative event.
2. Re-run verification over retained acquired objects and existing CAS objects.
3. Emit new `StoredObjectHashVerified` events with a new verifier version or algorithm.
4. Materialize new CAS paths from retained acquired objects where available, using a new non-overlapping CAS namespace such as `cas/sha256/v2/...`.
5. Rebuild projections from the event log, selecting the trusted verifier version.

If a previous CAS path was wrong, it is just an obsolete materialization. For example, bad SHA-256 outputs might have produced objects under `cas/sha256/v1/ab/cd/...`; corrected output can be written under `cas/sha256/v2/ab/cd/...`. Once the new tree is complete and projections no longer reference the bad namespace, the old tree is garbage and can be deleted.

This is why CAS storage keys are not stored in events. They depend on the hash implementation, verifier version, and CAS layout version. Events keep stable operation facts such as `stored_object_id`, `content_ref`, verifier version, and materialization occurrence; projections derive the active storage key for the selected trusted namespace.

If a deduplication projection was wrong, rebuild it. No source bytes were discarded as part of MVP deduplication.

If a historical inventory parser or path resolver was wrong, keep the inventory bytes and rerun a new `HistoricalInventoryScanRequested` with a new parser version or resolver configuration.

## Historical Inventory MVP

Initial manifest should focus on JPEG-relevant hash-plus-path files:

```text
../aidisks/Media.toc
../aidisks/recoverPicturesDec2016.toc
../aidisks/time_machine_backups.toc
../aidisks/missingMedia
../aidisks/missingbup
../aidisks/unique_timemachine_backup_files
```

Inventory ingestion behavior:

- Copy each listed file into immutable acquisition storage.
- Compute SHA-256 and byte count while copying each inventory file.
- Materialize each inventory file into CAS when its content is newly seen.
- Scan the inventory with `HistoricalInventoryScanRequested`, specifying parser, extension filters, and a path resolver.
- Parse the inventory from the acquired object, not from the original `../aidisks` path.
- Before resolving or copying a selected `.jpg` or `.jpeg` entry, check the projection of already seen content by historical hash and, when available, size.
- If the projection already contains that content, emit `HistoricalInventoryOccurrenceLinked` and skip file resolution and byte acquisition.
- Resolve selected `.jpg` and `.jpeg` entries to readable filesystem paths only when their historical hash has not already been seen.
- Use the original path only as provenance and for human reports.

Parser reducer behavior:

- Keep lines whose first field is a 64-character hex SHA-256.
- For `toc` and `missing_list`, keep only entries whose remaining text ends in `.jpg` or `.jpeg`, case-insensitively.
- Normalize SHA-256 to lowercase.
- Extract size from git-annex-style names such as `SHA256E-s12345--HASH.jpg` when present.
- Preserve the raw line in the projection.
- Resolve historical paths according to the scan request's path resolver.
- Do not create events, blobs, or assets from inventory entries alone.

Report behavior:

- Count historical JPEG entries loaded.
- Count historical entries skipped because their trusted hash was already seen.
- Count resolved and unresolved historical JPEG paths.
- List historical JPEG hashes matched by current scans.
- List expected historical JPEG hashes not yet seen in current scans.
- Show historical paths for matches where available.

## Acceptance Criteria

The MVP is complete when:

- A user can initialize a store.
- A user can register one or more local directory source roots.
- A user can acquire selected `aidisks` inventory files into immutable storage.
- Acquired inventory files are hashed while being copied and materialized into CAS when newly seen.
- A user can request an inventory scan with parser, extension filter, and path resolver configuration.
- Inventory scans first deduplicate matching `.jpg` and `.jpeg` entries against the projected already-seen hash set.
- Inventory scans avoid resolving and copying historical entries whose trusted hash is already seen.
- Inventory scans resolve and acquire referenced image files only for selected entries whose trusted hash is not already seen.
- A scan recursively finds `.jpg` and `.jpeg` files only.
- Every readable candidate JPEG is copied into Photostore while computing SHA-256 and byte count.
- Newly seen content is materialized into CAS using APFS clone.
- Duplicate source occurrences retain copied bytes until a later deduplication process recomputes hashes and performs a byte-for-byte comparison.
- Duplicate groups are projection output, not events.
- Historical inventory matches are projection output, not events.
- Asset records for unique JPEG content are projection output, not events.
- Failed acquisition is reported without stopping the scan.
- The event log contains only the event types defined in this plan.
- The SQLite projections can be deleted and rebuilt from `events.jsonl` and stored objects.
- A new hash verifier version can be run over retained acquired objects and CAS objects without needing the original external files.
- Corrected CAS materialization can use a new namespace such as `cas/sha256/v2/...` without changing historical events.
