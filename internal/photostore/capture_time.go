package photostore

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

const photoCaptureTimeReducerName = "photostore-capture-time"
const photoCaptureTimeReducerVersion = 1

type captureTimeProjection struct {
	Date      string
	LocalTime string
	UTCOffset string
	Precision string
	RawValue  string
}

func reducePhotoCaptureTime(tx *sql.Tx, ev Event, extractor map[string]any) error {
	fields := metadataFieldMap(ev.Payload["fields"])
	capture, ok := captureTimeFromFields(fields)
	if !ok {
		return nil
	}
	var relativePath, path string
	err := tx.QueryRow(`select relative_path, path from source_occurrences where source_occurrence_id = ?`, str(ev.Payload["source_occurrence_id"])).Scan(&relativePath, &path)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	filename := filepath.Base(relativePath)
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		filename = filepath.Base(path)
	}
	_, err = tx.Exec(`
		insert or replace into photo_capture_times(
			stored_object_id, content_ref, source_occurrence_id, scan_id, filename, relative_path,
			capture_date, capture_time_local, utc_offset, precision, source_kind, source_event_id,
			extractor_name, extractor_version, reducer_name, reducer_version, raw_value
		) values(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		str(ev.Payload["stored_object_id"]),
		str(ev.Payload["content_ref"]),
		str(ev.Payload["source_occurrence_id"]),
		str(ev.Payload["scan_id"]),
		filename,
		relativePath,
		capture.Date,
		capture.LocalTime,
		capture.UTCOffset,
		capture.Precision,
		"exif_datetime_original",
		ev.EventID,
		str(extractor["name"]),
		int64Value(extractor["version"]),
		photoCaptureTimeReducerName,
		photoCaptureTimeReducerVersion,
		capture.RawValue,
	)
	return err
}

func (s *Store) rebuildPhotoCaptureTimeProjection() error {
	rows, err := s.DB.Query(`
		select metadata_event_id, content_ref, extractor_name, extractor_version, stored_object_id, source_occurrence_id, scan_id, extracted_at_ms, fields_json, warnings_json
		from content_metadata
		where extractor_name = ? and extractor_version = ?`, metadataExtractorName, metadataExtractorVersion)
	if err != nil {
		return err
	}
	var inputs []struct {
		Event            Event
		ExtractorName    string
		ExtractorVersion int64
	}
	for rows.Next() {
		var input struct {
			Event            Event
			ExtractorName    string
			ExtractorVersion int64
		}
		var contentRef, storedObjectID, sourceOccurrenceID, scanID string
		var extractedAtMS int64
		var extractorName string
		var extractorVersion int64
		var fieldsJSON, warningsJSON string
		if err := rows.Scan(&input.Event.EventID, &contentRef, &extractorName, &extractorVersion, &storedObjectID, &sourceOccurrenceID, &scanID, &extractedAtMS, &fieldsJSON, &warningsJSON); err != nil {
			return err
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
			return err
		}
		input.Event.Payload = map[string]any{
			"content_ref":          contentRef,
			"stored_object_id":     storedObjectID,
			"source_occurrence_id": sourceOccurrenceID,
			"scan_id":              scanID,
			"extracted_at_ms":      extractedAtMS,
			"fields":               fields,
			"warnings":             json.RawMessage(warningsJSON),
		}
		input.ExtractorName = extractorName
		input.ExtractorVersion = extractorVersion
		inputs = append(inputs, input)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from photo_capture_times`); err != nil {
		return err
	}
	for _, input := range inputs {
		extractor := map[string]any{"name": input.ExtractorName, "version": input.ExtractorVersion}
		if err := reducePhotoCaptureTime(tx, input.Event, extractor); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func metadataFieldMap(v any) map[string]any {
	switch fields := v.(type) {
	case map[string]any:
		return fields
	case map[string]map[string]string:
		out := make(map[string]any, len(fields))
		for key, value := range fields {
			out[key] = value
		}
		return out
	default:
		return map[string]any{}
	}
}

func captureTimeFromFields(fields map[string]any) (captureTimeProjection, bool) {
	raw := metadataFieldRaw(fields, "datetime_original")
	if raw == "" {
		return captureTimeProjection{}, false
	}
	parsed, err := time.Parse("2006:01:02 15:04:05", raw)
	if err != nil {
		return captureTimeProjection{}, false
	}
	return captureTimeProjection{
		Date:      parsed.Format("2006-01-02"),
		LocalTime: parsed.Format("2006-01-02T15:04:05"),
		UTCOffset: metadataFieldRaw(fields, "offset_time_original"),
		Precision: "second",
		RawValue:  raw,
	}, true
}

func metadataFieldRaw(fields map[string]any, key string) string {
	v, ok := fields[key]
	if !ok {
		return ""
	}
	switch field := v.(type) {
	case map[string]string:
		return strings.TrimSpace(field["raw"])
	case map[string]any:
		return strings.TrimSpace(str(field["raw"]))
	default:
		return ""
	}
}
