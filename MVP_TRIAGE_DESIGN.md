# MVP Triage Design

This document defines the next implementation stage after the ingestion and review MVP. The goal is to make ingested photos actionable by introducing asset-scoped triage, using the vocabulary already established in [VISION.md](./VISION.md) and [DESIGN_OVERVIEW.md](./DESIGN_OVERVIEW.md).

The MVP keeps triage narrow:

- One projected asset per unique JPEG `content_ref`.
- One original asset version for the representative stored object.
- Asset-scoped quality labels: `Unrated`, `Best`, `Good`, `Poor`.
- Asset-scoped status: `Triage`, `Reviewed`.
- Asset-scoped visibility: `Normal`, `Private`.
- User-defined arbitrary labels, modeled as non-exclusive tags.
- UI views for assigning and filtering these labels.

The MVP does not implement RAW/JPEG pairing, edited derivative grouping, album sharing, deletion, public sharing, privacy enforcement beyond local UI filtering, or recipe-based versions.

## Vocabulary

An asset is the logical item the user thinks about. For this MVP, an asset maps one-to-one with a unique JPEG content value. Exact duplicate source occurrences attach to the same asset through projections; they do not create duplicate assets.

Quality is a mutually exclusive dimension:

```text
Unrated | Best | Good | Poor
```

`Poor` is descriptive quality vocabulary. It does not imply deletion or exclusion semantics.

User-defined labels are arbitrary, non-exclusive tags. Examples might include `Family`, `Travel`, `Receipt`, or any user-entered value. The system should not ship a fixed tag vocabulary beyond the quality dimension.

Status is a mutually exclusive workflow dimension:

```text
Triage | Reviewed
```

New assets default to `Triage`.

Visibility is a mutually exclusive local visibility dimension:

```text
Normal | Private
```

New assets default to `Normal`. For this MVP, `Private` affects local views and filters only. It is not a sharing-security feature because sharing is not in scope.

## Event Model

Events record user intent. Reducers compute current asset state and filterable views.

### AssetCreated

Records the allocated identity for a newly materialized JPEG content value.

Asset ids are allocated, not derived from `content_ref`. This avoids baking the MVP identity rule into externally visible ids before richer asset grouping exists. The event is emitted only after `ContentAddressMaterialized` for new JPEG content. Duplicate source occurrences and already-materialized content must not create another asset.

```json
{
  "schema_version": 1,
  "event_id": "evt_...",
  "timestamp_ms": 1782933120123,
  "type": "AssetCreated",
  "actor": {
    "kind": "system",
    "hostname": "host"
  },
  "asset_id": "asset_...",
  "content_ref": "sha256:...",
  "initial_version": {
    "asset_version_id": "av_...",
    "role": "original",
    "stored_object_id": "obj_...",
    "source_occurrence_id": "occ_..."
  },
  "defaults": {
    "quality": "Unrated",
    "status": "Triage",
    "visibility": "Normal"
  },
  "trigger": {
    "event_id": "evt_...",
    "type": "ContentAddressMaterialized"
  }
}
```

`AssetCreated` is the only MVP event that creates asset identity. Reducers should treat it as idempotent by `event_id`, `asset_id`, and `content_ref`.

The scan/acquisition path should use this rule synchronously:

```text
on ContentAddressMaterialized for new JPEG content:
  if no asset exists for content_ref:
    allocate asset_id and original asset_version_id
    append AssetCreated
```

The scan should not consider the item complete until `AssetCreated` is appended and projected. If the process stops after `ContentAddressMaterialized` but before `AssetCreated`, scan resume should continue the same acquisition workflow and append the missing `AssetCreated` event before doing asset-scoped work. This is not reducer-created asset identity; it is completion of an interrupted command workflow.

No other event should create an asset in the MVP. Metadata, thumbnail, source occurrence, label, status, and visibility events update projections for an existing asset. If a historical-inventory link references content that has no asset, that is a projection or ingestion inconsistency to report, not a reason for the inventory event to allocate asset identity.

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

### AssetStatusSet

Records the current workflow status selected by the user for an asset.

```json
{
  "schema_version": 1,
  "event_id": "evt_...",
  "timestamp_ms": 1782933120123,
  "type": "AssetStatusSet",
  "actor": {
    "kind": "local_user",
    "hostname": "host"
  },
  "asset_id": "asset_...",
  "content_ref": "sha256:...",
  "status": "Reviewed"
}
```

