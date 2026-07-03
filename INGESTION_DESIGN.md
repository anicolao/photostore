# Ingestion Design

This document describes a v0 ingestion design for Photostore.

The goal of v0 is to discover candidate photo files across disorganized storage locations, identify unique file content, preserve originals in immutable blob storage, and record enough provenance to answer where each file came from.

This design intentionally covers only discovery and ingestion. It does not cover triage, editing, albums, sharing, search, thumbnail generation, or long-term user workflows except where ingestion must produce inputs for them.

## Problem

The initial user environment may contain photos in many overlapping places:

- External drives.
- Local directory trees.
- Backup directories from old computers.
- Camera card dumps.
- Tar, zip, or other archive files.
- Export folders from photo tools.
- Repeated copies of the same files under different names.
- Near-duplicate files that are visually similar but have different bytes.

These sources may be messy:

- Directory names may be meaningful, accidental, duplicated, or misleading.
- Files may have misleading names or unfamiliar extensions.
- File timestamps may reflect copy time instead of capture time.
- Archives may contain nested directory trees or additional archives.
- Some files may be corrupt, partial, encrypted, unsupported, or unreadable.
- The same bytes may appear many times.
- Different bytes may represent the same logical photo, such as RAW plus JPEG or edited exports.

The ingestion system should handle this without requiring the user to clean up the source storage first.

## V0 Goals

V0 should:

- Walk one or more configured import roots.
- Inspect ordinary files and supported archive files.
- Import optional historical inventory metadata from prior recovery work.
- Identify candidate media files.
- Hash every readable candidate file by content.
- Store each unique byte sequence once as an immutable blob.
- Record every observed source occurrence, including duplicates.
- Reconcile observed files against historical hash inventories when available.
- Preserve provenance from source root to observed path.
- Extract basic technical metadata when possible.
- Create initial assets for newly ingested source files.
- Avoid modifying, moving, renaming, or deleting source files.
- Allow interrupted scans to resume safely.
- Allow the same source to be scanned repeatedly without creating duplicate blobs or duplicate asset records.
- Produce an import report for review.

V0 should optimize for safety, auditability, and completeness over clever organization.

## Non-Goals

V0 does not need to:

- Detect visual near-duplicates.
- Decide which duplicate path is canonical.
- Merge RAW plus JPEG captures into a single asset.
- Build a finished date-based library view.
- Generate thumbnails or previews.
- Classify quality, privacy, or media kind beyond basic file identification.
- Upload to cloud storage.
- Delete or reorganize source files.
- Ingest media files with missing, wrong, or unsupported extensions.
- Reliably recover damaged archives.
- Restore files directly from Kopia or other backup systems.
- Treat historical inventory entries as proof that bytes have been preserved.

These can be added later using the same event log and blob catalog.

## Core Model

Ingestion distinguishes between source occurrences, blobs, and assets.

### Source Root

A source root is a user-configured place to scan.

Examples:

```text
/Volumes/OldBackup
/Users/alice/Pictures
/mnt/archive/photo-dumps
```

A source root records:

- Stable root id.
- User-facing label.
- Root URI or filesystem path.
- Source type, such as `local_directory`, `external_drive`, or `readonly_archive_root`.
- Scan policy.
- Optional include and exclude rules.

V0 should treat source roots as read-only inputs.

### Historical Inventory

A historical inventory is metadata from earlier disk, backup, or recovery work.

The `../aidisks` notes describe a prior content-hash workflow built around:

- `.shasums` files: raw SHA-256 plus path inventories.
- `.toc` files: sorted hash-plus-path catalogs.
- `.lookup` files: sorted unique hash-only catalogs.
- `all`: an aggregate known-content hash set.
- missing-list files such as `missingMedia`, `missingbup`, and `unique_timemachine_backup_files`.
- Kopia snapshot catalogs such as `kopia8t.data3t1.toc`.
- restore notes identifying best available snapshots and known missing payloads.

V0 should be able to register these files as advisory ingestion metadata.

A historical inventory record should include:

- Inventory id.
- Inventory file path.
- Inventory type, such as `toc`, `lookup`, `missing_list`, or `restore_note`.
- Human-facing source label.
- Optional source group, such as `original_disk`, `kopia_snapshot`, `media_restore`, or `aggregate_known_hashes`.
- Parser version.
- Parse timestamp.
- Parsed hash count.
- Parsed path count, if paths are present.
- Parse warnings.

