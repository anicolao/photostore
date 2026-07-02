package photostore

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

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

func TestServerJobEventsWebSocketStreamsSnapshotAndProgress(t *testing.T) {
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

	conn, reader := dialEventWebSocket(t, ts.URL)
	defer conn.Close()
	initial := readWebSocketEvent(t, conn, reader)
	if initial.Type != "job_snapshot" {
		t.Fatalf("initial event type = %q, want job_snapshot", initial.Type)
	}

	postJSON(t, ts.URL+"/api/sources", map[string]string{"path": sourcePath, "label": "fixture"}, http.StatusCreated)
	var started Job
	postJSONInto(t, ts.URL+"/api/scans", map[string]string{}, http.StatusAccepted, &started)

	progressSeen := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		event := readWebSocketEvent(t, conn, reader)
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
	t.Fatal("timed out waiting for websocket job_finished event")
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

func dialEventWebSocket(t *testing.T, baseURL string) (net.Conn, *bufio.Reader) {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString([]byte("photostore-test!"))
	if _, err := io.WriteString(conn, "GET /api/events/ws HTTP/1.1\r\nHost: "+parsed.Host+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: "+key+"\r\n\r\n"); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		t.Fatalf("websocket upgrade status = %d, want 101", res.StatusCode)
	}
	return conn, reader
}

func readWebSocketEvent(t *testing.T, conn net.Conn, reader *bufio.Reader) ServerEvent {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := readWebSocketFrame(t, reader)
	var event ServerEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func readWebSocketFrame(t *testing.T, reader *bufio.Reader) []byte {
	t.Helper()
	first, err := reader.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	opcode := first & 0x0f
	if opcode != 1 {
		t.Fatalf("websocket opcode = %d, want text", opcode)
	}
	if second&0x80 != 0 {
		t.Fatal("server websocket frame unexpectedly masked")
	}
	length := uint64(second & 0x7f)
	switch length {
	case 126:
		var size [2]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			t.Fatal(err)
		}
		length = uint64(binary.BigEndian.Uint16(size[:]))
	case 127:
		var size [8]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			t.Fatal(err)
		}
		length = binary.BigEndian.Uint64(size[:])
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	return payload
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
	if err := st.acquireSourceFile(scanID, &entryID, sourceID, "source_root", filepath.Join(sourcePath, "A.JPG"), "A.JPG", "", "", partialReport); err != nil {
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
