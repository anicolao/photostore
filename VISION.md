# Vision

Photostore is a long-lived personal photo system for people who want control over their photo library, modern viewing workflows, and convenient sharing.

It should preserve the original archive, make everyday sorting pleasant, support serious editing workflows, and remain understandable years later.

## Product Goals

Photostore should make it easy to answer:

- What did I import?
- What is worth keeping?
- What is private?
- What is the best version of this photo?
- Where did this image come from?
- What edits produced this result?
- Which photos are ready to share?
- How can I rebuild my library view after an index, cache, or renderer changes?

The system should be suitable for large personal and family archives.

## User Experience Principles

### Triage Should Be Fast

Incoming assets should enter a triage flow where they can be classified with minimal effort. Triage uses independent dimensions:

- Quality: `Best`, `Good`, `Poor`, `Unrated`
- Media kind: `Photo`, `Video`, `RAW`, `Document`, `Scan`, `Screenshot`, `Other`
- Visibility: `Normal`, `Private`, `Hidden`
- Purpose or tags: `Documentary`, `Receipt`, `Family`, `Travel`, user-defined tags, and other labels

This keeps common triage simple while allowing a private asset to also be a photo, document, screenshot, or scan.

Triage should work well:

- On a desktop with keyboard shortcuts.
- On a phone with swipes and taps.
- On a tablet in a shared review session.
- Immediately after import.
- Later, when returning to an unfinished batch.

### Originals Should Be Sacred

Imported originals should remain immutable.

This applies to:

- Camera JPEGs.
- RAW files.
- Sidecar files.
- Scans.
- Documents.
- Screenshots.
- Videos.
- Existing edited exports imported from other tools.

Edits should create new versions or new recipes. The original should remain available.

### Views Should Be Flexible

Users should have many useful views over the same library.

The same asset should be viewable by:

- Capture date.
- Import batch.
- Quality.
- Media kind.
- Privacy.
- Camera or lens.
- Location.
- Album.
- Person or subject.
- Edit status.
- Sharing status.
- Custom query.
- Ad hoc collection.

Filesystem-like views provide compatibility with external tools and are generated from the event log and projections.

### Sharing Should Respect Intent

Sharing should be based on explicit asset versions and access policies.

The system should support:

- Private-only local viewing.
- Shared albums.
- Public links.
- Expiring links.
- Download permissions.
- Watermarked or resized exports.
- Revocation where technically possible.
- Auditable history of what was shared.

Private assets should require explicit inclusion before appearing in public projections or generated exports.

## System Goals

### Event Log As Source Of Truth

The authoritative record should be an append-only transaction log.

The log should record facts and user intent:

- A file appeared at an import location.
- A blob was hashed and stored.
- An asset was created.
- Metadata was extracted.
- A label was applied.
- Two files were linked as a RAW+JPEG pair.
- A user requested an edit.
- A renderer generated a derived output.
- An album was shared.

Derived state should be rebuildable from the event log.

This allows the system to regenerate outputs after:

- Image processing improvements.
- Thumbnail renderer changes.
- Metadata extractor changes.
- Schema changes.
- Search index rebuilds.
- Cache refreshes.
- Changes in edit rendering logic.
- Migration to a new storage backend.

### Projections Are Rebuildable

Databases, indexes, directory views, thumbnails, previews, and caches may be persisted for performance. They are projections derived from the event log.

Projection maintenance follows a replayable flow:

1. Fix the reducer, renderer, indexer, or projection logic.
2. Replay the event log.
3. Regenerate the affected state.

Recovery flows should repair projection logic, replay the event log, and regenerate the affected state.

### Storage Should Be Durable And Understandable

The storage system should be inspectable and recoverable.

The system should store immutable blobs by content hash and retain enough event history to rebuild the logical library.

The photo model should make source files, assets, recipes, projections, and sharing state explicit.

### Recipes Should Preserve Intent

When a user asks for a resized image, edited photo, rendered RAW, or exported album, the system should record the intended operation, inputs, and output recipe.

When rendering logic changes, the system should replay recorded intent and regenerate affected outputs.

Materialized outputs are caches derived from source blobs, recipes, and event history.

## Long-Term Ambition

Photostore should eventually feel like a personal photo operating system:

- Fast enough for daily use.
- Trustworthy enough for archival use.
- Explicit enough for debugging.
- Flexible enough for serious workflows.
- Durable enough that the data survives the software.