Historical inventories do not create blobs or assets by themselves. They help the scanner prioritize roots, explain duplicates, reconcile expected content, and report missing or recovered hashes.

### Source Occurrence

A source occurrence is one observed file-like object at one location during discovery.

Examples:

- A JPEG file at `/Volumes/OldBackup/DCIM/100CANON/IMG_1234.JPG`.
- A file inside `photos-2008.tar` at `2008/IMG_1234.JPG`.
- The same bytes found again in another backup directory.

A source occurrence records:

- Source root id.
- Scan id.
- Observed path.
- Container path, if the file is inside an archive.
- Path within the container.
- File size.
- Filesystem metadata, if available.
- Archive metadata, if available.
- Detection result.
- Hash result, if hashing succeeded.
- Error result, if discovery or hashing failed.

Source occurrences are facts about where bytes were seen. They are not the identity of the photo.

### Inventory Entry

An inventory entry is a historical claim that a hash appeared in some previous source.

For `.toc` and `.shasums`-style inputs, an entry may include:

- SHA-256 hash.
- Historical path.
- Optional size, if encoded in a git-annex filename or adjacent metadata.
- Historical source label.
- Inventory id.

For `.lookup` and `all` inputs, an entry may include only:

- SHA-256 hash.
- Inventory id.

V0 should treat these entries as claims, not verified current storage. A matching hash from a current scan can be linked back to matching inventory entries as additional provenance.

### Blob

A blob is an immutable byte sequence stored by content hash.

V0 blob identity should use:

- SHA-256 hash.
- Byte size.

If two source occurrences have the same hash and size, they refer to the same blob.

### Asset

An asset is the first logical Photostore object created from ingested source material.

For v0, asset creation should be conservative:

- A newly observed media blob creates one asset with one original version.
- A duplicate source occurrence of an already known blob does not create another asset.
- RAW plus JPEG pairing, sidecar linking, edited derivative detection, and logical merging are deferred.

This means v0 may create separate assets for files that a later system can relate or merge. That is acceptable because the original facts remain available in the event log.

## Discovery Flow

Discovery is a recursive traversal of configured source roots.

```text
SourceRootRegistered
        |
        v
IngestionScanStarted
        |
        v
directories, files, and archives observed
        |
        v
candidate files detected
        |
        v
candidate files hashed
        |
        v
unique blobs stored or matched
        |
        v
assets and source occurrences recorded
        |
        v
IngestionScanCompleted
```

### Directory Traversal

The scanner should walk directory trees deterministically enough to make logs and reports understandable.

V0 should:

- Follow normal directories.
- Skip symlink loops.
- Record symlinks as observations if useful, but avoid traversing them by default.
- Record permission errors and continue.
- Record unreadable files and continue.
- Support include and exclude patterns.
- Support maximum file size and maximum archive expansion limits.
- Select candidate media files by extension.

The scanner should not modify access times if the platform allows avoiding it, but this is a best-effort optimization, not a correctness requirement.

### Archive Traversal

V0 should support at least tar archives because old backups may be stored as tar files.

The archive scanner should:

- Treat an archive as a container.
- Enumerate regular file entries.
- Preserve both the archive path and the path inside the archive.
- Stream file content for hashing and storage when possible.
- Avoid extracting entire archives into unmanaged temporary directories.
- Apply limits to expansion size, entry count, path depth, and nested archive recursion.
- Record archive errors and continue with the rest of the scan when possible.

Supported v0 archive formats:

- `tar`
- `tar.gz`
- `tgz`

Possible later formats:

- `zip`
- `7z`
- `rar`
- Disk images.

Nested archives should be optional in v0. If enabled, the scan must record the full container chain.

Example container chain:

```text
/Volumes/Backup/photos.tar.gz
  -> 2008/raw-camera-dump.tar
  -> DCIM/100CANON/IMG_1234.JPG
```

## Candidate Detection

The scanner should classify files into candidate and non-candidate groups before expensive ingestion work.

V0 should rely on file extensions being correct. This keeps discovery simple and predictable for the first implementation.

Detection should:

- Normalize extensions case-insensitively.
- Treat configured media extensions as candidates.
- Treat configured archive extensions as containers.
- Skip files with missing, unsupported, or misleading extensions.
- Record skipped extensions in the import report.

Candidate media types for v0:

- JPEG.
- PNG.
- HEIC or HEIF recognized by extension.
- TIFF.
- GIF.
- WebP.
- RAW formats recognized by extension.
- Common video formats recognized by extension, such as MP4 and MOV.
- XMP sidecars.

