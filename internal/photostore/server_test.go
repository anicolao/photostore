package photostore

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	var summary StoreSummary
	getJSON(t, ts.URL+"/api/store", &summary)
	if summary.RetainedDuplicateBytes != int64(len("one")) {
		t.Fatalf("retained duplicate bytes = %d, want %d", summary.RetainedDuplicateBytes, len("one"))
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
