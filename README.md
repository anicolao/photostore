# Photostore

Photostore is an intended system for sorting, editing, viewing, preserving, and sharing large personal photo libraries.

The project is designed around a few core beliefs:

- Originals should be immutable and durable.
- User intent and system facts should be recorded as events.
- Databases, indexes, thumbnails, albums, and views should be rebuildable projections.
- Photo workflows need fast triage, rich metadata, non-destructive editing, and reliable sharing.
- The photo model should make assets, source files, recipes, projections, and sharing state explicit.

This repository contains the early Photostore design documents and a runnable ingestion MVP. The current implementation can initialize a local store, scan local JPEG directory trees, acquire historical `.toc` inventories, preserve bytes in a content-addressed store, deduplicate retained duplicates, extract EXIF metadata, generate thumbnails, and serve a local web UI for reviewing scan progress and photos.

## Intended Capabilities

Photostore is intended to support:

- Importing photos, videos, documents, scans, screenshots, RAW files, sidecars, and edited derivatives.
- Deduplicating exact file content using cryptographic hashes.
- Keeping original files immutable.
- Fast triage across desktop, mobile, tablet, and shared-screen workflows.
- Labeling assets by independent dimensions such as quality, media kind, privacy, and purpose.
- Viewing the library through generated projections: date, label, album, import, camera, location, person, query, or custom collection.
- Non-destructive editing through explicit intent and recipe history.
- Generating thumbnails, previews, exports, and share bundles from reproducible recipes.
- Sharing selected assets or albums with controlled visibility.
- Rebuilding derived state after bugs, schema changes, renderer changes, or storage migration.

## Design Documents

- [VISION.md](./VISION.md) describes the product and system goals.
- [DESIGN_OVERVIEW.md](./DESIGN_OVERVIEW.md) describes the intended architecture.
- [INGESTION_DESIGN.md](./INGESTION_DESIGN.md) describes the v0 discovery and ingestion design.
- [MVP_IMPLEMENTATION_PLAN.md](./MVP_IMPLEMENTATION_PLAN.md) describes the first ingestion implementation slice.
- [MVP_USAGE.md](./MVP_USAGE.md) describes how to run the first ingestion MVP.
- [MVP_UI_DESIGN.md](./MVP_UI_DESIGN.md) describes the first local web interface for ingestion.
- [E2E_GUIDE.md](./E2E_GUIDE.md) describes the Playwright testing strategy for the web interface.

## Architectural Direction

Photostore uses an append-only transaction log as the source of truth. The log records what happened and what the user intended.

Examples:

- A file was imported from a camera card.
- A blob with a given hash was stored.
- An asset was created from that blob.
- The user marked the asset as `Good`.
- The user classified the asset as a `Photo`.
- A RAW file and JPEG were paired as the same capture.
- The user requested a crop, exposure adjustment, or resize.
- A thumbnail was generated from a particular recipe and renderer version.
- An album was shared with a specific access policy.

Operational databases, search indexes, filesystem views, thumbnails, previews, and caches are projections of this event log.

## Storage Direction

Photostore uses a photo asset model backed by immutable blob storage.

The initial backend can be a local content-addressed filesystem layout. Later backends may include S3-compatible object storage, MinIO, Backblaze B2, Cloudflare R2, or similar systems.

## Operator Recipes

Photostore uses an operator-tree-inspired recipe model for derived artifacts.

A derived object is always better modeled as a reproducible recipe over original captures, imported source files, and recorded user intent. Everything the system presents is the result of replaying actions and intentions over immutable source material.

Examples:

- `resize(original, width=512, format=avif)`
- `render_raw(raw, sidecar, profile)`
- `apply_edit(original, operations)`
- `package_album_zip(asset_versions)`
- `encrypt(export, recipient)`

Materialized outputs can be cached as blobs, but they are projections. The recipe, source blobs, and event history are authoritative.

## Current Status

The repository has moved past design-only work. The implemented MVP is focused on ingestion and review:

- Go CLI and local web server in `cmd/photostore` and `internal/photostore`.
- SvelteKit UI in `web/`.
- Append-only JSONL event log with SQLite projections.
- `.jpg`/`.jpeg` source scans and historical `.toc` inventory scans.
- Content-addressed storage with hard-link materialization.
- Thumbnail, metadata, date-browsing, and duplicate-deduplication workflows.
- Go tests, Playwright E2E tests, screenshot baselines, and macOS CI.
