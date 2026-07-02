# Photo Time Views Design

This document designs date/time-based photo views for Photostore. It is a review document only; it does not specify an implementation patch.

The goal is to let the user browse ingested photos by when the photo was taken, while preserving Photostore's event-sourced model: events record durable facts and user intent, and reducers compute queryable views.

## Scope

The feature includes:

- Extracting capture-time metadata from acquired JPEG objects.
- Recording extraction facts as events.
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

## Metadata Sources

For the first version, supported capture-time sources are JPEG EXIF fields only:

- `DateTimeOriginal`
- `OffsetTimeOriginal`, when present
- `SubSecTimeOriginal`, when present
- `CreateDate`, as a separate extracted field, not as a fallback
- `ModifyDate`, as a separate extracted field, not as a fallback

The extractor should preserve raw source values and parse results separately. A malformed EXIF field is still useful evidence and should be visible in diagnostics.

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

### `MetadataExtractionRequested`

Records that metadata extraction should run for a set of stored objects.

Payload:

```json
{
  "request_id": "meta_req_...",
  "stored_object_ids": ["obj_..."],
  "media_kind": "jpeg",
  "extractor": {
    "name": "photostore-exif",
    "version": 1
  },
  "requested_at_ms": 1782931320123
}
```

For large batches, the event may reference a scan id instead of listing every object:

```json
{
  "request_id": "meta_req_...",
  "scan_id": "scan_...",
  "media_kind": "jpeg",
  "extractor": {
    "name": "photostore-exif",
    "version": 1
  },
  "requested_at_ms": 1782931320123
}
```

### `PhotoMetadataExtracted`

Records metadata extracted from one Photostore-owned stored object.

Payload:

```json
{
  "stored_object_id": "obj_...",
  "source_occurrence_id": "occ_...",
  "content_ref": "sha256:...:12345",
  "extractor": {
    "name": "photostore-exif",
    "version": 1
  },
  "extracted_at_ms": 1782931320456,
  "fields": {
    "datetime_original": {
      "raw": "2012:07:04 18:22:11",
      "parsed_local": "2012-07-04T18:22:11",
      "parse_status": "ok"
    },
    "offset_time_original": {
      "raw": "-04:00",
      "parse_status": "ok"
    },
    "subsec_time_original": {
      "raw": "42",
      "parse_status": "ok"
    },
    "create_date": {
      "raw": "2012:07:04 18:22:11",
      "parsed_local": "2012-07-04T18:22:11",
      "parse_status": "ok"
    },
    "modify_date": {
      "raw": "2014:01:12 09:03:02",
      "parsed_local": "2014-01-12T09:03:02",
      "parse_status": "ok"
    }
  },
  "warnings": []
}
```

If extraction fails, record an explicit failure event rather than omitting the object.

### `PhotoMetadataExtractionFailed`

Payload:

```json
{
  "stored_object_id": "obj_...",
  "source_occurrence_id": "occ_...",
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
2. Latest successful `PhotoMetadataExtracted` from the active extractor version where `DateTimeOriginal` parsed successfully.
3. No effective capture time.

`CreateDate`, `ModifyDate`, filesystem timestamps, paths, filenames, and ingestion times are not fallback capture-time sources.

The projection should preserve why a capture time was selected:

```text
stored_object_id
effective_capture_time_local
effective_capture_time_utc
utc_offset
precision
source_kind              # user_correction | exif_datetime_original | none
source_event_id
extractor_name
extractor_version
raw_value
parse_status
```

If `DateTimeOriginal` has no UTC offset, the local time can be used for local calendar grouping, but `effective_capture_time_utc` should be null. The UI should label this as "timezone unknown" instead of pretending it knows an absolute instant.

## Date Hierarchy Projection

The date hierarchy projection supports navigation without scanning every file at request time.

Suggested tables:

```text
photo_capture_times(
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
  raw_value
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
POST /api/metadata/extract
POST /api/objects/{stored_object_id}/capture-time
DELETE /api/objects/{stored_object_id}/capture-time
```

Commands should append events. Read APIs should query projections.

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

## Backfill Workflow

This feature needs a metadata extraction command over already ingested objects:

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

This is not a legacy fallback. It is an explicit command that creates missing required events for stored objects that predate metadata extraction.

## Open Questions

- Should the first implementation target stored objects directly, or introduce an asset projection first?
- Should capture-time corrections be per stored object for MVP and later lifted to assets?
- Should metadata extraction run automatically after ingestion, or remain an explicit command/job?
- Should the UI expose bulk date correction in the first version?
- What extractor library should be used in Go for EXIF parsing, and how should extractor versioning be defined?
