package photostore

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestCloneJobSerializesEmptyProgressArray(t *testing.T) {
	job := cloneJob(&Job{JobID: "job_test", Kind: "source_scan", Status: "running", StartedAtMS: 1710504000000})
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"progress":null`) {
		t.Fatalf("job JSON has null progress: %s", raw)
	}
	if !strings.Contains(string(raw), `"progress":[]`) {
		t.Fatalf("job JSON missing empty progress array: %s", raw)
	}
}

func TestServerPrunesOnlyOldCompletedJobs(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server := NewServer(st, ServerOptions{}).(*Server)
	server.jobsMu.Lock()
	defer server.jobsMu.Unlock()
	for i := 0; i < completedJobRetentionLimit+5; i++ {
		id := fmt.Sprintf("job_done_%03d", i)
		server.jobs[id] = &Job{JobID: id, Kind: "test", Status: "completed", StartedAtMS: int64(i)}
	}
	server.jobs["job_running"] = &Job{JobID: "job_running", Kind: "test", Status: "running", StartedAtMS: -1}
	server.pruneCompletedJobsLocked(completedJobRetentionLimit)
	got := len(server.jobs)
	if got != completedJobRetentionLimit+1 {
		t.Fatalf("jobs retained = %d, want %d", got, completedJobRetentionLimit+1)
	}
	if _, ok := server.jobs["job_running"]; !ok {
		t.Fatal("running job was pruned")
	}
	if _, ok := server.jobs["job_done_000"]; ok {
		t.Fatal("oldest completed job was retained")
	}
	if _, ok := server.jobs["job_done_104"]; !ok {
		t.Fatal("newest completed job was pruned")
	}
}

func TestServerDashboardAPIsAndSourceScanJob(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	jpegBytes := testJPEG(t)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), jpegBytes)
	mustWrite(t, filepath.Join(sourcePath, "B.jpeg"), jpegBytes)

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()

	postJSON(t, ts.URL+"/api/sources", map[string]string{"path": sourcePath, "label": "fixture"}, http.StatusCreated)
	var sources []SourceRoot
	getJSON(t, ts.URL+"/api/sources", &sources)
	if len(sources) != 1 || sources[0].Label != "fixture" {
		t.Fatalf("sources = %#v, want fixture source", sources)
	}

	var started Job
	postJSONInto(t, ts.URL+"/api/scans", map[string]string{}, http.StatusAccepted, &started)
	done := waitJob(t, ts.URL, started.JobID)
	if done.Status != "completed" {
		t.Fatalf("job status = %s, error = %v", done.Status, done.Error)
	}
	if done.ResultRef == nil || *done.ResultRef == "" {
		t.Fatalf("missing scan result ref: %#v", done)
	}
	var report ScanReport
	getJSON(t, ts.URL+"/api/scans/"+*done.ResultRef+"/report", &report)
	if report.SourceFilesAcquired != 2 {
		t.Fatalf("source files acquired = %d, want 2", report.SourceFilesAcquired)
	}
	if report.DuplicateAcquisitions != 1 {
		t.Fatalf("duplicate acquisitions = %d, want 1", report.DuplicateAcquisitions)
	}
	var acquired []AcquiredFileProjection
	getJSON(t, ts.URL+"/api/scans/"+*done.ResultRef+"/acquired", &acquired)
	if len(acquired) != 2 {
		t.Fatalf("acquired files = %d, want 2", len(acquired))
	}
	if acquired[0].ViewURL == "" {
		t.Fatalf("missing view url in acquired file: %#v", acquired[0])
	}
	if acquired[0].BytesURL == "" {
		t.Fatalf("missing bytes url in acquired file: %#v", acquired[0])
	}
	if acquired[0].ThumbnailURL == "" {
		t.Fatalf("missing thumbnail url in acquired file: %#v", acquired[0])
	}
	if acquired[0].Filename == "" {
		t.Fatalf("missing filename in acquired file: %#v", acquired[0])
	}
	var metadataSummary MetadataSummaryProjection
	getJSON(t, ts.URL+"/api/metadata/summary", &metadataSummary)
	if metadataSummary.FailedCount != 1 {
		t.Fatalf("metadata failed count = %d, want 1 for fixture without EXIF", metadataSummary.FailedCount)
	}
	var metadataFailures []MetadataPhotoProjection
	getJSON(t, ts.URL+"/api/metadata/failures", &metadataFailures)
	if len(metadataFailures) != 1 {
		t.Fatalf("metadata failures = %d, want 1", len(metadataFailures))
	}
	if metadataFailures[0].ErrorMessage == "" {
		t.Fatalf("metadata failure missing error message: %#v", metadataFailures[0])
	}
	if metadataFailures[0].Width == 0 || metadataFailures[0].Height == 0 || metadataFailures[0].PixelCount == 0 {
		t.Fatalf("metadata failure missing dimensions: %#v", metadataFailures[0])
	}
	var metadataMissing []MetadataPhotoProjection
	getJSON(t, ts.URL+"/api/metadata/missing", &metadataMissing)
	if len(metadataMissing) != 0 {
		t.Fatalf("metadata missing = %d, want 0", len(metadataMissing))
	}
	res, err := http.Get(ts.URL + acquired[0].BytesURL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("object bytes status = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", got)
	}
	thumb, err := http.Get(ts.URL + acquired[0].ThumbnailURL)
	if err != nil {
		t.Fatal(err)
	}
	defer thumb.Body.Close()
	if thumb.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail status = %d, want 200", thumb.StatusCode)
	}
	if got := thumb.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("thumbnail content type = %q, want image/jpeg", got)
	}
	thumbPath, _, err := st.ThumbnailFile(acquired[0].StoredObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(thumbPath); err != nil {
		t.Fatal(err)
	}
	regenerated, err := http.Get(ts.URL + acquired[0].ThumbnailURL)
	if err != nil {
		t.Fatal(err)
	}
	defer regenerated.Body.Close()
	if got := regenerated.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("regenerated thumbnail content type = %q, want image/jpeg", got)
	}
	var scans []ScanProjection
	getJSON(t, ts.URL+"/api/scans", &scans)
	if len(scans) != 1 || scans[0].ScanID != *done.ResultRef {
		t.Fatalf("scans = %#v, want completed scan %s", scans, *done.ResultRef)
	}
	if scans[0].Report == nil || scans[0].Report.DuplicateAcquisitions == nil || *scans[0].Report.DuplicateAcquisitions != 1 {
		t.Fatalf("scan report duplicate acquisitions = %#v, want known 1", scans[0].Report)
	}
	var summary StoreSummary
	getJSON(t, ts.URL+"/api/store", &summary)
	if summary.RetainedDuplicateBytes != int64(len(jpegBytes)) {
		t.Fatalf("retained duplicate bytes = %d, want %d", summary.RetainedDuplicateBytes, len(jpegBytes))
	}
	getJSON(t, ts.URL+"/api/sources", &sources)
	if sources[0].LastScanID == nil || *sources[0].LastScanID != *done.ResultRef {
		t.Fatalf("last scan id = %#v, want %s", sources[0].LastScanID, *done.ResultRef)
	}
	if sources[0].LastScanCompletedAtMS == nil {
		t.Fatal("missing last scan completed timestamp")
	}
}

func TestServerRejectsDisallowedHost(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(NewServer(st, ServerOptions{ListenAddr: "127.0.0.1:8080"}))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.test"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("disallowed host status = %d, want 403", res.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "127.0.0.1:8080"
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("allowed host status = %d, want 200", res.StatusCode)
	}
}

func TestServerRequiresJSONContentTypeForMutations(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()

	res, err := http.Post(ts.URL+"/api/sources", "text/plain", strings.NewReader(`{"path":"/tmp"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain mutation status = %d, want 415", res.StatusCode)
	}
}

