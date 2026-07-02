# Photo Time Views Design

This document designs date/time-based photo views for Photostore. It is a review document only; it does not specify an implementation patch.

The goal is to let the user browse ingested photos by when the photo was taken, while preserving Photostore's event-sourced model: events record durable facts and user intent, and reducers compute queryable views.

## Scope

The feature includes:

- Extracting capture-time metadata while scanning and acquiring JPEG bytes.
- Recording extraction facts as events.
- Avoiding duplicate metadata events when identical content already has metadata for the active extractor version.
- Comparing duplicate-content metadata observations against the existing metadata projection and recording explicit issues when they disagree.
- Allowing explicit user corrections when extracted capture time is wrong or missing.
- Building projections that support year, month, day, and timeline views.
- Adding UI routes that browse photos by capture date and link to existing image/object views.
- Reporting photos that cannot be placed on a capture-time timeline.

The feature excludes:

- Inferring capture time from source file paths, filenames, filesystem mtimes, or scan time.
- Silent fallback from one timestamp source to another.
- Support for non-JPEG media.
- Time-zone guessing beyond metadata that is explicitly present.
- Face/object recognition, album generation, or geospatial views.
- RAW/JPEG pairing.

## Principles

Capture-time views must be explainable. For any photo shown on a date, the system should be able to say why that date was selected.

The system should distinguish:

- Extracted metadata facts, such as EXIF `DateTimeOriginal`.
- Reducer-selected effective capture time.
- User corrections, such as "this photo was taken on 2007-08-12 at 15:31."

No reducer should invent a capture time from unrelated operational timestamps. If no supported capture-time metadata or correction exists, the asset belongs in an explicit undated view.

Metadata extraction should happen while ingestion already has the image bytes in hand. Scan/acquisition should not defer ordinary metadata extraction to a second pass for new content.

Metadata events are content facts for a specific extractor version. If a scan sees bytes whose `content_ref` already has a successful metadata event for the active extractor version, the scan should not append another identical `PhotoMetadataExtracted` event. It should compare the current observation to the existing metadata projection. If the comparison matches, no metadata event is needed. If it disagrees, append an explicit issue event.

When the extractor changes meaningfully, such as adding location fields for location-based indexing, the extractor version changes. The same content may then receive a new `PhotoMetadataExtracted` event for the new extractor version.

Timestamp parsing rules are reducer logic, not extractor logic. If parsing is improved, projections should be rebuilt from existing `PhotoMetadataExtracted` events instead of re-emitting metadata events.

## Metadata Sources

For the first version, supported capture-time sources are JPEG EXIF fields only:

- `DateTimeOriginal`
- `OffsetTimeOriginal`, when present
- `SubSecTimeOriginal`, when present
- `CreateDate`, as a separate extracted field, not as a fallback
- `ModifyDate`, as a separate extracted field, not as a fallback

The extractor should preserve durable raw source values. Reducers parse those raw values into capture-time projections. A malformed EXIF field is still useful evidence and should be visible in diagnostics because a future reducer version may interpret it differently.

The extractor must not use:

- Filesystem creation time.
- Filesystem modification time.
- Source path date fragments.
- Filename date fragments.
- Ingestion scan time.
- Historical inventory timestamps.

Those may be separate future features, but they require explicit events and review because they are not direct photo capture metadata.

## Events

Events should record extraction operations and user intent. Reducer conclusions, such as "effective capture date is 2009-04-18," should not be events unless the user explicitly corrected it.

### `PhotoMetadataExtracted`

Records metadata extracted from one newly observed content value using one extractor version.

Payload:

```json
{
  "stored_object_id": "obj_...",
  "source_occurrence_id": "occ_...",
  "scan_id": "scan_...",
  "content_ref": "sha256:...:12345",
  "extractor": {
    "name": "photostore-exif",
    "version": 1
  },
  "extraction_context": {
    "phase": "ingestion_scan",
    "source_kind": "source_root"
  },
  "extracted_at_ms": 1782931320456,
  "fields": {
    "datetime_original": {
      "raw": "2012:07:04 18:22:11"
    },
    "offset_time_original": {
      "raw": "-04:00"
    },
    "subsec_time_original": {
      "raw": "42"
    },
    "create_date": {
      "raw": "2012:07:04 18:22:11"
    },
    "modify_date": {
      "raw": "2014:01:12 09:03:02"
    }
  },
  "warnings": []
}
```

This event should be emitted when either:

- The content is new to the metadata projection.
- The content has metadata, but not for the active extractor version.

It should not be emitted merely because the same bytes appeared at another path.

### `PhotoMetadataRefreshRequested`

Records an explicit command to attempt metadata extraction for stored photo content that has no recorded metadata result for the active extractor version.

A metadata result is either:

- `PhotoMetadataExtracted`
- `PhotoMetadataExtractionFailed`

The refresh command must not retry content that already has either result for the active extractor version.

Payload:

