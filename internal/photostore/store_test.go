package photostore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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
	scanID, err := st.ScanSources()
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
	if _, err := st.ScanSources(); err != nil {
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
