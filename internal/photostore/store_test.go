package photostore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	sourceID, err := st.AddSourceRoot(sourcePath, "source")
	if err != nil {
		t.Fatal(err)
	}
	scanID, err := st.ScanSourceRootsWithOptions([]string{sourceID}, nil, ScanOptions{Workers: 4})
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
	if got := assetCreationRequiredCount(t, st); got != 0 {
		t.Fatalf("asset_creation_required rows after scan = %d, want 0", got)
	}
	var assetEvents int
	if err := st.DB.QueryRow(`select count(*) from events_applied where event_type = 'AssetCreated'`).Scan(&assetEvents); err != nil {
		t.Fatal(err)
	}
	if assetEvents != 1 {
		t.Fatalf("AssetCreated events = %d, want 1", assetEvents)
	}
}

func TestAssetsPagination(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), []byte("asset-a"))
	mustWrite(t, filepath.Join(sourcePath, "B.JPG"), []byte("asset-b"))
	mustWrite(t, filepath.Join(sourcePath, "C.JPG"), []byte("asset-c"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sourceID, err := st.AddSourceRoot(sourcePath, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScanSourceRootsWithOptions([]string{sourceID}, nil, ScanOptions{Workers: 2}); err != nil {
		t.Fatal(err)
	}

	firstPage, err := st.Assets(url.Values{"limit": []string{"2"}})
	if err != nil {
		t.Fatal(err)
	}
	if firstPage.Total != 3 || firstPage.Limit != 2 || firstPage.Offset != 0 || len(firstPage.Assets) != 2 {
		t.Fatalf("first assets page = %#v, want total 3 limit 2 offset 0 length 2", firstPage)
	}
	if firstPage.NextOffset == nil || *firstPage.NextOffset != 2 {
		t.Fatalf("first page next offset = %#v, want 2", firstPage.NextOffset)
	}
	if firstPage.PrevOffset != nil {
		t.Fatalf("first page prev offset = %#v, want nil", firstPage.PrevOffset)
	}

	secondPage, err := st.Assets(url.Values{"limit": []string{"2"}, "offset": []string{"2"}})
	if err != nil {
		t.Fatal(err)
	}
	if secondPage.Total != 3 || secondPage.Limit != 2 || secondPage.Offset != 2 || len(secondPage.Assets) != 1 {
		t.Fatalf("second assets page = %#v, want total 3 limit 2 offset 2 length 1", secondPage)
	}
	if secondPage.NextOffset != nil {
		t.Fatalf("second page next offset = %#v, want nil", secondPage.NextOffset)
	}
	if secondPage.PrevOffset == nil || *secondPage.PrevOffset != 0 {
		t.Fatalf("second page prev offset = %#v, want 0", secondPage.PrevOffset)
	}
	if firstPage.Assets[0].AssetID == secondPage.Assets[0].AssetID || firstPage.Assets[1].AssetID == secondPage.Assets[0].AssetID {
		t.Fatalf("second page repeated a first-page asset: first=%#v second=%#v", firstPage.Assets, secondPage.Assets)
	}
}

func TestAssetQualityMarksTriageAssetReviewed(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), []byte("asset-quality"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sourceID, err := st.AddSourceRoot(sourcePath, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScanSourceRootsWithOptions([]string{sourceID}, nil, ScanOptions{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	page, err := st.Assets(url.Values{"limit": []string{"1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) != 1 {
		t.Fatalf("assets = %#v, want one asset", page.Assets)
	}
	if page.Assets[0].Status != "Triage" {
		t.Fatalf("initial asset status = %s, want Triage", page.Assets[0].Status)
	}
	if err := st.SetAssetQuality(page.Assets[0].AssetID, "Good"); err != nil {
		t.Fatal(err)
	}
	detail, err := st.Asset(page.Assets[0].AssetID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Quality != "Good" || detail.Status != "Reviewed" {
		t.Fatalf("asset after quality = quality %s status %s, want Good Reviewed", detail.Quality, detail.Status)
	}
}

func TestAssetsFiltersDisjoinWithinCategoryAndConjoinAcrossCategories(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), []byte("asset-a"))
	mustWrite(t, filepath.Join(sourcePath, "B.JPG"), []byte("asset-b"))
	mustWrite(t, filepath.Join(sourcePath, "C.JPG"), []byte("asset-c"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sourceID, err := st.AddSourceRoot(sourcePath, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScanSourceRootsWithOptions([]string{sourceID}, nil, ScanOptions{Workers: 2}); err != nil {
		t.Fatal(err)
	}
	page, err := st.Assets(url.Values{"limit": []string{"10"}})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, asset := range page.Assets {
		byName[asset.Filename] = asset.AssetID
	}
	if err := st.SetAssetQuality(byName["A.JPG"], "Best"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAssetQuality(byName["B.JPG"], "Good"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAssetQuality(byName["C.JPG"], "Poor"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAssetVisibility(byName["B.JPG"], "Private"); err != nil {
		t.Fatal(err)
	}
	if err := st.ApplyAssetLabel(byName["A.JPG"], "Family"); err != nil {
		t.Fatal(err)
	}
	if err := st.ApplyAssetLabel(byName["B.JPG"], "Travel"); err != nil {
		t.Fatal(err)
	}

	filtered, err := st.Assets(url.Values{
		"quality":    []string{"Best", "Good"},
		"visibility": []string{"Normal"},
		"limit":      []string{"10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || len(filtered.Assets) != 1 || filtered.Assets[0].Filename != "A.JPG" {
		t.Fatalf("quality disjunction plus visibility conjunction = %#v, want A.JPG only", filtered)
	}
	labelled, err := st.Assets(url.Values{
		"label": []string{"family", "travel"},
		"limit": []string{"10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if labelled.Total != 2 {
		t.Fatalf("label disjunction total = %d, want 2: %#v", labelled.Total, labelled.Assets)
	}
	allStatuses, err := st.Assets(url.Values{
		"status": []string{"Triage", "Reviewed"},
		"limit":  []string{"10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if allStatuses.Total != 3 {
		t.Fatalf("status disjunction total = %d, want 3", allStatuses.Total)
	}
}

func TestAssetsFilterByDateMegapixelsAndSortByDate(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	oldLarge := jpegWithEXIF(t, map[uint16]string{
		0x9003: "2010:01:02 03:04:05",
		0xa002: "2000",
		0xa003: "1000",
	})
	newSmall := jpegWithEXIF(t, map[uint16]string{
		0x9003: "2012:03:04 05:06:07",
		0xa002: "640",
		0xa003: "480",
	})
	mustWrite(t, filepath.Join(sourcePath, "OLD_LARGE.JPG"), oldLarge)
	mustWrite(t, filepath.Join(sourcePath, "NEW_SMALL.JPG"), newSmall)
	mustWrite(t, filepath.Join(sourcePath, "NO_DATE.JPG"), []byte("no-date"))

	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sourceID, err := st.AddSourceRoot(sourcePath, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ScanSourceRootsWithOptions([]string{sourceID}, nil, ScanOptions{Workers: 2}); err != nil {
		t.Fatal(err)
	}

	dated, err := st.Assets(url.Values{"has_date": []string{"1"}, "sort": []string{"date_desc"}, "limit": []string{"10"}})
	if err != nil {
		t.Fatal(err)
	}
	if dated.Total != 2 || len(dated.Assets) != 2 {
		t.Fatalf("dated assets = %#v, want two dated assets", dated)
	}
	if dated.Assets[0].Filename != "NEW_SMALL.JPG" || dated.Assets[1].Filename != "OLD_LARGE.JPG" {
		t.Fatalf("date desc order = %s, %s; want NEW_SMALL.JPG, OLD_LARGE.JPG", dated.Assets[0].Filename, dated.Assets[1].Filename)
	}
	large, err := st.Assets(url.Values{"has_date": []string{"1"}, "min_megapixels": []string{"1"}, "limit": []string{"10"}})
	if err != nil {
		t.Fatal(err)
	}
	if large.Total != 1 || len(large.Assets) != 1 || large.Assets[0].Filename != "OLD_LARGE.JPG" {
		t.Fatalf("large dated assets = %#v, want OLD_LARGE.JPG only", large)
	}
	nav, err := st.AssetNavigation(large.Assets[0].AssetID, url.Values{"has_date": []string{"1"}, "sort": []string{"date_asc"}})
	if err != nil {
		t.Fatal(err)
	}
	if nav.Current.Filename != "OLD_LARGE.JPG" || nav.Next == nil || nav.Next.Filename != "NEW_SMALL.JPG" || nav.Previous != nil {
		t.Fatalf("navigation = %#v, want OLD_LARGE then NEW_SMALL", nav)
	}
	if len(nav.Window) != 2 || nav.Window[0].Filename != "OLD_LARGE.JPG" || nav.Window[1].Filename != "NEW_SMALL.JPG" {
		t.Fatalf("navigation window = %#v, want OLD_LARGE then NEW_SMALL", nav.Window)
	}
	if nav.Current.ThumbnailURL == "" || nav.Window[0].ThumbnailURL == "" || nav.Window[1].ThumbnailURL == "" {
		t.Fatalf("navigation thumbnails missing: %#v", nav)
	}
}

func TestInitExistingStoreDoesNotAppendDuplicateInitializedEvent(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var initializedEvents int
	if err := st.DB.QueryRow(`select count(*) from events_applied where event_type = 'StoreInitialized'`).Scan(&initializedEvents); err != nil {
		t.Fatal(err)
	}
	if initializedEvents != 1 {
		t.Fatalf("StoreInitialized events = %d, want 1", initializedEvents)
	}
}

func TestEventsIncludeActorHostname(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSourceRoot(t.TempDir(), "source"); err != nil {
		t.Fatal(err)
	}
	ev := latestEventOfType(t, st, "SourceRootRegistered")
	if _, ok := ev.Actor["hostname"]; !ok {
		t.Fatalf("actor = %#v, want hostname", ev.Actor)
	}
}

func TestIngestionScanFailedUpdatesProjection(t *testing.T) {
	st, err := Init(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	scanID := "scan_failed"
	if err := st.appendEvent("IngestionScanStarted", nil, &scanID, map[string]any{
		"scan_id":       scanID,
		"started_at_ms": int64(123),
		"source_roots":  []map[string]any{},
		"policy":        map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	report := &ScanReport{ScanID: scanID, CandidateFilesSeen: 1}
	if err := st.appendScanFailed(scanID, report, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	var status string
	var started, completed int64
	var statsJSON string
	if err := st.DB.QueryRow(`select status, started_at_ms, completed_at_ms, stats_json from scans where scan_id = ?`, scanID).Scan(&status, &started, &completed, &statsJSON); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("scan status = %q, want failed", status)
	}
	if started != 123 || completed == 0 {
		t.Fatalf("scan times started=%d completed=%d, want preserved start and failure time", started, completed)
	}
	if !strings.Contains(statsJSON, `"candidate_files_seen":1`) {
		t.Fatalf("stats = %s, want candidate count", statsJSON)
	}
}

func TestOpenReplaysUnappliedEventLogTail(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	ev := sourceRootRegisteredEvent(root, "evt_unapplied_source", "src_unapplied", "unapplied", nowMS()+1)
	if err := st.writeEvent(ev); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var label string
	if err := st.DB.QueryRow(`select label from source_roots where source_root_id = ?`, "src_unapplied").Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "unapplied" {
		t.Fatalf("label = %q, want unapplied", label)
	}
	var applied int
	if err := st.DB.QueryRow(`select count(*) from events_applied where event_id = ?`, ev.EventID).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("applied count = %d, want 1", applied)
	}
	assertProjectionOffsetAtLogEnd(t, st)
}

func TestOpenWithoutProjectionCursorReplaysWholeLogToRepairOlderHoles(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	first := sourceRootRegisteredEvent(root, "evt_unapplied_older", "src_unapplied_older", "older", nowMS()+1)
	second := sourceRootRegisteredEvent(root, "evt_applied_later", "src_applied_later", "later", nowMS()+2)
	if err := st.writeEvent(first); err != nil {
		t.Fatal(err)
	}
	if err := st.writeEvent(second); err != nil {
		t.Fatal(err)
	}
	if err := st.applyEvent(second); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`delete from projection_state`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, sourceRootID := range []string{"src_unapplied_older", "src_applied_later"} {
		var count int
		if err := st.DB.QueryRow(`select count(*) from source_roots where source_root_id = ?`, sourceRootID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s count = %d, want 1", sourceRootID, count)
		}
	}
	assertProjectionOffsetAtLogEnd(t, st)
}

func TestOpenDrainsAssetCreationRequiredProjection(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	scanID := "scan_asset_required"
	occID := "occ_asset_required"
	objID := "obj_asset_required"
	ref := contentRef(sha([]byte("asset-required")), int64(len("asset-required")))
	if err := st.appendEvent("SourceFileAcquired", nil, &scanID, map[string]any{
		"scan_id":                 scanID,
		"source_occurrence_id":    occID,
		"stored_object_id":        objID,
		"purpose":                 "source_media",
		"source_kind":             "source_root",
		"source_root_id":          "src_asset_required",
		"path":                    filepath.Join(root, "IMG.JPG"),
		"relative_path":           "IMG.JPG",
		"historical_inventory_id": nil,
		"inventory_entry_id":      nil,
		"acquired_storage_key":    "objects/acquired/" + objID,
		"source_file":             map[string]any{},
		"copy_result": map[string]any{
			"bytes_copied":   int64(len("asset-required")),
			"hash_algorithm": "sha256",
			"hash":           sha([]byte("asset-required")),
		},
		"storage_disposition": map[string]any{
			"cas_existed_at_ingest":    false,
			"acquired_object_retained": true,
			"temporary_copy_discarded": false,
		},
		"content_ref": ref,
	}); err != nil {
		t.Fatal(err)
	}
	materializedEventID, err := st.appendEventReturnID("ContentAddressMaterialized", nil, nil, map[string]any{
		"stored_object_id": objID,
		"content_ref":      ref,
		"materialization":  map[string]any{"method": "hard_link", "created": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := assetCreationRequiredCount(t, st); got != 1 {
		t.Fatalf("asset_creation_required rows before reopen = %d, want 1", got)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if got := assetCreationRequiredCount(t, st); got != 0 {
		t.Fatalf("asset_creation_required rows after reopen = %d, want 0", got)
	}
	var assetID string
	if err := st.DB.QueryRow(`select asset_id from assets where content_ref = ?`, ref).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	assetEvent := latestEventOfType(t, st, "AssetCreated")
	if assetEvent.CausationID == nil || *assetEvent.CausationID != materializedEventID {
		t.Fatalf("AssetCreated causation = %v, want %s", assetEvent.CausationID, materializedEventID)
	}
}

func TestOpenReplaysProjectionWhenReducerVersionChanges(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	scanID := "scan_projection_version"
	occID := "occ_projection_version"
	objID := "obj_projection_version"
	ref := contentRef(sha([]byte("projection-version")), int64(len("projection-version")))
	if err := st.appendEvent("SourceFileAcquired", nil, &scanID, map[string]any{
		"scan_id":                 scanID,
		"source_occurrence_id":    occID,
		"stored_object_id":        objID,
		"purpose":                 "source_media",
		"source_kind":             "source_root",
		"source_root_id":          "src_projection_version",
		"path":                    filepath.Join(root, "IMG.JPG"),
		"relative_path":           "IMG.JPG",
		"historical_inventory_id": nil,
		"inventory_entry_id":      nil,
		"acquired_storage_key":    "objects/acquired/" + objID,
		"source_file":             map[string]any{},
		"copy_result": map[string]any{
			"bytes_copied":   int64(len("projection-version")),
			"hash_algorithm": "sha256",
			"hash":           sha([]byte("projection-version")),
		},
		"storage_disposition": map[string]any{
			"cas_existed_at_ingest":    false,
			"acquired_object_retained": true,
			"temporary_copy_discarded": false,
		},
		"content_ref": ref,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.appendEventReturnID("ContentAddressMaterialized", nil, nil, map[string]any{
		"stored_object_id": objID,
		"content_ref":      ref,
		"materialization":  map[string]any{"method": "hard_link", "created": true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`delete from asset_creation_required`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`update projection_state set projection_version = ? where projection_name = ?`, mainProjectionVersion-1, "main"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if got := assetCreationRequiredCount(t, st); got != 0 {
		t.Fatalf("asset_creation_required rows after version replay = %d, want 0", got)
	}
	var assetID string
	if err := st.DB.QueryRow(`select asset_id from assets where content_ref = ?`, ref).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := st.DB.QueryRow(`select projection_version from projection_state where projection_name = ?`, "main").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != mainProjectionVersion {
		t.Fatalf("projection version = %d, want %d", version, mainProjectionVersion)
	}
}

func TestConcurrentStoreHandlesSerializeEventLogCursor(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	other, err := Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	stores := []*Store{st, other}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := stores[i%len(stores)]
			_, err := store.AddSourceRoot(filepath.Join(root, "source", strconv.Itoa(i)), "source-"+strconv.Itoa(i))
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertProjectionOffsetAtLogEnd(t, st)
	var sources int
	if err := st.DB.QueryRow(`select count(*) from source_roots`).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if sources != 20 {
		t.Fatalf("source roots = %d, want 20", sources)
	}
}

func assetCreationRequiredCount(t *testing.T, st *Store) int {
	t.Helper()
	var count int
	if err := st.DB.QueryRow(`select count(*) from asset_creation_required`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertProjectionOffsetAtLogEnd(t *testing.T, st *Store) {
	t.Helper()
	size, err := st.eventLogSize()
	if err != nil {
		t.Fatal(err)
	}
	var offset int64
	if err := st.DB.QueryRow(`select next_offset from projection_state where projection_name = ?`, "main").Scan(&offset); err != nil {
		t.Fatal(err)
	}
	if offset != size {
		t.Fatalf("projection next_offset = %d, want event log size %d", offset, size)
	}
}

func sourceRootRegisteredEvent(root, eventID, sourceRootID, label string, recordedAtMS int64) Event {
	return Event{
		EventID:       eventID,
		EventType:     "SourceRootRegistered",
		SchemaVersion: schemaVersion,
		RecordedAtMS:  recordedAtMS,
		Actor:         map[string]any{"type": "test", "id": "store-test"},
		Payload: map[string]any{
			"source_root_id": sourceRootID,
			"label":          label,
			"root_path":      filepath.Join(root, sourceRootID),
			"source_type":    "local_directory",
			"scan_policy": map[string]any{
				"recursive":            true,
				"follow_symlinks":      false,
				"candidate_extensions": []string{".jpg", ".jpeg"},
			},
		},
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

func TestNewContentMaterializesCASAsHardLink(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	content := []byte("new-content")
	mustWrite(t, filepath.Join(sourcePath, "A.JPG"), content)

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
	acquiredPath, canonicalPath := acquiredAndCanonicalPaths(t, st)
	assertSameFile(t, acquiredPath, canonicalPath)
	assertFileMode(t, acquiredPath, 0o400)
	assertFileMode(t, canonicalPath, 0o400)
	summary, err := st.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.RetainedDuplicateBytes != 0 {
		t.Fatalf("retained duplicate bytes for new hard-linked content = %d, want 0", summary.RetainedDuplicateBytes)
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
	assertFileMode(t, duplicatePath, 0o400)
	assertFileMode(t, canonicalPath, 0o400)
	summary, err := st.VerifyAndDeduplicate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Candidates != 2 || summary.Deduplicated != 2 || summary.BytesReleased != int64(len(content)) {
		t.Fatalf("deduplicate summary = %#v, want retained duplicate bytes released", summary)
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
	assertFileMode(t, duplicatePath, 0o400)
	assertFileMode(t, canonicalPath, 0o400)
	allAcquiredPaths := retainedAcquiredPaths(t, st)
	for _, path := range allAcquiredPaths {
		assertSameFile(t, path, canonicalPath)
	}
	var retained int
	if err := st.DB.QueryRow(`select count(*) from source_content_links where acquired_object_retained = 1`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 0 {
		t.Fatalf("retained duplicate links = %d, want 0", retained)
	}
	var events int
	if err := st.DB.QueryRow(`select count(*) from events_applied where event_type = 'DuplicateSourceObjectDeduplicated'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Fatalf("dedup events = %d, want 2", events)
	}
}

func TestStaleDeduplicationStrategyRequiresReassessment(t *testing.T) {
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
	candidate := retainedDuplicateCandidate(t, st)
	if err := st.appendEvent("DuplicateSourceObjectDeduplicated", nil, nil, map[string]any{
		"source_occurrence_id": candidate.SourceOccurrenceID,
		"stored_object_id":     candidate.StoredObjectID,
		"content_ref":          candidate.ContentRef,
		"deduplicated_at_ms":   nowMS(),
		"verification": map[string]any{
			"hash_algorithm":  "sha256",
			"canonical_hash":  sha(content),
			"duplicate_hash":  sha(content),
			"byte_comparison": true,
			"bytes_compared":  len(content),
		},
		"storage": map[string]any{
			"duplicate_deleted": true,
			"replacement": map[string]any{
				"method": "old_strategy",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	stale, err := st.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if stale.RetainedDuplicateBytes != int64(len(content)) {
		t.Fatalf("stale retained duplicate bytes = %d, want %d", stale.RetainedDuplicateBytes, len(content))
	}
	summary, err := st.VerifyAndDeduplicate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Candidates != 2 || summary.Deduplicated != 2 {
		t.Fatalf("deduplicate summary = %#v, want stale candidates reassessed", summary)
	}
	current, err := st.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if current.RetainedDuplicateBytes != 0 {
		t.Fatalf("current retained duplicate bytes = %d, want 0", current.RetainedDuplicateBytes)
	}
	assertSameFile(t, retainedDuplicatePathByKey(t, st, candidate.AcquiredStorageKey), filepath.Join(st.Root, filepath.FromSlash(casKey(candidate.ContentRef))))
	var strategyName string
	var strategyVersion int
	if err := st.DB.QueryRow(`select strategy_name, strategy_version from duplicate_deduplications where source_occurrence_id = ?`, candidate.SourceOccurrenceID).Scan(&strategyName, &strategyVersion); err != nil {
		t.Fatal(err)
	}
	if strategyName != dedupStrategyName || strategyVersion != dedupStrategyVersion {
		t.Fatalf("strategy = %s v%d, want %s v%d", strategyName, strategyVersion, dedupStrategyName, dedupStrategyVersion)
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

func retainedDuplicateCandidate(t *testing.T, st *Store) duplicateCandidate {
	t.Helper()
	var candidate duplicateCandidate
	if err := st.DB.QueryRow(`
		select scl.source_occurrence_id, scl.stored_object_id, scl.content_ref, st.acquired_storage_key
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		where scl.cas_existed_at_ingest = 1
		limit 1`).Scan(&candidate.SourceOccurrenceID, &candidate.StoredObjectID, &candidate.ContentRef, &candidate.AcquiredStorageKey); err != nil {
		t.Fatal(err)
	}
	return candidate
}

func acquiredAndCanonicalPaths(t *testing.T, st *Store) (string, string) {
	t.Helper()
	var key, ref string
	if err := st.DB.QueryRow(`
		select st.acquired_storage_key, scl.content_ref
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		limit 1`).Scan(&key, &ref); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(st.Root, filepath.FromSlash(key)), filepath.Join(st.Root, filepath.FromSlash(casKey(ref)))
}

func retainedDuplicatePathByKey(t *testing.T, st *Store, key string) string {
	t.Helper()
	return filepath.Join(st.Root, filepath.FromSlash(key))
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

func retainedAcquiredPaths(t *testing.T, st *Store) []string {
	t.Helper()
	rows, err := st.DB.Query(`
		select st.acquired_storage_key
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		order by scl.source_occurrence_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Join(st.Root, filepath.FromSlash(key)))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return paths
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
