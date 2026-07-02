package photostore

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const metadataExtractorName = "photostore-exif"
const metadataExtractorVersion = 2

type MetadataRefreshSummary struct {
	RequestID string `json:"request_id"`
	Attempted int    `json:"attempted"`
	Extracted int    `json:"extracted"`
	Failed    int    `json:"failed"`
	Skipped   int    `json:"skipped"`
	Issues    int    `json:"issues"`
}

type metadataCandidate struct {
	StoredObjectID     string
	SourceOccurrenceID string
	ScanID             string
	ContentRef         string
	StorageKey         string
}

type metadataObservation struct {
	Fields   map[string]map[string]string
	Warnings []string
}

func (s *Store) RefreshMissingMetadata(progress ProgressFunc) (MetadataRefreshSummary, error) {
	requestID := newID("meta_req")
	requestEventID, err := s.appendEventReturnID("PhotoMetadataRefreshRequested", nil, nil, map[string]any{
		"request_id":      requestID,
		"requested_at_ms": nowMS(),
		"selector": map[string]any{
			"type": "missing_metadata_results",
		},
		"extractor": metadataExtractorPayload(),
	})
	if err != nil {
		return MetadataRefreshSummary{}, err
	}
	candidates, err := s.metadataMissingCandidates()
	if err != nil {
		return MetadataRefreshSummary{}, err
	}
	summary := MetadataRefreshSummary{RequestID: requestID}
	for _, candidate := range candidates {
		summary.Attempted++
		progressf(progress, "extracting metadata for %s", candidate.StoredObjectID)
		result, err := s.recordMetadataForCandidate(candidate, &requestEventID)
		if err != nil {
			return summary, err
		}
		switch result {
		case "extracted":
			summary.Extracted++
		case "failed":
			summary.Failed++
		case "issue":
			summary.Issues++
		default:
			summary.Skipped++
		}
	}
	progressf(progress, "metadata refresh attempted: %d, extracted: %d, failed: %d, skipped: %d, issues: %d", summary.Attempted, summary.Extracted, summary.Failed, summary.Skipped, summary.Issues)
	return summary, nil
}

