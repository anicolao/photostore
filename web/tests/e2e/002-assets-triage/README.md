# Test: Asset Triage

Scan duplicate JPEG content, filter the triage queue, navigate asset decisions, set labels, and verify reviewed assets.

## The triage fixture source is scanned and assets are available from the dashboard.

![The triage fixture source is scanned and assets are available from the dashboard.](./screenshots/000-triage-source-scanned.png)

**Verifications:**
- [x] Scan job completed
- [x] Assets entry point is visible

---

## The asset grid shows duplicated JPEG content as one asset with default triage state.

![The asset grid shows duplicated JPEG content as one asset with default triage state.](./screenshots/001-asset-grid-defaults.png)

**Verifications:**
- [x] Assets heading is visible
- [x] Duplicate fixture content appears as one asset card
- [x] Asset pager reports the current page range
- [x] Default quality is Unrated
- [x] Default status is Triage
- [x] Default visibility is Normal

---

## Filter buttons build a triage queue of dated photos above one megapixel sorted by capture date.

![Filter buttons build a triage queue of dated photos above one megapixel sorted by capture date.](./screenshots/002-triage-queue-filtered.png)

**Verifications:**
- [x] Triage status filter is active
- [x] Known date filter is active
- [x] Large image filter is active
- [x] Date ascending sort is active by default
- [x] Large dated triage item A is visible
- [x] Large dated triage item B is visible
- [x] Small dated item is excluded
- [x] No-date item is excluded

---

## The asset detail view shows triage controls, navigation, and both source occurrences.

![The asset detail view shows triage controls, navigation, and both source occurrences.](./screenshots/003-asset-detail-provenance.png)

**Verifications:**
- [x] Asset detail thumbnail is visible
- [x] Asset source count is two
- [x] Source provenance lists original fixture path
- [x] Source provenance lists duplicate fixture path
- [x] Advance to next is checked by default
- [x] Next asset navigation is available

---

## Setting quality marks the asset reviewed in the reducer and advances to the next triage item.

![Setting quality marks the asset reviewed in the reducer and advances to the next triage item.](./screenshots/004-asset-quality-advances.png)

**Verifications:**
- [x] The detail view advanced to TRIAGE_B.JPG
- [x] The reviewed asset left the Triage navigation queue
- [x] The next asset remains in Triage

---

## Turning off advance keeps the current asset visible while quality still marks it reviewed.

![Turning off advance keeps the current asset visible while quality still marks it reviewed.](./screenshots/005-asset-quality-no-advance.png)

**Verifications:**
- [x] The detail view remains on TRIAGE_B.JPG
- [x] Good quality is selected
- [x] Reviewed status is selected by the quality reducer

---

## The asset detail view records quality, status, visibility, and a user-defined label.

![The asset detail view records quality, status, visibility, and a user-defined label.](./screenshots/006-asset-triaged.png)

**Verifications:**
- [x] Best quality is selected
- [x] Reviewed status is selected
- [x] Private visibility is selected
- [x] Family label is visible

---

## A direct status query URL filters the asset grid and preserves the active filter state.

![A direct status query URL filters the asset grid and preserves the active filter state.](./screenshots/007-asset-status-query-filter.png)

**Verifications:**
- [x] Reviewed status filter is active
- [x] Status-filtered grid contains the reviewed asset
- [x] Status-filtered grid also contains the second reviewed asset
- [x] Status-filtered pager shows two reviewed assets

---

## Filter buttons combine quality disjunction with status, visibility, and label conjunctions.

![Filter buttons combine quality disjunction with status, visibility, and label conjunctions.](./screenshots/008-asset-filters.png)

**Verifications:**
- [x] Best filter is active
- [x] Good filter is active as a second quality choice
- [x] Reviewed filter is active
- [x] Private filter is active
- [x] Family label filter is active
- [x] Filtered grid still contains the triaged asset
- [x] Filtered grid excludes the Good asset because it is not private or labelled
- [x] URL preserves both selected quality values

---

## A user-defined label can be removed from the asset.

![A user-defined label can be removed from the asset.](./screenshots/009-asset-label-removed.png)

**Verifications:**
- [x] Family label is no longer visible

---

