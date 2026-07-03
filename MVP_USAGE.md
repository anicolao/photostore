# MVP Usage

This document describes the first runnable Photostore ingestion MVP.

The MVP is intentionally narrow:

- It scans local directory trees.
- It ingests files with `.jpg` or `.jpeg` extensions, case-insensitively.
- It hashes bytes while copying them into Photostore.
- It stores retained acquired objects under `objects/acquired/`.
- It materializes newly seen content under `cas/sha256/v1/`.
- It stores generated JPEG thumbnails under `thumbnails/jpeg/240/orient-v2/`.
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

The development shell provides Go, Bun, Node.js, SQLite, Git, and GitHub CLI tooling.

## Build And Test

```sh
go test ./...
go build ./cmd/photostore
cd web && bun install --frozen-lockfile
cd web && bun run check
cd web && bun run build
cd web && bun run test:e2e:install
cd web && bun run test:e2e
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
  thumbnails/jpeg/240/orient-v2/
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
go run ./cmd/photostore scan --store ./photostore-data --verbose
```

The command prints progress to stderr. It prints a `scan_...` id and, with `--verbose`, prints the final report automatically.

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
  --resolver-root /path/to/restored/Media \
  --verbose
```

During inventory scans, Photostore first checks the projection of already seen content by trusted historical hash. If a matching hash is already present, it emits a compact link event and skips resolving/copying that historical path.

Only entries whose trusted hash is not already seen are resolved to files and acquired.

## Duplicate Garbage Reporting

Acquisition records duplicate observations. New content is materialized into CAS
as a hard link from the acquired object. A later verifier/deduplicator
recomputes hashes, performs a byte-for-byte comparison, and relinks acquired
objects to the canonical CAS inode for the current deduplication strategy.

Scan reports include:

- `duplicate_acquisitions`
- `duplicate_garbage_bytes`

These numbers estimate the retained duplicate data created by the scan.

## Current Limits

- The implementation is currently serial internally, though the CLI accepts `--workers` for compatibility with the MVP plan.
- Newly seen CAS content is materialized with a hard link from the acquired object; filesystems without hard-link support are not supported.
- Projection replay is maintained from the event log using a byte-offset cursor.
- Archive traversal is not implemented.
- Non-JPEG media is not implemented.

## Serve The MVP Web UI

Start the local web interface:

```sh
go run ./cmd/photostore serve --store ./photostore-data
```

Then open `http://127.0.0.1:8080`. The server binds to loopback by default and
refuses public addresses unless `--allow-public-bind` is passed explicitly.
Completed scans generate thumbnails for acquired JPEGs. The scan drilldown view
uses those thumbnails when available and shows a placeholder for photos whose
thumbnail has not been generated yet. Thumbnail directories are projection
outputs; if thumbnail rendering changes, Photostore can use a new renderer
namespace and leave older thumbnails as garbage.

For frontend development:

```sh
go run ./cmd/photostore serve --store ./photostore-data --addr 127.0.0.1:8080 --api-only
cd web && bun run dev
```
