# Test: Dashboard Source Scan

Register a source root, scan it, inspect the compact progress log, and drill into acquired photo thumbnails.

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
- [x] First acquired file link serves image/jpeg
- [x] First thumbnail serves image/jpeg

---

