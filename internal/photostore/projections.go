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
	Report        *ScanReport     `json:"report,omitempty"`
}

type HistoricalInventoryProjection struct {
	ID             string `json:"historical_inventory_id"`
	StoredObjectID string `json:"stored_object_id"`
	OriginalPath   string `json:"original_path"`
	Label          string `json:"label"`
	Group          string `json:"group"`
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
		if report, err := s.Report(scan.ScanID); err == nil {
			scan.Report = report
		}
		out = append(out, scan)
	}
	return out, rows.Err()
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
