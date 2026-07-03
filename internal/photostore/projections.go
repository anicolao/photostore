package photostore

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type StoreSummary struct {
	StorePath                string `json:"store_path"`
	EventCount               int    `json:"event_count"`
	SourceRootCount          int    `json:"source_root_count"`
	HistoricalInventoryCount int    `json:"historical_inventory_count"`
	ScanCount                int    `json:"scan_count"`
	ContentCount             int    `json:"content_count"`
	RetainedDuplicateBytes   int64  `json:"retained_duplicate_bytes"`
	LastScanCompletedAtMS    *int64 `json:"last_scan_completed_at_ms"`
}

type ScanProjection struct {
	ScanID        string          `json:"scan_id"`
	Status        string          `json:"status"`
	StartedAtMS   *int64          `json:"started_at_ms"`
	CompletedAtMS *int64          `json:"completed_at_ms"`
	Stats         json.RawMessage `json:"stats"`
	Report        *ScanReportView `json:"report,omitempty"`
}

type ScanReportView struct {
	ScanID                       string `json:"scan_id"`
	SourceRootsScanned           *int   `json:"source_roots_scanned"`
	DirectoriesSeen              *int   `json:"directories_seen"`
	RegularFilesSeen             *int   `json:"regular_files_seen"`
	CandidateFilesSeen           *int   `json:"candidate_files_seen"`
	SourceFilesAcquired          *int   `json:"source_files_acquired"`
	SourceFileAcquireFailures    *int   `json:"source_file_acquire_failures"`
	ContentAddressesMaterialized *int   `json:"content_addresses_materialized"`
	DuplicateAcquisitions        *int   `json:"duplicate_acquisitions"`
	DuplicateGarbageBytes        *int64 `json:"duplicate_garbage_bytes"`
	NonCandidateFilesSkipped     *int   `json:"non_candidate_files_skipped"`
	HistoricalJPEGEntriesLoaded  *int   `json:"historical_jpeg_entries_loaded"`
	HistoricalEntriesAlreadySeen *int   `json:"historical_entries_already_seen"`
	HistoricalEntriesResolved    *int   `json:"historical_entries_resolved"`
	HistoricalEntriesUnresolved  *int   `json:"historical_entries_unresolved"`
}

type HistoricalInventoryProjection struct {
	ID             string `json:"historical_inventory_id"`
	StoredObjectID string `json:"stored_object_id"`
	OriginalPath   string `json:"original_path"`
	Label          string `json:"label"`
	Group          string `json:"group"`
}

type AcquiredFileProjection struct {
	SourceOccurrenceID string `json:"source_occurrence_id"`
	StoredObjectID     string `json:"stored_object_id"`
	SourceKind         string `json:"source_kind"`
	SourceRootID       string `json:"source_root_id,omitempty"`
	Path               string `json:"path"`
	RelativePath       string `json:"relative_path"`
	Filename           string `json:"filename"`
	ScanID             string `json:"scan_id"`
	ContentRef         string `json:"content_ref"`
	ViewURL            string `json:"view_url"`
	BytesURL           string `json:"bytes_url"`
	ThumbnailURL       string `json:"thumbnail_url"`
}

type PhotoDateBucket struct {
	BucketKey    string `json:"bucket_key"`
	DisplayLabel string `json:"display_label"`
	PhotoCount   int    `json:"photo_count"`
}

type PhotoDateBucketResponse struct {
	BucketKind string            `json:"bucket_kind"`
	BucketKey  string            `json:"bucket_key"`
	Buckets    []PhotoDateBucket `json:"buckets"`
}

type DatedPhotoProjection struct {
	StoredObjectID   string `json:"stored_object_id"`
	ContentRef       string `json:"content_ref"`
	Filename         string `json:"filename"`
	RelativePath     string `json:"relative_path"`
	CaptureDate      string `json:"capture_date,omitempty"`
	CaptureTimeLocal string `json:"capture_time_local,omitempty"`
	UTCOffset        string `json:"utc_offset,omitempty"`
	Precision        string `json:"precision,omitempty"`
	ViewURL          string `json:"view_url"`
	BytesURL         string `json:"bytes_url"`
	ThumbnailURL     string `json:"thumbnail_url"`
}