Allowed `status` values:

```text
Triage
Reviewed
```

Current status is computed from the latest `AssetStatusSet` event for an asset. If no such event exists after `AssetCreated`, status is `Triage`.

### AssetVisibilitySet

Records the current local visibility selected by the user for an asset.

```json
{
  "schema_version": 1,
  "event_id": "evt_...",
  "timestamp_ms": 1782933120123,
  "type": "AssetVisibilitySet",
  "actor": {
    "kind": "local_user",
    "hostname": "host"
  },
  "asset_id": "asset_...",
  "content_ref": "sha256:...",
  "visibility": "Private"
}
```

Allowed `visibility` values:

```text
Normal
Private
```

Current visibility is computed from the latest `AssetVisibilitySet` event for an asset. If no such event exists after `AssetCreated`, visibility is `Normal`.

## Projections

The MVP adds asset and triage projections to SQLite. These projections are rebuildable from the event log and immutable stored objects.

```text
assets(
  asset_id primary key,
  content_ref unique,
  representative_stored_object_id,
  original_filename,
  first_source_occurrence_id,
  first_scan_id,
  created_event_id,
  created_at_ms
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

asset_status(
  asset_id primary key,
  status,
  set_event_id,
  set_at_ms
)

asset_visibility(
  asset_id primary key,
  visibility,
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

Default quality is `Unrated` from `AssetCreated` when no `QualityLabelSet` event exists. Default status is `Triage` when no `AssetStatusSet` event exists. Default visibility is `Normal` when no `AssetVisibilitySet` event exists.

The representative stored object should be selected by a stable projection rule, initially using the earliest source occurrence for the content. This is a display choice; user intent should attach to the asset, not to the representative object.

## API

The MVP adds read APIs:

```text
GET /api/assets
GET /api/assets/{asset_id}
GET /api/assets/{asset_id}/sources
GET /api/assets?quality=Best
GET /api/assets?status=Triage
GET /api/assets?visibility=Private
GET /api/assets?label=travel
GET /api/labels
```

The MVP adds command APIs:

```text
POST /api/assets/{asset_id}/quality
POST /api/assets/{asset_id}/status
POST /api/assets/{asset_id}/visibility
POST /api/assets/{asset_id}/labels
DELETE /api/assets/{asset_id}/labels
```

Command payloads:

Set quality:

```json
{ "quality": "Good" }
```

Set status:

```json
{ "status": "Reviewed" }
```

Set visibility:

```json
{ "visibility": "Private" }
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
- Open an asset detail route showing the representative image, metadata summary, source occurrences, current quality, status, visibility, and labels.
- Let the user set quality with `Unrated`, `Best`, `Good`, and `Poor`.
- Let the user set status with `Triage` and `Reviewed`.
- Let the user set visibility with `Normal` and `Private`.
- Let the user add and remove arbitrary labels.
- Provide filter controls for quality, status, visibility, and user-defined labels.

The UI should not imply deletion, sharing, public privacy enforcement, or album membership. `Poor` is only a quality label, and `Private` is only local visibility state in this MVP.

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
- Set one asset to `Reviewed`.
- Set one asset to `Private`.
- Add a user-defined label.
- Remove the user-defined label.
- Filter by quality.
- Filter by status.
- Filter by visibility.
- Filter by user-defined label.
- Open asset detail and verify source occurrence provenance is visible.

The generated scenario README and screenshots must be checked in. The existing E2E timeout rules still apply: no `waitForTimeout`, and no wait longer than 2000 ms.

## Implementation Order

1. Add `AssetCreated` emission after `ContentAddressMaterialized` for new JPEG content.
2. Add asset projections and read APIs.
3. Add the asset grid/detail UI with read-only data.
4. Add `QualityLabelSet` command handling, projection updates, and UI controls.
5. Add `AssetStatusSet` and `AssetVisibilitySet`, projection updates, and UI controls.
6. Add `AssetLabelApplied` and `AssetLabelRemoved`, projection updates, and UI controls.
7. Add filter views for quality, status, visibility, and user-defined labels.
8. Add the dedicated E2E scenario.

Each step should be end-to-end enough to inspect from the UI before deepening the next layer.
