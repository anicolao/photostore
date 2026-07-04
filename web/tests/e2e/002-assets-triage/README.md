# Test: Asset Triage

Scan duplicate JPEG content, open the asset view, set quality/status/visibility, manage labels, and filter the asset grid.

## The triage fixture source is scanned and assets are available from the dashboard.

![The triage fixture source is scanned and assets are available from the dashboard.](./screenshots/000-triage-source-scanned.png)

**Verifications:**
- [x] Scan job completed
- [x] Assets entry point is visible

---

## The asset grid shows one asset for duplicated JPEG content with default triage state.

![The asset grid shows one asset for duplicated JPEG content with default triage state.](./screenshots/001-asset-grid-defaults.png)

**Verifications:**
- [x] Assets heading is visible
- [x] Duplicate fixture content appears as one asset card
- [x] Default quality is Unrated
- [x] Default status is Triage
- [x] Default visibility is Normal

---

## The asset detail view shows triage controls and both source occurrences.

![The asset detail view shows triage controls and both source occurrences.](./screenshots/002-asset-detail-provenance.png)

**Verifications:**
- [x] Asset detail thumbnail is visible
- [x] Asset source count is two
- [x] Source provenance lists original fixture path
- [x] Source provenance lists duplicate fixture path

---

## The asset detail view records quality, status, visibility, and a user-defined label.

![The asset detail view records quality, status, visibility, and a user-defined label.](./screenshots/003-asset-triaged.png)

**Verifications:**
- [x] Best quality is selected
- [x] Reviewed status is selected
- [x] Private visibility is selected
- [x] Family label is visible

---

## The asset grid filters by quality, status, visibility, and user-defined label.

![The asset grid filters by quality, status, visibility, and user-defined label.](./screenshots/004-asset-filters.png)

**Verifications:**
- [x] Best filter is active
- [x] Reviewed filter is active
- [x] Private filter is active
- [x] Filtered grid still contains the triaged asset

---

## A user-defined label can be removed from the asset.

![A user-defined label can be removed from the asset.](./screenshots/005-asset-label-removed.png)

**Verifications:**
- [x] Family label is no longer visible

---

