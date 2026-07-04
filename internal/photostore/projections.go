package photostore

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	ThumbnailGarbageBytes    int64  `json:"thumbnail_garbage_bytes"`
	ThumbnailGarbageFiles    int    `json:"thumbnail_garbage_files"`
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

type ObjectNavigationProjection struct {
	List     string                `json:"list"`
	Label    string                `json:"label"`
	Current  ObjectNavigationItem  `json:"current"`
	Previous *ObjectNavigationItem `json:"previous"`
	Next     *ObjectNavigationItem `json:"next"`
}

type ObjectNavigationItem struct {
	StoredObjectID string `json:"stored_object_id"`
	Filename       string `json:"filename"`
	ViewURL        string `json:"view_url"`
}

type AssetProjection struct {
	AssetID                string   `json:"asset_id"`
	ContentRef             string   `json:"content_ref"`
	RepresentativeObjectID string   `json:"representative_stored_object_id"`
	Filename               string   `json:"filename"`
	Quality                string   `json:"quality"`
	Status                 string   `json:"status"`
	Visibility             string   `json:"visibility"`
	Labels                 []string `json:"labels"`
	CaptureDate            string   `json:"capture_date,omitempty"`
	CaptureTimeLocal       string   `json:"capture_time_local,omitempty"`
	Camera                 string   `json:"camera,omitempty"`
	ViewURL                string   `json:"view_url"`
	BytesURL               string   `json:"bytes_url"`
	ThumbnailURL           string   `json:"thumbnail_url"`
	SourceOccurrenceCount  int      `json:"source_occurrence_count"`
	CreatedAtMS            int64    `json:"created_at_ms"`
}

func (a AssetProjection) MarshalJSON() ([]byte, error) {
	type assetAlias AssetProjection
	if a.Labels == nil {
		a.Labels = []string{}
	}
	return json.Marshal(assetAlias(a))
}

type AssetPageProjection struct {
	Assets     []AssetProjection `json:"assets"`
	Total      int               `json:"total"`
	Limit      int               `json:"limit"`
	Offset     int               `json:"offset"`
	NextOffset *int              `json:"next_offset,omitempty"`
	PrevOffset *int              `json:"prev_offset,omitempty"`
}

func (p AssetPageProjection) MarshalJSON() ([]byte, error) {
	type pageAlias AssetPageProjection
	if p.Assets == nil {
		p.Assets = []AssetProjection{}
	}
	return json.Marshal(pageAlias(p))
}

type AssetDetailProjection struct {
	AssetProjection
	Sources []AssetSourceProjection `json:"sources"`
}

func (d AssetDetailProjection) MarshalJSON() ([]byte, error) {
	type assetAlias AssetProjection
	if d.Labels == nil {
		d.Labels = []string{}
	}
	if d.Sources == nil {
		d.Sources = []AssetSourceProjection{}
	}
	return json.Marshal(struct {
		assetAlias
		Sources []AssetSourceProjection `json:"sources"`
	}{
		assetAlias: assetAlias(d.AssetProjection),
		Sources:    d.Sources,
	})
}

type AssetSourceProjection struct {
	SourceOccurrenceID string `json:"source_occurrence_id"`
	StoredObjectID     string `json:"stored_object_id"`
	SourceKind         string `json:"source_kind"`
	SourceRootID       string `json:"source_root_id,omitempty"`
	Path               string `json:"path"`
	RelativePath       string `json:"relative_path"`
	ScanID             string `json:"scan_id"`
}

type AssetNavigationProjection struct {
	List     string                `json:"list"`
	Label    string                `json:"label"`
	Current  AssetNavigationItem   `json:"current"`
	Previous *AssetNavigationItem  `json:"previous"`
	Next     *AssetNavigationItem  `json:"next"`
	Window   []AssetNavigationItem `json:"window"`
}

