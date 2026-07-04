package photostore

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJPEGThumbnailAppliesEXIFOrientation(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "oriented.jpg")
	dstPath := filepath.Join(root, "thumb.jpg")
	jpegBytes := jpegWithEXIFOrientation(twoColorJPEG(t, 12, 8), 3)
	if err := os.WriteFile(srcPath, jpegBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeJPEGThumbnail(srcPath, dstPath); err != nil {
		t.Fatal(err)
	}
	got := decodeJPEGFile(t, dstPath)
	r, g, b, _ := got.At(0, 0).RGBA()
	if b>>8 < 160 || r>>8 > 100 || g>>8 > 100 {
		t.Fatalf("thumbnail top-left = rgb(%d,%d,%d), want rotated blue source bottom", r>>8, g>>8, b>>8)
	}
}

func TestWriteJPEGThumbnailSwapsDimensionsForEXIFRotate90(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "oriented.jpg")
	dstPath := filepath.Join(root, "thumb.jpg")
	jpegBytes := jpegWithEXIFOrientation(twoColorJPEG(t, 12, 8), 6)
	if err := os.WriteFile(srcPath, jpegBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeJPEGThumbnail(srcPath, dstPath); err != nil {
		t.Fatal(err)
	}
	got := decodeJPEGFile(t, dstPath)
	if got.Bounds().Dx() != 8 || got.Bounds().Dy() != 12 {
		t.Fatalf("thumbnail dimensions = %dx%d, want 8x12", got.Bounds().Dx(), got.Bounds().Dy())
	}
}

func TestReadJPEGOrientation(t *testing.T) {
	orientation, err := readJPEGOrientation(bytes.NewReader(jpegWithEXIFOrientation(twoColorJPEG(t, 8, 8), 8)))
	if err != nil {
		t.Fatal(err)
	}
	if orientation != 8 {
		t.Fatalf("orientation = %d, want 8", orientation)
	}
}

func TestThumbnailsAreStoredPerContent(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	sourcePath := filepath.Join(root, "source")
	mustMkdir(t, sourcePath)
	content := twoColorJPEG(t, 16, 12)
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
	scanID, err := st.ScanSources(nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := st.EnsureThumbnailsForScan(scanID, nil)
	if summary.Generated != 1 || summary.Existing != 1 || summary.Failed != 0 {
		t.Fatalf("thumbnail summary = %#v, want one generated and one content duplicate already present", summary)
	}
	files, err := st.AcquiredFiles(scanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("acquired files = %d, want 2", len(files))
	}
	first, ok, err := st.ThumbnailFile(files[0].StoredObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("first content thumbnail missing")
	}
	second, ok, err := st.ThumbnailFile(files[1].StoredObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("second content thumbnail missing")
	}
	if first != second {
		t.Fatalf("thumbnail paths = %q and %q, want same content thumbnail", first, second)
	}

	secondScanID, err := st.ScanSources(nil)
	if err != nil {
		t.Fatal(err)
	}
	secondSummary := st.EnsureThumbnailsForScan(secondScanID, nil)
	if secondSummary.Generated != 0 || secondSummary.Existing != 2 || secondSummary.Failed != 0 {
		t.Fatalf("second thumbnail summary = %#v, want no duplicate work", secondSummary)
	}
}

func TestThumbnailGarbageCollectsInactiveRendererNamespaces(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	st, err := Init(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	currentPath := filepath.Join(storePath, "thumbnails", "jpeg", "240", thumbnailRendererVersion, "aa", "bb", "current.jpg")
	stalePath := filepath.Join(storePath, "thumbnails", "jpeg", "240", "old-renderer", "cc", "dd", "stale.jpg")
	mustMkdir(t, filepath.Dir(currentPath))
	mustMkdir(t, filepath.Dir(stalePath))
	mustWrite(t, currentPath, []byte("current"))
	mustWrite(t, stalePath, []byte("stale bytes"))

	summary, err := st.ThumbnailGarbageSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Files != 1 || summary.Bytes != int64(len("stale bytes")) {
		t.Fatalf("garbage summary = %#v, want one stale file", summary)
	}

	removed, err := st.CollectThumbnailGarbage(nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Files != summary.Files || removed.Bytes != summary.Bytes {
		t.Fatalf("removed summary = %#v, want %#v", removed, summary)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale thumbnail stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("current thumbnail removed: %v", err)
	}

	after, err := st.ThumbnailGarbageSummary()
	if err != nil {
		t.Fatal(err)
	}
	if after.Files != 0 || after.Bytes != 0 {
		t.Fatalf("garbage after collection = %#v, want empty", after)
	}
}

func twoColorJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		c := color.RGBA{R: 230, G: 20, B: 20, A: 255}
		if y >= height/2 {
			c = color.RGBA{R: 20, G: 20, B: 230, A: 255}
		}
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeJPEGFile(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func jpegWithEXIFOrientation(jpegBytes []byte, orientation int) []byte {
	if len(jpegBytes) < 2 || !bytes.Equal(jpegBytes[:2], []byte{0xff, 0xd8}) {
		return jpegBytes
	}
	payload := []byte{
		'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0, 42,
		0, 0, 0, 8,
		0, 1,
		0x01, 0x12,
		0, 3,
		0, 0, 0, 1,
		byte(orientation >> 8), byte(orientation), 0, 0,
		0, 0, 0, 0,
	}
	segmentLen := len(payload) + 2
	out := make([]byte, 0, len(jpegBytes)+len(payload)+4)
	out = append(out, jpegBytes[:2]...)
	out = append(out, 0xff, 0xe1, byte(segmentLen>>8), byte(segmentLen))
	out = append(out, payload...)
	out = append(out, jpegBytes[2:]...)
	return out
}
