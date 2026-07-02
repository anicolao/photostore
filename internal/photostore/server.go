package photostore

import (
	"bufio"
	"crypto/sha1"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed static/*
var fallbackStatic embed.FS

type ServerOptions struct {
	APIOnly bool
}

type Server struct {
	store   *Store
	apiOnly bool
	mux     *http.ServeMux
	mu      sync.Mutex
	jobsMu  sync.Mutex
	jobs    map[string]*Job
	subsMu  sync.Mutex
	subs    map[chan ServerEvent]struct{}
}

type Job struct {
	JobID        string   `json:"job_id"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	StartedAtMS  int64    `json:"started_at_ms"`
	FinishedAtMS *int64   `json:"finished_at_ms"`
	ResultRef    *string  `json:"result_ref"`
	Error        *string  `json:"error"`
	Progress     []string `json:"progress"`
}

type ServerEvent struct {
	Type         string `json:"type"`
	RecordedAtMS int64  `json:"recorded_at_ms"`
	Job          *Job   `json:"job,omitempty"`
	Jobs         []*Job `json:"jobs,omitempty"`
}

func NewServer(store *Store, opts ServerOptions) http.Handler {
	s := &Server{
		store:   store,
		apiOnly: opts.APIOnly,
		mux:     http.NewServeMux(),
		jobs:    map[string]*Job{},
		subs:    map[chan ServerEvent]struct{}{},
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
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
	s.mux.HandleFunc("GET /api/photos/dates", s.handlePhotoYears)
	s.mux.HandleFunc("GET /api/photos/dates/{year}", s.handlePhotoMonths)
	s.mux.HandleFunc("GET /api/photos/dates/{year}/{month}", s.handlePhotoDays)
	s.mux.HandleFunc("GET /api/photos/dates/{year}/{month}/{day}", s.handleDatedPhotos)
	s.mux.HandleFunc("GET /api/photos/undated", s.handleUndatedPhotos)
	s.mux.HandleFunc("GET /api/inventories", s.handleInventories)
	s.mux.HandleFunc("POST /api/inventories/acquire", s.handleAcquireInventory)
	s.mux.HandleFunc("POST /api/inventories/{historical_inventory_id}/scan", s.handleScanInventory)
	s.mux.HandleFunc("POST /api/metadata/refresh-missing", s.handleRefreshMissingMetadata)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("GET /api/events/ws", s.handleEventWebSocket)
	s.mux.HandleFunc("GET /api/jobs", s.handleJobs)
	s.mux.HandleFunc("GET /api/jobs/{job_id}", s.handleJob)
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStore(w http.ResponseWriter, r *http.Request) {
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
		scanID, err := s.store.ScanSources(progress)
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
		scanID, err := s.store.ScanSourceRoots([]string{sourceRootID}, progress)
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

func (s *Server) handleStoredObjectBytes(w http.ResponseWriter, r *http.Request) {
	file, err := s.store.StoredObjectFile(r.PathValue("stored_object_id"))
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
			s.jobsMu.Lock()
			job.Progress = append(job.Progress, message)
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
		s.jobsMu.Unlock()
		s.broadcast(ServerEvent{Type: "job_finished", Job: copyJob})
		s.broadcast(ServerEvent{Type: "projection_changed"})
	}()
	return copyJob
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
	copyJob.Progress = append([]string(nil), job.Progress...)
	return &copyJob
}

func (s *Server) handleEventWebSocket(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		writeErrorStatus(w, http.StatusBadRequest, errors.New("websocket upgrade is required"))
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		writeErrorStatus(w, http.StatusBadRequest, errors.New("Sec-WebSocket-Key is required"))
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeErrorStatus(w, http.StatusInternalServerError, errors.New("websocket hijacking is unavailable"))
		return
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	if err := writeWebSocketUpgrade(rw, key); err != nil {
		return
	}
	ch := s.subscribe()
	defer s.unsubscribe(ch)
	if err := writeWebSocketJSON(conn, ServerEvent{Type: "job_snapshot", RecordedAtMS: nowMS(), Jobs: s.allJobs()}); err != nil {
		return
	}
	for event := range ch {
		if err := writeWebSocketJSON(conn, event); err != nil {
			return
		}
	}
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
		}
	}
}

func writeWebSocketUpgrade(rw *bufio.ReadWriter, key string) error {
	_, err := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", webSocketAccept(key))
	if err != nil {
		return err
	}
	return rw.Flush()
}

func webSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func writeWebSocketJSON(conn net.Conn, event ServerEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return writeWebSocketText(conn, payload)
}

func writeWebSocketText(w io.Writer, payload []byte) error {
	header := []byte{0x81}
	switch n := len(payload); {
	case n <= 125:
		header = append(header, byte(n))
	case n <= 65535:
		header = append(header, 126, byte(n>>8), byte(n))
	default:
		header = append(header, 127)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(n))
		header = append(header, size[:]...)
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
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
	if serveBuildFile(w, r) {
		return
	}
	if r.URL.Path != "/" {
		r = r.Clone(r.Context())
		r.URL.Path = "/"
	}
	fsys, _ := fs.Sub(fallbackStatic, "static")
	http.FileServer(http.FS(fsys)).ServeHTTP(w, r)
}

func serveBuildFile(w http.ResponseWriter, r *http.Request) bool {
	root := filepath.Join("web", "build")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return false
	}
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
	return true
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
