# MVP Usage

This document describes the first runnable Photostore ingestion MVP.

The MVP is intentionally narrow:

- It scans local directory trees.
- It ingests files with `.jpg` or `.jpeg` extensions, case-insensitively.
- It hashes bytes while copying them into Photostore.
- It stores retained acquired objects under `objects/acquired/`.
- It materializes newly seen content under `cas/sha256/v1/`.
- It records append-only JSONL events under `events/events.jsonl`.
- It maintains rebuildable SQLite projections in `projections.sqlite3`.
- It can acquire historical `.toc`-style inventory files and use their trusted hashes to skip duplicate byte acquisition.

## Development Environment

This repository uses Nix flakes.

```sh
nix develop
```

If you use `direnv`:

```sh
direnv allow
```

The development shell provides Go, SQLite, Git, and GitHub CLI tooling.

## Build And Test

```sh
go test ./...
go build ./cmd/photostore
```

## Initialize A Store

```sh
go run ./cmd/photostore init --store ./photostore-data
```

This creates:

```text
photostore-data/
  events/events.jsonl
  objects/acquired/
  cas/sha256/v1/
  tmp/
  projections.sqlite3
  reports/
```

## Scan A Directory

Register a local source root:

```sh
go run ./cmd/photostore source add \
  --store ./photostore-data \
  --path /path/to/photos \
  --label photos
```

Run a scan:

```sh
go run ./cmd/photostore scan --store ./photostore-data
```

The command prints a `scan_...` id.

Show the report:

```sh
go run ./cmd/photostore report \
  --store ./photostore-data \
  --scan-id scan_...
```

## Historical Inventory Flow

Acquire a historical inventory file:

```sh
go run ./cmd/photostore inventory acquire \
  --store ./photostore-data \
  --path ../aidisks/Media.toc \
  --label Media \
  --group media_restore
```

The command prints an `inv_...` id.

Scan the acquired inventory for JPEG entries:

```sh
go run ./cmd/photostore inventory scan \
  --store ./photostore-data \
  --inventory inv_... \
  --type toc \
  --ext .jpg \
  --ext .jpeg \
  --resolver-root /path/to/restored/Media
```

During inventory scans, Photostore first checks the projection of already seen content by trusted historical hash. If a matching hash is already present, it emits a compact link event and skips resolving/copying that historical path.

Only entries whose trusted hash is not already seen are resolved to files and acquired.

## Current Limits

- The implementation is currently serial internally, though the CLI accepts `--workers` for compatibility with the MVP plan.
- APFS clone is attempted with `cp -c`; ordinary copy is used as a fallback.
- Verification and safe deduplication are follow-up processes and are not implemented yet.
- Projections are maintained while events are appended; a full projection rebuild command is not implemented yet.
- Archive traversal is not implemented.
- Non-JPEG media is not implemented.
