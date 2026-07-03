package photostore

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanExtractsMetadataOncePerContent(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	jpegBytes := jpegWithEXIF(t, map[uint16]string{
		0x010f: "Canon",
		0x0110: "EOS 5D",
		0x1234: "custom value",
		0x9003: "2012:07:04 18:22:11",
		0x9011: "-04:00",
	})
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), jpegBytes)
	mustWrite(t, filepath.Join(sourcePath, "B.JPG"), jpegBytes)

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSourceRoot(sourcePath, "source"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScanSources(nil); err != nil {
		t.Fatal(err)
	}

	var metadataRows int
	if err := st.DB.QueryRow(`select count(*) from content_metadata`).Scan(&metadataRows); err != nil {
		t.Fatal(err)
	}
	if metadataRows != 1 {
		t.Fatalf("content metadata rows = %d, want 1", metadataRows)
	}
	var failures int
	if err := st.DB.QueryRow(`select count(*) from content_metadata_failures`).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != 0 {
		t.Fatalf("metadata failures = %d, want 0", failures)
	}
	var fieldsJSON string
	if err := st.DB.QueryRow(`select fields_json from content_metadata`).Scan(&fieldsJSON); err != nil {
		t.Fatal(err)
	}
	var fields map[string]map[string]string
	if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
		t.Fatal(err)
	}
	if got := fields["datetime_original"]["raw"]; got != "2012:07:04 18:22:11" {
		t.Fatalf("datetime_original raw = %q, want EXIF value", got)
	}
	if got := fields["make"]["raw"]; got != "Canon" {
		t.Fatalf("make raw = %q, want camera make", got)
	}
	if got := fields["model"]["raw"]; got != "EOS 5D" {
		t.Fatalf("model raw = %q, want camera model", got)
	}
	if got := fields["exif_tag_1234"]["raw"]; got != "custom value" {
		t.Fatalf("unknown EXIF tag raw = %q, want retained value", got)
	}
	if strings.Contains(fieldsJSON, "parsed") {
		t.Fatalf("metadata event contains parsed reducer data: %s", fieldsJSON)
	}
	metadataEvent := latestEventOfType(t, st, "PhotoMetadataExtracted")
	context := mapValue(metadataEvent.Payload["extraction_context"])
	if got := str(context["phase"]); got != "ingestion_scan" {
		t.Fatalf("metadata extraction phase = %q, want ingestion_scan", got)
	}
	if got := str(context["source_kind"]); got != "source_root" {
		t.Fatalf("metadata extraction source kind = %q, want source_root", got)
	}
	var captureDate string
	var captureTime string
	var offset string
	var captureSourceKind string
	if err := st.DB.QueryRow(`select capture_date, capture_time_local, utc_offset, source_kind from photo_capture_times`).Scan(&captureDate, &captureTime, &offset, &captureSourceKind); err != nil {
		t.Fatal(err)
	}
	if captureDate != "2012-07-04" {
		t.Fatalf("capture date = %q, want reducer date", captureDate)
	}
	if captureTime != "2012-07-04T18:22:11" {
		t.Fatalf("capture time = %q, want reducer local timestamp", captureTime)
	}
	if offset != "-04:00" {
		t.Fatalf("capture offset = %q, want EXIF offset", offset)
	}
	if captureSourceKind != "exif_datetime_original" {
		t.Fatalf("capture source kind = %q, want exif_datetime_original", captureSourceKind)
	}

	years, err := st.PhotoYears()
	if err != nil {
		t.Fatal(err)
	}
	if len(years.Buckets) != 1 || years.Buckets[0].BucketKey != "2012" || years.Buckets[0].PhotoCount != 1 {
		t.Fatalf("year buckets = %#v, want one 2012 unique content bucket", years.Buckets)
	}
	photos, err := st.DatedPhotos("2012", "07", "04")
	if err != nil {
		t.Fatal(err)
	}
	if len(photos.Photos) != 1 || photos.Photos[0].Filename != "A.JPG" {
		t.Fatalf("dated photos = %#v, want representative A.JPG", photos.Photos)
	}
}