func TestServerJobEventsStreamSendsSnapshotAndProgress(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), testJPEG(t))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()

	res, reader := openEventStream(t, ts.URL, "")
	defer res.Body.Close()
	initial := readSSEEvent(t, res, reader)
	if initial.Type != "job_snapshot" {
		t.Fatalf("initial event type = %q, want job_snapshot", initial.Type)
	}

	postJSON(t, ts.URL+"/api/sources", map[string]string{"path": sourcePath, "label": "fixture"}, http.StatusCreated)
	var started Job
	postJSONInto(t, ts.URL+"/api/scans", map[string]string{}, http.StatusAccepted, &started)

	progressSeen := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		event := readSSEEvent(t, res, reader)
		if event.Type == "job_progress" && event.Job != nil && event.Job.JobID == started.JobID && len(event.Job.Progress) > 0 {
			progressSeen = true
		}
		if event.Type == "job_finished" && event.Job != nil && event.Job.JobID == started.JobID {
			if event.Job.Status != "completed" {
				t.Fatalf("finished job status = %s, want completed", event.Job.Status)
			}
			if !progressSeen {
				t.Fatal("job finished before any progress event was observed")
			}
			return
		}
	}
	t.Fatal("timed out waiting for event stream job_finished event")
}

func TestServerJobsIncludePersistedScanJobsAfterRestart(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), testJPEG(t))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	postJSON(t, ts.URL+"/api/sources", map[string]string{"path": sourcePath, "label": "fixture"}, http.StatusCreated)
	var started Job
	postJSONInto(t, ts.URL+"/api/scans", map[string]string{}, http.StatusAccepted, &started)
	done := waitJob(t, ts.URL, started.JobID)
	if done.Status != "completed" {
		t.Fatalf("job status = %s, error = %v", done.Status, done.Error)
	}
	ts.Close()

	restarted := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer restarted.Close()
	var jobs []Job
	getJSON(t, restarted.URL+"/api/jobs", &jobs)
	if len(jobs) == 0 {
		t.Fatal("no jobs returned after server restart")
	}
	if jobs[0].JobID != scanJobID(*done.ResultRef) {
		t.Fatalf("job id = %s, want persisted scan job %s", jobs[0].JobID, scanJobID(*done.ResultRef))
	}
	if jobs[0].Status != "completed" {
		t.Fatalf("persisted job status = %s, want completed", jobs[0].Status)
	}
	if jobs[0].ResultRef == nil || *jobs[0].ResultRef != *done.ResultRef {
		t.Fatalf("persisted job result = %#v, want %s", jobs[0].ResultRef, *done.ResultRef)
	}
	if !strings.Contains(strings.Join(jobs[0].Progress, "\n"), "thumbnails generated: 1, already present: 0, unavailable: 0") {
		t.Fatalf("persisted job progress missing thumbnail summary: %#v", jobs[0].Progress)
	}
}