func (s *Store) metadataMissingCandidates() ([]metadataCandidate, error) {
	rows, err := s.DB.Query(`
		select so.stored_object_id, so.source_occurrence_id, so.scan_id, scl.content_ref, st.acquired_storage_key
		from source_occurrences so
		join source_content_links scl on scl.source_occurrence_id = so.source_occurrence_id
		join stored_objects st on st.stored_object_id = so.stored_object_id
		where st.purpose = 'source_media'
			and not exists (
				select 1 from content_metadata cm
				where cm.content_ref = scl.content_ref
					and cm.extractor_name = ?
					and cm.extractor_version = ?
			)
			and not exists (
				select 1 from content_metadata_failures cmf
				where cmf.content_ref = scl.content_ref
					and cmf.extractor_name = ?
					and cmf.extractor_version = ?
			)
		group by scl.content_ref
		order by min(so.path)`, metadataExtractorName, metadataExtractorVersion, metadataExtractorName, metadataExtractorVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []metadataCandidate
	for rows.Next() {
		var c metadataCandidate
		if err := rows.Scan(&c.StoredObjectID, &c.SourceOccurrenceID, &c.ScanID, &c.ContentRef, &c.StorageKey); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) recordMetadataForSourceFile(scanID string, causationID *string, sourceOccurrenceID, storedObjectID, contentRef, acquiredKey string) error {
	_, err := s.recordMetadataForCandidate(metadataCandidate{
		StoredObjectID:     storedObjectID,
		SourceOccurrenceID: sourceOccurrenceID,
		ScanID:             scanID,
		ContentRef:         contentRef,
		StorageKey:         acquiredKey,
	}, causationID)
	return err
}

func (s *Store) recordMetadataForCandidate(candidate metadataCandidate, causationID *string) (string, error) {
	path := filepath.Join(s.Root, filepath.FromSlash(candidate.StorageKey))
	observation, extractErr := extractJPEGMetadata(path)
	existing, hasSuccess, err := s.metadataFields(candidate.ContentRef)
	if err != nil {
		return "", err
	}
	if hasSuccess {
		if extractErr != nil {
			return s.appendMetadataMismatch(candidate, causationID, existing, map[string]map[string]string{}, extractErr.Error())
		}
		if reflect.DeepEqual(existing, observation.Fields) {
			return "skipped", nil
		}
		return s.appendMetadataMismatch(candidate, causationID, existing, observation.Fields, "")
	}
	hasFailure, err := s.metadataFailureExists(candidate.ContentRef)
	if err != nil {
		return "", err
	}
	if hasFailure {
		return "skipped", nil
	}
	if extractErr != nil {
		if err := s.appendEvent("PhotoMetadataExtractionFailed", causationID, &candidate.ScanID, map[string]any{
			"stored_object_id":     candidate.StoredObjectID,
			"source_occurrence_id": candidate.SourceOccurrenceID,
			"scan_id":              candidate.ScanID,
			"content_ref":          candidate.ContentRef,
			"extractor":            metadataExtractorPayload(),
			"failed_at_ms":         nowMS(),
			"error":                errPayload(extractErr, false),
		}); err != nil {
			return "", err
		}
		return "failed", nil
	}
	if err := s.appendEvent("PhotoMetadataExtracted", causationID, &candidate.ScanID, map[string]any{
		"stored_object_id":     candidate.StoredObjectID,
		"source_occurrence_id": candidate.SourceOccurrenceID,
		"scan_id":              candidate.ScanID,
		"content_ref":          candidate.ContentRef,
		"extractor":            metadataExtractorPayload(),
		"extraction_context": map[string]any{
			"phase":       "ingestion_scan",
			"source_kind": "source_root",
		},
		"extracted_at_ms": nowMS(),
		"fields":          observation.Fields,
		"warnings":        observation.Warnings,
	}); err != nil {
		return "", err
	}
	return "extracted", nil
}

func (s *Store) appendMetadataMismatch(candidate metadataCandidate, causationID *string, existing, observed map[string]map[string]string, observedError string) (string, error) {
	payload := map[string]any{
		"stored_object_id":          candidate.StoredObjectID,
		"source_occurrence_id":      candidate.SourceOccurrenceID,
		"scan_id":                   candidate.ScanID,
		"content_ref":               candidate.ContentRef,
		"extractor":                 metadataExtractorPayload(),
		"detected_at_ms":            nowMS(),
		"existing_metadata_fields":  existing,
		"observed_metadata_fields":  observed,
		"observed_extraction_error": nullable(observedError),
		"issue": map[string]any{
			"type":     "metadata_mismatch",
			"severity": "error",
		},
	}
	if err := s.appendEvent("PhotoMetadataObservationMismatchDetected", causationID, &candidate.ScanID, payload); err != nil {
		return "", err
	}
	return "issue", nil
}

func (s *Store) metadataFields(contentRef string) (map[string]map[string]string, bool, error) {
	var fieldsJSON string
	err := s.DB.QueryRow(`select fields_json from content_metadata where content_ref = ? and extractor_name = ? and extractor_version = ?`, contentRef, metadataExtractorName, metadataExtractorVersion).Scan(&fieldsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var fields map[string]map[string]string
	if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
		return nil, false, err
	}
	return fields, true, nil
}

func (s *Store) metadataFailureExists(contentRef string) (bool, error) {
	var found int
	err := s.DB.QueryRow(`select 1 from content_metadata_failures where content_ref = ? and extractor_name = ? and extractor_version = ?`, contentRef, metadataExtractorName, metadataExtractorVersion).Scan(&found)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func metadataExtractorPayload() map[string]any {
	return map[string]any{
		"name":    metadataExtractorName,
		"version": metadataExtractorVersion,
	}
}

func extractJPEGMetadata(path string) (metadataObservation, error) {
	f, err := os.Open(path)
	if err != nil {
		return metadataObservation{}, err
	}
	defer f.Close()
	payload, err := jpegEXIFPayload(f)
	if err != nil {
		return metadataObservation{}, err
	}
	fields, err := exifRawFields(payload)
	if err != nil {
		return metadataObservation{}, err
	}
	if len(fields) == 0 {
		return metadataObservation{}, errors.New("no supported EXIF metadata fields")
	}
	return metadataObservation{Fields: fields}, nil
}

func jpegEXIFPayload(r io.Reader) ([]byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	if header != [2]byte{0xff, 0xd8} {
		return nil, errors.New("not a JPEG file")
	}
	for {
		marker, err := nextJPEGMarker(r)
		if err != nil {
			return nil, err
		}
		if marker == 0xda || marker == 0xd9 {
			return nil, errors.New("missing EXIF segment")
		}
		var lenBuf [2]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return nil, err
		}
		segmentLen := int(binary.BigEndian.Uint16(lenBuf[:]))
		if segmentLen < 2 {
			return nil, fmt.Errorf("invalid jpeg segment length %d", segmentLen)
		}
		payloadLen := segmentLen - 2
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			return payload, nil
		}
	}
}

func exifRawFields(payload []byte) (map[string]map[string]string, error) {
	const exifHeader = "Exif\x00\x00"
	if len(payload) < len(exifHeader)+8 || string(payload[:len(exifHeader)]) != exifHeader {
		return nil, errors.New("missing EXIF header")
	}
	tiff := payload[len(exifHeader):]
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, errors.New("unsupported EXIF byte order")
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return nil, errors.New("invalid TIFF marker")
	}
	fields := map[string]map[string]string{}
	ifd0 := int(order.Uint32(tiff[4:8]))
	pointers := map[uint16]int{}
	if err := readEXIFIFD(tiff, order, ifd0, "ifd0", fields, pointers); err != nil {
		return nil, err
	}
	for _, pointer := range []struct {
		tag uint16
		ifd string
	}{
		{tag: 0x8769, ifd: "exif"},
		{tag: 0x8825, ifd: "gps"},
	} {
		offset := pointers[pointer.tag]
		if offset > 0 {
			if err := readEXIFIFD(tiff, order, offset, pointer.ifd, fields, nil); err != nil {
				return nil, err
			}
		}
	}
	if offset := pointers[0xa005]; offset > 0 {
		if err := readEXIFIFD(tiff, order, offset, "interop", fields, nil); err != nil {
			return nil, err
		}
	}
	return fields, nil
}

