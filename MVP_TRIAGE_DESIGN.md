# MVP Triage Design

This document defines the next implementation stage after the ingestion and review MVP. The goal is to make ingested photos actionable by introducing asset-scoped triage, using the vocabulary already established in [VISION.md](./VISION.md) and [DESIGN_OVERVIEW.md](./DESIGN_OVERVIEW.md).

The MVP keeps triage narrow:

- One projected asset per unique JPEG `content_ref`.
- One original asset version for the representative stored object.
- Asset-scoped quality labels: `Unrated`, `Best`, `Good`, `Poor`.
- User-defined arbitrary labels, modeled as non-exclusive tags.
- UI views for assigning and filtering these labels.

The MVP does not implement RAW/JPEG pairing, edited derivative grouping, album sharing, deletion, privacy enforcement, or recipe-based versions.

## Vocabulary

An asset is the logical item the user thinks about. For this MVP, an asset maps one-to-one with a unique JPEG content value. Exact duplicate source occurrences attach to the same asset through projections; they do not create duplicate assets.

Quality is a mutually exclusive dimension:

```text
Unrated | Best | Good | Poor
```

`Poor` is descriptive quality vocabulary. It does not imply deletion or exclusion semantics.

User-defined labels are arbitrary, non-exclusive tags. Examples might include `Family`, `Travel`, `Receipt`, or any user-entered value. The system should not ship a fixed tag vocabulary beyond the quality dimension.

## Event Model

Events record user intent. Reducers compute current asset state and filterable views.

### Asset Projection

Do not emit an event for asset creation in the MVP. Asset records are projection output from durable ingestion facts:

- `SourceFileAcquired`
- `ContentAddressMaterialized`
- metadata events
- source occurrence links

The MVP asset id is deterministic:

```text
asset_ + first 32 lowercase hex characters of the SHA-256 value in content_ref
```

This is intentionally conservative. The first asset projection is one asset per unique JPEG `content_ref`, so duplicate source occurrences converge on the same asset id. If a later model groups RAW/JPEG pairs or edited derivatives into richer assets, that should be a new projection version with an explicit migration design.

### QualityLabelSet

Records the current quality label selected by the user for an asset.

```json
{
  "schema_version": 1,
  "event_id": "evt_...",
  "timestamp_ms": 1782933120123,
  "type": "QualityLabelSet",
  "actor": {
    "kind": "local_user",
    "hostname": "host"
  },
  "asset_id": "asset_...",
  "content_ref": "sha256:...",
  "quality": "Good"
}
```

Allowed `quality` values:

```text
Unrated
Best
Good
Poor
```

Current quality is computed from the latest `QualityLabelSet` event for an asset. Previous quality is projection state and must not be copied into the event.

### AssetLabelApplied

Records that a user-defined label should apply to an asset.

```json
{
  "schema_version": 1,
  "event_id": "evt_...",
  "timestamp_ms": 1782933120123,
  "type": "AssetLabelApplied",
  "actor": {
    "kind": "local_user",
    "hostname": "host"
  },
  "asset_id": "asset_...",
  "content_ref": "sha256:...",
  "label": "Travel"
}
```

The reducer owns normalization rules. The event records the user-entered display label. If label normalization rules change later, a new reducer version can rebuild the query projection from display labels.

### AssetLabelRemoved

Records that a user-defined label should no longer apply to an asset.

```json
{
  "schema_version": 1,
  "event_id": "evt_...",
  "timestamp_ms": 1782933120123,
  "type": "AssetLabelRemoved",
  "actor": {
    "kind": "local_user",
    "hostname": "host"
  },
  "asset_id": "asset_...",
  "content_ref": "sha256:...",
  "label": "Travel"
}
```

Removing a label that is not currently applied is idempotent. The reducer normalizes the event label using the active label reducer version and removes the matching current projection row.

## Projections

The MVP adds asset and triage projections to SQLite. These projections are rebuildable from the event log and immutable stored objects.

```text
assets(
  asset_id primary key,
  asset_projection_version,
  content_ref unique,
  representative_stored_object_id,
  original_filename,
  first_source_occurrence_id,
  first_scan_id,
  created_from_event_id
)

asset_versions(
  asset_version_id primary key,
  asset_id,
  role,
  stored_object_id,
  content_ref,
  source_occurrence_id
)

asset_quality(
  asset_id primary key,
  quality,
  set_event_id,
  set_at_ms
)

asset_labels(
  asset_id,
  normalized_label,
  display_label,
  applied_event_id,
  applied_at_ms,
  primary key(asset_id, normalized_label)
)

label_catalog(
  normalized_label primary key,
  display_label,
  asset_count,
  last_applied_at_ms
)
```

Default quality is `Unrated` when no `QualityLabelSet` event exists.

The representative stored object should be selected deterministically, initially using the earliest source occurrence for the content. This is a projection choice; user intent should attach to the asset, not to the representative object.

## API

The MVP adds read APIs:

```text
GET /api/assets
GET /api/assets/{asset_id}
GET /api/assets/{asset_id}/sources
GET /api/assets?quality=Best
GET /api/assets?label=travel
GET /api/labels
```

The MVP adds command APIs:

```text
POST /api/assets/{asset_id}/quality
POST /api/assets/{asset_id}/labels
DELETE /api/assets/{asset_id}/labels
```

Command payloads:

Set quality:

```json
{ "quality": "Good" }
```

Apply label:

```json
{ "label": "Travel" }
```

Remove label:

```json
{ "label": "Travel" }
```

Commands append events first. Projections update through the normal event application path.

## UI

The first triage UI should be a tracer-bullet implementation from projection to browser:

- Add an Assets entry point from the dashboard.
- Render an asset thumbnail grid with filename, capture date if known, current quality, and user-defined labels.
- Open an asset detail route showing the representative image, metadata summary, source occurrences, current quality, and labels.
- Let the user set quality with `Unrated`, `Best`, `Good`, and `Poor`.
- Let the user add and remove arbitrary labels.
- Provide filter controls for quality and user-defined labels.

The UI should not imply deletion, sharing, privacy enforcement, or album membership. `Poor` is only a quality label.

Keyboard shortcuts can be added in this stage only if they are covered by E2E tests. A minimal mapping would be:

```text
0 -> Unrated
1 -> Poor
2 -> Good
3 -> Best
```

## E2E Coverage

Add a new E2E scenario separate from the ingestion dashboard scenario.

The scenario should:

- Start with an initialized store and ingested JPEG fixtures.
- Open the Assets view.
- Confirm duplicate source occurrences appear as one asset.
- Set one asset to `Best`.
- Set another asset to `Poor`.
- Add a user-defined label.
- Remove the user-defined label.
- Filter by quality.
- Filter by user-defined label.
- Open asset detail and verify source occurrence provenance is visible.

The generated scenario README and screenshots must be checked in. The existing E2E timeout rules still apply: no `waitForTimeout`, and no wait longer than 2000 ms.

## Implementation Order

1. Add asset projections and read APIs.
2. Add the asset grid/detail UI with read-only data.
3. Add `QualityLabelSet` command handling, projection updates, and UI controls.
4. Add `AssetLabelApplied` and `AssetLabelRemoved`, projection updates, and UI controls.
5. Add filter views for quality and user-defined labels.
6. Add the dedicated E2E scenario.

Each step should be end-to-end enough to inspect from the UI before deepening the next layer.

## Open Questions

- Should `status` remain deferred until quality and arbitrary labels are working?
- Should `visibility` be implemented before any sharing/export work, or introduced together with privacy enforcement?