func TestServerServesThumbnailPlaceholderWhenGenerationFails(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "bad.JPG"), []byte("not a jpeg"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()

	postJSON(t, ts.URL+"/api/sources", map[string]string{"path": sourcePath, "label": "fixture"}, http.StatusCreated)
	var started Job
	postJSONInto(t, ts.URL+"/api/scans", map[string]string{}, http.StatusAccepted, &started)
	done := waitJob(t, ts.URL, started.JobID)
	if done.Status != "completed" {
		t.Fatalf("job status = %s, error = %v", done.Status, done.Error)
	}
	progress := strings.Join(done.Progress, "\n")
	if !strings.Contains(progress, "thumbnail unavailable for bad.JPG (bad.JPG; object ") {
		t.Fatalf("thumbnail progress did not identify unavailable file: %s", progress)
	}
	if !strings.Contains(progress, "thumbnails generated: 0, already present: 0, unavailable: 1") {
		t.Fatalf("thumbnail summary is not actionable: %s", progress)
	}
	var acquired []AcquiredFileProjection
	getJSON(t, ts.URL+"/api/scans/"+*done.ResultRef+"/acquired", &acquired)
	if len(acquired) != 1 {
		t.Fatalf("acquired files = %d, want 1", len(acquired))
	}
	placeholder, err := http.Get(ts.URL + acquired[0].ThumbnailURL)
	if err != nil {
		t.Fatal(err)
	}
	defer placeholder.Body.Close()
	if got := placeholder.Header.Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("placeholder content type = %q, want image/svg+xml", got)
	}
}

func openEventStream(t *testing.T, baseURL, origin string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		t.Fatalf("event stream status = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		res.Body.Close()
		t.Fatalf("event stream content type = %q, want text/event-stream", got)
	}
	return res, bufio.NewReader(res.Body)
}

func TestServerRejectsCrossOriginEventStream(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://evil.test")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin event stream status = %d, want 403", res.StatusCode)
	}
}

func readSSEEvent(t *testing.T, res *http.Response, reader *bufio.Reader) ServerEvent {
	t.Helper()
	type result struct {
		event ServerEvent
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var data string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- result{err: err}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if data == "" {
					continue
				}
				var event ServerEvent
				err := json.Unmarshal([]byte(data), &event)
				done <- result{event: event, err: err}
				return
			}
			if strings.HasPrefix(line, "data: ") {
				data += strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.event
	case <-time.After(2 * time.Second):
		res.Body.Close()
		t.Fatal("timed out waiting for event stream event")
		return ServerEvent{}
	}
}