func readEXIFIFD(tiff []byte, order binary.ByteOrder, offset int, ifdName string, fields map[string]map[string]string, pointers map[uint16]int) error {
	if offset < 8 || offset+2 > len(tiff) {
		return errors.New("invalid EXIF IFD offset")
	}
	entries := int(order.Uint16(tiff[offset : offset+2]))
	pos := offset + 2
	for i := 0; i < entries; i++ {
		if pos+12 > len(tiff) {
			return errors.New("truncated EXIF IFD entry")
		}
		entry := tiff[pos : pos+12]
		tag := order.Uint16(entry[0:2])
		typ := order.Uint16(entry[2:4])
		count := order.Uint32(entry[4:8])
		if exifPointerTag(tag) && typ == 4 && count == 1 && pointers != nil {
			pointers[tag] = int(order.Uint32(entry[8:12]))
		}
		name := exifFieldName(ifdName, tag)
		field := map[string]string{
			"tag":   fmt.Sprintf("0x%04x", tag),
			"ifd":   ifdName,
			"type":  exifTypeName(typ),
			"count": fmt.Sprint(count),
		}
		if raw, ok := exifFieldValue(tiff, order, entry, typ, count); ok {
			field["raw"] = raw
		}
		fields[name] = field
		pos += 12
	}
	return nil
}

func exifPointerTag(tag uint16) bool {
	return tag == 0x8769 || tag == 0x8825 || tag == 0xa005
}

func exifFieldName(ifdName string, tag uint16) string {
	if name := knownEXIFFieldName(ifdName, tag); name != "" {
		return name
	}
	return fmt.Sprintf("%s_tag_%04x", ifdName, tag)
}

