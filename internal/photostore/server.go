package photostore

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
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

func NewServer(store *Store, opts ServerOptions) http.Handler {
	s := &Server{
		store:   store,
		apiOnly: opts.APIOnly,
		mux:     http.NewServeMux(),
		jobs:    map[string]*Job{},
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
	s.mux.HandleFunc("GET /api/scans/{scan_id}/report", s.handleScanReport)
	s.mux.HandleFunc("GET /api/inventories", s.handleInventories)
	s.mux.HandleFunc("POST /api/inventories/acquire", s.handleAcquireInventory)
	s.mux.HandleFunc("POST /api/inventories/{historical_inventory_id}/scan", s.handleScanInventory)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
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
		return s.store.ScanSources(progress)
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleStartSingleSourceScan(w http.ResponseWriter, r *http.Request) {
	sourceRootID := r.PathValue("source_root_id")
	job := s.startJob("source_scan", func(progress ProgressFunc) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.store.ScanSourceRoots([]string{sourceRootID}, progress)
	})
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleScanReport(w http.ResponseWriter, r *http.Request) {
	report, err := s.store.Report(r.PathValue("scan_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
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
		return s.store.ScanInventoryWithProgress(invID, req.Type, req.Extensions, req.ResolverRoot, req.StripPrefixes, req.CaseSensitive, progress)
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

func (s *Server) startJob(kind string, work func(ProgressFunc) (string, error)) *Job {
	job := &Job{JobID: newID("job"), Kind: kind, Status: "running", StartedAtMS: nowMS()}
	s.jobsMu.Lock()
	s.jobs[job.JobID] = job
	s.jobsMu.Unlock()
	go func() {
		progress := func(message string) {
			s.jobsMu.Lock()
			job.Progress = append(job.Progress, message)
			s.jobsMu.Unlock()
		}
		result, err := work(progress)
		finished := nowMS()
		s.jobsMu.Lock()
		defer s.jobsMu.Unlock()
		job.FinishedAtMS = &finished
		if err != nil {
			msg := err.Error()
			job.Error = &msg
			job.Status = "failed"
			return
		}
		job.ResultRef = &result
		job.Status = "completed"
	}()
	copyJob := *job
	return &copyJob
}

func (s *Server) job(id string) (*Job, bool) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	copyJob := *job
	copyJob.Progress = append([]string(nil), job.Progress...)
	return &copyJob, true
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
