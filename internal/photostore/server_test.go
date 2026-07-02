package photostore

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerDashboardAPIsAndSourceScanJob(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), []byte("one"))
	mustWrite(t, filepath.Join(sourcePath, "B.jpeg"), []byte("one"))

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
	if summary.RetainedDuplicateBytes != int64(len("one")) {
		t.Fatalf("retained duplicate bytes = %d, want %d", summary.RetainedDuplicateBytes, len("one"))
	}
	getJSON(t, ts.URL+"/api/sources", &sources)
	if sources[0].LastScanID == nil || *sources[0].LastScanID != *done.ResultRef {
		t.Fatalf("last scan id = %#v, want %s", sources[0].LastScanID, *done.ResultRef)
	}
	if sources[0].LastScanCompletedAtMS == nil {
		t.Fatal("missing last scan completed timestamp")
	}
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