func TestServerServesExistingThumbnailWhileDatabaseIsLocked(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), testJPEG(t))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sourceID, err := st.AddSourceRoot(sourcePath, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	scanID, err := st.ScanSourceRoots([]string{sourceID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	st.EnsureThumbnailsForScan(scanID, nil)
	acquired, err := st.AcquiredFiles(scanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(acquired) != 1 {
		t.Fatalf("acquired files = %d, want 1", len(acquired))
	}
	lockDB, err := sql.Open("sqlite", filepath.Join(storePath, "projections.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer lockDB.Close()
	if _, err := lockDB.Exec(`pragma busy_timeout = 1`); err != nil {
		t.Fatal(err)
	}
	tx, err := lockDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`insert into events_applied(event_id,event_type,recorded_at_ms) values(?,?,?)`, "evt_external_lock", "TestLock", int64(1)); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()
	res, err := http.Get(ts.URL + acquired[0].ThumbnailURL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail status = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("thumbnail content type = %q, want image/jpeg", got)
	}
}

func TestStoreConfiguresSQLiteForInteractiveReadsDuringWrites(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var busyTimeout int
	if err := st.DB.QueryRow(`pragma busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout < 10000 {
		t.Fatalf("busy_timeout = %d, want at least 10000", busyTimeout)
	}
	var journalMode string
	if err := st.DB.QueryRow(`pragma journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 18))
	for y := 0; y < 18; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: uint8(80 + x*5), G: uint8(100 + y*6), B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestServerCanScanSingleSourceRoot(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourceA := filepath.Join(root, "a")
	sourceB := filepath.Join(root, "b")
	mustMkdir(t, sourceA)
	mustMkdir(t, sourceB)
	mustWrite(t, filepath.Join(sourceA, "A.JPG"), []byte("a"))
	mustWrite(t, filepath.Join(sourceB, "B.JPG"), []byte("b"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	idA, err := st.AddSourceRoot(sourceA, "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSourceRoot(sourceB, "b"); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()

	var started Job
	postJSONInto(t, ts.URL+"/api/sources/"+idA+"/scan", map[string]string{}, http.StatusAccepted, &started)
	done := waitJob(t, ts.URL, started.JobID)
	if done.Status != "completed" {
		t.Fatalf("job status = %s, error = %v", done.Status, done.Error)
	}
	var report ScanReport
	getJSON(t, ts.URL+"/api/scans/"+*done.ResultRef+"/report", &report)
	if report.SourceRootsScanned != 1 {
		t.Fatalf("source roots scanned = %d, want 1", report.SourceRootsScanned)
	}
	if report.SourceFilesAcquired != 1 {
		t.Fatalf("source files acquired = %d, want 1", report.SourceFilesAcquired)
	}
}

func TestServerCanResumeStartedSourceScan(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), testJPEG(t))
	mustWrite(t, filepath.Join(sourcePath, "B.JPG"), testJPEG(t))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sourceID, err := st.AddSourceRoot(sourcePath, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	scanID := "scan_interrupted"
	if err := st.appendEvent("SourceRootScanRequested", nil, &scanID, map[string]any{
		"scan_id":              scanID,
		"source_root_ids":      []string{sourceID},
		"candidate_extensions": []string{".jpg", ".jpeg"},
		"requested_by":         "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.appendEvent("IngestionScanStarted", nil, &scanID, map[string]any{
		"scan_id":       scanID,
		"started_at_ms": int64(123),
	}); err != nil {
		t.Fatal(err)
	}
	entryID, err := st.appendEventReturnID("SourceEntryObserved", nil, &scanID, map[string]any{
		"scan_id":                 scanID,
		"source_root_id":          sourceID,
		"source_kind":             "source_root",
		"path":                    filepath.Join(sourcePath, "A.JPG"),
		"relative_path":           "A.JPG",
		"historical_inventory_id": nil,
		"inventory_entry_id":      nil,
		"entry_type":              "regular_file",
		"filesystem":              statPayload(filepath.Join(sourcePath, "A.JPG")),
		"candidate_reason": map[string]any{
			"method":    "extension",
			"extension": ".jpg",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	partialReport := &ScanReport{ScanID: scanID}
	if err := st.acquireSourceFile(scanID, &entryID, sourceID, "source_root", filepath.Join(sourcePath, "A.JPG"), "A.JPG", "", "", partialReport, nil); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()
	var started Job
	postJSONInto(t, ts.URL+"/api/scans/"+scanID+"/resume", map[string]string{}, http.StatusAccepted, &started)
	done := waitJob(t, ts.URL, started.JobID)
	if done.Status != "completed" {
		t.Fatalf("resume job status = %s, error = %v", done.Status, done.Error)
	}
	if done.ResultRef == nil || *done.ResultRef != scanID {
		t.Fatalf("resume result ref = %#v, want %s", done.ResultRef, scanID)
	}
	var report ScanReport
	getJSON(t, ts.URL+"/api/scans/"+scanID+"/report", &report)
	if report.SourceFilesAcquired != 2 {
		t.Fatalf("source files acquired = %d, want 2", report.SourceFilesAcquired)
	}
	var acquired []AcquiredFileProjection
	getJSON(t, ts.URL+"/api/scans/"+scanID+"/acquired", &acquired)
	if len(acquired) != 2 {
		t.Fatalf("acquired files = %d, want 2", len(acquired))
	}
}

func TestScansPreserveUnknownDuplicateStats(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	scanID := "scan_legacy"
	if _, err := st.DB.Exec(`insert into scans(scan_id,status,completed_at_ms,stats_json) values(?,?,?,?)`, scanID, "completed", int64(123), `{"source_files_acquired":533}`); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(st.Root, "reports", "scan-"+scanID+".json")
	if err := os.WriteFile(reportPath, []byte(`{"scan_id":"scan_legacy","source_files_acquired":533}`), 0o644); err != nil {
		t.Fatal(err)
	}

	scans, err := st.Scans(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 1 {
		t.Fatalf("scans = %d, want 1", len(scans))
	}
	if scans[0].Report == nil {
		t.Fatal("missing scan report projection")
	}
	if scans[0].Report.SourceFilesAcquired == nil || *scans[0].Report.SourceFilesAcquired != 533 {
		t.Fatalf("source files acquired = %#v, want known 533", scans[0].Report.SourceFilesAcquired)
	}
	if scans[0].Report.DuplicateAcquisitions != nil {
		t.Fatalf("duplicate acquisitions = %#v, want unknown", scans[0].Report.DuplicateAcquisitions)
	}
	if scans[0].Report.DuplicateGarbageBytes != nil {
		t.Fatalf("duplicate garbage bytes = %#v, want unknown", scans[0].Report.DuplicateGarbageBytes)
	}
}

func TestMissingScanReportIsNotFound(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	scanID := "scan_started_without_report"
	if _, err := st.DB.Exec(`insert into scans(scan_id,status,started_at_ms,stats_json) values(?,?,?,?)`, scanID, "started", int64(123), `{}`); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewServer(st, ServerOptions{}))
	defer ts.Close()
	res, err := http.Get(ts.URL + "/api/scans/" + scanID + "/report")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing report status = %d, want 404", res.StatusCode)
	}
}

func TestServeStaticFallback(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	NewServer(st, ServerOptions{}).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("Photostore")) {
		t.Fatalf("fallback page did not render Photostore title")
	}
}

func TestServeBuildFileRequiresExplicitBuildDir(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	buildDir := filepath.Join(t.TempDir(), "build")
	mustMkdir(t, buildDir)
	mustWrite(t, filepath.Join(buildDir, "index.html"), []byte("Custom Build"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	NewServer(st, ServerOptions{}).ServeHTTP(rr, req)
	if bytes.Contains(rr.Body.Bytes(), []byte("Custom Build")) {
		t.Fatal("served build directory without explicit BuildDir")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rr = httptest.NewRecorder()
	NewServer(st, ServerOptions{BuildDir: buildDir}).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("Custom Build")) {
		t.Fatalf("explicit build dir response = %q, want custom build", rr.Body.String())
	}
}

func postJSON(t *testing.T, url string, body any, wantStatus int) {
	t.Helper()
	postJSONInto(t, url, body, wantStatus, nil)
}

func postJSONInto(t *testing.T, url string, body any, wantStatus int, out any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		t.Fatalf("POST %s status = %d, want %d", url, res.StatusCode, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func waitJob(t *testing.T, baseURL, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var job Job
		getJSON(t, baseURL+"/api/jobs/"+id, &job)
		if job.Status != "running" {
			return job
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for job %s", id)
	return Job{}
}