func knownEXIFFieldName(ifdName string, tag uint16) string {
	switch tag {
	case 0x010f:
		return "make"
	case 0x0110:
		return "model"
	case 0x0112:
		return "orientation"
	case 0x0132:
		return "modify_date"
	case 0x829a:
		return "exposure_time"
	case 0x829d:
		return "f_number"
	case 0x8827:
		return "iso_speed_ratings"
	case 0x882a:
		return "time_zone_offset"
	case 0x9000:
		return "exif_version"
	case 0x9003:
		return "datetime_original"
	case 0x9004:
		return "create_date"
	case 0x9201:
		return "shutter_speed_value"
	case 0x9202:
		return "aperture_value"
	case 0x9204:
		return "exposure_bias_value"
	case 0x9209:
		return "flash"
	case 0x920a:
		return "focal_length"
	case 0x9011:
		return "offset_time_original"
	case 0x9291:
		return "subsec_time_original"
	case 0xa002:
		return "pixel_x_dimension"
	case 0xa003:
		return "pixel_y_dimension"
	case 0xa405:
		return "focal_length_in_35mm_film"
	case 0xa406:
		return "scene_capture_type"
	default:
		return ""
	}
}

func exifTypeName(typ uint16) string {
	switch typ {
	case 1:
		return "byte"
	case 2:
		return "ascii"
	case 3:
		return "short"
	case 4:
		return "long"
	case 5:
		return "rational"
	case 7:
		return "undefined"
	case 9:
		return "slong"
	case 10:
		return "srational"
	default:
		return fmt.Sprintf("type_%d", typ)
	}
}

func exifFieldValue(tiff []byte, order binary.ByteOrder, entry []byte, typ uint16, count uint32) (string, bool) {
	if count == 0 {
		return "", false
	}
	raw, ok := exifValueBytes(tiff, order, entry, typ, count)
	if !ok {
		return "", false
	}
	switch typ {
	case 2:
		return string(bytes.TrimRight(raw, "\x00")), true
	case 1, 7:
		if count > 32 {
			return "", false
		}
		values := make([]string, 0, len(raw))
		for _, b := range raw {
			values = append(values, fmt.Sprint(b))
		}
		return strings.Join(values, ","), true
	case 3:
		values := make([]string, 0, count)
		for i := 0; i+2 <= len(raw); i += 2 {
			values = append(values, fmt.Sprint(order.Uint16(raw[i:i+2])))
		}
		return strings.Join(values, ","), true
	case 4:
		values := make([]string, 0, count)
		for i := 0; i+4 <= len(raw); i += 4 {
			values = append(values, fmt.Sprint(order.Uint32(raw[i:i+4])))
		}
		return strings.Join(values, ","), true
	case 5:
		values := make([]string, 0, count)
		for i := 0; i+8 <= len(raw); i += 8 {
			values = append(values, fmt.Sprintf("%d/%d", order.Uint32(raw[i:i+4]), order.Uint32(raw[i+4:i+8])))
		}
		return strings.Join(values, ","), true
	case 9:
		values := make([]string, 0, count)
		for i := 0; i+4 <= len(raw); i += 4 {
			values = append(values, fmt.Sprint(int32(order.Uint32(raw[i:i+4]))))
		}
		return strings.Join(values, ","), true
	case 10:
		values := make([]string, 0, count)
		for i := 0; i+8 <= len(raw); i += 8 {
			values = append(values, fmt.Sprintf("%d/%d", int32(order.Uint32(raw[i:i+4])), int32(order.Uint32(raw[i+4:i+8]))))
		}
		return strings.Join(values, ","), true
	default:
		return "", false
	}
}

func exifValueBytes(tiff []byte, order binary.ByteOrder, entry []byte, typ uint16, count uint32) ([]byte, bool) {
	typeSize := exifTypeSize(typ)
	if typeSize == 0 {
		return nil, false
	}
	size := int(count) * typeSize
	var raw []byte
	if size <= 4 {
		raw = entry[8 : 8+size]
	} else {
		offset := int(order.Uint32(entry[8:12]))
		if offset < 0 || offset+size > len(tiff) {
			return nil, false
		}
		raw = tiff[offset : offset+size]
	}
	return raw, true
}

func exifTypeSize(typ uint16) int {
	switch typ {
	case 1, 2, 7:
		return 1
	case 3:
		return 2
	case 4, 9:
		return 4
	case 5, 10:
		return 8
	default:
		return 0
	}
}