```json
{
  "request_id": "meta_req_...",
  "requested_at_ms": 1782931320123,
  "selector": {
    "type": "missing_metadata_results"
  },
  "extractor": {
    "name": "photostore-exif",
    "version": 1
  }
}
```

### `PhotoMetadataExtractionFailed`

Payload:

```json
{
  "stored_object_id": "obj_...",
  "source_occurrence_id": "occ_...",
  "scan_id": "scan_...",
  "content_ref": "sha256:...:12345",
  "extractor": {
    "name": "photostore-exif",
    "version": 1
  },
  "failed_at_ms": 1782931320456,
  "error": {
    "type": "decode_error",
    "message": "missing EXIF segment",
    "retryable": false
  }
}
```

### `PhotoMetadataObservationMatched`

This is not an event.

When a scan sees duplicate content and the active extractor version already has metadata for that `content_ref`, the scanner should compare the current extracted metadata observation to the metadata projection. If it matches, that is a reducer/query conclusion and should remain unlogged.

### `PhotoMetadataObservationMismatchDetected`

Records that a duplicate-content observation produced metadata that disagrees with the existing metadata projection for the same `content_ref` and extractor version.

This should be rare. It can indicate a nondeterministic extractor, corrupt stored bytes, an incorrect `content_ref`, an extractor bug, or a parsing/versioning error.

Payload:

```json
{
  "stored_object_id": "obj_...",
  "source_occurrence_id": "occ_...",
  "scan_id": "scan_...",
  "content_ref": "sha256:...:12345",
  "extractor": {
    "name": "photostore-exif",
    "version": 1
  },
  "detected_at_ms": 1782931320456,
  "existing_metadata_event_id": "evt_...",
  "differences": [
    {
      "field": "datetime_original.raw",
      "existing": "2012:07:04 18:22:11",
      "observed": "2012:07:04 18:22:12"
    }
  ],
  "issue": {
    "type": "metadata_mismatch",
    "severity": "error"
  }
}
```

### `CaptureTimeCorrected`

Records user intent for the effective capture time of an asset or stored object.

Payload:

```json
{
  "target": {
    "type": "stored_object",
    "id": "obj_..."
  },
  "capture_time": {
    "local": "2012-07-04T18:22:11",
    "utc_offset": "-04:00",
    "precision": "second"
  },
  "reason": "camera clock was correct but timezone was missing",
  "corrected_at_ms": 1782931320789
}
```

Precision should be explicit:

- `year`
- `month`
- `day`
- `minute`
- `second`
- `subsecond`

The target may become `asset` once the asset model is richer. For the current JPEG ingestion model, `stored_object` is sufficient.

### `CaptureTimeCleared`

Records user intent that a prior correction should no longer provide effective capture time.

Payload:

```json
{
  "target": {
    "type": "stored_object",
    "id": "obj_..."
  },
  "cleared_at_ms": 1782931320999,
  "reason": "correction was applied to the wrong photo"
}
```

## Effective Capture Time Reducer

The reducer computes effective capture time for each stored photo.

Selection order:

1. Latest non-cleared `CaptureTimeCorrected` for the target.
2. Latest successful `PhotoMetadataExtracted` from the active extractor version whose raw `DateTimeOriginal` the active capture-time reducer version parses successfully.
3. No effective capture time.

`CreateDate`, `ModifyDate`, filesystem timestamps, paths, filenames, and ingestion times are not fallback capture-time sources.

The projection should preserve why a capture time was selected:

```text
content_ref
representative_stored_object_id
effective_capture_time_local
effective_capture_time_utc
utc_offset
precision
source_kind              # user_correction | exif_datetime_original | none
source_event_id
extractor_name
extractor_version
reducer_name
reducer_version
raw_value
parse_status
```

If `DateTimeOriginal` has no UTC offset, the local time can be used for local calendar grouping, but `effective_capture_time_utc` should be null. The UI should label this as "timezone unknown" instead of pretending it knows an absolute instant.

## Date Hierarchy Projection

The date hierarchy projection supports navigation without scanning every file at request time.

Suggested tables:

```text
photo_capture_times(
  content_ref,
  stored_object_id primary key,
  source_occurrence_id,
  scan_id,
  filename,
  relative_path,
  effective_capture_time_local,
  effective_capture_time_utc,
  utc_offset,
  precision,
  source_kind,
  source_event_id,
  extractor_name,
  extractor_version,
  reducer_name,
  reducer_version,
  raw_value
)

content_metadata(
  content_ref,
  extractor_name,
  extractor_version,
  metadata_event_id,
  extracted_at_ms,
  fields_json,
  warnings_json,
  primary key(content_ref, extractor_name, extractor_version)
)

capture_time_reducer_runs(
  reducer_name,
  reducer_version,
  extractor_name,
  extractor_version,
  reduced_at_ms
)

metadata_issues(
  issue_event_id primary key,
  content_ref,
  stored_object_id,
  source_occurrence_id,
  extractor_name,
  extractor_version,
  issue_type,
  severity,
  details_json
)

photo_date_buckets(
  bucket_kind,       # year | month | day | undated
  bucket_key,        # 2012 | 2012-07 | 2012-07-04 | undated
  photo_count,
  representative_stored_object_id
)
```

