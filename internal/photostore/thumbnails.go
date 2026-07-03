package photostore

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	xdraw "golang.org/x/image/draw"
)

const thumbnailMaxDimension = 240
const thumbnailRendererVersion = "orient-v3"

const thumbnailPlaceholderSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="240" height="180" viewBox="0 0 240 180" role="img" aria-label="Thumbnail pending"><rect width="240" height="180" fill="#eef1f5"/><path d="M58 124l34-38 28 29 18-20 44 49H58z" fill="#c7d0db"/><circle cx="164" cy="58" r="18" fill="#d7dee7"/><text x="120" y="158" text-anchor="middle" font-family="system-ui, -apple-system, sans-serif" font-size="14" fill="#667085">thumbnail pending</text></svg>`

type ThumbnailSummary struct {
	ScanID    string           `json:"scan_id"`
	Generated int              `json:"generated"`
	Existing  int              `json:"already_present"`
	Failed    int              `json:"unavailable"`
	Issues    []ThumbnailIssue `json:"issues,omitempty"`
}

type ThumbnailIssue struct {
	Filename       string `json:"filename"`
	Source         string `json:"source"`
	StoredObjectID string `json:"stored_object_id"`
	Error          string `json:"error"`
}

type thumbnailContentGroup struct {
	ContentRef string
	Path       string
	Files      []AcquiredFileProjection
}

type thumbnailResult struct {
	Group     thumbnailContentGroup
	Existing  bool
	Generated bool
	Err       error
}

func (s *Store) EnsureThumbnailsForScan(scanID string, progress ProgressFunc) ThumbnailSummary {
	files, err := s.AcquiredFiles(scanID)
	if err != nil {
		progressf(progress, "thumbnail scan lookup failed: %v", err)
		return ThumbnailSummary{ScanID: scanID, Failed: 1}
	}
	summary := ThumbnailSummary{ScanID: scanID}
	groups, groupingIssues := s.thumbnailContentGroups(files)
	for _, issue := range groupingIssues {
		summary.Failed++
		summary.Issues = append(summary.Issues, issue)
		progressf(progress, "thumbnail path failed for %s: %s", issue.Filename, issue.Error)
	}
	jobs := make(chan thumbnailContentGroup)
	results := make(chan thumbnailResult)
	var wg sync.WaitGroup
	workers := thumbnailWorkers()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range jobs {
				results <- s.ensureThumbnailForContent(group)
			}
		}()
	}
	go func() {
		for _, group := range groups {
			jobs <- group
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	processedGroups := 0
	for result := range results {
		count := len(result.Group.Files)
		file := result.Group.Files[0]
		processedGroups++
		if result.Err != nil {
			summary.Failed += count
			summary.Issues = append(summary.Issues, thumbnailIssue(file, result.Err))
			progressCountf(progress, processedGroups, len(groups), "thumbnail unavailable for %s (%s; object %s): %v", file.Filename, thumbnailSourceLabel(file), file.StoredObjectID, result.Err)
			continue
		}
		if result.Existing {
			summary.Existing += count
			progressCountf(progress, processedGroups, len(groups), "thumbnail already present for %s", file.Filename)
			continue
		}
		if result.Generated {
			summary.Generated++
			if count > 1 {
				summary.Existing += count - 1
			}
			progressCountf(progress, processedGroups, len(groups), "thumbnail generated for %s", file.Filename)
		}
	}
	progressf(progress, "thumbnails generated: %d, already present: %d, unavailable: %d", summary.Generated, summary.Existing, summary.Failed)
	if err := s.writeThumbnailReport(summary); err != nil {
		progressf(progress, "thumbnail report write failed: %v", err)
	}
	return summary
}

func (s *Store) thumbnailContentGroups(files []AcquiredFileProjection) ([]thumbnailContentGroup, []ThumbnailIssue) {
	byRef := map[string]*thumbnailContentGroup{}
	var issues []ThumbnailIssue
	for _, file := range files {
		if file.ContentRef == "" {
			issues = append(issues, thumbnailIssue(file, fmt.Errorf("missing content ref")))
			continue
		}
		thumb, err := s.thumbnailPath(file.ContentRef)
		if err != nil {
			issues = append(issues, thumbnailIssue(file, err))
			continue
		}
		group, ok := byRef[file.ContentRef]
		if !ok {
			group = &thumbnailContentGroup{ContentRef: file.ContentRef, Path: thumb}
			byRef[file.ContentRef] = group
		}
		group.Files = append(group.Files, file)
	}
	groups := make([]thumbnailContentGroup, 0, len(byRef))
	for _, group := range byRef {
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].ContentRef < groups[j].ContentRef
	})
	return groups, issues
}

func (s *Store) ensureThumbnailForContent(group thumbnailContentGroup) thumbnailResult {
	info, err := os.Stat(group.Path)
	if err == nil {
		if info.IsDir() {
			return thumbnailResult{Group: group, Err: fmt.Errorf("thumbnail path is a directory")}
		}
		return thumbnailResult{Group: group, Existing: true}
	}
	if err != nil && !os.IsNotExist(err) {
		return thumbnailResult{Group: group, Err: err}
	}
	if err := s.writeThumbnailForObject(group.Files[0].StoredObjectID, group.Path); err != nil {
		info, statErr := os.Stat(group.Path)
		if statErr == nil && !info.IsDir() {
			return thumbnailResult{Group: group, Existing: true}
		}
		return thumbnailResult{Group: group, Err: err}
	}
	return thumbnailResult{Group: group, Generated: true}
}

func thumbnailWorkers() int {
	return workersFromEnv("PHOTOSTORE_THUMBNAIL_WORKERS")
}

func workersFromEnv(name string) int {
	if raw := os.Getenv(name); raw != "" {
		workers, err := strconv.Atoi(raw)
		if err == nil && workers > 0 {
			return workers
		}
	}
	return boundedCPUWorkers()
}

func thumbnailIssue(file AcquiredFileProjection, err error) ThumbnailIssue {
	return ThumbnailIssue{
		Filename:       file.Filename,
		Source:         thumbnailSourceLabel(file),
		StoredObjectID: file.StoredObjectID,
		Error:          err.Error(),
	}
}

func thumbnailSourceLabel(file AcquiredFileProjection) string {
	if file.RelativePath != "" {
		return file.RelativePath
	}
	return file.Path
}

func (s *Store) writeThumbnailReport(summary ThumbnailSummary) error {
	path := filepath.Join(s.Root, "reports", "thumbnails-scan-"+summary.ScanID+".json")
	b, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func (s *Store) ThumbnailReport(scanID string) (*ThumbnailSummary, error) {
	b, err := os.ReadFile(filepath.Join(s.Root, "reports", "thumbnails-scan-"+scanID+".json"))
	if err != nil {
		return nil, err
	}
	var summary ThumbnailSummary
	if err := json.Unmarshal(b, &summary); err != nil {
		return nil, err
	}
	if summary.ScanID == "" {
		summary.ScanID = scanID
	}
	return &summary, nil
}

func (s *Store) EnsureThumbnailForObject(storedObjectID string) error {
	path, ok, err := s.ThumbnailFile(storedObjectID)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return s.writeThumbnailForObject(storedObjectID, path)
}

func (s *Store) writeThumbnailForObject(storedObjectID, dstPath string) error {
	src, err := s.CanonicalObjectFile(storedObjectID)
	if err != nil {
		return err
	}
	return writeJPEGThumbnail(src.Path, dstPath)
}

func (s *Store) ThumbnailFile(storedObjectID string) (string, bool, error) {
	contentRef, err := s.contentRefForStoredObject(storedObjectID)
	if err != nil {
		return "", false, err
	}
	path, err := s.thumbnailPath(contentRef)
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

func (s *Store) thumbnailPath(contentRef string) (string, error) {
	algo, hash, _ := parseContentRef(contentRef)
	if algo != "sha256" || !isSHA256(hash) {
		return "", fmt.Errorf("invalid content ref %q", contentRef)
	}
	return filepath.Join(s.Root, "thumbnails", "jpeg", fmt.Sprint(thumbnailMaxDimension), thumbnailRendererVersion, hash[:2], hash[2:4], hash+".jpg"), nil
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
	orientation := jpegOrientation(srcPath)
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions %dx%d", width, height)
	}
	orientedWidth, orientedHeight := orientedDimensions(width, height, orientation)
	dstWidth, dstHeight := thumbnailSize(orientedWidth, orientedHeight)
	thumb := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	oriented := orientedImage{src: img, orientation: orientation, width: width, height: height}
	xdraw.CatmullRom.Scale(thumb, thumb.Bounds(), oriented, oriented.Bounds(), xdraw.Over, nil)
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

type orientedImage struct {
	src         image.Image
	orientation int
	width       int
	height      int
}

func (img orientedImage) ColorModel() color.Model {
	return img.src.ColorModel()
}

func (img orientedImage) Bounds() image.Rectangle {
	width, height := orientedDimensions(img.width, img.height, img.orientation)
	return image.Rect(0, 0, width, height)
}

func (img orientedImage) At(x, y int) color.Color {
	sx, sy := orientedToSource(x, y, img.width, img.height, img.orientation)
	bounds := img.src.Bounds()
	return img.src.At(sx+bounds.Min.X, sy+bounds.Min.Y)
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

func orientedDimensions(width, height, orientation int) (int, int) {
	switch orientation {
	case 5, 6, 7, 8:
		return height, width
	default:
		return width, height
	}
}

func orientedToSource(x, y, width, height, orientation int) (int, int) {
	switch orientation {
	case 2:
		return width - 1 - x, y
	case 3:
		return width - 1 - x, height - 1 - y
	case 4:
		return x, height - 1 - y
	case 5:
		return y, x
	case 6:
		return y, height - 1 - x
	case 7:
		return width - 1 - y, height - 1 - x
	case 8:
		return width - 1 - y, x
	default:
		return x, y
	}
}

func jpegOrientation(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 1
	}
	defer f.Close()
	orientation, err := readJPEGOrientation(f)
	if err != nil || orientation < 1 || orientation > 8 {
		return 1
	}
	return orientation
}

func readJPEGOrientation(r io.Reader) (int, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 1, err
	}
	if header != [2]byte{0xff, 0xd8} {
		return 1, nil
	}
	for {
		marker, err := nextJPEGMarker(r)
		if err != nil {
			return 1, err
		}
		if marker == 0xda || marker == 0xd9 {
			return 1, nil
		}
		var lenBuf [2]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return 1, err
		}
		segmentLen := int(binary.BigEndian.Uint16(lenBuf[:]))
		if segmentLen < 2 {
			return 1, fmt.Errorf("invalid jpeg segment length %d", segmentLen)
		}
		payloadLen := segmentLen - 2
		if marker != 0xe1 {
			if _, err := io.CopyN(io.Discard, r, int64(payloadLen)); err != nil {
				return 1, err
			}
			continue
		}
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return 1, err
		}
		if orientation, ok := exifOrientation(payload); ok {
			return orientation, nil
		}
	}
}

func nextJPEGMarker(r io.Reader) (byte, error) {
	var b [1]byte
	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		if b[0] == 0xff {
			break
		}
	}
	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		if b[0] != 0xff {
			return b[0], nil
		}
	}
}

func exifOrientation(payload []byte) (int, bool) {
	const exifHeader = "Exif\x00\x00"
	if len(payload) < len(exifHeader)+8 || string(payload[:len(exifHeader)]) != exifHeader {
		return 1, false
	}
	tiff := payload[len(exifHeader):]
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1, false
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 1, false
	}
	ifdOffset := int(order.Uint32(tiff[4:8]))
	if ifdOffset < 8 || ifdOffset+2 > len(tiff) {
		return 1, false
	}
	entries := int(order.Uint16(tiff[ifdOffset : ifdOffset+2]))
	pos := ifdOffset + 2
	for i := 0; i < entries; i++ {
		if pos+12 > len(tiff) {
			return 1, false
		}
		entry := tiff[pos : pos+12]
		tag := order.Uint16(entry[0:2])
		typ := order.Uint16(entry[2:4])
		count := order.Uint32(entry[4:8])
		if tag == 0x0112 && typ == 3 && count == 1 {
			return int(order.Uint16(entry[8:10])), true
		}
		pos += 12
	}
	return 1, false
}