type ObjectMetadataProjection struct {
	StoredObjectID   string                       `json:"stored_object_id"`
	ContentRef       string                       `json:"content_ref"`
	ExtractorName    string                       `json:"extractor_name"`
	ExtractorVersion int64                        `json:"extractor_version"`
	MetadataEventID  string                       `json:"metadata_event_id"`
	Fields           map[string]map[string]string `json:"fields"`
}

type MetadataSummaryProjection struct {
	ContentCount     int    `json:"content_count"`
	ExtractedCount   int    `json:"extracted_count"`
	FailedCount      int    `json:"failed_count"`
	MissingCount     int    `json:"missing_count"`
	ExtractorName    string `json:"extractor_name"`
	ExtractorVersion int    `json:"extractor_version"`
}

type MetadataPhotoProjection struct {
	StoredObjectID string `json:"stored_object_id"`
	ContentRef     string `json:"content_ref"`
	Filename       string `json:"filename"`
	RelativePath   string `json:"relative_path"`
	Status         string `json:"status"`
	ErrorMessage   string `json:"error_message,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	PixelCount     int    `json:"pixel_count,omitempty"`
	ViewURL        string `json:"view_url"`
	ThumbnailURL   string `json:"thumbnail_url"`
}

type DatedPhotoResponse struct {
	BucketKey string                 `json:"bucket_key"`
	Photos    []DatedPhotoProjection `json:"photos"`
}

func (s *Store) Summary() (StoreSummary, error) {
	summary := StoreSummary{StorePath: s.Root}
	queries := []struct {
		sql string
		dst *int
	}{
		{`select count(*) from events_applied`, &summary.EventCount},
		{`select count(*) from source_roots`, &summary.SourceRootCount},
		{`select count(*) from historical_inventories`, &summary.HistoricalInventoryCount},
		{`select count(*) from scans`, &summary.ScanCount},
		{`select count(*) from content_addresses`, &summary.ContentCount},
	}
	for _, q := range queries {
		if err := s.DB.QueryRow(q.sql).Scan(q.dst); err != nil {
			return summary, err
		}
	}
	rows, err := s.DB.Query(`
		select scl.content_ref
		from source_content_links scl
		where scl.cas_existed_at_ingest = 1
			and not exists (
				select 1 from duplicate_deduplications dd
				where dd.source_occurrence_id = scl.source_occurrence_id
					and dd.strategy_name = ?
					and dd.strategy_version = ?
			)`, dedupStrategyName, dedupStrategyVersion)
	if err != nil {
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return summary, err
		}
		_, _, size := parseContentRef(ref)
		summary.RetainedDuplicateBytes += size
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}
	var completed sql.NullInt64
	err = s.DB.QueryRow(`select max(completed_at_ms) from scans where completed_at_ms is not null`).Scan(&completed)
	if err != nil {
		return summary, err
	}
	if completed.Valid {
		summary.LastScanCompletedAtMS = &completed.Int64
	}
	return summary, nil
}

func (s *Store) SourceRoots() ([]SourceRoot, error) {
	return s.sourceRoots()
}

func (s *Store) HistoricalInventories() ([]HistoricalInventoryProjection, error) {
	rows, err := s.DB.Query(`select historical_inventory_id, stored_object_id, original_path, label, group_name from historical_inventories order by original_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoricalInventoryProjection
	for rows.Next() {
		var inv HistoricalInventoryProjection
		if err := rows.Scan(&inv.ID, &inv.StoredObjectID, &inv.OriginalPath, &inv.Label, &inv.Group); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *Store) Scans(limit int) ([]ScanProjection, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.DB.Query(`select scan_id, status, started_at_ms, completed_at_ms, coalesce(stats_json, '{}') from scans order by coalesce(completed_at_ms, started_at_ms, 0) desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScanProjection
	for rows.Next() {
		var scan ScanProjection
		var started sql.NullInt64
		var completed sql.NullInt64
		var stats string
		if err := rows.Scan(&scan.ScanID, &scan.Status, &started, &completed, &stats); err != nil {
			return nil, err
		}
		scan.Stats = json.RawMessage(stats)
		if started.Valid {
			scan.StartedAtMS = &started.Int64
		}
		if completed.Valid {
			scan.CompletedAtMS = &completed.Int64
		}
		if report, err := s.scanReportView(scan.ScanID); err == nil {
			scan.Report = report
		}
		out = append(out, scan)
	}
	return out, rows.Err()
}

func (s *Store) scanReportView(scanID string) (*ScanReportView, error) {
	b, err := os.ReadFile(filepath.Join(s.Root, "reports", "scan-"+scanID+".json"))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	view := &ScanReportView{ScanID: scanID}
	setString(raw, "scan_id", &view.ScanID)
	view.SourceRootsScanned = intPtr(raw, "source_roots_scanned")
	view.DirectoriesSeen = intPtr(raw, "directories_seen")
	view.RegularFilesSeen = intPtr(raw, "regular_files_seen")
	view.CandidateFilesSeen = intPtr(raw, "candidate_files_seen")
	view.SourceFilesAcquired = intPtr(raw, "source_files_acquired")
	view.SourceFileAcquireFailures = intPtr(raw, "source_file_acquire_failures")
	view.ContentAddressesMaterialized = intPtr(raw, "content_addresses_materialized")
	view.DuplicateAcquisitions = intPtr(raw, "duplicate_acquisitions")
	view.DuplicateGarbageBytes = int64Ptr(raw, "duplicate_garbage_bytes")
	view.NonCandidateFilesSkipped = intPtr(raw, "non_candidate_files_skipped")
	view.HistoricalJPEGEntriesLoaded = intPtr(raw, "historical_jpeg_entries_loaded")
	view.HistoricalEntriesAlreadySeen = intPtr(raw, "historical_entries_already_seen")
	view.HistoricalEntriesResolved = intPtr(raw, "historical_entries_resolved")
	view.HistoricalEntriesUnresolved = intPtr(raw, "historical_entries_unresolved")
	return view, nil
}

func setString(raw map[string]json.RawMessage, key string, dst *string) {
	v, ok := raw[key]
	if !ok {
		return
	}
	_ = json.Unmarshal(v, dst)
}

func intPtr(raw map[string]json.RawMessage, key string) *int {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	var out int
	if err := json.Unmarshal(v, &out); err != nil {
		return nil
	}
	return &out
}

func int64Ptr(raw map[string]json.RawMessage, key string) *int64 {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	var out int64
	if err := json.Unmarshal(v, &out); err != nil {
		return nil
	}
	return &out
}

func (s *Store) RecentEvents(limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	f, err := os.Open(filepath.Join(s.Root, "events", "events.jsonl"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	ring := make([]string, 0, limit)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024), 1024*1024*16)
	for sc.Scan() {
		if len(ring) == limit {
			copy(ring, ring[1:])
			ring[len(ring)-1] = sc.Text()
		} else {
			ring = append(ring, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(ring))
	for _, line := range ring {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func (s *Store) AcquiredFiles(scanID string) ([]AcquiredFileProjection, error) {
	rows, err := s.DB.Query(`
		select so.source_occurrence_id,
			coalesce(so.stored_object_id, ''),
			so.source_kind,
			coalesce(so.source_root_id, ''),
			so.path,
			so.relative_path,
			so.scan_id,
			coalesce(scl.content_ref, '')
		from source_occurrences so
		left join source_content_links scl on scl.source_occurrence_id = so.source_occurrence_id
		where so.scan_id = ? and so.stored_object_id is not null
		order by so.relative_path, so.path`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AcquiredFileProjection
	for rows.Next() {
		var file AcquiredFileProjection
		if err := rows.Scan(&file.SourceOccurrenceID, &file.StoredObjectID, &file.SourceKind, &file.SourceRootID, &file.Path, &file.RelativePath, &file.ScanID, &file.ContentRef); err != nil {
			return nil, err
		}
		file.Filename = filepath.Base(file.RelativePath)
		if file.Filename == "." || file.Filename == string(filepath.Separator) || file.Filename == "" {
			file.Filename = filepath.Base(file.Path)
		}
		file.ViewURL = "/objects/" + file.StoredObjectID
		file.BytesURL = "/api/objects/" + file.StoredObjectID + "/bytes"
		file.ThumbnailURL = "/api/objects/" + file.StoredObjectID + "/thumbnail"
		out = append(out, file)
	}
	return out, rows.Err()
}

func (s *Store) PhotoYears() (PhotoDateBucketResponse, error) {
	buckets, err := s.photoDateBuckets("year", "")
	return PhotoDateBucketResponse{BucketKind: "year", Buckets: buckets}, err
}

func (s *Store) PhotoMonths(year string) (PhotoDateBucketResponse, error) {
	buckets, err := s.photoDateBuckets("month", year)
	return PhotoDateBucketResponse{BucketKind: "month", BucketKey: year, Buckets: buckets}, err
}

func (s *Store) PhotoDays(year, month string) (PhotoDateBucketResponse, error) {
	key := year + "-" + month
	buckets, err := s.photoDateBuckets("day", key)
	return PhotoDateBucketResponse{BucketKind: "day", BucketKey: key, Buckets: buckets}, err
}

func (s *Store) DatedPhotos(year, month, day string) (DatedPhotoResponse, error) {
	key := year + "-" + month + "-" + day
	photos, err := s.photosForCaptureDate(key)
	return DatedPhotoResponse{BucketKey: key, Photos: photos}, err
}

func (s *Store) UndatedPhotos() (DatedPhotoResponse, error) {
	rows, err := s.DB.Query(`
		select scl.content_ref,
			min(scl.stored_object_id),
			min(coalesce(so.relative_path, '')),
			min(coalesce(so.path, ''))
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		left join source_occurrences so on so.source_occurrence_id = scl.source_occurrence_id
		where st.purpose = 'source_media'
			and not exists (select 1 from photo_capture_times pct where pct.content_ref = scl.content_ref)
		group by scl.content_ref
		order by min(coalesce(so.relative_path, so.path))`)
	if err != nil {
		return DatedPhotoResponse{}, err
	}
	defer rows.Close()
	var photos []DatedPhotoProjection
	for rows.Next() {
		var photo DatedPhotoProjection
		var path string
		if err := rows.Scan(&photo.ContentRef, &photo.StoredObjectID, &photo.RelativePath, &path); err != nil {
			return DatedPhotoResponse{}, err
		}
		photo.Filename = filenameForProjection(photo.RelativePath, path)
		photo.ViewURL = "/objects/" + photo.StoredObjectID
		photo.BytesURL = "/api/objects/" + photo.StoredObjectID + "/bytes"
		photo.ThumbnailURL = "/api/objects/" + photo.StoredObjectID + "/thumbnail"
		photos = append(photos, photo)
	}
	return DatedPhotoResponse{BucketKey: "undated", Photos: photos}, rows.Err()
}

func (s *Store) photoDateBuckets(kind, prefix string) ([]PhotoDateBucket, error) {
	var expr string
	var where string
	var args []any
	switch kind {
	case "year":
		expr = `substr(capture_date, 1, 4)`
	case "month":
		expr = `substr(capture_date, 1, 7)`
		where = `where substr(capture_date, 1, 4) = ?`
		args = append(args, prefix)
	case "day":
		expr = `capture_date`
		where = `where substr(capture_date, 1, 7) = ?`
		args = append(args, prefix)
	default:
		return nil, fmt.Errorf("unsupported photo date bucket kind %q", kind)
	}
	rows, err := s.DB.Query(fmt.Sprintf(`select %s as bucket_key, count(*) from photo_capture_times %s group by bucket_key order by bucket_key desc`, expr, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var buckets []PhotoDateBucket
	for rows.Next() {
		var bucket PhotoDateBucket
		if err := rows.Scan(&bucket.BucketKey, &bucket.PhotoCount); err != nil {
			return nil, err
		}
		bucket.DisplayLabel = photoBucketLabel(kind, bucket.BucketKey)
		buckets = append(buckets, bucket)
	}
	return buckets, rows.Err()
}

func (s *Store) photosForCaptureDate(captureDate string) ([]DatedPhotoProjection, error) {
	rows, err := s.DB.Query(`
		select stored_object_id, content_ref, filename, relative_path, capture_date, capture_time_local, utc_offset, precision
		from photo_capture_times
		where capture_date = ?
		order by capture_time_local, filename`, captureDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []DatedPhotoProjection
	for rows.Next() {
		var photo DatedPhotoProjection
		if err := rows.Scan(&photo.StoredObjectID, &photo.ContentRef, &photo.Filename, &photo.RelativePath, &photo.CaptureDate, &photo.CaptureTimeLocal, &photo.UTCOffset, &photo.Precision); err != nil {
			return nil, err
		}
		photo.ViewURL = "/objects/" + photo.StoredObjectID
		photo.BytesURL = "/api/objects/" + photo.StoredObjectID + "/bytes"
		photo.ThumbnailURL = "/api/objects/" + photo.StoredObjectID + "/thumbnail"
		photos = append(photos, photo)
	}
	return photos, rows.Err()
}

func photoBucketLabel(kind, key string) string {
	switch kind {
	case "year":
		return key
	case "month":
		if len(key) == 7 {
			return key[:4] + "-" + key[5:7]
		}
	case "day":
		return key
	}
	return key
}

func filenameForProjection(relativePath, path string) string {
	filename := filepath.Base(relativePath)
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		filename = filepath.Base(path)
	}
	return filename
}

func (s *Store) ObjectMetadata(storedObjectID string) (ObjectMetadataProjection, error) {
	var contentRef string
	err := s.DB.QueryRow(`select content_ref from source_content_links where stored_object_id = ? order by source_occurrence_id limit 1`, storedObjectID).Scan(&contentRef)
	if err != nil {
		return ObjectMetadataProjection{}, err
	}
	var out ObjectMetadataProjection
	var fieldsJSON string
	out.StoredObjectID = storedObjectID
	err = s.DB.QueryRow(`
		select content_ref, extractor_name, extractor_version, metadata_event_id, fields_json
		from content_metadata
		where content_ref = ? and extractor_name = ? and extractor_version = ?`,
		contentRef, metadataExtractorName, metadataExtractorVersion,
	).Scan(&out.ContentRef, &out.ExtractorName, &out.ExtractorVersion, &out.MetadataEventID, &fieldsJSON)
	if err != nil {
		return ObjectMetadataProjection{}, err
	}
	if err := json.Unmarshal([]byte(fieldsJSON), &out.Fields); err != nil {
		return ObjectMetadataProjection{}, err
	}
	return out, nil
}

func (s *Store) MetadataSummary() (MetadataSummaryProjection, error) {
	summary := MetadataSummaryProjection{
		ExtractorName:    metadataExtractorName,
		ExtractorVersion: metadataExtractorVersion,
	}
	if err := s.DB.QueryRow(`
		select count(distinct scl.content_ref)
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		where st.purpose = 'source_media'`).Scan(&summary.ContentCount); err != nil {
		return summary, err
	}
	if err := s.DB.QueryRow(`select count(*) from content_metadata where extractor_name = ? and extractor_version = ?`, metadataExtractorName, metadataExtractorVersion).Scan(&summary.ExtractedCount); err != nil {
		return summary, err
	}
	if err := s.DB.QueryRow(`select count(*) from content_metadata_failures where extractor_name = ? and extractor_version = ?`, metadataExtractorName, metadataExtractorVersion).Scan(&summary.FailedCount); err != nil {
		return summary, err
	}
	if err := s.DB.QueryRow(`
		select count(*)
		from (
			select distinct scl.content_ref
			from source_content_links scl
			join stored_objects st on st.stored_object_id = scl.stored_object_id
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
		)`, metadataExtractorName, metadataExtractorVersion, metadataExtractorName, metadataExtractorVersion).Scan(&summary.MissingCount); err != nil {
		return summary, err
	}
	return summary, nil
}

func (s *Store) MetadataFailures() ([]MetadataPhotoProjection, error) {
	rows, err := s.DB.Query(`
		select cmf.stored_object_id,
			cmf.content_ref,
			coalesce(so.relative_path, ''),
			coalesce(so.path, ''),
			cmf.error_json,
			st.acquired_storage_key
		from content_metadata_failures cmf
		join stored_objects st on st.stored_object_id = cmf.stored_object_id
		left join source_occurrences so on so.source_occurrence_id = cmf.source_occurrence_id
		where cmf.extractor_name = ? and cmf.extractor_version = ?
		order by coalesce(so.relative_path, so.path), cmf.stored_object_id`, metadataExtractorName, metadataExtractorVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []MetadataPhotoProjection
	for rows.Next() {
		var photo MetadataPhotoProjection
		var path string
		var errorJSON string
		var storageKey string
		if err := rows.Scan(&photo.StoredObjectID, &photo.ContentRef, &photo.RelativePath, &path, &errorJSON, &storageKey); err != nil {
			return nil, err
		}
		photo.Filename = filenameForProjection(photo.RelativePath, path)
		photo.Status = "failed"
		photo.ErrorMessage = errorMessageFromJSON(errorJSON)
		photo.Width, photo.Height = s.dimensionsForStorageKey(storageKey)
		photo.PixelCount = photo.Width * photo.Height
		photo.ViewURL = "/objects/" + photo.StoredObjectID
		photo.ThumbnailURL = "/api/objects/" + photo.StoredObjectID + "/thumbnail"
		photos = append(photos, photo)
	}
	return photos, rows.Err()
}

func (s *Store) MetadataMissing() ([]MetadataPhotoProjection, error) {
	rows, err := s.DB.Query(`
		select scl.content_ref,
			min(scl.stored_object_id),
			min(coalesce(so.relative_path, '')),
			min(coalesce(so.path, ''))
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		left join source_occurrences so on so.source_occurrence_id = scl.source_occurrence_id
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
		order by min(coalesce(so.relative_path, so.path))`, metadataExtractorName, metadataExtractorVersion, metadataExtractorName, metadataExtractorVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []MetadataPhotoProjection
	for rows.Next() {
		var photo MetadataPhotoProjection
		var path string
		if err := rows.Scan(&photo.ContentRef, &photo.StoredObjectID, &photo.RelativePath, &path); err != nil {
			return nil, err
		}
		photo.Filename = filenameForProjection(photo.RelativePath, path)
		photo.Status = "missing"
		photo.ViewURL = "/objects/" + photo.StoredObjectID
		photo.ThumbnailURL = "/api/objects/" + photo.StoredObjectID + "/thumbnail"
		photos = append(photos, photo)
	}
	return photos, rows.Err()
}

func errorMessageFromJSON(raw string) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw
	}
	return payload.Message
}

func (s *Store) dimensionsForStorageKey(storageKey string) (int, int) {
	path := filepath.Join(s.Root, filepath.FromSlash(storageKey))
	width, height, err := jpegDimensions(path)
	if err != nil {
		return 0, 0
	}
	return width, height
}

type StoredObjectFile struct {
	Path         string
	OriginalPath string
	ContentRef   string
}

func (s *Store) StoredObjectFile(storedObjectID string) (StoredObjectFile, error) {
	var key string
	var originalPath string
	err := s.DB.QueryRow(`select acquired_storage_key, original_path from stored_objects where stored_object_id = ?`, storedObjectID).Scan(&key, &originalPath)
	if err != nil {
		return StoredObjectFile{}, err
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return StoredObjectFile{}, errors.New("stored object path escapes store root")
	}
	path := filepath.Join(s.Root, clean)
	rel, err := filepath.Rel(s.Root, path)
	if err != nil {
		return StoredObjectFile{}, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return StoredObjectFile{}, errors.New("stored object path escapes store root")
	}
	contentRef, _ := s.contentRefForStoredObject(storedObjectID)
	return StoredObjectFile{Path: path, OriginalPath: originalPath, ContentRef: contentRef}, nil
}

func (s *Store) CanonicalObjectFile(storedObjectID string) (StoredObjectFile, error) {
	file, err := s.StoredObjectFile(storedObjectID)
	if err != nil {
		return StoredObjectFile{}, err
	}
	if file.ContentRef == "" {
		return StoredObjectFile{}, sql.ErrNoRows
	}
	return StoredObjectFile{
		Path:         filepath.Join(s.Root, filepath.FromSlash(casKey(file.ContentRef))),
		OriginalPath: file.OriginalPath,
		ContentRef:   file.ContentRef,
	}, nil
}

func (s *Store) contentRefForStoredObject(storedObjectID string) (string, error) {
	var contentRef string
	err := s.DB.QueryRow(`select content_ref from source_content_links where stored_object_id = ? order by source_occurrence_id limit 1`, storedObjectID).Scan(&contentRef)
	return contentRef, err
}
