# Design Overview

This document describes the intended eventual architecture for Photostore.

## Core Principle

Photostore is an event-sourced photo asset system.

Photostore uses an append-only event log as its authoritative record. Runtime views are projections:

- Relational database tables.
- Search indexes.
- Filesystem views.
- Thumbnail caches.
- Preview caches.
- Sharing manifests.
- Album pages.
- Statistics.
- Background job state.

The event log records what happened and what the user intended. Projections reduce those events into forms that are fast to query or serve.

## Major Components

```text
                  import sources
                        |
                        v
                 ingestion workers
                        |
                        v
  immutable blobs <-> event log -> projection reducers -> query database
                        |                 |
                        |                 v
                        |          search / views / albums
                        |
                        v
              recipe execution layer
                        |
                        v
        thumbnails / previews / edits / exports
                        |
                        v
                 serving and sharing
```

## Event Log

The event log should be append-only and durable.

Events should be explicit, versioned, replayable, and grounded in durable inputs.

### Event Shape

An event should include:

- Event id.
- Event type.
- Schema version.
- Timestamp recorded by the system.
- Actor or process that produced it.
- Causation id, where one event directly caused another.
- Correlation id, for a larger workflow such as an import batch.
- Payload.
- Optional references to blobs, recipes, assets, versions, or external paths.

Example event types:

```text
ImportBatchStarted
ImportFileObserved
BlobStored
BlobAlreadyKnown
AssetCreated
AssetVersionCreated
ExifExtracted
CaptureTimeCorrected
QualityLabelSet
MediaKindSet
VisibilitySet
TagAdded
TagRemoved
AssetsPaired
AlbumCreated
AlbumItemAdded
EditRequested
RecipeDefined
RecipeMaterialized
ShareCreated
SharePolicyChanged
ProjectionRebuildRequested
```

### Facts And Intent

The log should distinguish between facts discovered by the system and intent expressed by users.

Facts:

- This file had these bytes at import time.
- This blob has this SHA-256 hash.
- EXIF extraction produced this metadata using this extractor version.
- A derived preview was rendered using this recipe and renderer version.

Intent:

- The user marked this asset as `Best`.
- The user corrected the capture date.
- The user requested this crop.
- The user added this asset to an album.
- The user shared this album with download disabled.

This distinction matters because facts may be superseded by better extraction or rendering, while user intent should remain stable.

### Correction Events

Events are preserved as append-only records during normal operation.

Corrections should be represented as new events:

- `CaptureTimeCorrected` records the selected capture time.
- `QualityLabelSet` records the selected quality label.
- `RecipeSuperseded` records replacement recipe semantics.
- `ShareRevoked` records a sharing lifecycle transition.

Administrative repair tools should preserve the append-only system model.

## Projections

Projection reducers consume the event log and build queryable state.

Important projections include:

- Asset catalog.
- Blob catalog.
- Asset version graph.
- Current labels.
- Albums and collections.
- Import batches.
- Search index.
- Date hierarchy.
- Privacy visibility index.
- Sharing manifests.
- Recipe dependency graph.
- Materialization cache index.
- Filesystem-compatible views.

Reducers should be deterministic for a given event stream and reducer version.

When reducer logic changes, projections can be rebuilt from the log. The system may support partial rebuilds based on event ranges, affected assets, or dependency tracking.

## Storage Model

Photostore should use immutable content-addressed blobs for file bytes.

### Blobs

A blob is an immutable byte sequence with metadata such as:

- Hash, initially SHA-256.
- Size.
- Media type, if known.
- Storage backend.
- Storage key.
- Creation event.
- Optional secondary hashes.
- Optional integrity check history.

Blob identity should be based on content. Application identity should include asset meaning, relationships, labels, versions, and event history.

### Assets

An asset is the logical item a user thinks about.

Examples:

- A photo.
- A RAW capture with associated JPEG.
- A document scan.
- A screenshot.
- A video.
- An edited image imported from elsewhere.

Assets may have multiple versions and related blobs.

### Asset Versions

An asset version represents a meaningful file, rendering, or state associated with an asset.

Possible roles:

- `original`
- `raw`
- `camera_jpeg`
- `sidecar`
- `edit`
- `preview`
- `thumbnail`
- `export`
- `share_render`

Versions can point to source blobs, recipes, and materialized recipe outputs.

### Backends

The system should define a small blob storage interface:

```text
put_blob(stream) -> blob_ref
get_blob(blob_ref) -> stream
has_blob(hash, size) -> bool
verify_blob(blob_ref) -> verification_result
delete_unreferenced_blob(blob_ref)
```

Initial backend:

- Local filesystem content-addressed store.

Possible later backends:

- S3-compatible object storage.
- MinIO.
- Backblaze B2.
- Cloudflare R2.
- Network-attached storage.
- Read-only removable archives.

## Naming And Filesystem Views

Human-friendly names are generated projections.

A date-based view may expose paths such as:

```text
2002/12/24/083122-123.JPG
```

This path is useful for browsing, export, and compatibility. Asset identity is recorded in the event log and projections.

Date-based views account for:

- Missing or corrected capture timestamps.
- Timezones can be ambiguous.
- Multiple cameras can produce colliding names.
- Edits and exports may preserve or alter timestamps.
- Some assets are documents, screenshots, or scans.

Filesystem views may also expose:

```text
imports/<batch>/
quality/Best/
quality/Good/
kind/Photo/
visibility/Private/
albums/<album>/
tools/photoshop/<workspace>/
queries/<saved-query>/
```

These views should be generated from projections. Writable views should translate writes into events.

## Triage And Labels

Triage should begin with a low-friction state for newly imported assets.

The system should track independent label dimensions.

Suggested dimensions:

```text
quality:    Unrated | Best | Good | Poor
kind:       Photo | Video | RAW | Document | Scan | Screenshot | Other
visibility: Normal | Private | Hidden
status:     Triage | Reviewed | NeedsMetadata | NeedsEdit
tags:       user-defined, non-exclusive
```

The exact vocabulary can evolve. The architecture should allow:

- Mutually exclusive dimensions where appropriate.
- Non-exclusive tags where appropriate.
- Label history.
- Bulk operations.
- Default views such as `quality in (Best, Good) and kind = Photo and visibility = Normal`.

## Operator Recipes

Photostore represents derived artifacts as operator recipes.

A recipe is a typed description of how to produce bytes or metadata from inputs.

Examples:

```text
blob(sha256)
resize(input, width=512, format=avif, quality=70)
render_raw(raw_blob, sidecar_blob, profile, renderer_version)
apply_edits(input, operations)
extract_exif(blob, extractor_version)
package_album(asset_versions, format=zip)
encrypt(input, recipient)
```

Recipes should be:

- Typed.
- Versioned.
- Deterministic where possible.
- Explicit about input blobs, input recipes, and operator versions.
- Safe to evaluate in controlled execution environments.
- Cacheable.

The initial operator set should be constrained and typed for reliability.

### Materialization

A recipe may be materialized into a blob.

The materialized blob is useful for serving and caching. It is a projection of source blobs, recorded intent, and recipe execution.

When resize behavior or renderer semantics change:

1. Fix the renderer or define a new operator version.
2. Mark affected recipes as stale or superseded.
3. Replay or re-materialize them.
4. Update serving projections.

## Editing Model

Editing should be non-destructive and event-sourced.

When a user edits an asset, Photostore should record:

- The source asset version.
- The requested edit operation.
- Parameters.
- Tool or renderer version.
- Optional external project files.
- Output recipe.
- Materialized output blob, if generated.

External tools can be supported through managed workspaces. For example, a Photoshop-oriented workspace may expose files in a conventional directory while Photostore observes writes, stores new blobs, and records resulting events.

The system should distinguish:

- Imported external edits, which are original facts about existing files.
- Photostore-native non-destructive edits, which are replayable recipes.
- Cached renderings, which are materialized recipe outputs.

## Serving Model

Serving should use derived assets for normal viewing workflows.

Common served artifacts:

- Tiny placeholder.
- Small thumbnail.
- Grid thumbnail.
- Large preview.
- Full-resolution display version.
- Original download.
- Album export.

The serving layer should consult projections to determine:

- Which version is current.
- Whether the viewer has access.
- Whether a suitable materialized derivative exists.
- Whether a recipe should be evaluated on demand.
- Whether a signed URL, proxied response, or local stream should be used.

Private assets require explicit authorization before appearing in public caches or generated views.

## Imports

Importing should be modeled as a workflow with durable events.

Typical import flow:

1. Observe a file in an import source.
2. Hash the file.
3. Store or recognize the blob.
4. Extract metadata.
5. Create or associate an asset.
6. Detect exact duplicates.
7. Detect related files such as RAW+JPEG pairs or sidecars.
8. Place the asset in triage.
9. Generate initial thumbnails and previews.

The import source may be:

- Camera card.
- Local directory.
- Phone upload.
- Existing photo library export.
- Cloud import.
- Scanner workflow.
- Ad hoc drop folder.

The import event history should make it possible to reconstruct what was imported, from where, and how it was classified.

## Deduplication And Relationships

SHA-256 deduplication handles exact byte duplicates.

The system should also model richer relationships:

- Same capture.
- RAW+JPEG pair.
- Sidecar for RAW.
- Edited derivative.
- Export of version.
- Burst sequence.
- Live-photo pair.
- Visual duplicate.
- Similar image.
- Document pages in a set.

Some relationships are facts inferred by the system. Others are user-confirmed intent. The event log should preserve that distinction.

## Query And Search

Search operates over projections derived from the event log.

Useful query dimensions include:

- Labels.
- Tags.
- Dates.
- Imports.
- Albums.
- People.
- Places.
- Camera metadata.
- File type.
- Dimensions.
- Edit status.
- Sharing status.
- Privacy.
- Similarity.
- Text extracted from documents or screenshots.

Search indexes can be rebuilt from events and blob-derived metadata.

## Consistency Model

The system can be eventually consistent internally.

After an event is appended:

- Projections may update asynchronously.
- Thumbnails may appear later.
- Search may lag.
- Sharing manifests may be regenerated.

User-facing workflows should make pending work visible where it matters.

Critical actions, such as import completion or share publication, should have clear durability boundaries.

## Garbage Collection

Garbage collection should be conservative.

Before collecting an unreferenced blob, the system must consider:

- Event log references.
- Current projections.
- Recipe dependencies.
- Materialized derivatives.
- Shares.
- Retention policies.
- Retention windows.
- User-defined preservation rules.

Original imported blobs should be retained. Explicit deletion should follow an evented workflow with appropriate safeguards.

Derived blobs are retained according to cache policy and regenerated from their recipes.

## Security And Privacy

The system should treat privacy as a first-class part of the model.

Important concerns:

- Private labels must affect projections and serving.
- Shares should be explicit and auditable.
- Public exports should strip metadata according to policy.
- Original downloads may use different policy from previews.
- Operator execution should be sandboxed for externally supplied files.
- Remote storage credentials must be isolated.
- Logs should redact private filenames, paths, and share URLs according to policy.

## Recovery And Migration

A healthy Photostore installation should be recoverable from:

- Event log.
- Blob store.
- Configuration.
- Operator implementations or pinned renderer versions where required.

The system should support:

- Projection rebuilds.
- Blob verification.
- Storage backend migration.
- Renderer upgrades.
- Recipe re-materialization.
- Schema evolution through versioned events.
- Export to simple directory trees for escape hatches.

## Current MVP Decisions

The ingestion MVP has resolved the first implementation slice this way:

- Event log storage is append-only JSONL with one event per line.
- SQLite is the first projection database.
- Event ordering is single-writer per local store; multi-device append is out of scope.
- The first UI is a local loopback web server, not a sharing surface.
- The first media type is JPEG by `.jpg`/`.jpeg` extension.
- Metadata extraction runs during scan for new content and can be refreshed explicitly for unattempted content.
- Originals and acquired source objects are immutable `0400` files. Retained duplicates are replaced only by the explicit verify-and-deduplicate workflow after hash recomputation and byte comparison.

## Open Design Questions

These questions remain outside the ingestion MVP:

- What is the minimal useful recipe operator set?
- How should mobile or offline clients append events?
- How should writable filesystem views be implemented?
- What sharing model is needed after the local UI?
- What media types beyond JPEG should be first-class in version one?
- What is the long-term deletion and retention policy for originals?

## Architectural Summary

Photostore should be built as:

- An event log that records durable facts and user intent.
- Immutable blob storage for original and derived bytes.
- Rebuildable projections for fast queries and views.
- Typed operator recipes for derived artifacts.
- Non-destructive asset versioning.
- A serving layer that enforces privacy and uses cached derivatives.
- A storage strategy based on proven blob storage interfaces.

The custom value of Photostore is the combination of event-sourced intent, durable original preservation, explicit photo asset relationships, reproducible derivations, and ergonomic triage and sharing workflows.