V0 may skip documents, scans, and screenshots as distinct categories. If they are image files, they can still be ingested as source media. Rich classification can happen later.

Detection results should be recorded even for skipped files so reports can explain what happened. Later versions can add magic-byte or MIME sniffing if extension-only discovery misses too much.

## Historical Inventory Metadata

The `aidisks` metadata is valuable because it already contains years of content-based disk and backup comparisons. V0 should use it as a read-only planning and reconciliation input.

### Useful Metadata

The most useful artifacts for ingestion are:

- `Media.toc`: target inventory for the old `Media` git-annex archive.
- `recoverPicturesDec2016.toc`: inventory for a photo recovery target.
- `time_machine_backups.toc`: inventory for Time Machine backup content.
- `missingMedia`, `missingbup`, and `unique_timemachine_backup_files`: prior comparison outputs that identify content believed to be absent from a known aggregate.
- `*.shasums.toc` and standalone `*.toc` files: hash-plus-path catalogs for disks, restored snapshots, and mounted backups.
- `*.shasums.lookup` and `all`: hash-only catalogs for membership checks.
- Kopia snapshot analysis notes: source ranking and known missing payloads.
- Media restore notes: recommended restore source and overlay payloads.

V0 should not need to understand every historical file in `aidisks`. It should start with parsers for hash-keyed text files and a small manually curated inventory manifest.

### Inventory Manifest

V0 should support an explicit manifest that names which historical files to load and how to interpret them.

Example:

```text
inventory ../aidisks/Media.toc type=toc label=Media target group=media_restore
inventory ../aidisks/kopia8t.data3t1.toc type=toc label=Kopia Data3T1 group=kopia_snapshot
inventory ../aidisks/all type=lookup label=Known historical hashes group=aggregate_known_hashes
inventory ../aidisks/missingMedia type=toc label=Media missing from historical aggregate group=missing_list
```

The manifest avoids guessing the meaning of every file by name. Later versions can add automatic discovery.

### Parsing Rules

V0 should parse historical inventory files conservatively:

- Accept lines whose first field is a 64-character lowercase or uppercase hex SHA-256.
- Treat the remaining text as an optional historical path or payload.
- Preserve the raw line for audit.
- Normalize the hash to lowercase.
- Do not require paths to exist.
- Do not assume every parsed hash is media.
- Ignore non-hash lines with a parse warning count.
- Support hash-plus-path files such as `.toc` and `.shasums.toc`.

Hash-only files such as `.lookup` and `all` are part of the broader historical inventory model, but they are not implemented in the current MVP parser. They should be added as an explicit parser/version update rather than silently accepted by the hash-plus-path parser.

For git-annex object paths such as:

```text
SHA256E-s90574382--34c398...d8d.mkv
```

V0 may extract:

- Expected SHA-256.
- Expected byte size.
- Expected extension.

This is useful for reports, but the actual ingested blob identity still comes from hashing current bytes.

### Reconciliation

When a current scan hashes a candidate file, the scanner should check the hash against loaded historical inventories.

Matches should be recorded as additional provenance:

- This blob was observed at the current source path.
- The same hash appeared in one or more historical inventories.
- Historical paths and source labels are retained as clues.

Non-matches are also useful:

- A newly ingested blob may not appear in the historical aggregate `all`.
- A historical media hash may not be found in current scanned roots.
- A known missing payload may remain missing.

Reconciliation should be report-only in v0. It should not prevent ingestion and should not automatically delete, merge, or prefer one source over another.

### Source Prioritization

Historical metadata can guide which roots to scan first.

For the old `Media` git-annex archive, the `aidisks` notes identify the best primary Kopia snapshot as:

```text
k4d350867fb6c79d48b75343f7b18b5d2
root@pluto-2:/Volumes/Data 3T1
2021-04-21 20:54:38 EDT
```

The notes also identify `kopia8t.data3t1.toc` as nearly complete for `Media/.git/annex/objects`, with supplemental payloads available from TV3T snapshot `k3ca461dd8a3c140d6578554e56e77f96`.

V0 should use this only as planning metadata:

- Report these sources as high-value roots to restore or mount before scanning.
- After a restore is available as ordinary files, scan it like any other source root.
- Compare scanned hashes against `Media.toc` and the Kopia catalogs.
- Report which expected media hashes were recovered and which remain unseen.

V0 should not call Kopia, mount snapshots, or perform restores automatically.

### Known Media Restore Gaps

The `aidisks` notes identify these notable `Media` annex payloads:

