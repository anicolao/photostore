# Test: Dashboard Source Scan

Register a source root, scan it, inspect progress, drill into thumbnails, browse photos by date, and trigger metadata refresh.

## The initialized store dashboard starts empty.

![The initialized store dashboard starts empty.](./screenshots/000-empty-dashboard.png)

**Verifications:**
- [x] Photostore heading is visible
- [x] Source count is zero
- [x] Recent scans empty state is visible

---

## The fixture source root is registered and has never been scanned.

![The fixture source root is registered and has never been scanned.](./screenshots/001-source-registered.png)

**Verifications:**
- [x] Source count is one
- [x] Fixture source is listed
- [x] Source last scan shows Never

---

## The per-source scan completes with compact progress visible.

![The per-source scan completes with compact progress visible.](./screenshots/002-scan-completed-compact-progress.png)

**Verifications:**
- [x] Scan job completed
- [x] Latest progress message is visible
- [x] Full job log is hidden by default
- [x] Source last scan is no longer Never
- [x] Source scan button is re-enabled
- [x] Scan table shows completed scan
- [x] Duplicate bytes summary is updated

---

## Reloading the dashboard restores the latest completed job status and thumbnail summary.

![Reloading the dashboard restores the latest completed job status and thumbnail summary.](./screenshots/003-completed-job-restored-after-reload.png)

**Verifications:**
- [x] Completed job status is restored
- [x] Thumbnail summary remains visible

---

## A scan row can restore its job status into the status panel.

![A scan row can restore its job status into the status panel.](./screenshots/004-scan-status-selected-from-table.png)

**Verifications:**
- [x] Selected scan status is visible
- [x] Selected scan thumbnail summary is visible

---

## Opening the job log reveals the scrollable acquisition log.

![Opening the job log reveals the scrollable acquisition log.](./screenshots/005-job-log-opened.png)

**Verifications:**
- [x] Job log contains acquisition messages
- [x] Open log button changed to Close log

---

## The acquired count opens a thumbnail grid with image links.

![The acquired count opens a thumbnail grid with image links.](./screenshots/006-acquired-files-drilldown.png)

**Verifications:**
- [x] Photos heading is visible
- [x] Photo grid lists A.JPG by filename
- [x] Photo grid does not show the absolute source path
- [x] Generated thumbnails are visible
- [x] First acquired file opens the image view
- [x] First thumbnail serves image/jpeg

---

## The image view shows the original image and a readable information side panel.

![The image view shows the original image and a readable information side panel.](./screenshots/007-image-exif-side-panel.png)

**Verifications:**
- [x] Image view renders the photo
- [x] Open original serves image/jpeg
- [x] EXIF panel is visible
- [x] Camera summary is visible
- [x] Capture date summary is visible
- [x] Location summary is visible
- [x] Raw EXIF debug section is available

---

## The date browser lists years derived from raw EXIF metadata.

![The date browser lists years derived from raw EXIF metadata.](./screenshots/008-photos-by-date-years.png)

**Verifications:**
- [x] Photos by date heading is visible
- [x] Capture year 2012 is listed
- [x] Duplicate content is counted once in the year bucket

---

## Selecting a year lists capture months.

![Selecting a year lists capture months.](./screenshots/009-photos-by-date-months.png)

**Verifications:**
- [x] Selected year heading is visible
- [x] Capture month 2012-07 is listed

---

## Selecting a month lists capture days.

![Selecting a month lists capture days.](./screenshots/010-photos-by-date-days.png)

**Verifications:**
- [x] Selected month heading is visible
- [x] Capture day 2012-07-04 is listed

---

## Selecting a capture day opens a thumbnail grid for that date.

![Selecting a capture day opens a thumbnail grid for that date.](./screenshots/011-photos-by-date-thumbnails.png)

**Verifications:**
- [x] Selected capture day heading is visible
- [x] Date photo grid lists the representative filename
- [x] Date photo grid does not show duplicate content twice
- [x] Date thumbnail is visible

---

## The metadata review page shows extraction results and photos where no metadata was found.

![The metadata review page shows extraction results and photos where no metadata was found.](./screenshots/012-metadata-review.png)

**Verifications:**
- [x] Metadata heading is visible
- [x] One unique content item has extracted metadata
- [x] One photo has no metadata found
- [x] No current extractor work remains unscanned
- [x] Failed metadata list identifies bad.JPG
- [x] Failed metadata list shows the extraction error
- [x] Unscanned metadata empty state is visible

---

## The dashboard can trigger a metadata refresh for photos without recorded metadata results.

![The dashboard can trigger a metadata refresh for photos without recorded metadata results.](./screenshots/013-metadata-refresh-triggered.png)

**Verifications:**
- [x] Metadata refresh job completed
- [x] Metadata refresh reports no missing work after scan-time extraction

---