func TestCaptureTimeDoesNotFallBackToCreateOrModifyDate(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	jpegBytes := jpegWithEXIF(t, map[uint16]string{
		0x9004: "2013:08:05 12:30:00",
		0x0132: "2014:09:06 13:31:01",
	})
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), jpegBytes)

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSourceRoot(sourcePath, "source"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScanSources(nil); err != nil {
		t.Fatal(err)
	}

	var captureRows int
	if err := st.DB.QueryRow(`select count(*) from photo_capture_times`).Scan(&captureRows); err != nil {
		t.Fatal(err)
	}
	if captureRows != 0 {
		t.Fatalf("capture time rows = %d, want 0 without DateTimeOriginal", captureRows)
	}
	undated, err := st.UndatedPhotos()
	if err != nil {
		t.Fatal(err)
	}
	if len(undated.Photos) != 1 || undated.Photos[0].Filename != "A.JPG" {
		t.Fatalf("undated photos = %#v, want A.JPG", undated.Photos)
	}
}

func TestRefreshMissingMetadataRecordsFailureOnce(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	content := []byte("not a jpeg")
	objID := "obj_manual"
	occID := "occ_manual"
	scanID := "scan_manual"
	key := filepath.Join("objects", "acquired", objID)
	mustWrite(t, filepath.Join(st.Root, key), content)
	ref := contentRef(sha(content), int64(len(content)))
	if err := st.appendEvent("SourceFileAcquired", nil, &scanID, map[string]any{
		"scan_id":              scanID,
		"source_occurrence_id": occID,
		"stored_object_id":     objID,
		"purpose":              "source_media",
		"content_ref":          ref,
		"source_root_id":       nil,
		"source_kind":          "source_root",
		"path":                 "/missing/A.JPG",
		"relative_path":        "A.JPG",
		"acquired_storage_key": filepath.ToSlash(key),
		"storage_disposition": map[string]any{
			"cas_existed_at_ingest":    false,
			"acquired_object_retained": true,
		},
	}); err != nil {
		t.Fatal(err)
	}

	summary, err := st.RefreshMissingMetadata(nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Attempted != 1 || summary.Failed != 1 {
		t.Fatalf("summary = %#v, want one failed attempt", summary)
	}
	var failures int
	if err := st.DB.QueryRow(`select count(*) from content_metadata_failures`).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != 1 {
		t.Fatalf("metadata failures = %d, want 1", failures)
	}
	summary, err = st.RefreshMissingMetadata(nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Attempted != 0 {
		t.Fatalf("second refresh attempted = %d, want 0", summary.Attempted)
	}
}

func TestRefreshMissingMetadataRecordsRefreshContext(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	content := jpegWithEXIF(t, map[uint16]string{
		0x010f: "Canon",
		0x0110: "EOS 5D",
		0x9003: "2012:07:04 18:22:11",
	})
	objID := "obj_manual"
	occID := "occ_manual"
	scanID := "scan_manual"
	key := filepath.Join("objects", "acquired", objID)
	mustWrite(t, filepath.Join(st.Root, key), content)
	ref := contentRef(sha(content), int64(len(content)))
	if err := st.appendEvent("SourceFileAcquired", nil, &scanID, map[string]any{
		"scan_id":              scanID,
		"source_occurrence_id": occID,
		"stored_object_id":     objID,
		"purpose":              "source_media",
		"content_ref":          ref,
		"source_kind":          "source_root",
		"path":                 "/missing/A.JPG",
		"relative_path":        "A.JPG",
		"acquired_storage_key": filepath.ToSlash(key),
		"storage_disposition": map[string]any{
			"cas_existed_at_ingest":    false,
			"acquired_object_retained": true,
		},
	}); err != nil {
		t.Fatal(err)
	}

	summary, err := st.RefreshMissingMetadata(nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Extracted != 1 {
		t.Fatalf("summary extracted = %d, want 1", summary.Extracted)
	}
	metadataEvent := latestEventOfType(t, st, "PhotoMetadataExtracted")
	context := mapValue(metadataEvent.Payload["extraction_context"])
	if got := str(context["phase"]); got != "metadata_refresh_missing" {
		t.Fatalf("metadata extraction phase = %q, want metadata_refresh_missing", got)
	}
	if got := str(context["source_kind"]); got != "source_root" {
		t.Fatalf("metadata extraction source kind = %q, want source_root", got)
	}
}

func TestMetadataMismatchDetectedForDuplicateContentObservation(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	contentRef := "sha256:v1:12:existing"
	scanID := "scan_manual"
	if err := st.appendEvent("PhotoMetadataExtracted", nil, &scanID, map[string]any{
		"stored_object_id":     "obj_existing",
		"source_occurrence_id": "occ_existing",
		"scan_id":              scanID,
		"content_ref":          contentRef,
		"extractor":            metadataExtractorPayload(),
		"extraction_context": map[string]any{
			"phase":       "ingestion_scan",
			"source_kind": "source_root",
		},
		"extracted_at_ms": nowMS(),
		"fields": map[string]map[string]string{
			"make": {"raw": "Canon"},
		},
		"warnings": []string{},
	}); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join("objects", "acquired", "obj_observed")
	mustWrite(t, filepath.Join(st.Root, key), jpegWithEXIF(t, map[uint16]string{
		0x010f: "Nikon",
		0x9003: "2012:07:04 18:22:11",
	}))

	result, err := st.recordMetadataForCandidate(metadataCandidate{
		StoredObjectID:     "obj_observed",
		SourceOccurrenceID: "occ_observed",
		ScanID:             scanID,
		ContentRef:         contentRef,
		StorageKey:         filepath.ToSlash(key),
		SourceKind:         "source_root",
		Phase:              "ingestion_scan",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "issue" {
		t.Fatalf("metadata result = %q, want issue", result)
	}
	var issues int
	if err := st.DB.QueryRow(`select count(*) from metadata_issues where content_ref = ?`, contentRef).Scan(&issues); err != nil {
		t.Fatal(err)
	}
	if issues != 1 {
		t.Fatalf("metadata issues = %d, want 1", issues)
	}
}

func jpegWithEXIF(t *testing.T, fields map[uint16]string) []byte {
	t.Helper()
	base := testJPEG(t)
	payload := exifPayload(t, fields)
	segmentLen := len(payload) + 2
	app1 := []byte{0xff, 0xe1, byte(segmentLen >> 8), byte(segmentLen)}
	out := append([]byte{}, base[:2]...)
	out = append(out, app1...)
	out = append(out, payload...)
	out = append(out, base[2:]...)
	return out
}

func latestEventOfType(t *testing.T, st *Store, eventType string) Event {
	t.Helper()
	events, err := st.RecentEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType == eventType {
			return events[i]
		}
	}
	t.Fatalf("event %s not found", eventType)
	return Event{}
}

func exifPayload(t *testing.T, fields map[uint16]string) []byte {
	t.Helper()
	var tiff bytes.Buffer
	tiff.Write([]byte{'I', 'I'})
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(42))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(8))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(1))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0x8769))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(4))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(1))
	exifIFDOffset := uint32(8 + 2 + 12 + 4)
	_ = binary.Write(&tiff, binary.LittleEndian, exifIFDOffset)
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0))
	entries := make([]uint16, 0, len(fields))
	for tag := range fields {
		entries = append(entries, tag)
	}
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(len(entries)))
	dataStart := int(exifIFDOffset) + 2 + len(entries)*12 + 4
	data := bytes.Buffer{}
	for _, tag := range entries {
		value := append([]byte(fields[tag]), 0)
		_ = binary.Write(&tiff, binary.LittleEndian, tag)
		_ = binary.Write(&tiff, binary.LittleEndian, uint16(2))
		_ = binary.Write(&tiff, binary.LittleEndian, uint32(len(value)))
		if len(value) <= 4 {
			var inline [4]byte
			copy(inline[:], value)
			tiff.Write(inline[:])
		} else {
			_ = binary.Write(&tiff, binary.LittleEndian, uint32(dataStart+data.Len()))
			data.Write(value)
		}
	}
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0))
	tiff.Write(data.Bytes())
	return append([]byte("Exif\x00\x00"), tiff.Bytes()...)
}