- `34c39853ba11ea328b385f7f3c81552443a209682340f4c96cb41921635dfd8d.mkv`: missing from Data3T1, available in TV3T.
- `385e78305135da40cfa5eab240621c52767a4d8daff9dacc607c09b48ada7802.mp4`: missing from Data3T1, available in TV3T and ST8T/latest catalogs.
- `366fe69301e6468d744d23a465b730305c13634f23b17f0b485ff98a444b7eb2.m4v`: not found in copied catalogs or aggregate `all`.
- `f197013a4286e36737a00c3c7dc8d24615d04a0ad9df9bb26ce2f7bf0fa373d0`: `.DS_Store`, not a media payload.

V0 reports should have a section for such expected historical hashes, showing whether the current ingestion run found matching bytes.

## Hashing And Deduplication

Every readable candidate file should be streamed through a cryptographic hash.

V0 hash:

- SHA-256.

Deduplication rule:

```text
same SHA-256 + same byte size = same blob
```

If the blob already exists:

- Do not store the bytes again.
- Record the source occurrence.
- Emit a `BlobAlreadyKnown` event.

If the blob does not exist:

- Store the bytes in immutable blob storage.
- Verify the stored bytes when practical.
- Emit a `BlobStored` event.

Hashing should be independent of path, filename, mtime, EXIF metadata interpretation, and archive membership.

## Blob Storage

V0 should use the local content-addressed backend described in the design overview.

Example storage key:

```text
sha256/ab/cd/abcdef...fullhash
```

The write path should be crash-tolerant:

1. Stream source bytes into a temporary object while hashing.
2. Finalize the hash and size.
3. If the blob already exists, discard the temporary object.
4. If the blob is new, atomically move the temporary object into its content-addressed location.
5. Optionally verify size and hash after the move.
6. Emit the storage event.

The system should never trust a partially written blob.

## Metadata Extraction

V0 should extract basic metadata after blob identification.

Useful fields:

- Detected media type.
- Pixel width and height, where applicable.
- Duration, for video if available.
- EXIF capture time, if available.
- Camera make and model, if available.
- Original filename from source occurrence.
- File size.
- Hash.

Metadata extraction is a fact produced by a specific extractor version.

If extraction fails:

- Record the failure.
- Keep the blob.
- Keep the source occurrence.
- Continue the scan.

Metadata should not be required for ingestion success.

## Events

V0 ingestion should extend the event vocabulary with explicit discovery events.

```text
SourceRootRegistered
HistoricalInventoryRegistered
HistoricalInventoryParsed
HistoricalInventoryParseFailed
SourceRootScanRequested
IngestionScanStarted
SourceEntryObserved
SourceEntrySkipped
ArchiveOpened
ArchiveEntryObserved
CandidateFileDetected
CandidateFileRejected
SourceFileHashStarted
SourceFileHashCompleted
SourceFileHashFailed
BlobStored
BlobAlreadyKnown
SourceOccurrenceRecorded
HistoricalInventoryMatchRecorded
AssetCreated
AssetVersionCreated
MetadataExtractionStarted
MetadataExtracted
MetadataExtractionFailed
IngestionScanCompleted
IngestionScanFailed
```

Events should include:

- Event id.
- Event type.
- Schema version.
- Timestamp.
- Actor or process id.
- Causation id.
- Correlation id for the scan.
- Payload.

The scan id should be the correlation id for all events produced by a scan.

Historical inventory parsing may use its own correlation id. Matches between scanned blobs and historical entries should use the scan id as the correlation id and reference the inventory id.

## Idempotency

Ingestion must tolerate repeated scans.

Stable identities:

- Source root id identifies a configured root.
- Historical inventory id identifies a registered metadata file.
- Inventory entry id is derived from inventory id, hash, and historical path when present.
- Scan id identifies one traversal run.
- Blob id is based on hash and size.
- Asset id is created once for a new media blob.
- Source occurrence id is derived from source root, observed path, container chain, size, and content hash when available.

Recommended idempotency behavior:

- Re-observing the same source occurrence in a later scan records that it was seen again.
- Re-observing the same blob does not create a duplicate blob.
- Re-observing the same blob does not create a duplicate asset.
- If a path previously pointed to one blob and now points to another, record a new source occurrence fact rather than overwriting the old one.

The event log should preserve historical observations. Projections can present the latest state.

## Resumability

V0 scans may be long-running and should survive interruption.

The scanner should maintain a checkpoint projection or job table derived from events. This state can be rebuilt or discarded, but it is useful operationally.

