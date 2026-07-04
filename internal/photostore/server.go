package photostore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ServerOptions struct {
	APIOnly     bool
	ListenAddr  string
	BuildDir    string
	ScanOptions ScanOptions
}

type Server struct {
	store                    *Store
	apiOnly                  bool
	hostPolicy               hostPolicy
	buildDir                 string
	scanOptions              ScanOptions
	mux                      *http.ServeMux
	mu                       sync.Mutex
	jobsMu                   sync.Mutex
	jobs                     map[string]*Job
	subsMu                   sync.Mutex
	subs                     map[chan ServerEvent]struct{}
	eventStreamDroppedEvents atomic.Uint64
}

type Job struct {
	JobID           string   `json:"job_id"`
	Kind            string   `json:"kind"`
	Status          string   `json:"status"`
	StartedAtMS     int64    `json:"started_at_ms"`
	FinishedAtMS    *int64   `json:"finished_at_ms"`
	ResultRef       *string  `json:"result_ref"`
	Error           *string  `json:"error"`
	Progress        []string `json:"progress"`
	ProgressCurrent *int     `json:"progress_current"`
	ProgressTotal   *int     `json:"progress_total"`
}

type ServerEvent struct {
	Type         string `json:"type"`
	RecordedAtMS int64  `json:"recorded_at_ms"`
	Job          *Job   `json:"job,omitempty"`
	Jobs         []*Job `json:"jobs,omitempty"`
}

const completedJobRetentionLimit = 100

func NewServer(store *Store, opts ServerOptions) http.Handler {
	s := &Server{
		store:       store,
		apiOnly:     opts.APIOnly,
		hostPolicy:  newHostPolicy(opts.ListenAddr),
		buildDir:    opts.BuildDir,
		scanOptions: serverScanOptions(opts.ScanOptions),
		mux:         http.NewServeMux(),
		jobs:        map[string]*Job{},
		subs:        map[chan ServerEvent]struct{}{},
	}
	s.routes()
	return s
}

func serverScanOptions(opts ScanOptions) ScanOptions {
	if opts.Workers != 0 {
		return opts
	}
	if raw, ok := os.LookupEnv("PHOTOSTORE_SCAN_WORKERS"); ok {
		workers, err := strconv.Atoi(raw)
		if err == nil {
			opts.Workers = workers
		}
	}
	return opts
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.hostAllowed(r.Host) {
		writeErrorStatus(w, http.StatusForbidden, errors.New("host is not allowed"))
		return
	}
	if isMutatingAPIRequest(r) && !isJSONContentType(r.Header.Get("Content-Type")) {
		writeErrorStatus(w, http.StatusUnsupportedMediaType, errors.New("content-type must be application/json"))
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) hostAllowed(host string) bool {
	return s.hostPolicy.allowed(host)
}

type hostPolicy struct {
	allowAnyHost bool
	port         string
	hosts        map[string]struct{}
}

func newHostPolicy(listenAddr string) hostPolicy {
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		return hostPolicy{allowAnyHost: true}
	}
	host, port := splitHostPort(listenAddr)
	host = canonicalHostName(host)
	if isUnspecifiedHost(host) {
		return hostPolicy{allowAnyHost: true, port: port}
	}
	hosts := map[string]struct{}{host: {}}
	if isLoopbackHost(host) {
		hosts["localhost"] = struct{}{}
		hosts["127.0.0.1"] = struct{}{}
		hosts["::1"] = struct{}{}
	}
	return hostPolicy{port: port, hosts: hosts}
}

func (p hostPolicy) allowed(hostHeader string) bool {
	host, port := splitHostPort(hostHeader)
	if host == "" {
		return false
	}
	if p.port != "" && port != p.port {
		return false
	}
	if p.allowAnyHost {
		return true
	}
	_, ok := p.hosts[canonicalHostName(host)]
	return ok
}

func canonicalHostName(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

func isUnspecifiedHost(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		return net.JoinHostPort(strings.ToLower(parsedHost), parsedPort)
	}
	return host
}

func isMutatingAPIRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return strings.HasPrefix(r.URL.Path, "/api/")
	default:
		return false
	}
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/store", s.handleStore)
	s.mux.HandleFunc("GET /api/sources", s.handleSources)
	s.mux.HandleFunc("POST /api/sources", s.handleAddSource)
	s.mux.HandleFunc("POST /api/sources/{source_root_id}/scan", s.handleStartSingleSourceScan)
	s.mux.HandleFunc("GET /api/scans", s.handleScans)
	s.mux.HandleFunc("POST /api/scans", s.handleStartSourceScan)
	s.mux.HandleFunc("POST /api/scans/{scan_id}/resume", s.handleResumeScan)
	s.mux.HandleFunc("GET /api/scans/{scan_id}/report", s.handleScanReport)
	s.mux.HandleFunc("GET /api/scans/{scan_id}/acquired", s.handleScanAcquiredFiles)
	s.mux.HandleFunc("GET /api/objects/{stored_object_id}/bytes", s.handleStoredObjectBytes)
	s.mux.HandleFunc("GET /api/objects/{stored_object_id}/thumbnail", s.handleStoredObjectThumbnail)
	s.mux.HandleFunc("GET /api/objects/{stored_object_id}/metadata", s.handleStoredObjectMetadata)
	s.mux.HandleFunc("GET /api/objects/{stored_object_id}/navigation", s.handleStoredObjectNavigation)
	s.mux.HandleFunc("GET /api/objects/{stored_object_id}/map.png", s.handleStoredObjectMap)
	s.mux.HandleFunc("GET /api/photos/dates", s.handlePhotoYears)
	s.mux.HandleFunc("GET /api/photos/dates/{year}", s.handlePhotoMonths)
	s.mux.HandleFunc("GET /api/photos/dates/{year}/{month}", s.handlePhotoDays)
	s.mux.HandleFunc("GET /api/photos/dates/{year}/{month}/{day}", s.handleDatedPhotos)
	s.mux.HandleFunc("GET /api/photos/undated", s.handleUndatedPhotos)
	s.mux.HandleFunc("GET /api/assets", s.handleAssets)
	s.mux.HandleFunc("GET /api/assets/{asset_id}", s.handleAsset)
	s.mux.HandleFunc("GET /api/assets/{asset_id}/sources", s.handleAssetSources)
	s.mux.HandleFunc("POST /api/assets/{asset_id}/quality", s.handleSetAssetQuality)
	s.mux.HandleFunc("POST /api/assets/{asset_id}/status", s.handleSetAssetStatus)
	s.mux.HandleFunc("POST /api/assets/{asset_id}/visibility", s.handleSetAssetVisibility)
	s.mux.HandleFunc("POST /api/assets/{asset_id}/labels", s.handleApplyAssetLabel)
	s.mux.HandleFunc("DELETE /api/assets/{asset_id}/labels", s.handleRemoveAssetLabel)
	s.mux.HandleFunc("GET /api/labels", s.handleLabels)
	s.mux.HandleFunc("GET /api/inventories", s.handleInventories)
	s.mux.HandleFunc("POST /api/inventories/acquire", s.handleAcquireInventory)
	s.mux.HandleFunc("POST /api/inventories/{historical_inventory_id}/scan", s.handleScanInventory)
	s.mux.HandleFunc("GET /api/metadata/summary", s.handleMetadataSummary)
	s.mux.HandleFunc("GET /api/metadata/failures", s.handleMetadataFailures)
	s.mux.HandleFunc("GET /api/metadata/missing", s.handleMetadataMissing)
	s.mux.HandleFunc("POST /api/metadata/refresh-missing", s.handleRefreshMissingMetadata)
	s.mux.HandleFunc("POST /api/duplicates/deduplicate", s.handleDeduplicateDuplicates)
	s.mux.HandleFunc("POST /api/thumbnails/gc", s.handleCollectThumbnailGarbage)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("GET /api/events/stream", s.handleEventStream)
	s.mux.HandleFunc("GET /api/jobs", s.handleJobs)
	s.mux.HandleFunc("GET /api/jobs/{job_id}", s.handleJob)
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                          true,
		"event_stream_dropped_events": s.eventStreamDroppedEvents.Load(),
	})
}

func (s *Server) handleStore(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary, err := s.store.Summary()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	roots, err := s.store.SourceRoots()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roots)
}

