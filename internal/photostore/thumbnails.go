package photostore

import (
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
)

const thumbnailMaxDimension = 240

const thumbnailPlaceholderSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="240" height="180" viewBox="0 0 240 180" role="img" aria-label="Thumbnail pending"><rect width="240" height="180" fill="#eef1f5"/><path d="M58 124l34-38 28 29 18-20 44 49H58z" fill="#c7d0db"/><circle cx="164" cy="58" r="18" fill="#d7dee7"/><text x="120" y="158" text-anchor="middle" font-family="system-ui, -apple-system, sans-serif" font-size="14" fill="#667085">thumbnail pending</text></svg>`

type ThumbnailSummary struct {
	Generated int
	Skipped   int
	Failed    int
}

func (s *Store) EnsureThumbnailsForScan(scanID string, progress ProgressFunc) ThumbnailSummary {
	files, err := s.AcquiredFiles(scanID)
	if err != nil {
		progressf(progress, "thumbnail scan lookup failed: %v", err)
		return ThumbnailSummary{Failed: 1}
	}
	var summary ThumbnailSummary
	for _, file := range files {
		thumb, ok, err := s.ThumbnailFile(file.StoredObjectID)
		if err != nil {
			summary.Failed++
			progressf(progress, "thumbnail path failed for %s: %v", file.Filename, err)
			continue
		}
		if ok {
			summary.Skipped++
			continue
		}
		src, err := s.StoredObjectFile(file.StoredObjectID)
		if err != nil {
			summary.Failed++
			progressf(progress, "thumbnail source lookup failed for %s: %v", file.Filename, err)
			continue
		}
		if err := writeJPEGThumbnail(src.Path, thumb); err != nil {
			summary.Failed++
			progressf(progress, "thumbnail unavailable for %s: %v", file.Filename, err)
			continue
		}
		summary.Generated++
		progressf(progress, "thumbnail generated for %s", file.Filename)
	}
	progressf(progress, "thumbnails generated: %d, skipped: %d, unavailable: %d", summary.Generated, summary.Skipped, summary.Failed)
	return summary
}

func (s *Store) ThumbnailFile(storedObjectID string) (string, bool, error) {
	path, err := s.thumbnailPath(storedObjectID)
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(path)
	if err == nil {
		return path, !info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return path, false, nil
	}
	return "", false, err
}

func (s *Store) thumbnailPath(storedObjectID string) (string, error) {
	if storedObjectID == "" || strings.ContainsAny(storedObjectID, `/\`) || storedObjectID == "." || storedObjectID == ".." {
		return "", fmt.Errorf("invalid stored object id %q", storedObjectID)
	}
	return filepath.Join(s.Root, "thumbnails", "jpeg", fmt.Sprint(thumbnailMaxDimension), storedObjectID+".jpg"), nil
}

func writeJPEGThumbnail(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	img, err := jpeg.Decode(src)
	if err != nil {
		return err
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions %dx%d", width, height)
	}
	dstWidth, dstHeight := thumbnailSize(width, height)
	thumb := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := 0; y < dstHeight; y++ {
		sy := bounds.Min.Y + y*height/dstHeight
		for x := 0; x < dstWidth; x++ {
			sx := bounds.Min.X + x*width/dstWidth
			thumb.Set(x, y, img.At(sx, sy))
		}
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dstPath), ".thumbnail-*.jpg")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := jpeg.Encode(tmp, thumb, &jpeg.Options{Quality: 82}); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dstPath)
}

func thumbnailSize(width, height int) (int, int) {
	if width <= thumbnailMaxDimension && height <= thumbnailMaxDimension {
		return width, height
	}
	if width >= height {
		dstWidth := thumbnailMaxDimension
		dstHeight := height * thumbnailMaxDimension / width
		if dstHeight < 1 {
			dstHeight = 1
		}
		return dstWidth, dstHeight
	}
	dstHeight := thumbnailMaxDimension
	dstWidth := width * thumbnailMaxDimension / height
	if dstWidth < 1 {
		dstWidth = 1
	}
	return dstWidth, dstHeight
}