Checkpoint state may include:

- Current source root.
- Directory traversal frontier.
- Archive traversal frontier where practical.
- Files already hashed in the current scan.
- Errors already reported.

On resume:

- Already stored blobs are detected by hash and size.
- Already recorded source occurrences are not duplicated.
- Incomplete temporary blobs are discarded.
- The scan emits a continuation event or starts a new scan linked to the interrupted scan.

V0 may choose simpler behavior: restart the scan from the beginning and rely on idempotent blob and occurrence handling. This is acceptable if reports clearly distinguish repeated observations from new findings.

## Import Report

Each scan should produce a human-readable report projection.

The report should answer:

- Which roots were scanned?
- Which historical inventories were loaded?
- How many directories, archives, and files were observed?
- How many candidate media files were found?
- How many unique blobs were stored?
- How many candidate files were duplicates of existing blobs?
- How many assets were created?
- Which ingested blobs matched historical inventories?
- Which expected historical media hashes were not found in scanned roots?
- Which files failed to read, hash, parse, or store?
- Which archives failed to open or exceeded limits?
- Which files were skipped and why?
- Which source paths point to the same blob?

The report is a projection and can be regenerated from ingestion events.

## V0 Data Projections

The minimum useful projections are:

- Blob catalog: hash, size, storage location, first stored event.
- Source occurrence catalog: source root, path, container chain, hash, size, scan history, errors.
- Historical inventory catalog: inventory metadata, parsed hash entries, source labels, parse warnings.
- Historical reconciliation catalog: blob-to-inventory matches and expected historical hashes not yet seen.
- Asset catalog: asset id, original blob, first source occurrence, basic metadata.
- Scan report: counts, failures, duplicates, new blobs, new assets.
- Error catalog: source path, error type, retryability, scan id.

These projections support review and later triage without requiring a full photo application.

## Failure Handling

The scanner should be biased toward continuing.

Recoverable failures:

- Permission denied.
- File disappeared during scan.
- File changed during scan.
- Unsupported format.
- Corrupt media metadata.
- Corrupt archive entry.
- Archive exceeds configured limits.
- File has no supported extension.
- Historical inventory line cannot be parsed.
- Historical inventory path does not exist.

Fatal failures:

- Event log unavailable.
- Blob store unavailable.
- Configuration cannot be loaded.
- Storage integrity check fails for a newly stored blob.

Recoverable failures should be recorded as events and included in reports. Fatal failures should stop the scan and emit `IngestionScanFailed` if the event log is still available.

## Safety Rules

V0 ingestion must follow these rules:

- Source roots are read-only.
- Source files are never deleted.
- Source files are never renamed.
- Source directories are never reorganized.
- Archive contents are not extracted into permanent unmanaged locations.
- Blob writes are atomic.
- Original blob bytes are immutable after storage.
- Metadata extraction failures do not prevent preserving bytes.
- User-visible reports distinguish skipped files from failed files.
- Historical inventories are advisory and never replace hashing current bytes.

## Open Questions

- Should v0 ingest videos, or only still-image formats plus sidecars?
- Should XMP sidecars become separate assets initially, or source versions waiting for later pairing?
- Should repeated observations of the same path across scans be separate events every time, or compacted when unchanged?
- Which archive size and expansion limits are appropriate for the initial local machine?
- Which `aidisks` inventory files should be in the initial manifest?
- Should historical missing-list files be treated as expected-missing watchlists or just ordinary hash inventories?
- Should the initial implementation prioritize restart-from-beginning idempotency over fine-grained checkpoint resume?

## Suggested V0 Implementation Slice

A narrow first implementation can be:

1. Register local directory source roots.
2. Register selected `aidisks` `.toc`, `.lookup`, and missing-list files through an explicit manifest.
3. Parse historical hash inventories.
4. Walk directories without following symlinks.
5. Select JPEG, PNG, TIFF, HEIC, common RAW extensions, MP4, MOV, and XMP by extension.
6. Stream candidates through SHA-256.
7. Store unique blobs in a local content-addressed filesystem.
8. Record source occurrences and blob events.
9. Record matches between ingested blobs and historical inventories.
10. Create one asset per new media blob.
11. Extract basic metadata where easy.
12. Generate a scan report with historical reconciliation.
13. Add tar and tar.gz traversal after ordinary directory ingestion works.

This slice is enough to start turning a disorganized collection of backups, archives, and directory trees into a durable Photostore source of truth.