func (s *Server) handleAddSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Label string `json:"label"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if req.Path == "" {
		writeErrorStatus(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	s.mu.Lock()
	id, err := s.store.AddSourceRoot(req.Path, req.Label)
	s.mu.Unlock()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"source_root_id": id})
}

func (s *Server) handleScans(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	scans, err := s.store.Scans(limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scans)
}

func (s *Server) handleStartSourceScan(w http.ResponseWriter, r *http.Request) {
	job := s.startJob("source_scan", func(progress ProgressFunc) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		scanID, err := s.store.ScanSourcesWithOptions(progress, s.scanOptions)
		if err != nil {
			return "", err
		}
		s.store.EnsureThumbnailsForScan(scanID, progress)
		return scanID, nil
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleStartSingleSourceScan(w http.ResponseWriter, r *http.Request) {
	sourceRootID := r.PathValue("source_root_id")
	job := s.startJob("source_scan", func(progress ProgressFunc) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		scanID, err := s.store.ScanSourceRootsWithOptions([]string{sourceRootID}, progress, s.scanOptions)
		if err != nil {
			return "", err
		}
		s.store.EnsureThumbnailsForScan(scanID, progress)
		return scanID, nil
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleResumeScan(w http.ResponseWriter, r *http.Request) {
	scanID := r.PathValue("scan_id")
	job := s.startJob("source_scan_resume", func(progress ProgressFunc) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		resumedScanID, err := s.store.ResumeSourceScan(scanID, progress)
		if err != nil {
			return "", err
		}
		s.store.EnsureThumbnailsForScan(resumedScanID, progress)
		return resumedScanID, nil
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleScanReport(w http.ResponseWriter, r *http.Request) {
	report, err := s.store.Report(r.PathValue("scan_id"))
	if err != nil {
		if os.IsNotExist(err) {
			writeErrorStatus(w, http.StatusNotFound, err)
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleScanAcquiredFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.AcquiredFiles(r.PathValue("scan_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) handlePhotoYears(w http.ResponseWriter, r *http.Request) {
	resp, err := s.store.PhotoYears()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePhotoMonths(w http.ResponseWriter, r *http.Request) {
	resp, err := s.store.PhotoMonths(r.PathValue("year"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePhotoDays(w http.ResponseWriter, r *http.Request) {
	resp, err := s.store.PhotoDays(r.PathValue("year"), r.PathValue("month"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDatedPhotos(w http.ResponseWriter, r *http.Request) {
	resp, err := s.store.DatedPhotos(r.PathValue("year"), r.PathValue("month"), r.PathValue("day"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUndatedPhotos(w http.ResponseWriter, r *http.Request) {
	resp, err := s.store.UndatedPhotos()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.store.Assets(r.URL.Query())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	asset, err := s.store.Asset(r.PathValue("asset_id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorStatus(w, http.StatusNotFound, err)
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleAssetSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.AssetSources(r.PathValue("asset_id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorStatus(w, http.StatusNotFound, err)
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) handleSetAssetQuality(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quality string `json:"quality"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SetAssetQuality(r.PathValue("asset_id"), req.Quality); err != nil {
		writeAssetCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSetAssetStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SetAssetStatus(r.PathValue("asset_id"), req.Status); err != nil {
		writeAssetCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSetAssetVisibility(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Visibility string `json:"visibility"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SetAssetVisibility(r.PathValue("asset_id"), req.Visibility); err != nil {
		writeAssetCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleApplyAssetLabel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.ApplyAssetLabel(r.PathValue("asset_id"), req.Label); err != nil {
		writeAssetCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRemoveAssetLabel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.RemoveAssetLabel(r.PathValue("asset_id"), req.Label); err != nil {
		writeAssetCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLabels(w http.ResponseWriter, r *http.Request) {
	labels, err := s.store.Labels()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func writeAssetCommandError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorStatus(w, http.StatusNotFound, err)
		return
	}
	writeErrorStatus(w, http.StatusBadRequest, err)
}

func (s *Server) handleStoredObjectBytes(w http.ResponseWriter, r *http.Request) {
	file, err := s.store.CanonicalObjectFile(r.PathValue("stored_object_id"))
	if err != nil {
		writeErrorStatus(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", contentTypeForPath(file.OriginalPath))
	w.Header().Set("Content-Disposition", "inline")
	http.ServeFile(w, r, file.Path)
}

func (s *Server) handleStoredObjectThumbnail(w http.ResponseWriter, r *http.Request) {
	storedObjectID := r.PathValue("stored_object_id")
	path, ok, err := s.store.ThumbnailFile(storedObjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	if ok {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Disposition", "inline")
		http.ServeFile(w, r, path)
		return
	}
	if _, err := s.store.StoredObjectFile(storedObjectID); err != nil {
		writeErrorStatus(w, http.StatusNotFound, err)
		return
	}
	if err := s.store.EnsureThumbnailForObject(storedObjectID); err != nil {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(thumbnailPlaceholderSVG))
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeFile(w, r, path)
}

func (s *Server) handleStoredObjectMetadata(w http.ResponseWriter, r *http.Request) {
	metadata, err := s.store.ObjectMetadata(r.PathValue("stored_object_id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorStatus(w, http.StatusNotFound, err)
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metadata)
}

func (s *Server) handleStoredObjectNavigation(w http.ResponseWriter, r *http.Request) {
	nav, err := s.store.ObjectNavigation(r.PathValue("stored_object_id"), r.URL.Query())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorStatus(w, http.StatusNotFound, err)
			return
		}
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, nav)
}

func (s *Server) handleStoredObjectMap(w http.ResponseWriter, r *http.Request) {
	metadata, err := s.store.ObjectMetadata(r.PathValue("stored_object_id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorStatus(w, http.StatusNotFound, err)
			return
		}
		writeError(w, err)
		return
	}
	coords, ok := gpsCoordinatesFromMetadata(metadata.Fields)
	if !ok {
		writeErrorStatus(w, http.StatusNotFound, errors.New("object has no GPS location"))
		return
	}
	img, err := s.store.renderMapFragment(coords.Lat, coords.Lon)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_ = png.Encode(w, img)
}

func (s *Server) handleInventories(w http.ResponseWriter, r *http.Request) {
	invs, err := s.store.HistoricalInventories()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, invs)
}

func (s *Server) handleAcquireInventory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Label string `json:"label"`
		Group string `json:"group"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if req.Path == "" {
		writeErrorStatus(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	job := s.startJob("inventory_acquire", func(progress ProgressFunc) (string, error) {
		progressf(progress, "acquiring historical inventory %s", req.Path)
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.store.AcquireInventory(req.Path, req.Label, req.Group)
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleScanInventory(w http.ResponseWriter, r *http.Request) {
	invID := r.PathValue("historical_inventory_id")
	var req struct {
		Type          string   `json:"type"`
		Extensions    []string `json:"extensions"`
		ResolverRoot  string   `json:"resolver_root"`
		StripPrefixes []string `json:"strip_prefixes"`
		CaseSensitive bool     `json:"case_sensitive"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	job := s.startJob("inventory_scan", func(progress ProgressFunc) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		scanID, err := s.store.ScanInventoryWithProgress(invID, req.Type, req.Extensions, req.ResolverRoot, req.StripPrefixes, req.CaseSensitive, progress)
		if err != nil {
			return "", err
		}
		s.store.EnsureThumbnailsForScan(scanID, progress)
		return scanID, nil
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleMetadataSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.store.MetadataSummary()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleMetadataFailures(w http.ResponseWriter, r *http.Request) {
	photos, err := s.store.MetadataFailures()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, photos)
}

func (s *Server) handleMetadataMissing(w http.ResponseWriter, r *http.Request) {
	photos, err := s.store.MetadataMissing()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, photos)
}

func (s *Server) handleRefreshMissingMetadata(w http.ResponseWriter, r *http.Request) {
	job := s.startJob("metadata_refresh_missing", func(progress ProgressFunc) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		summary, err := s.store.RefreshMissingMetadata(progress)
		if err != nil {
			return "", err
		}
		return summary.RequestID, nil
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleDeduplicateDuplicates(w http.ResponseWriter, r *http.Request) {
	job := s.startJob("duplicate_deduplication", func(progress ProgressFunc) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		summary, err := s.store.VerifyAndDeduplicate(progress)
		if err != nil {
			return "", err
		}
		return summary.RequestID, nil
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleCollectThumbnailGarbage(w http.ResponseWriter, r *http.Request) {
	job := s.startJob("thumbnail_gc", func(progress ProgressFunc) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		summary, err := s.store.CollectThumbnailGarbage(progress)
		if err != nil {
			return "", err
		}
		progressf(progress, "thumbnail garbage files removed: %d", summary.Files)
		progressf(progress, "thumbnail garbage bytes removed: %d", summary.Bytes)
		return "thumbnail_gc", nil
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.store.RecentEvents(limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.job(r.PathValue("job_id"))
	if !ok {
		writeErrorStatus(w, http.StatusNotFound, errors.New("job not found"))
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.allJobs())
}

func (s *Server) startJob(kind string, work func(ProgressFunc) (string, error)) *Job {
	job := &Job{JobID: newID("job"), Kind: kind, Status: "running", StartedAtMS: nowMS()}
	s.jobsMu.Lock()
	s.jobs[job.JobID] = job
	copyJob := cloneJob(job)
	s.jobsMu.Unlock()
	s.broadcast(ServerEvent{Type: "job_started", Job: copyJob})
	go func() {
		progress := func(message string) {
			displayMessage, current, total := parseProgressMessage(message)
			s.jobsMu.Lock()
			job.Progress = append(job.Progress, displayMessage)
			if current != nil && total != nil {
				job.ProgressCurrent = current
				job.ProgressTotal = total
			}
			copyJob := cloneJob(job)
			s.jobsMu.Unlock()
			s.broadcast(ServerEvent{Type: "job_progress", Job: copyJob})
		}
		result, err := work(progress)
		finished := nowMS()
		s.jobsMu.Lock()
		job.FinishedAtMS = &finished
		if err != nil {
			msg := err.Error()
			job.Error = &msg
			job.Status = "failed"
		} else {
			job.ResultRef = &result
			job.Status = "completed"
		}
		copyJob := cloneJob(job)
		s.pruneCompletedJobsLocked(completedJobRetentionLimit)
		s.jobsMu.Unlock()
		s.broadcast(ServerEvent{Type: "job_finished", Job: copyJob})
		s.broadcast(ServerEvent{Type: "projection_changed"})
	}()
	return copyJob
}

func (s *Server) pruneCompletedJobsLocked(limit int) {
	if limit < 0 {
		limit = 0
	}
	completed := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job.Status != "running" {
			completed = append(completed, job)
		}
	}
	if len(completed) <= limit {
		return
	}
	sort.Slice(completed, func(i, j int) bool {
		return completed[i].StartedAtMS > completed[j].StartedAtMS
	})
	for _, job := range completed[limit:] {
		delete(s.jobs, job.JobID)
	}
}

func (s *Server) jobList() []*Job {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, cloneJob(job))
	}
	return out
}

func (s *Server) job(id string) (*Job, bool) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	return cloneJob(job), true
}

func cloneJob(job *Job) *Job {
	copyJob := *job
	copyJob.Progress = append([]string{}, job.Progress...)
	if job.ProgressCurrent != nil {
		current := *job.ProgressCurrent
		copyJob.ProgressCurrent = &current
	}
	if job.ProgressTotal != nil {
		total := *job.ProgressTotal
		copyJob.ProgressTotal = &total
	}
	return &copyJob
}

func parseProgressMessage(message string) (string, *int, *int) {
	if !strings.HasPrefix(message, progressCountPrefix) {
		return message, nil, nil
	}
	displayMessage := ProgressMessageText(message)
	rest := strings.TrimPrefix(message, progressCountPrefix)
	parts := strings.SplitN(rest, "\x1f", 2)
	if len(parts) != 2 {
		return message, nil, nil
	}
	counts := strings.SplitN(parts[0], "/", 2)
	if len(counts) != 2 {
		return displayMessage, nil, nil
	}
	current, errCurrent := strconv.Atoi(counts[0])
	total, errTotal := strconv.Atoi(counts[1])
	if errCurrent != nil || errTotal != nil || total < 0 || current < 0 {
		return displayMessage, nil, nil
	}
	return displayMessage, &current, &total
}

func (s *Server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if !s.eventStreamOriginAllowed(r) {
		writeErrorStatus(w, http.StatusForbidden, errors.New("event stream origin is not allowed"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorStatus(w, http.StatusInternalServerError, errors.New("event streaming is unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	if err := writeSSEEvent(w, ServerEvent{Type: "job_snapshot", RecordedAtMS: nowMS(), Jobs: s.allJobs()}); err != nil {
		return
	}
	flusher.Flush()
	ch := s.subscribe()
	defer s.unsubscribe(ch)
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, event ServerEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func (s *Server) eventStreamOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	originHost, _ := splitHostPort(parsed.Host)
	requestHost, _ := splitHostPort(r.Host)
	return strings.EqualFold(originHost, requestHost) && s.hostAllowed(r.Host)
}

func (s *Server) subscribe() chan ServerEvent {
	ch := make(chan ServerEvent, 128)
	s.subsMu.Lock()
	s.subs[ch] = struct{}{}
	s.subsMu.Unlock()
	return ch
}

func (s *Server) unsubscribe(ch chan ServerEvent) {
	s.subsMu.Lock()
	delete(s.subs, ch)
	s.subsMu.Unlock()
	close(ch)
}

func (s *Server) broadcast(event ServerEvent) {
	if event.RecordedAtMS == 0 {
		event.RecordedAtMS = nowMS()
	}
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- event:
		default:
			// The SSE stream is a progress notification channel, not the durable
			// source of truth. Slow subscribers can miss updates and resync from
			// /api/jobs; count drops so loss is visible in /api/health.
			s.eventStreamDroppedEvents.Add(1)
		}
	}
}

func splitHostPort(host string) (string, string) {
	host = normalizeHost(host)
	parsedHost, parsedPort, err := net.SplitHostPort(host)
	if err == nil {
		return parsedHost, parsedPort
	}
	return host, ""
}

func (s *Server) allJobs() []*Job {
	jobs := s.jobList()
	liveScans := map[string]struct{}{}
	for _, job := range jobs {
		if job.ResultRef != nil && *job.ResultRef != "" {
			liveScans[*job.ResultRef] = struct{}{}
		}
	}
	persisted, err := s.persistedScanJobs()
	if err == nil {
		for _, job := range persisted {
			if job.ResultRef != nil {
				if _, ok := liveScans[*job.ResultRef]; ok {
					continue
				}
			}
			jobs = append(jobs, job)
		}
	}
	sort.SliceStable(jobs, func(i, j int) bool {
		return jobs[i].StartedAtMS > jobs[j].StartedAtMS
	})
	return jobs
}

func (s *Server) persistedScanJobs() ([]*Job, error) {
	scans, err := s.store.Scans(100)
	if err != nil {
		return nil, err
	}
	out := make([]*Job, 0, len(scans))
	for _, scan := range scans {
		scanID := scan.ScanID
		startedAt := int64(0)
		if scan.StartedAtMS != nil {
			startedAt = *scan.StartedAtMS
		} else if scan.CompletedAtMS != nil {
			startedAt = *scan.CompletedAtMS
		}
		status := scan.Status
		if status == "started" {
			status = "interrupted"
		}
		var finishedAt *int64
		if scan.CompletedAtMS != nil {
			finished := *scan.CompletedAtMS
			finishedAt = &finished
		}
		progress := s.persistedScanProgress(scan)
		out = append(out, &Job{
			JobID:        scanJobID(scanID),
			Kind:         "scan",
			Status:       status,
			StartedAtMS:  startedAt,
			FinishedAtMS: finishedAt,
			ResultRef:    &scanID,
			Progress:     progress,
		})
	}
	return out, nil
}

func (s *Server) persistedScanProgress(scan ScanProjection) []string {
	progress := []string{fmt.Sprintf("scan %s: %s", scan.ScanID, scan.Status)}
	if scan.Report != nil {
		if scan.Report.SourceFilesAcquired != nil {
			progress = append(progress, fmt.Sprintf("source files acquired: %d", *scan.Report.SourceFilesAcquired))
		}
		if scan.Report.DuplicateAcquisitions != nil {
			progress = append(progress, fmt.Sprintf("duplicate acquisitions: %d", *scan.Report.DuplicateAcquisitions))
		}
		if scan.Report.DuplicateGarbageBytes != nil {
			progress = append(progress, fmt.Sprintf("duplicate garbage bytes: %d", *scan.Report.DuplicateGarbageBytes))
		}
	}
	if summary, err := s.store.ThumbnailReport(scan.ScanID); err == nil {
		for _, issue := range summary.Issues {
			progress = append(progress, fmt.Sprintf("thumbnail unavailable for %s (%s; object %s): %s", issue.Filename, issue.Source, issue.StoredObjectID, issue.Error))
		}
		progress = append(progress, fmt.Sprintf("thumbnails generated: %d, already present: %d, unavailable: %d", summary.Generated, summary.Existing, summary.Failed))
		return progress
	}
	if scan.Status == "completed" {
		progress = append(progress, fmt.Sprintf("thumbnail report missing for scan %s", scan.ScanID))
	}
	return progress
}

func scanJobID(scanID string) string {
	return "scan_job_" + scanID
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if s.apiOnly {
		http.NotFound(w, r)
		return
	}
	if err := s.serveBuildFile(w, r); err != nil {
		writeErrorStatus(w, http.StatusServiceUnavailable, err)
		return
	}
}

func (s *Server) serveBuildFile(w http.ResponseWriter, r *http.Request) error {
	if err := ValidateBuildDir(s.buildDir); err != nil {
		return err
	}
	root := s.buildDir
	path := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		path = "index.html"
	}
	if path == "." || path == "" {
		path = "index.html"
	}
	full := filepath.Join(root, path)
	if info, err := os.Stat(full); err != nil || info.IsDir() {
		full = filepath.Join(root, "index.html")
	}
	http.ServeFile(w, r, full)
	return nil
}

func ValidateBuildDir(buildDir string) error {
	if buildDir == "" {
		return errors.New("web UI build directory is required; run `cd web && bun run build`, pass --build-dir, or pass --api-only")
	}
	info, err := os.Stat(buildDir)
	if err != nil {
		return fmt.Errorf("web UI build directory %q is not available: %w; run `cd web && bun run build`, pass --build-dir, or pass --api-only", buildDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("web UI build directory %q is not a directory; pass --build-dir or pass --api-only", buildDir)
	}
	indexPath := filepath.Join(buildDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return fmt.Errorf("web UI build directory %q is missing index.html: %w; run `cd web && bun run build`, pass --build-dir, or pass --api-only", buildDir, err)
	}
	return nil
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	writeErrorStatus(w, http.StatusInternalServerError, err)
}

func writeErrorStatus(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": fmt.Sprint(err)})
}

func contentTypeForPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}

type gpsCoordinates struct {
	Lat float64
	Lon float64
}

func gpsCoordinatesFromMetadata(fields map[string]map[string]string) (gpsCoordinates, bool) {
	lat, ok := gpsCoordinateFromMetadata(fields, "gps_latitude", "gps_latitude_ref")
	if !ok {
		return gpsCoordinates{}, false
	}
	lon, ok := gpsCoordinateFromMetadata(fields, "gps_longitude", "gps_longitude_ref")
	if !ok {
		return gpsCoordinates{}, false
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return gpsCoordinates{}, false
	}
	return gpsCoordinates{Lat: lat, Lon: lon}, true
}

func gpsCoordinateFromMetadata(fields map[string]map[string]string, valueKey string, refKey string) (float64, bool) {
	raw := strings.TrimSpace(fields[valueKey]["raw"])
	if raw == "" {
		return 0, false
	}
	parts := strings.Split(raw, ",")
	if len(parts) < 3 {
		return 0, false
	}
	degrees, ok := rationalFloat(strings.TrimSpace(parts[0]))
	if !ok {
		return 0, false
	}
	minutes, ok := rationalFloat(strings.TrimSpace(parts[1]))
	if !ok {
		return 0, false
	}
	seconds, ok := rationalFloat(strings.TrimSpace(parts[2]))
	if !ok {
		return 0, false
	}
	value := degrees + minutes/60 + seconds/3600
	switch strings.ToUpper(strings.TrimSpace(fields[refKey]["raw"])) {
	case "S", "W":
		value *= -1
	case "N", "E", "":
	default:
		return 0, false
	}
	return value, true
}

func rationalFloat(raw string) (float64, bool) {
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		value, err := strconv.ParseFloat(raw, 64)
		return value, err == nil && isFiniteFloat(value)
	}
	num, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, false
	}
	den, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || den == 0 {
		return 0, false
	}
	value := num / den
	return value, isFiniteFloat(value)
}

func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

const (
	mapTileSize        = 256
	mapFragmentWidth   = 520
	mapFragmentHeight  = 260
	mapFragmentZoom    = 15
	mapTileCacheMaxAge = 7 * 24 * time.Hour
)

var mapTileHTTPClient = &http.Client{Timeout: 15 * time.Second}

func (s *Store) renderMapFragment(lat float64, lon float64) (image.Image, error) {
	centerX, centerY := lonLatToGlobalPixel(lat, lon, mapFragmentZoom)
	left := centerX - float64(mapFragmentWidth)/2
	top := centerY - float64(mapFragmentHeight)/2
	firstTileX := int(math.Floor(left / mapTileSize))
	firstTileY := int(math.Floor(top / mapTileSize))
	lastTileX := int(math.Floor((left + mapFragmentWidth - 1) / mapTileSize))
	lastTileY := int(math.Floor((top + mapFragmentHeight - 1) / mapTileSize))

	canvas := image.NewRGBA(image.Rect(0, 0, mapFragmentWidth, mapFragmentHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{R: 238, G: 241, B: 245, A: 255}), image.Point{}, draw.Src)
	for tileY := firstTileY; tileY <= lastTileY; tileY++ {
		for tileX := firstTileX; tileX <= lastTileX; tileX++ {
			tile, err := s.openMapTile(mapFragmentZoom, tileX, tileY)
			if err != nil {
				return nil, err
			}
			dstX := int(math.Round(float64(tileX*mapTileSize) - left))
			dstY := int(math.Round(float64(tileY*mapTileSize) - top))
			draw.Draw(canvas, image.Rect(dstX, dstY, dstX+mapTileSize, dstY+mapTileSize), tile, image.Point{}, draw.Over)
		}
	}
	drawMapMarker(canvas, mapFragmentWidth/2, mapFragmentHeight/2)
	return canvas, nil
}

func lonLatToGlobalPixel(lat float64, lon float64, zoom int) (float64, float64) {
	sinLat := math.Sin(lat * math.Pi / 180)
	tileCount := 1 << uint(zoom)
	scale := float64(mapTileSize * tileCount)
	x := (lon + 180) / 360 * scale
	y := (0.5 - math.Log((1+sinLat)/(1-sinLat))/(4*math.Pi)) * scale
	return x, y
}

func (s *Store) openMapTile(z int, x int, y int) (image.Image, error) {
	path := s.mapTileCachePath(z, x, y)
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < mapTileCacheMaxAge {
		tile, err := decodeMapTile(path)
		if err == nil {
			return tile, nil
		}
		_ = os.Remove(path)
	}
	if err := s.downloadMapTile(z, x, y, path); err != nil {
		return nil, err
	}
	return decodeMapTile(path)
}

func (s *Store) mapTileCachePath(z int, x int, y int) string {
	return filepath.Join(s.Root, "map-tiles", "openstreetmap", fmt.Sprint(z), fmt.Sprint(x), fmt.Sprintf("%d.png", y))
}

func (s *Store) downloadMapTile(z int, x int, y int, path string) error {
	tileURL := mapTileURL(z, x, y)
	req, err := http.NewRequest(http.MethodGet, tileURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "photostore/0 (+https://github.com/anicolao/photostore)")
	res, err := mapTileHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("map tile %s returned %s", tileURL, res.Status)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tile-*.png")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := io.Copy(tmp, res.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func mapTileURL(z int, x int, y int) string {
	template := os.Getenv("PHOTOSTORE_MAP_TILE_URL_TEMPLATE")
	if template == "" {
		template = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	}
	out := strings.ReplaceAll(template, "{z}", fmt.Sprint(z))
	out = strings.ReplaceAll(out, "{x}", fmt.Sprint(x))
	out = strings.ReplaceAll(out, "{y}", fmt.Sprint(y))
	return out
}

func decodeMapTile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func drawMapMarker(img *image.RGBA, cx int, cy int) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 235}
	red := color.RGBA{R: 197, G: 34, B: 31, A: 255}
	for y := -12; y <= 12; y++ {
		for x := -12; x <= 12; x++ {
			dist := math.Sqrt(float64(x*x + y*y))
			if dist <= 12 {
				img.Set(cx+x, cy+y, white)
			}
			if dist <= 7 {
				img.Set(cx+x, cy+y, red)
			}
			if dist <= 3 {
				img.Set(cx+x, cy+y, color.White)
			}
		}
	}
}
