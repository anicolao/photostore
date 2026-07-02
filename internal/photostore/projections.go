package photostore

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
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
	ThumbnailURL       string `json:"thumbnail_url"`
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
	rows, err := s.DB.Query(`select content_ref from source_content_links where cas_existed_at_ingest = 1 and acquired_object_retained = 1`)
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
		file.ViewURL = "/api/objects/" + file.StoredObjectID + "/bytes"
		file.ThumbnailURL = "/api/objects/" + file.StoredObjectID + "/thumbnail"
		out = append(out, file)
	}
	return out, rows.Err()
}

type StoredObjectFile struct {
	Path         string
	OriginalPath string
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
	return StoredObjectFile{Path: path, OriginalPath: originalPath}, nil
}
