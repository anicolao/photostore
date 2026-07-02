package photostore

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const schemaVersion = 1

var deterministicIDCounter atomic.Uint64

type Store struct {
	Root string
	DB   *sql.DB
}

type Event struct {
	EventID       string         `json:"event_id"`
	EventType     string         `json:"event_type"`
	SchemaVersion int            `json:"schema_version"`
	RecordedAtMS  int64          `json:"recorded_at_ms"`
	Actor         map[string]any `json:"actor"`
	CausationID   *string        `json:"causation_id"`
	CorrelationID *string        `json:"correlation_id"`
	Payload       map[string]any `json:"payload"`
}

type SourceRoot struct {
	ID                    string  `json:"source_root_id"`
	Path                  string  `json:"path"`
	Label                 string  `json:"label"`
	LastScanID            *string `json:"last_scan_id"`
	LastScanCompletedAtMS *int64  `json:"last_scan_completed_at_ms"`
}

type ScanReport struct {
	ScanID                       string `json:"scan_id"`
	SourceRootsScanned           int    `json:"source_roots_scanned"`
	DirectoriesSeen              int    `json:"directories_seen"`
	RegularFilesSeen             int    `json:"regular_files_seen"`
	CandidateFilesSeen           int    `json:"candidate_files_seen"`
	SourceFilesAcquired          int    `json:"source_files_acquired"`
	SourceFileAcquireFailures    int    `json:"source_file_acquire_failures"`
	ContentAddressesMaterialized int    `json:"content_addresses_materialized"`
	DuplicateAcquisitions        int    `json:"duplicate_acquisitions"`
	DuplicateGarbageBytes        int64  `json:"duplicate_garbage_bytes"`
	NonCandidateFilesSkipped     int    `json:"non_candidate_files_skipped"`
	HistoricalJPEGEntriesLoaded  int    `json:"historical_jpeg_entries_loaded"`
	HistoricalEntriesAlreadySeen int    `json:"historical_entries_already_seen"`
	HistoricalEntriesResolved    int    `json:"historical_entries_resolved"`
	HistoricalEntriesUnresolved  int    `json:"historical_entries_unresolved"`
}

type ProgressFunc func(message string)

type inventoryEntry struct {
	ID             string
	SHA256         string
	Size           *int64
	HistoricalPath string
	ResolvedPath   string
	Extension      string
	RawLine        string
}

