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
)

const metadataExtractorName = "photostore-exif"
const metadataExtractorVersion = 1

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
	exifIFD := 0
	if err := readEXIFIFD(tiff, order, ifd0, fields, &exifIFD); err != nil {
		return nil, err
	}
	if exifIFD > 0 {
		if err := readEXIFIFD(tiff, order, exifIFD, fields, nil); err != nil {
			return nil, err
		}
	}
	return fields, nil
}

func readEXIFIFD(tiff []byte, order binary.ByteOrder, offset int, fields map[string]map[string]string, exifIFD *int) error {
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
		if tag == 0x8769 && typ == 4 && count == 1 && exifIFD != nil {
			*exifIFD = int(order.Uint32(entry[8:12]))
		}
		if name := exifFieldName(tag); name != "" {
			if raw, ok := exifASCIIValue(tiff, order, entry, typ, count); ok {
				fields[name] = map[string]string{"raw": raw}
			}
		}
		pos += 12
	}
	return nil
}

func exifFieldName(tag uint16) string {
	switch tag {
	case 0x0132:
		return "modify_date"
	case 0x9003:
		return "datetime_original"
	case 0x9004:
		return "create_date"
	case 0x9011:
		return "offset_time_original"
	case 0x9291:
		return "subsec_time_original"
	default:
		return ""
	}
}

func exifASCIIValue(tiff []byte, order binary.ByteOrder, entry []byte, typ uint16, count uint32) (string, bool) {
	if typ != 2 || count == 0 {
		return "", false
	}
	var raw []byte
	if count <= 4 {
		raw = entry[8 : 8+count]
	} else {
		offset := int(order.Uint32(entry[8:12]))
		if offset < 0 || offset+int(count) > len(tiff) {
			return "", false
		}
		raw = tiff[offset : offset+int(count)]
	}
	return string(bytes.TrimRight(raw, "\x00")), true
}