The reducer should be rebuildable from events. It should not read source files outside Photostore-owned storage.

## API Design

Initial read APIs:

```text
GET /api/photos/dates
GET /api/photos/dates/{year}
GET /api/photos/dates/{year}/{month}
GET /api/photos/dates/{year}/{month}/{day}
GET /api/photos/undated
GET /api/objects/{stored_object_id}/metadata
```

Example `GET /api/photos/dates`:

```json
[
  {
    "bucket_kind": "year",
    "bucket_key": "2012",
    "photo_count": 1840,
    "representative_thumbnail_url": "/api/objects/obj_.../thumbnail"
  }
]
```

Example day response:

```json
{
  "bucket_kind": "day",
  "bucket_key": "2012-07-04",
  "photos": [
    {
      "stored_object_id": "obj_...",
      "filename": "IMG_1234.JPG",
      "capture_time_local": "2012-07-04T18:22:11",
      "utc_offset": "-04:00",
      "precision": "second",
      "capture_time_source": "exif_datetime_original",
      "thumbnail_url": "/api/objects/obj_.../thumbnail",
      "view_url": "/api/objects/obj_.../bytes"
    }
  ]
}
```

Initial command APIs:

```text
POST /api/metadata/refresh-missing
POST /api/objects/{stored_object_id}/capture-time
DELETE /api/objects/{stored_object_id}/capture-time
```

Commands should append events. Read APIs should query projections.

Metadata extraction normally runs during ingestion scans. `POST /api/metadata/refresh-missing` is an explicit maintenance command for stored photo content that has no success or failure result for the active extractor version.

## UI Design

Add a Photos by Date section to the web UI.

Routes:

```text
/photos/dates
/photos/dates/2012
/photos/dates/2012/07
/photos/dates/2012/07/04
/photos/undated
```

Views:

- Year grid: year, count, representative thumbnail.
- Month grid: month name, count, representative thumbnail.
- Day grid: date, count, representative thumbnail.
- Day photo grid: thumbnails, filename, local capture time, source indicator.
- Undated grid: photos that have no effective capture time.

The UI should make source quality visible:

- `EXIF DateTimeOriginal`
- `User corrected`
- `Timezone unknown`
- `Undated`

The UI should provide a way to open metadata details for a photo. Details should show raw extracted values and the event/source used for the effective capture time.

## Scan-Time Extraction Workflow

During source scan acquisition:

1. Copy JPEG bytes into the acquired object while computing SHA-256 and size.
2. Compute `content_ref` from the measured hash and size.
3. Extract metadata from the Photostore-owned acquired object while the scan still has the object in scope.
4. Query the metadata projection for `(content_ref, extractor_name, extractor_version)`.
5. If no projection row exists, append `PhotoMetadataExtracted` or `PhotoMetadataExtractionFailed`.
6. If a projection row exists, compare the current observation to the projected metadata.
7. If the observation matches, append no metadata event.
8. If the observation differs, append `PhotoMetadataObservationMismatchDetected`.

This workflow prevents the event log from growing with repeated identical metadata facts for duplicate files while still detecting unexpected extractor or storage inconsistencies.

The scanner must use the Photostore-owned acquired object for extraction, not the original external path, so the operation remains grounded in durable input.

## Extractor Version Workflow

When the durable extracted artifact changes, create a new extractor version. Examples:

- Adding GPS/location fields.
- Recording additional raw fields needed by a new projection.
- Changing how EXIF tags are located or decoded from JPEG bytes.

For a new extractor version, already stored content needs an explicit command:

```text
photostore metadata extract --store ./photostore-data --scan-id scan_...
photostore metadata extract --store ./photostore-data --all
```

The command should:

- Read stored objects from Photostore-owned storage.
- Extract metadata from JPEG bytes.
- Append `PhotoMetadataExtracted` or `PhotoMetadataExtractionFailed`.
- Update projections through normal event application.
- Produce a metadata extraction report.

This is not a fallback. It is an explicit command for applying a new extractor version to existing Photostore-owned content.

## Reducer Version Workflow

When timestamp interpretation changes, create a new reducer version and rebuild projections from existing metadata events. Examples:

- Changing timestamp parsing rules.
- Fixing EXIF offset parsing.
- Changing how missing offsets are represented in local calendar buckets.
- Changing precision handling for subsecond fields.

These changes should not emit new `PhotoMetadataExtracted` events because the durable extracted artifact has not changed. They should update reducer metadata in projections, such as `reducer_name`, `reducer_version`, and `parse_status`.

## Open Questions

- Should the first implementation target stored objects directly, or introduce an asset projection first?
- Should capture-time corrections be per stored object for MVP and later lifted to assets?
- Should metadata extraction block scan completion, or can scan completion report pending metadata extraction failures separately?
- Should the UI expose bulk date correction in the first version?
- What extractor library should be used in Go for EXIF parsing, and how should extractor versioning be defined?