func Init(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{
		filepath.Join(abs, "events"),
		filepath.Join(abs, "objects", "acquired"),
		filepath.Join(abs, "cas", "sha256", "v1"),
		filepath.Join(abs, "thumbnails", "jpeg", "240", thumbnailRendererVersion),
		filepath.Join(abs, "tmp"),
		filepath.Join(abs, "reports"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
	}
	st, err := Open(abs)
	if err != nil {
		return nil, err
	}
	if err := st.appendEvent("StoreInitialized", nil, nil, map[string]any{
		"store_path":                abs,
		"event_log_path":            filepath.Join(abs, "events", "events.jsonl"),
		"acquired_object_root_path": filepath.Join(abs, "objects", "acquired"),
		"cas_root_path":             filepath.Join(abs, "cas"),
		"projection_path":           filepath.Join(abs, "projections.sqlite3"),
		"implementation": map[string]any{
			"name":           "photostore",
			"language":       "go",
			"mvp_plan":       "MVP_IMPLEMENTATION_PLAN.md",
			"schema_version": schemaVersion,
		},
	}); err != nil {
		_ = st.Close()
		return nil, err
	}
	return st, nil
}

func Open(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := requireInitialized(abs); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, "thumbnails", "jpeg", "240", thumbnailRendererVersion), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(abs, "projections.sqlite3"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	st := &Store{Root: abs, DB: db}
	if err := st.configureSQLite(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := st.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) configureSQLite() error {
	pragmas := []string{
		`pragma busy_timeout = 10000`,
		`pragma journal_mode = wal`,
		`pragma synchronous = normal`,
	}
	for _, pragma := range pragmas {
		if _, err := s.DB.Exec(pragma); err != nil {
			return err
		}
	}
	return nil
}

func requireInitialized(root string) error {
	required := []string{
		filepath.Join(root, "events"),
		filepath.Join(root, "objects", "acquired"),
		filepath.Join(root, "cas", "sha256", "v1"),
		filepath.Join(root, "tmp"),
		filepath.Join(root, "reports"),
	}
	for _, path := range required {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("store is not initialized at %s; run `photostore init` to initialize the default store, or pass `--store PATH` to use a different initialized store", root)
		}
		if !info.IsDir() {
			return fmt.Errorf("store is not initialized at %s; %s is not a directory", root, path)
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func (s *Store) AddSourceRoot(path, label string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if label == "" {
		label = filepath.Base(abs)
	}
	var existing string
	err = s.DB.QueryRow(`select source_root_id from source_roots where root_path = ?`, abs).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id := newID("src")
	return id, s.appendEvent("SourceRootRegistered", nil, nil, map[string]any{
		"source_root_id": id,
		"label":          label,
		"root_path":      abs,
		"source_type":    "local_directory",
		"scan_policy": map[string]any{
			"recursive":            true,
			"follow_symlinks":      false,
			"candidate_extensions": []string{".jpg", ".jpeg"},
		},
	})
}

func (s *Store) AcquireInventory(path, label, group string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	invID := newID("inv")
	objID := newID("obj")
	key := filepath.Join("objects", "acquired", objID)
	dest := filepath.Join(s.Root, key)
	result, err := copyHash(abs, dest)
	if err != nil {
		_ = s.appendEvent("HistoricalInventoryFileAcquireFailed", nil, nil, map[string]any{
			"purpose":       "historical_inventory",
			"original_path": abs,
			"error":         errPayload(err, true),
		})
		return "", err
	}
	contentRef := contentRef(result.Hash, result.Size)
	if err := s.appendEvent("HistoricalInventoryFileAcquired", nil, nil, map[string]any{
		"historical_inventory_id": invID,
		"stored_object_id":        objID,
		"purpose":                 "historical_inventory",
		"original_path":           abs,
		"label":                   label,
		"group":                   group,
		"acquired_storage_key":    filepath.ToSlash(key),
		"source_file":             statPayload(abs),
		"copy_result": map[string]any{
			"bytes_copied":   result.Size,
			"hash_algorithm": "sha256",
			"hash":           result.Hash,
			"content_ref":    contentRef,
		},
	}); err != nil {
		return "", err
	}
	if !s.contentAddressExists(contentRef) {
		if err := s.materialize(objID, filepath.ToSlash(key), contentRef); err != nil {
			return "", err
		}
	}
	return invID, nil
}

func (s *Store) ScanSources(progress ProgressFunc) (string, error) {
	return s.ScanSourceRoots(nil, progress)
}

func (s *Store) ResumeSourceScan(scanID string, progress ProgressFunc) (string, error) {
	if scanID == "" {
		return "", errors.New("scan id is required")
	}
	status, err := s.scanStatus(scanID)
	if err != nil {
		return "", err
	}
	if status == "completed" {
		return scanID, nil
	}
	roots, err := s.sourceRootsForScan(scanID)
	if err != nil {
		return "", err
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("scan %s cannot be resumed: no source roots are recorded for it", scanID)
	}
	processed, err := s.processedSourceRelativePaths(scanID)
	if err != nil {
		return "", err
	}
	report, err := s.resumeReport(scanID, roots)
	if err != nil {
		return "", err
	}
	if err := s.appendEvent("IngestionScanResumeRequested", nil, &scanID, map[string]any{
		"scan_id":          scanID,
		"resumed_at_ms":    nowMS(),
		"source_roots":     rootPayloads(roots),
		"already_acquired": report.SourceFilesAcquired,
	}); err != nil {
		return "", err
	}
	for _, root := range roots {
		progressf(progress, "resuming source root %s (%s)", root.Label, root.Path)
		err := filepath.WalkDir(root.Path, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				report.DirectoriesSeen++
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			report.RegularFilesSeen++
			if !isJPEG(path) {
				report.NonCandidateFilesSkipped++
				return nil
			}
			report.CandidateFilesSeen++
			rel, _ := filepath.Rel(root.Path, path)
			relSlash := filepath.ToSlash(rel)
			if processed[root.ID][relSlash] {
				return nil
			}
			progressf(progress, "acquiring %s", path)
			entryID, err := s.appendEventReturnID("SourceEntryObserved", nil, &scanID, map[string]any{
				"scan_id":                 scanID,
				"source_root_id":          root.ID,
				"source_kind":             "source_root",
				"path":                    path,
				"relative_path":           relSlash,
				"historical_inventory_id": nil,
				"inventory_entry_id":      nil,
				"entry_type":              "regular_file",
				"filesystem":              statPayload(path),
				"candidate_reason": map[string]any{
					"method":    "extension",
					"extension": strings.ToLower(filepath.Ext(path)),
				},
			})
			if err != nil {
				return err
			}
			if err := s.acquireSourceFile(scanID, &entryID, root.ID, "source_root", path, relSlash, "", "", report); err != nil {
				report.SourceFileAcquireFailures++
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	if err := s.writeReport(report); err != nil {
		return "", err
	}
	if err := s.appendEvent("IngestionScanCompleted", nil, &scanID, map[string]any{
		"scan_id":         scanID,
		"completed_at_ms": nowMS(),
		"status":          "completed",
		"stats": map[string]any{
			"source_roots_scanned":           report.SourceRootsScanned,
			"directories_seen":               report.DirectoriesSeen,
			"regular_files_seen":             report.RegularFilesSeen,
			"candidate_files_seen":           report.CandidateFilesSeen,
			"source_files_acquired":          report.SourceFilesAcquired,
			"source_file_acquire_failures":   report.SourceFileAcquireFailures,
			"content_addresses_materialized": report.ContentAddressesMaterialized,
			"duplicate_acquisitions":         report.DuplicateAcquisitions,
			"duplicate_garbage_bytes":        report.DuplicateGarbageBytes,
			"non_candidate_files_skipped":    report.NonCandidateFilesSkipped,
		},
		"report_paths": map[string]any{
			"json": filepath.Join(s.Root, "reports", "scan-"+scanID+".json"),
			"text": filepath.Join(s.Root, "reports", "scan-"+scanID+".txt"),
		},
	}); err != nil {
		return "", err
	}
	return scanID, nil
}

func (s *Store) ScanSourceRoots(sourceRootIDs []string, progress ProgressFunc) (string, error) {
	scanID := newID("scan")
	roots, err := s.sourceRoots()
	if err != nil {
		return "", err
	}
	if len(sourceRootIDs) > 0 {
		roots, err = filterSourceRoots(roots, sourceRootIDs)
		if err != nil {
			return "", err
		}
	}
	rootIDs := make([]string, 0, len(roots))
	for _, r := range roots {
		rootIDs = append(rootIDs, r.ID)
	}
	if err := s.appendEvent("SourceRootScanRequested", nil, &scanID, map[string]any{
		"scan_id":              scanID,
		"source_root_ids":      rootIDs,
		"candidate_extensions": []string{".jpg", ".jpeg"},
		"requested_by":         "cli",
	}); err != nil {
		return "", err
	}
	if err := s.appendEvent("IngestionScanStarted", nil, &scanID, map[string]any{
		"scan_id":       scanID,
		"started_at_ms": nowMS(),
		"source_roots":  rootPayloads(roots),
		"policy": map[string]any{
			"recursive":            true,
			"follow_symlinks":      false,
			"candidate_extensions": []string{".jpg", ".jpeg"},
		},
	}); err != nil {
		return "", err
	}
	report := &ScanReport{ScanID: scanID, SourceRootsScanned: len(roots)}
	for _, root := range roots {
		progressf(progress, "scanning source root %s (%s)", root.Label, root.Path)
		err := filepath.WalkDir(root.Path, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				report.DirectoriesSeen++
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			report.RegularFilesSeen++
			if !isJPEG(path) {
				report.NonCandidateFilesSkipped++
				return nil
			}
			report.CandidateFilesSeen++
			progressf(progress, "acquiring %s", path)
			rel, _ := filepath.Rel(root.Path, path)
			entryID, err := s.appendEventReturnID("SourceEntryObserved", nil, &scanID, map[string]any{
				"scan_id":                 scanID,
				"source_root_id":          root.ID,
				"source_kind":             "source_root",
				"path":                    path,
				"relative_path":           filepath.ToSlash(rel),
				"historical_inventory_id": nil,
				"inventory_entry_id":      nil,
				"entry_type":              "regular_file",
				"filesystem":              statPayload(path),
				"candidate_reason": map[string]any{
					"method":    "extension",
					"extension": strings.ToLower(filepath.Ext(path)),
				},
			})
			if err != nil {
				return err
			}
			if err := s.acquireSourceFile(scanID, &entryID, root.ID, "source_root", path, filepath.ToSlash(rel), "", "", report); err != nil {
				report.SourceFileAcquireFailures++
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	if err := s.writeReport(report); err != nil {
		return "", err
	}
	if err := s.appendEvent("IngestionScanCompleted", nil, &scanID, map[string]any{
		"scan_id":         scanID,
		"completed_at_ms": nowMS(),
		"status":          "completed",
		"stats": map[string]any{
			"source_roots_scanned":           report.SourceRootsScanned,
			"directories_seen":               report.DirectoriesSeen,
			"regular_files_seen":             report.RegularFilesSeen,
			"candidate_files_seen":           report.CandidateFilesSeen,
			"source_files_acquired":          report.SourceFilesAcquired,
			"source_file_acquire_failures":   report.SourceFileAcquireFailures,
			"content_addresses_materialized": report.ContentAddressesMaterialized,
			"duplicate_acquisitions":         report.DuplicateAcquisitions,
			"duplicate_garbage_bytes":        report.DuplicateGarbageBytes,
			"non_candidate_files_skipped":    report.NonCandidateFilesSkipped,
		},
		"report_paths": map[string]any{
			"json": filepath.Join(s.Root, "reports", "scan-"+scanID+".json"),
			"text": filepath.Join(s.Root, "reports", "scan-"+scanID+".txt"),
		},
	}); err != nil {
		return "", err
	}
	return scanID, nil
}

func (s *Store) ScanInventory(invID, invType string, exts []string, resolverRoot string, stripPrefixes []string, caseSensitive bool) (string, error) {
	return s.ScanInventoryWithProgress(invID, invType, exts, resolverRoot, stripPrefixes, caseSensitive, nil)
}

func (s *Store) ScanInventoryWithProgress(invID, invType string, exts []string, resolverRoot string, stripPrefixes []string, caseSensitive bool, progress ProgressFunc) (string, error) {
	scanID := newID("scan")
	inv, err := s.inventory(invID)
	if err != nil {
		return "", err
	}
	if invType == "" {
		invType = "toc"
	}
	if len(exts) == 0 {
		exts = []string{".jpg", ".jpeg"}
	}
	if len(stripPrefixes) == 0 {
		stripPrefixes = []string{"./"}
	}
	if err := s.appendEvent("HistoricalInventoryScanRequested", nil, &scanID, map[string]any{
		"scan_id":                 scanID,
		"historical_inventory_id": inv.ID,
		"label":                   inv.Label,
		"group":                   inv.Group,
		"inventory_type":          invType,
		"original_path":           inv.OriginalPath,
		"stored_object_id":        inv.StoredObjectID,
		"parser": map[string]any{
			"name":    "hash_keyed_text",
			"version": 1,
		},
		"filter": map[string]any{
			"candidate_extensions":      exts,
			"hash_only_entries_allowed": false,
		},
		"path_resolver": map[string]any{
			"type":           "root_relative",
			"resolver_root":  resolverRoot,
			"strip_prefixes": stripPrefixes,
			"case_sensitive": caseSensitive,
		},
		"requested_by": "cli",
	}); err != nil {
		return "", err
	}
	entries, err := parseInventory(filepath.Join(s.Root, inv.AcquiredStorageKey), inv.ID, scanID, exts)
	if err != nil {
		return "", err
	}
	report := &ScanReport{ScanID: scanID, HistoricalJPEGEntriesLoaded: len(entries)}
	progressf(progress, "loaded %d matching historical inventory entries", len(entries))
	for _, ent := range entries {
		ent.ResolvedPath = resolveHistoricalPath(resolverRoot, ent.HistoricalPath, stripPrefixes)
		if err := s.upsertInventoryEntry(scanID, inv.ID, ent); err != nil {
			return "", err
		}
		content, seen, err := s.seenContent(ent.SHA256, ent.Size)
		if err != nil {
			return "", err
		}
		if seen {
			report.HistoricalEntriesAlreadySeen++
			progressf(progress, "skipping already-seen historical entry %s", ent.ID)
			if err := s.appendEvent("HistoricalInventoryOccurrenceLinked", nil, &scanID, map[string]any{
				"scan_id":                 scanID,
				"source_occurrence_id":    newID("occ"),
				"historical_inventory_id": inv.ID,
				"inventory_entry_id":      ent.ID,
				"content_ref":             content,
				"link_basis": map[string]any{
					"type":           "trusted_historical_inventory_hash",
					"inventory_type": invType,
					"parser": map[string]any{
						"name":    "hash_keyed_text",
						"version": 1,
					},
				},
			}); err != nil {
				return "", err
			}
			continue
		}
		if ent.ResolvedPath == "" {
			report.HistoricalEntriesUnresolved++
			continue
		}
		if _, err := os.Stat(ent.ResolvedPath); err != nil {
			report.HistoricalEntriesUnresolved++
			continue
		}
		report.HistoricalEntriesResolved++
		progressf(progress, "acquiring historical entry %s from %s", ent.ID, ent.ResolvedPath)
		entryID, err := s.appendEventReturnID("SourceEntryObserved", nil, &scanID, map[string]any{
			"scan_id":                 scanID,
			"source_root_id":          nil,
			"source_kind":             "historical_inventory_resolved_path",
			"path":                    ent.ResolvedPath,
			"relative_path":           filepath.ToSlash(ent.HistoricalPath),
			"historical_inventory_id": inv.ID,
			"inventory_entry_id":      ent.ID,
			"entry_type":              "regular_file",
			"filesystem":              statPayload(ent.ResolvedPath),
			"candidate_reason": map[string]any{
				"method":    "historical_inventory_extension",
				"extension": ent.Extension,
			},
		})
		if err != nil {
			return "", err
		}
		if err := s.acquireSourceFile(scanID, &entryID, "", "historical_inventory_resolved_path", ent.ResolvedPath, filepath.ToSlash(ent.HistoricalPath), inv.ID, ent.ID, report); err != nil {
			report.SourceFileAcquireFailures++
		}
	}
	if err := s.writeReport(report); err != nil {
		return "", err
	}
	return scanID, nil
}

func (s *Store) Report(scanID string) (*ScanReport, error) {
	b, err := os.ReadFile(filepath.Join(s.Root, "reports", "scan-"+scanID+".json"))
	if err != nil {
		return nil, err
	}
	var r ScanReport
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) initSchema() error {
	stmts := []string{
		`create table if not exists events_applied(event_id text primary key, event_type text, recorded_at_ms integer)`,
		`create table if not exists source_roots(source_root_id text primary key, root_path text unique, label text, source_type text, policy_json text)`,
		`create table if not exists stored_objects(stored_object_id text primary key, purpose text, acquired_storage_key text, original_path text, first_event_id text)`,
		`create table if not exists source_content_links(source_occurrence_id text primary key, stored_object_id text, content_ref text, cas_existed_at_ingest integer, acquired_object_retained integer, link_event_id text)`,
		`create table if not exists verified_hashes(stored_object_id text, algorithm text, verifier_version integer, hash text, size integer, content_ref text, verified_event_id text)`,
		`create table if not exists content_addresses(content_ref text primary key, algorithm text, hash text, size integer, derived_cas_storage_key text, first_materialized_event_id text)`,
		`create table if not exists source_occurrences(source_occurrence_id text primary key, stored_object_id text, source_kind text, source_root_id text, path text, relative_path text, scan_id text, historical_inventory_id text, inventory_entry_id text, source_event_id text)`,
		`create table if not exists source_root_scans(source_root_id text, scan_id text, requested_event_id text, primary key(source_root_id, scan_id))`,
		`create table if not exists historical_inventories(historical_inventory_id text primary key, stored_object_id text, original_path text, label text, group_name text, acquired_event_id text, acquired_storage_key text)`,
		`create table if not exists historical_inventory_scans(scan_id text primary key, historical_inventory_id text, inventory_type text, parser_json text, filter_json text, path_resolver_json text, requested_event_id text)`,
		`create table if not exists historical_inventory_entries(inventory_entry_id text primary key, scan_id text, historical_inventory_id text, sha256 text, size integer, historical_path text, resolved_path text, extension text, raw_line text)`,
		`create table if not exists historical_matches(content_ref text, stored_object_id text, historical_inventory_id text, inventory_entry_id text)`,
		`create table if not exists historical_seen_links(source_occurrence_id text primary key, historical_inventory_id text, inventory_entry_id text, content_ref text, link_event_id text)`,
		`create table if not exists asset_projection(asset_projection_id text primary key, content_ref text unique, representative_stored_object_id text, asset_kind text)`,
		`create table if not exists scans(scan_id text primary key, status text, started_at_ms integer, completed_at_ms integer, stats_json text)`,
		`create table if not exists content_metadata(content_ref text, extractor_name text, extractor_version integer, metadata_event_id text, stored_object_id text, source_occurrence_id text, scan_id text, extracted_at_ms integer, fields_json text, warnings_json text, primary key(content_ref, extractor_name, extractor_version))`,
		`create table if not exists content_metadata_failures(content_ref text, extractor_name text, extractor_version integer, failure_event_id text, stored_object_id text, source_occurrence_id text, scan_id text, failed_at_ms integer, error_json text, primary key(content_ref, extractor_name, extractor_version))`,
		`create table if not exists metadata_issues(issue_event_id text primary key, content_ref text, stored_object_id text, source_occurrence_id text, scan_id text, extractor_name text, extractor_version integer, detected_at_ms integer, issue_type text, severity text, details_json text)`,
		`create table if not exists photo_capture_times(stored_object_id text primary key, content_ref text, source_occurrence_id text, scan_id text, filename text, relative_path text, capture_date text, capture_time_local text, utc_offset text, precision text, source_kind text, source_event_id text, extractor_name text, extractor_version integer, reducer_name text, reducer_version integer, raw_value text)`,
	}
	for _, stmt := range stmts {
		if _, err := s.DB.Exec(stmt); err != nil {
			return err
		}
	}
	if err := s.rebuildPhotoCaptureTimeProjection(); err != nil {
		return err
	}
	return nil
}

func (s *Store) appendEvent(eventType string, causationID *string, correlationID *string, payload map[string]any) error {
	_, err := s.appendEventReturnID(eventType, causationID, correlationID, payload)
	return err
}

func (s *Store) appendEventReturnID(eventType string, causationID *string, correlationID *string, payload map[string]any) (string, error) {
	ev := Event{
		EventID:       newID("evt"),
		EventType:     eventType,
		SchemaVersion: schemaVersion,
		RecordedAtMS:  nowMS(),
		Actor: map[string]any{
			"type": "process",
			"id":   "photostore-cli",
			"pid":  os.Getpid(),
		},
		CausationID:   causationID,
		CorrelationID: correlationID,
		Payload:       payload,
	}
	if err := s.writeEvent(ev); err != nil {
		return "", err
	}
	if err := s.applyEvent(ev); err != nil {
		return "", err
	}
	return ev.EventID, nil
}

func (s *Store) writeEvent(ev Event) error {
	path := filepath.Join(s.Root, "events", "events.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func (s *Store) applyEvent(ev Event) error {
	pj := func(k string) string { return mustJSON(ev.Payload[k]) }
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`insert or ignore into events_applied(event_id,event_type,recorded_at_ms) values(?,?,?)`, ev.EventID, ev.EventType, ev.RecordedAtMS); err != nil {
		return err
	}
	switch ev.EventType {
	case "SourceRootRegistered":
		_, err = tx.Exec(`insert or ignore into source_roots values(?,?,?,?,?)`, str(ev.Payload["source_root_id"]), str(ev.Payload["root_path"]), str(ev.Payload["label"]), str(ev.Payload["source_type"]), pj("scan_policy"))
	case "SourceRootScanRequested":
		for _, sourceRootID := range stringSlice(ev.Payload["source_root_ids"]) {
			if _, err = tx.Exec(`insert or ignore into source_root_scans(source_root_id,scan_id,requested_event_id) values(?,?,?)`, sourceRootID, str(ev.Payload["scan_id"]), ev.EventID); err != nil {
				break
			}
		}
	case "HistoricalInventoryFileAcquired":
		_, err = tx.Exec(`insert or ignore into stored_objects values(?,?,?,?,?)`, str(ev.Payload["stored_object_id"]), str(ev.Payload["purpose"]), str(ev.Payload["acquired_storage_key"]), str(ev.Payload["original_path"]), ev.EventID)
		if err == nil {
			_, err = tx.Exec(`insert or ignore into historical_inventories values(?,?,?,?,?,?,?)`, str(ev.Payload["historical_inventory_id"]), str(ev.Payload["stored_object_id"]), str(ev.Payload["original_path"]), str(ev.Payload["label"]), str(ev.Payload["group"]), ev.EventID, str(ev.Payload["acquired_storage_key"]))
		}
	case "SourceFileAcquired":
		occ := str(ev.Payload["source_occurrence_id"])
		stored := nullableString(ev.Payload["stored_object_id"])
		_, err = tx.Exec(`insert or ignore into source_occurrences values(?,?,?,?,?,?,?,?,?,?)`, occ, stored, str(ev.Payload["source_kind"]), nullableString(ev.Payload["source_root_id"]), str(ev.Payload["path"]), str(ev.Payload["relative_path"]), str(ev.Payload["scan_id"]), nullableString(ev.Payload["historical_inventory_id"]), nullableString(ev.Payload["inventory_entry_id"]), ev.EventID)
		if err == nil && stored.Valid {
			_, err = tx.Exec(`insert or ignore into stored_objects values(?,?,?,?,?)`, stored.String, str(ev.Payload["purpose"]), str(ev.Payload["acquired_storage_key"]), str(ev.Payload["path"]), ev.EventID)
		}
		if err == nil {
			disp := mapValue(ev.Payload["storage_disposition"])
			_, err = tx.Exec(`insert or ignore into source_content_links values(?,?,?,?,?,?)`, occ, stored, str(ev.Payload["content_ref"]), boolInt(disp["cas_existed_at_ingest"]), boolInt(disp["acquired_object_retained"]), ev.EventID)
		}
	case "ContentAddressMaterialized":
		ref := str(ev.Payload["content_ref"])
		algo, hash, size := parseContentRef(ref)
		_, err = tx.Exec(`insert or ignore into content_addresses values(?,?,?,?,?,?)`, ref, algo, hash, size, casKey(ref), ev.EventID)
	case "HistoricalInventoryScanRequested":
		_, err = tx.Exec(`insert or ignore into historical_inventory_scans values(?,?,?,?,?,?,?)`, str(ev.Payload["scan_id"]), str(ev.Payload["historical_inventory_id"]), str(ev.Payload["inventory_type"]), pj("parser"), pj("filter"), pj("path_resolver"), ev.EventID)
	case "HistoricalInventoryOccurrenceLinked":
		_, err = tx.Exec(`insert or ignore into historical_seen_links values(?,?,?,?,?)`, str(ev.Payload["source_occurrence_id"]), str(ev.Payload["historical_inventory_id"]), str(ev.Payload["inventory_entry_id"]), str(ev.Payload["content_ref"]), ev.EventID)
	case "IngestionScanStarted":
		_, err = tx.Exec(`insert or replace into scans(scan_id,status,started_at_ms,stats_json) values(?,?,?,coalesce((select stats_json from scans where scan_id=?),'{}'))`, str(ev.Payload["scan_id"]), "started", int64Value(ev.Payload["started_at_ms"]), str(ev.Payload["scan_id"]))
	case "IngestionScanCompleted":
		_, err = tx.Exec(`insert or replace into scans(scan_id,status,completed_at_ms,stats_json) values(?,?,?,?)`, str(ev.Payload["scan_id"]), "completed", int64Value(ev.Payload["completed_at_ms"]), mustJSON(ev.Payload["stats"]))
	case "PhotoMetadataExtracted":
		extractor := mapValue(ev.Payload["extractor"])
		_, err = tx.Exec(`insert or ignore into content_metadata values(?,?,?,?,?,?,?,?,?,?)`, str(ev.Payload["content_ref"]), str(extractor["name"]), int64Value(extractor["version"]), ev.EventID, str(ev.Payload["stored_object_id"]), str(ev.Payload["source_occurrence_id"]), str(ev.Payload["scan_id"]), int64Value(ev.Payload["extracted_at_ms"]), pj("fields"), pj("warnings"))
		if err == nil {
			err = reducePhotoCaptureTime(tx, ev, extractor)
		}
	case "PhotoMetadataExtractionFailed":
		extractor := mapValue(ev.Payload["extractor"])
		_, err = tx.Exec(`insert or ignore into content_metadata_failures values(?,?,?,?,?,?,?,?,?)`, str(ev.Payload["content_ref"]), str(extractor["name"]), int64Value(extractor["version"]), ev.EventID, str(ev.Payload["stored_object_id"]), str(ev.Payload["source_occurrence_id"]), str(ev.Payload["scan_id"]), int64Value(ev.Payload["failed_at_ms"]), pj("error"))
	case "PhotoMetadataObservationMismatchDetected":
		extractor := mapValue(ev.Payload["extractor"])
		issue := mapValue(ev.Payload["issue"])
		_, err = tx.Exec(`insert or ignore into metadata_issues values(?,?,?,?,?,?,?,?,?,?,?)`, ev.EventID, str(ev.Payload["content_ref"]), str(ev.Payload["stored_object_id"]), str(ev.Payload["source_occurrence_id"]), str(ev.Payload["scan_id"]), str(extractor["name"]), int64Value(extractor["version"]), int64Value(ev.Payload["detected_at_ms"]), str(issue["type"]), str(issue["severity"]), mustJSON(ev.Payload))
	case "DuplicateSourceObjectDeduplicated":
		_, err = tx.Exec(`update source_content_links set acquired_object_retained = 0 where source_occurrence_id = ?`, str(ev.Payload["source_occurrence_id"]))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) acquireSourceFile(scanID string, causationID *string, sourceRootID, sourceKind, path, rel, invID, entryID string, report *ScanReport) error {
	occID := newID("occ")
	objID := newID("obj")
	key := filepath.Join("objects", "acquired", objID)
	dest := filepath.Join(s.Root, key)
	result, err := copyHash(path, dest)
	if err != nil {
		_ = s.appendEvent("SourceFileAcquireFailed", causationID, &scanID, map[string]any{
			"scan_id":        scanID,
			"source_root_id": nullable(sourceRootID),
			"path":           path,
			"relative_path":  rel,
			"error":          errPayload(err, true),
		})
		return err
	}
	ref := contentRef(result.Hash, result.Size)
	existed := s.contentAddressExists(ref)
	payload := map[string]any{
		"scan_id":                 scanID,
		"source_occurrence_id":    occID,
		"stored_object_id":        objID,
		"purpose":                 "source_media",
		"content_ref":             ref,
		"source_root_id":          nullable(sourceRootID),
		"source_kind":             sourceKind,
		"path":                    path,
		"relative_path":           rel,
		"historical_inventory_id": nullable(invID),
		"inventory_entry_id":      nullable(entryID),
		"acquired_storage_key":    filepath.ToSlash(key),
		"source_file_before_copy": statPayload(path),
		"copy_result": map[string]any{
			"bytes_copied":   result.Size,
			"hash_algorithm": "sha256",
			"hash":           result.Hash,
		},
		"storage_disposition": map[string]any{
			"cas_existed_at_ingest":    existed,
			"acquired_object_retained": true,
			"temporary_copy_discarded": false,
		},
	}
	if err := s.appendEvent("SourceFileAcquired", causationID, &scanID, payload); err != nil {
		return err
	}
	if err := s.recordMetadataForSourceFile(scanID, causationID, occID, objID, ref, filepath.ToSlash(key)); err != nil {
		return err
	}
	report.SourceFilesAcquired++
	if existed {
		report.DuplicateAcquisitions++
		report.DuplicateGarbageBytes += result.Size
	}
	if !existed {
		if err := s.materialize(objID, filepath.ToSlash(key), ref); err != nil {
			return err
		}
		report.ContentAddressesMaterialized++
	}
	return nil
}

func (s *Store) materialize(objID, acquiredKey, ref string) error {
	src := filepath.Join(s.Root, filepath.FromSlash(acquiredKey))
	dst := filepath.Join(s.Root, filepath.FromSlash(casKey(ref)))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := cloneOrCopy(src, dst); err != nil {
		return err
	}
	return s.appendEvent("ContentAddressMaterialized", nil, nil, map[string]any{
		"stored_object_id": objID,
		"content_ref":      ref,
		"materialization": map[string]any{
			"method":  "apfs_clone_or_copy",
			"created": true,
		},
	})
}

func (s *Store) contentAddressExists(ref string) bool {
	_, err := os.Stat(filepath.Join(s.Root, filepath.FromSlash(casKey(ref))))
	return err == nil
}

func (s *Store) sourceRoots() ([]SourceRoot, error) {
	rows, err := s.DB.Query(`select source_root_id, root_path, label from source_roots order by root_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceRoot
	for rows.Next() {
		var r SourceRoot
		if err := rows.Scan(&r.ID, &r.Path, &r.Label); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		var scanID sql.NullString
		var completed sql.NullInt64
		err := s.DB.QueryRow(`
			select scan_id, completed_at_ms
			from (
				select srs.scan_id, sc.completed_at_ms
				from source_root_scans srs
				join scans sc on sc.scan_id = srs.scan_id
				where srs.source_root_id = ? and sc.completed_at_ms is not null
				union
				select so.scan_id, sc.completed_at_ms
				from source_occurrences so
				join scans sc on sc.scan_id = so.scan_id
				where so.source_root_id = ? and sc.completed_at_ms is not null
			)
			order by completed_at_ms desc
			limit 1`, out[i].ID, out[i].ID).Scan(&scanID, &completed)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		if scanID.Valid {
			out[i].LastScanID = &scanID.String
		}
		if completed.Valid {
			out[i].LastScanCompletedAtMS = &completed.Int64
		}
	}
	return out, nil
}

func (s *Store) scanStatus(scanID string) (string, error) {
	var status string
	if err := s.DB.QueryRow(`select status from scans where scan_id = ?`, scanID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("scan %s is not known", scanID)
		}
		return "", err
	}
	return status, nil
}

func (s *Store) sourceRootsForScan(scanID string) ([]SourceRoot, error) {
	rows, err := s.DB.Query(`
		select distinct sr.source_root_id, sr.root_path, sr.label
		from source_roots sr
		join source_root_scans srs on srs.source_root_id = sr.source_root_id
		where srs.scan_id = ?
		order by sr.root_path`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roots []SourceRoot
	for rows.Next() {
		var root SourceRoot
		if err := rows.Scan(&root.ID, &root.Path, &root.Label); err != nil {
			return nil, err
		}
		roots = append(roots, root)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(roots) > 0 {
		return roots, nil
	}
	rows, err = s.DB.Query(`
		select distinct sr.source_root_id, sr.root_path, sr.label
		from source_roots sr
		join source_occurrences so on so.source_root_id = sr.source_root_id
		where so.scan_id = ?
		order by sr.root_path`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var root SourceRoot
		if err := rows.Scan(&root.ID, &root.Path, &root.Label); err != nil {
			return nil, err
		}
		roots = append(roots, root)
	}
	return roots, rows.Err()
}

func (s *Store) processedSourceRelativePaths(scanID string) (map[string]map[string]bool, error) {
	rows, err := s.DB.Query(`
		select coalesce(source_root_id, ''), relative_path
		from source_occurrences
		where scan_id = ? and source_kind = 'source_root' and stored_object_id is not null`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var rootID, rel string
		if err := rows.Scan(&rootID, &rel); err != nil {
			return nil, err
		}
		if out[rootID] == nil {
			out[rootID] = map[string]bool{}
		}
		out[rootID][rel] = true
	}
	return out, rows.Err()
}

func (s *Store) resumeReport(scanID string, roots []SourceRoot) (*ScanReport, error) {
	report := &ScanReport{ScanID: scanID, SourceRootsScanned: len(roots)}
	rows, err := s.DB.Query(`
		select coalesce(scl.content_ref, ''), scl.cas_existed_at_ingest
		from source_occurrences so
		left join source_content_links scl on scl.source_occurrence_id = so.source_occurrence_id
		where so.scan_id = ? and so.stored_object_id is not null`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var existed sql.NullInt64
		if err := rows.Scan(&ref, &existed); err != nil {
			return nil, err
		}
		report.SourceFilesAcquired++
		if existed.Valid && existed.Int64 == 1 {
			report.DuplicateAcquisitions++
			_, _, size := parseContentRef(ref)
			report.DuplicateGarbageBytes += size
		} else if existed.Valid {
			report.ContentAddressesMaterialized++
		}
	}
	return report, rows.Err()
}

func filterSourceRoots(roots []SourceRoot, ids []string) ([]SourceRoot, error) {
	wanted := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			wanted[id] = true
		}
	}
	if len(wanted) == 0 {
		return roots, nil
	}
	out := make([]SourceRoot, 0, len(wanted))
	for _, root := range roots {
		if wanted[root.ID] {
			out = append(out, root)
			delete(wanted, root.ID)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("unknown source root id: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

type inventoryProjection struct {
	ID                 string
	StoredObjectID     string
	OriginalPath       string
	Label              string
	Group              string
	AcquiredStorageKey string
}

func (s *Store) inventory(id string) (inventoryProjection, error) {
	var inv inventoryProjection
	err := s.DB.QueryRow(`select historical_inventory_id, stored_object_id, original_path, label, group_name, acquired_storage_key from historical_inventories where historical_inventory_id = ?`, id).Scan(&inv.ID, &inv.StoredObjectID, &inv.OriginalPath, &inv.Label, &inv.Group, &inv.AcquiredStorageKey)
	return inv, err
}

func (s *Store) upsertInventoryEntry(scanID, invID string, e inventoryEntry) error {
	var size any
	if e.Size != nil {
		size = *e.Size
	}
	_, err := s.DB.Exec(`insert or replace into historical_inventory_entries values(?,?,?,?,?,?,?,?,?)`, e.ID, scanID, invID, e.SHA256, size, e.HistoricalPath, e.ResolvedPath, e.Extension, e.RawLine)
	return err
}

func (s *Store) seenContent(hash string, size *int64) (string, bool, error) {
	if size != nil {
		ref := contentRef(hash, *size)
		var found string
		err := s.DB.QueryRow(`select content_ref from content_addresses where content_ref = ? union select content_ref from source_content_links where content_ref = ? limit 1`, ref, ref).Scan(&found)
		if err == nil {
			return found, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
	}
	prefix := "sha256:" + strings.ToLower(hash) + ":%"
	var found string
	err := s.DB.QueryRow(`select content_ref from content_addresses where content_ref like ? union select content_ref from source_content_links where content_ref like ? limit 1`, prefix, prefix).Scan(&found)
	if err == nil {
		return found, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return "", false, err
}

func (s *Store) writeReport(report *ScanReport) error {
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	jsonPath := filepath.Join(s.Root, "reports", "scan-"+report.ScanID+".json")
	if err := os.WriteFile(jsonPath, b, 0o644); err != nil {
		return err
	}
	lines := []string{
		"Photostore scan report",
		"scan: " + report.ScanID,
		fmt.Sprintf("candidate files: %d", report.CandidateFilesSeen),
		fmt.Sprintf("source files acquired: %d", report.SourceFilesAcquired),
		fmt.Sprintf("duplicate acquisitions: %d", report.DuplicateAcquisitions),
		fmt.Sprintf("duplicate garbage bytes: %d", report.DuplicateGarbageBytes),
		fmt.Sprintf("historical entries loaded: %d", report.HistoricalJPEGEntriesLoaded),
		fmt.Sprintf("historical entries already seen: %d", report.HistoricalEntriesAlreadySeen),
	}
	return os.WriteFile(filepath.Join(s.Root, "reports", "scan-"+report.ScanID+".txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

type copyResult struct {
	Hash string
	Size int64
}

func copyHash(src, dst string) (copyResult, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return copyResult{}, err
	}
	in, err := os.Open(src)
	if err != nil {
		return copyResult{}, err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return copyResult{}, err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h), in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyResult{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return copyResult{}, closeErr
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return copyResult{}, err
	}
	if err := chmodCompleteFile(dst); err != nil {
		return copyResult{}, err
	}
	return copyResult{Hash: hex.EncodeToString(h.Sum(nil)), Size: n}, nil
}

func cloneOrCopy(src, dst string) error {
	if err := exec.Command("cp", "-c", src, dst).Run(); err == nil {
		return chmodCompleteFile(dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	return chmodCompleteFile(dst)
}

func chmodCompleteFile(path string) error {
	return os.Chmod(path, 0o600)
}

var annexSize = regexp.MustCompile(`SHA256E-s([0-9]+)--[0-9a-fA-F]{64}`)

func parseInventory(path, invID, scanID string, exts []string) ([]inventoryEntry, error) {
	allowed := map[string]bool{}
	for _, ext := range exts {
		allowed[strings.ToLower(ext)] = true
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []inventoryEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024), 1024*1024*16)
	for sc.Scan() {
		raw := sc.Text()
		fields := strings.Fields(raw)
		if len(fields) < 2 || !isSHA256(fields[0]) {
			continue
		}
		historicalPath := strings.Join(fields[1:], " ")
		ext := strings.ToLower(filepath.Ext(historicalPath))
		if !allowed[ext] {
			continue
		}
		hash := strings.ToLower(fields[0])
		var size *int64
		if m := annexSize.FindStringSubmatch(historicalPath); len(m) == 2 {
			var parsed int64
			fmt.Sscanf(m[1], "%d", &parsed)
			size = &parsed
		}
		idHash := sha256.Sum256([]byte(invID + "\x00" + hash + "\x00" + historicalPath))
		entries = append(entries, inventoryEntry{
			ID:             "ient_" + hex.EncodeToString(idHash[:]),
			SHA256:         hash,
			Size:           size,
			HistoricalPath: historicalPath,
			Extension:      ext,
			RawLine:        raw,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].HistoricalPath < entries[j].HistoricalPath })
	return entries, nil
}

func resolveHistoricalPath(root, historicalPath string, stripPrefixes []string) string {
	if root == "" || historicalPath == "" {
		return ""
	}
	p := filepath.ToSlash(historicalPath)
	for _, prefix := range stripPrefixes {
		p = strings.TrimPrefix(p, filepath.ToSlash(prefix))
	}
	return filepath.Join(root, filepath.FromSlash(p))
}

func rootPayloads(roots []SourceRoot) []map[string]any {
	out := make([]map[string]any, 0, len(roots))
	for _, root := range roots {
		out = append(out, map[string]any{
			"source_root_id": root.ID,
			"root_path":      root.Path,
			"label":          root.Label,
		})
	}
	return out
}

func contentRef(hash string, size int64) string {
	return fmt.Sprintf("sha256:%s:%d", strings.ToLower(hash), size)
}

func parseContentRef(ref string) (string, string, int64) {
	parts := strings.Split(ref, ":")
	if len(parts) != 3 {
		return "", "", 0
	}
	var size int64
	fmt.Sscanf(parts[2], "%d", &size)
	return parts[0], parts[1], size
}

func casKey(ref string) string {
	_, hash, _ := parseContentRef(ref)
	if len(hash) < 4 {
		return filepath.ToSlash(filepath.Join("cas", "sha256", "v1", hash))
	}
	return filepath.ToSlash(filepath.Join("cas", "sha256", "v1", hash[:2], hash[2:4], hash))
}

func isJPEG(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".jpg" || ext == ".jpeg"
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func statPayload(path string) map[string]any {
	st, err := os.Stat(path)
	if err != nil {
		return map[string]any{}
	}
	return map[string]any{
		"size":     st.Size(),
		"mtime_ns": st.ModTime().UnixNano(),
	}
}

func errPayload(err error, retryable bool) map[string]any {
	return map[string]any{
		"type":      "io_error",
		"message":   err.Error(),
		"retryable": retryable,
	}
}

func progressf(progress ProgressFunc, format string, args ...any) {
	if progress != nil {
		progress(fmt.Sprintf(format, args...))
	}
}

func newID(prefix string) string {
	if os.Getenv("PHOTOSTORE_DETERMINISTIC_IDS") == "1" {
		n := deterministicIDCounter.Add(1)
		return fmt.Sprintf("%s_%012d", prefix, n)
	}
	return prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func nowMS() int64 {
	if fixed := os.Getenv("PHOTOSTORE_FIXED_NOW_MS"); fixed != "" {
		if parsed, err := strconv.ParseInt(fixed, 10, 64); err == nil {
			return parsed
		}
	}
	return time.Now().UnixMilli()
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func str(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableString(v any) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	s := str(v)
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func int64Value(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	default:
		return 0
	}
}

func boolInt(v any) int {
	b, _ := v.(bool)
	if b {
		return 1
	}
	return 0
}

func mapValue(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := str(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