type AssetNavigationItem struct {
	AssetID      string `json:"asset_id"`
	Filename     string `json:"filename"`
	Quality      string `json:"quality"`
	ViewURL      string `json:"view_url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
}

type LabelProjection struct {
	NormalizedLabel string `json:"normalized_label"`
	DisplayLabel    string `json:"display_label"`
	AssetCount      int    `json:"asset_count"`
	LastAppliedAtMS int64  `json:"last_applied_at_ms"`
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
	thumbnailGarbage, err := s.ThumbnailGarbageSummary()
	if err != nil {
		return summary, err
	}
	summary.ThumbnailGarbageBytes = thumbnailGarbage.Bytes
	summary.ThumbnailGarbageFiles = thumbnailGarbage.Files
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
		with representative_paths as (
			select scl.content_ref, min(so.relative_path) as relative_path
			from source_content_links scl
			join source_occurrences so on so.source_occurrence_id = scl.source_occurrence_id
			group by scl.content_ref
		),
		representatives as (
			select scl.content_ref, min(so.stored_object_id) as stored_object_id, rp.relative_path
			from representative_paths rp
			join source_content_links scl on scl.content_ref = rp.content_ref
			join source_occurrences so on so.source_occurrence_id = scl.source_occurrence_id
				and so.relative_path = rp.relative_path
			group by scl.content_ref, rp.relative_path
		)
		select rep.stored_object_id, pct.content_ref, rep.relative_path, pct.capture_date, pct.capture_time_local, pct.utc_offset, pct.precision
		from photo_capture_times pct
		join representatives rep on rep.content_ref = pct.content_ref
		where pct.capture_date = ?
		order by pct.capture_time_local, rep.relative_path`, captureDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []DatedPhotoProjection
	for rows.Next() {
		var photo DatedPhotoProjection
		if err := rows.Scan(&photo.StoredObjectID, &photo.ContentRef, &photo.RelativePath, &photo.CaptureDate, &photo.CaptureTimeLocal, &photo.UTCOffset, &photo.Precision); err != nil {
			return nil, err
		}
		photo.Filename = filepath.Base(photo.RelativePath)
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

func (s *Store) Assets(query url.Values) (AssetPageProjection, error) {
	parts := assetQueryParts(query)
	limit := boundedQueryInt(query.Get("limit"), 60, 1, 200)
	offset := boundedQueryInt(query.Get("offset"), 0, 0, 1_000_000_000)
	var total int
	if err := s.DB.QueryRow(fmt.Sprintf(`
		%s
		select count(*)
		%s
		where %s`, parts.WithSQL, assetFromSQL, parts.WhereSQL), parts.Args...).Scan(&total); err != nil {
		return AssetPageProjection{}, err
	}
	pageArgs := append(append([]any{}, parts.Args...), limit, offset)
	rows, err := s.DB.Query(fmt.Sprintf(`
		%s
		select a.asset_id,
			a.content_ref,
			a.representative_stored_object_id,
			coalesce(a.original_filename, ''),
			coalesce(aq.quality, 'Unrated'),
			coalesce(ast.status, 'Triage'),
			coalesce(av.visibility, 'Normal'),
			coalesce(pct.capture_date, ''),
			coalesce(pct.capture_time_local, ''),
			coalesce(a.created_at_ms, 0),
			(select count(*) from source_content_links scl where scl.content_ref = a.content_ref)
		%s
		where %s
		order by %s
		limit ? offset ?`, parts.WithSQL, assetFromSQL, parts.WhereSQL, parts.OrderSQL), pageArgs...)
	if err != nil {
		return AssetPageProjection{}, err
	}
	assets := []AssetProjection{}
	for rows.Next() {
		var asset AssetProjection
		if err := rows.Scan(&asset.AssetID, &asset.ContentRef, &asset.RepresentativeObjectID, &asset.Filename, &asset.Quality, &asset.Status, &asset.Visibility, &asset.CaptureDate, &asset.CaptureTimeLocal, &asset.CreatedAtMS, &asset.SourceOccurrenceCount); err != nil {
			rows.Close()
			return AssetPageProjection{}, err
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AssetPageProjection{}, err
	}
	if err := rows.Close(); err != nil {
		return AssetPageProjection{}, err
	}
	if err := s.hydrateAssets(&assets); err != nil {
		return AssetPageProjection{}, err
	}
	page := AssetPageProjection{Assets: assets, Total: total, Limit: limit, Offset: offset}
	if offset+limit < total {
		next := offset + limit
		page.NextOffset = &next
	}
	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		page.PrevOffset = &prev
	}
	return page, nil
}

const assetFromSQL = `
	from assets a
	left join asset_quality aq on aq.asset_id = a.asset_id
	left join asset_status ast on ast.asset_id = a.asset_id
	left join asset_visibility av on av.asset_id = a.asset_id
	left join capture pct on pct.content_ref = a.content_ref
	left join content_metadata cm on cm.content_ref = a.content_ref
		and cm.extractor_name = ?
		and cm.extractor_version = ?`

type assetQuery struct {
	WithSQL  string
	WhereSQL string
	OrderSQL string
	Args     []any
	Label    string
}

func assetQueryParts(query url.Values) assetQuery {
	where := []string{"1 = 1"}
	args := []any{metadataExtractorName, metadataExtractorVersion}
	if assetID := query.Get("asset_id"); assetID != "" {
		where = append(where, "a.asset_id = ?")
		args = append(args, assetID)
	}
	if qualities := nonEmptyValues(query["quality"]); len(qualities) > 0 {
		clause, clauseArgs := inStringClause("coalesce(aq.quality, 'Unrated')", qualities)
		where = append(where, clause)
		args = append(args, clauseArgs...)
	}
	if statuses := nonEmptyValues(query["status"]); len(statuses) > 0 {
		clause, clauseArgs := inStringClause("coalesce(ast.status, 'Triage')", statuses)
		where = append(where, clause)
		args = append(args, clauseArgs...)
	}
	if visibilities := nonEmptyValues(query["visibility"]); len(visibilities) > 0 {
		clause, clauseArgs := inStringClause("coalesce(av.visibility, 'Normal')", visibilities)
		where = append(where, clause)
		args = append(args, clauseArgs...)
	}
	if labels := normalizedQueryLabels(query["label"]); len(labels) > 0 {
		clause, clauseArgs := inStringClause("filter_label.normalized_label", labels)
		where = append(where, `exists (
			select 1 from asset_labels filter_label
			where filter_label.asset_id = a.asset_id and `+clause+`
		)`)
		args = append(args, clauseArgs...)
	}
	if truthyQuery(query.Get("has_date")) {
		where = append(where, "pct.capture_date is not null and pct.capture_date != ''")
	}
	if truthyQuery(query.Get("min_megapixels")) {
		where = append(where, `cast(json_extract(cm.fields_json, '$.pixel_x_dimension.raw') as integer) *
			cast(json_extract(cm.fields_json, '$.pixel_y_dimension.raw') as integer) >= ?`)
		args = append(args, 1_000_000)
	}
	order := "coalesce(pct.capture_time_local, ''), a.original_filename, a.asset_id"
	label := "Assets by date ascending"
	switch query.Get("sort") {
	case "date_desc":
		order = "coalesce(pct.capture_time_local, '') desc, a.original_filename, a.asset_id"
		label = "Assets by date descending"
	}
	return assetQuery{
		WithSQL: `with capture as (
			select content_ref, min(capture_date) as capture_date, min(capture_time_local) as capture_time_local
			from photo_capture_times
			group by content_ref
		)`,
		WhereSQL: strings.Join(where, " and "),
		OrderSQL: order,
		Args:     args,
		Label:    label,
	}
}

func nonEmptyValues(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizedQueryLabels(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := normalizeAssetLabel(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func inStringClause(expr string, values []string) (string, []any) {
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		args[i] = value
	}
	return fmt.Sprintf("%s in (%s)", expr, strings.Join(placeholders, ",")), args
}

func truthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Store) Asset(assetID string) (AssetDetailProjection, error) {
	page, err := s.Assets(url.Values{"asset_id": []string{assetID}, "limit": []string{"1"}})
	if err != nil {
		return AssetDetailProjection{}, err
	}
	if len(page.Assets) == 0 {
		return AssetDetailProjection{}, sql.ErrNoRows
	}
	sources, err := s.AssetSources(assetID)
	if err != nil {
		return AssetDetailProjection{}, err
	}
	return AssetDetailProjection{AssetProjection: page.Assets[0], Sources: sources}, nil
}

func (s *Store) AssetNavigation(assetID string, query url.Values) (AssetNavigationProjection, error) {
	nextQuery := url.Values{}
	for key, values := range query {
		if key == "limit" || key == "offset" {
			continue
		}
		for _, value := range values {
			nextQuery.Add(key, value)
		}
	}
	parts := assetQueryParts(nextQuery)
	rows, err := s.DB.Query(fmt.Sprintf(`
		%s
		select a.asset_id,
			coalesce(a.original_filename, ''),
			coalesce(aq.quality, 'Unrated'),
			a.representative_stored_object_id,
			coalesce(cast(json_extract(cm.fields_json, '$.pixel_x_dimension.raw') as integer), 0),
			coalesce(cast(json_extract(cm.fields_json, '$.pixel_y_dimension.raw') as integer), 0)
		%s
		where %s
		order by %s`, parts.WithSQL, assetFromSQL, parts.WhereSQL, parts.OrderSQL), parts.Args...)
	if err != nil {
		return AssetNavigationProjection{}, err
	}
	defer rows.Close()
	items := []AssetNavigationItem{}
	for rows.Next() {
		var item AssetNavigationItem
		var representativeObjectID string
		if err := rows.Scan(&item.AssetID, &item.Filename, &item.Quality, &representativeObjectID, &item.Width, &item.Height); err != nil {
			return AssetNavigationProjection{}, err
		}
		item.ThumbnailURL = "/api/objects/" + representativeObjectID + "/thumbnail"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AssetNavigationProjection{}, err
	}
	return assetNavigationFromItems(assetID, parts.Label, nextQuery, items)
}

func assetNavigationFromItems(assetID, label string, query url.Values, items []AssetNavigationItem) (AssetNavigationProjection, error) {
	currentIndex := -1
	for i := range items {
		items[i].ViewURL = assetViewURLWithContext(items[i].AssetID, query)
		if items[i].AssetID == assetID {
			currentIndex = i
		}
	}
	if currentIndex < 0 {
		return AssetNavigationProjection{}, sql.ErrNoRows
	}
	out := AssetNavigationProjection{
		List:    "assets",
		Label:   label,
		Current: items[currentIndex],
	}
	windowStart := currentIndex - 3
	if windowStart < 0 {
		windowStart = 0
	}
	windowEnd := windowStart + 7
	if windowEnd > len(items) {
		windowEnd = len(items)
		windowStart = windowEnd - 7
		if windowStart < 0 {
			windowStart = 0
		}
	}
	out.Window = append(out.Window, items[windowStart:windowEnd]...)
	if currentIndex > 0 {
		prev := items[currentIndex-1]
		out.Previous = &prev
	}
	if currentIndex+1 < len(items) {
		next := items[currentIndex+1]
		out.Next = &next
	}
	return out, nil
}

func assetViewURLWithContext(assetID string, query url.Values) string {
	next := url.Values{}
	for key, values := range query {
		for _, value := range values {
			next.Add(key, value)
		}
	}
	encoded := next.Encode()
	if encoded == "" {
		return "/assets/" + assetID
	}
	return "/assets/" + assetID + "?" + encoded
}

func (s *Store) AssetSources(assetID string) ([]AssetSourceProjection, error) {
	var contentRef string
	if err := s.DB.QueryRow(`select content_ref from assets where asset_id = ?`, assetID).Scan(&contentRef); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(`
		select so.source_occurrence_id,
			coalesce(so.stored_object_id, ''),
			so.source_kind,
			coalesce(so.source_root_id, ''),
			so.path,
			so.relative_path,
			so.scan_id
		from source_content_links scl
		join source_occurrences so on so.source_occurrence_id = scl.source_occurrence_id
		where scl.content_ref = ?
		order by so.relative_path, so.path`, contentRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []AssetSourceProjection{}
	for rows.Next() {
		var source AssetSourceProjection
		if err := rows.Scan(&source.SourceOccurrenceID, &source.StoredObjectID, &source.SourceKind, &source.SourceRootID, &source.Path, &source.RelativePath, &source.ScanID); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (s *Store) Labels() ([]LabelProjection, error) {
	rows, err := s.DB.Query(`select normalized_label, display_label, asset_count, last_applied_at_ms from label_catalog order by display_label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []LabelProjection{}
	for rows.Next() {
		var label LabelProjection
		if err := rows.Scan(&label.NormalizedLabel, &label.DisplayLabel, &label.AssetCount, &label.LastAppliedAtMS); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func boundedQueryInt(raw string, fallback, min, max int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (s *Store) hydrateAssets(assets *[]AssetProjection) error {
	if len(*assets) == 0 {
		return nil
	}
	assetIDs := make([]string, 0, len(*assets))
	contentRefs := make([]string, 0, len(*assets))
	assetIndex := map[string]int{}
	contentRefSet := map[string]struct{}{}
	for i := range *assets {
		asset := &(*assets)[i]
		asset.ViewURL = "/objects/" + asset.RepresentativeObjectID
		asset.BytesURL = "/api/objects/" + asset.RepresentativeObjectID + "/bytes"
		asset.ThumbnailURL = "/api/objects/" + asset.RepresentativeObjectID + "/thumbnail"
		asset.Labels = []string{}
		assetIDs = append(assetIDs, asset.AssetID)
		assetIndex[asset.AssetID] = i
		if _, ok := contentRefSet[asset.ContentRef]; !ok {
			contentRefs = append(contentRefs, asset.ContentRef)
			contentRefSet[asset.ContentRef] = struct{}{}
		}
	}
	if err := s.hydrateAssetLabels(assets, assetIDs, assetIndex); err != nil {
		return err
	}
	return s.hydrateAssetCameras(assets, contentRefs)
}

func (s *Store) hydrateAssetLabels(assets *[]AssetProjection, assetIDs []string, assetIndex map[string]int) error {
	query, args := inQuery(`select asset_id, display_label from asset_labels where asset_id in (%s) order by display_label`, assetIDs)
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID, label string
		if err := rows.Scan(&assetID, &label); err != nil {
			return err
		}
		if index, ok := assetIndex[assetID]; ok {
			(*assets)[index].Labels = append((*assets)[index].Labels, label)
		}
	}
	return rows.Err()
}

func (s *Store) hydrateAssetCameras(assets *[]AssetProjection, contentRefs []string) error {
	query, args := inQuery(`
		select content_ref, fields_json
		from content_metadata
		where extractor_name = ? and extractor_version = ? and content_ref in (%s)`, contentRefs)
	args = append([]any{metadataExtractorName, metadataExtractorVersion}, args...)
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	cameras := map[string]string{}
	for rows.Next() {
		var contentRef, fieldsJSON string
		if err := rows.Scan(&contentRef, &fieldsJSON); err != nil {
			return err
		}
		var fields map[string]map[string]string
		if err := json.Unmarshal([]byte(fieldsJSON), &fields); err != nil {
			continue
		}
		make := metadataRaw(fields, "make")
		model := metadataRaw(fields, "model")
		cameras[contentRef] = strings.TrimSpace(strings.TrimSpace(make + " " + model))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range *assets {
		(*assets)[i].Camera = cameras[(*assets)[i].ContentRef]
	}
	return nil
}

func inQuery(format string, values []string) (string, []any) {
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		args[i] = value
	}
	return fmt.Sprintf(format, strings.Join(placeholders, ",")), args
}

func metadataRaw(fields map[string]map[string]string, key string) string {
	field, ok := fields[key]
	if !ok {
		return ""
	}
	return field["raw"]
}

func (s *Store) ObjectNavigation(storedObjectID string, query url.Values) (ObjectNavigationProjection, error) {
	list := query.Get("list")
	var items []ObjectNavigationItem
	var label string
	switch list {
	case "scan":
		scanID := query.Get("scan_id")
		if scanID == "" {
			return ObjectNavigationProjection{}, errors.New("scan_id is required for scan navigation")
		}
		files, err := s.AcquiredFiles(scanID)
		if err != nil {
			return ObjectNavigationProjection{}, err
		}
		items = navigationItemsFromAcquired(files)
		label = "Scan " + scanID
	case "date":
		date := query.Get("date")
		if date == "" {
			return ObjectNavigationProjection{}, errors.New("date is required for date navigation")
		}
		photos, err := s.photosForCaptureDate(date)
		if err != nil {
			return ObjectNavigationProjection{}, err
		}
		items = navigationItemsFromDated(photos)
		label = date
	case "undated":
		resp, err := s.UndatedPhotos()
		if err != nil {
			return ObjectNavigationProjection{}, err
		}
		items = navigationItemsFromDated(resp.Photos)
		label = "Undated photos"
	case "metadata_failed":
		photos, err := s.MetadataFailures()
		if err != nil {
			return ObjectNavigationProjection{}, err
		}
		items = navigationItemsFromMetadata(photos)
		label = "No metadata found"
	case "metadata_missing":
		photos, err := s.MetadataMissing()
		if err != nil {
			return ObjectNavigationProjection{}, err
		}
		items = navigationItemsFromMetadata(photos)
		label = "Not scanned by current extractor"
	default:
		return ObjectNavigationProjection{}, errors.New("unsupported or missing navigation list")
	}
	return objectNavigationFromItems(storedObjectID, list, label, query, items)
}

func navigationItemsFromAcquired(files []AcquiredFileProjection) []ObjectNavigationItem {
	items := make([]ObjectNavigationItem, 0, len(files))
	for _, file := range files {
		items = append(items, ObjectNavigationItem{StoredObjectID: file.StoredObjectID, Filename: file.Filename})
	}
	return items
}

func navigationItemsFromDated(photos []DatedPhotoProjection) []ObjectNavigationItem {
	items := make([]ObjectNavigationItem, 0, len(photos))
	for _, photo := range photos {
		items = append(items, ObjectNavigationItem{StoredObjectID: photo.StoredObjectID, Filename: photo.Filename})
	}
	return items
}

func navigationItemsFromMetadata(photos []MetadataPhotoProjection) []ObjectNavigationItem {
	items := make([]ObjectNavigationItem, 0, len(photos))
	for _, photo := range photos {
		items = append(items, ObjectNavigationItem{StoredObjectID: photo.StoredObjectID, Filename: photo.Filename})
	}
	return items
}

func objectNavigationFromItems(storedObjectID, list, label string, query url.Values, items []ObjectNavigationItem) (ObjectNavigationProjection, error) {
	currentIndex := -1
	for i := range items {
		items[i].ViewURL = objectViewURLWithContext(items[i].StoredObjectID, query)
		if items[i].StoredObjectID == storedObjectID {
			currentIndex = i
		}
	}
	if currentIndex < 0 {
		return ObjectNavigationProjection{}, sql.ErrNoRows
	}
	out := ObjectNavigationProjection{
		List:    list,
		Label:   label,
		Current: items[currentIndex],
	}
	if currentIndex > 0 {
		prev := items[currentIndex-1]
		out.Previous = &prev
	}
	if currentIndex+1 < len(items) {
		next := items[currentIndex+1]
		out.Next = &next
	}
	return out, nil
}

func objectViewURLWithContext(storedObjectID string, query url.Values) string {
	next := url.Values{}
	for key, values := range query {
		for _, value := range values {
			next.Add(key, value)
		}
	}
	encoded := next.Encode()
	if encoded == "" {
		return "/objects/" + storedObjectID
	}
	return "/objects/" + storedObjectID + "?" + encoded
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
