package photostore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanSourcesAcquiresOnlyJPEGs(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "IMG1.JPG"), []byte("jpeg-one"))
	mustWrite(t, filepath.Join(sourcePath, "notes.txt"), []byte("not-media"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSourceRoot(sourcePath, "source"); err != nil {
		t.Fatal(err)
	}
	scanID, err := st.ScanSources(nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := st.Report(scanID)
	if err != nil {
		t.Fatal(err)
	}
	if report.CandidateFilesSeen != 1 {
		t.Fatalf("candidate files = %d, want 1", report.CandidateFilesSeen)
	}
	if report.SourceFilesAcquired != 1 {
		t.Fatalf("source files acquired = %d, want 1", report.SourceFilesAcquired)
	}
	if report.NonCandidateFilesSkipped != 1 {
		t.Fatalf("non-candidates skipped = %d, want 1", report.NonCandidateFilesSkipped)
	}

	ref := contentRef(sha([]byte("jpeg-one")), int64(len("jpeg-one")))
	if !st.contentAddressExists(ref) {
		t.Fatalf("expected CAS object for %s", ref)
	}
}

func TestHistoricalInventoryScanSkipsAlreadySeenHash(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	resolverRoot := filepath.Join(root, "resolved")
	mustMkdir(t, sourcePath)
	mustMkdir(t, resolverRoot)

	content := []byte("same-jpeg")
	hash := sha(content)
	mustWrite(t, filepath.Join(sourcePath, "IMG1.JPG"), content)
	mustWrite(t, filepath.Join(resolverRoot, "IMG1_COPY.JPG"), content)
	tocPath := filepath.Join(root, "inventory.toc")
	mustWrite(t, tocPath, []byte(hash+" ./IMG1_COPY.JPG\n"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSourceRoot(sourcePath, "source"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScanSources(nil); err != nil {
		t.Fatal(err)
	}
	invID, err := st.AcquireInventory(tocPath, "inventory", "test")
	if err != nil {
		t.Fatal(err)
	}
	scanID, err := st.ScanInventory(invID, "toc", []string{".jpg", ".jpeg"}, resolverRoot, []string{"./"}, true)
	if err != nil {
		t.Fatal(err)
	}
	report, err := st.Report(scanID)
	if err != nil {
		t.Fatal(err)
	}
	if report.HistoricalJPEGEntriesLoaded != 1 {
		t.Fatalf("historical entries loaded = %d, want 1", report.HistoricalJPEGEntriesLoaded)
	}
	if report.HistoricalEntriesAlreadySeen != 1 {
		t.Fatalf("already-seen entries = %d, want 1", report.HistoricalEntriesAlreadySeen)
	}
	if report.SourceFilesAcquired != 0 {
		t.Fatalf("source files acquired = %d, want 0", report.SourceFilesAcquired)
	}

	var links int
	if err := st.DB.QueryRow(`select count(*) from historical_seen_links`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if links != 1 {
		t.Fatalf("historical links = %d, want 1", links)
	}
}

func TestScanSourcesReportsDuplicateGarbage(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), []byte("same"))
	mustWrite(t, filepath.Join(sourcePath, "B.jpeg"), []byte("same"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSourceRoot(sourcePath, "source"); err != nil {
		t.Fatal(err)
	}
	scanID, err := st.ScanSources(nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := st.Report(scanID)
	if err != nil {
		t.Fatal(err)
	}
	if report.DuplicateAcquisitions != 1 {
		t.Fatalf("duplicate acquisitions = %d, want 1", report.DuplicateAcquisitions)
	}
	if report.DuplicateGarbageBytes != int64(len("same")) {
		t.Fatalf("duplicate garbage bytes = %d, want %d", report.DuplicateGarbageBytes, len("same"))
	}
}

func TestVerifyAndDeduplicateReleasesDuplicateBytes(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	content := []byte("same")
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), content)
	mustWrite(t, filepath.Join(sourcePath, "B.jpeg"), content)

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSourceRoot(sourcePath, "source"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScanSources(nil); err != nil {
		t.Fatal(err)
	}
	before, err := st.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if before.RetainedDuplicateBytes != int64(len(content)) {
		t.Fatalf("retained duplicate bytes before dedup = %d, want %d", before.RetainedDuplicateBytes, len(content))
	}
	duplicatePath := retainedDuplicatePath(t, st)
	canonicalPath := retainedDuplicateCanonicalPath(t, st)
	assertFileMode(t, duplicatePath, 0o600)
	assertFileMode(t, canonicalPath, 0o600)
	summary, err := st.VerifyAndDeduplicate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Candidates != 1 || summary.Deduplicated != 1 || summary.BytesReleased != int64(len(content)) {
		t.Fatalf("deduplicate summary = %#v, want one released duplicate", summary)
	}
	after, err := st.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if after.RetainedDuplicateBytes != 0 {
		t.Fatalf("retained duplicate bytes after dedup = %d, want 0", after.RetainedDuplicateBytes)
	}
	got, err := os.ReadFile(duplicatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("deduplicated object bytes = %q, want %q", got, content)
	}
	assertSameFile(t, duplicatePath, canonicalPath)
	assertFileMode(t, duplicatePath, 0o600)
	assertFileMode(t, canonicalPath, 0o600)
	var retained int
	if err := st.DB.QueryRow(`select count(*) from source_content_links where acquired_object_retained = 1 and cas_existed_at_ingest = 1`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 0 {
		t.Fatalf("retained duplicate links = %d, want 0", retained)
	}
	var events int
	if err := st.DB.QueryRow(`select count(*) from events_applied where event_type = 'DuplicateSourceObjectDeduplicated'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("dedup events = %d, want 1", events)
	}
}

func TestOpenUninitializedStoreReturnsActionableError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-store")
	_, err := Open(root)
	if err == nil {
		t.Fatal("Open succeeded for an uninitialized store")
	}
	got := err.Error()
	if !strings.Contains(got, "photostore init") {
		t.Fatalf("error = %q, want default init guidance", got)
	}
	if !strings.Contains(got, "--store PATH") {
		t.Fatalf("error = %q, want alternate store guidance", got)
	}
}

func retainedDuplicatePath(t *testing.T, st *Store) string {
	t.Helper()
	var key string
	if err := st.DB.QueryRow(`
		select st.acquired_storage_key
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		where scl.cas_existed_at_ingest = 1 and scl.acquired_object_retained = 1
		limit 1`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(st.Root, filepath.FromSlash(key))
}

func retainedDuplicateCanonicalPath(t *testing.T, st *Store) string {
	t.Helper()
	var ref string
	if err := st.DB.QueryRow(`
		select scl.content_ref
		from source_content_links scl
		where scl.cas_existed_at_ingest = 1 and scl.acquired_object_retained = 1
		limit 1`).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(st.Root, filepath.FromSlash(casKey(ref)))
}

func assertSameFile(t *testing.T, left, right string) {
	t.Helper()
	leftInfo, err := os.Stat(left)
	if err != nil {
		t.Fatal(err)
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(leftInfo, rightInfo) {
		t.Fatalf("%s and %s are not the same hard-linked file", left, right)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
