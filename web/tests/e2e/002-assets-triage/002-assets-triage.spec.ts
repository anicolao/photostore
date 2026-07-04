import { expect, test } from '@playwright/test';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { TestStepHelper } from '../helpers/test-step-helper';

const fixtureJPEG = Buffer.from(
  '/9j/2wCEAAUDBAQEAwUEBAQFBQUGBwwIBwcHBw8LCwkMEQ8SEhEPERETFhwXExQaFRERGCEYGh0dHx8fExciJCIeJBweHx4BBQUFBwYHDggIDh4UERQeHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHv/AABEIABIAGAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AM+20/p8taltp/T5a2rbT+ny1p22n9Plr1K2P8zx8tzLbUxbbT+ny1Y/s/8A2a6W20/p8tWP7P8A9mvMnj9dz7fDZl7m5n2wHHArTtgOOBWbbdq07btXJWPyPLehp2wHHAqxgegqvbdqsV5k9z7bD/Af/9k=',
  'base64'
);

test('asset triage labels and filters assets', async ({ page }, testInfo) => {
  const tester = new TestStepHelper(page, testInfo);
  tester.setMetadata('Asset Triage', 'Scan duplicate JPEG content, open the asset view, set quality/status/visibility, manage labels, and filter the asset grid.');

  const source = '/tmp/photostore-e2e-triage-source';
  rmSync(source, { recursive: true, force: true });
  mkdirSync(source, { recursive: true });
  writeFileSync(`${source}/TRIAGE_A.JPG`, fixtureJPEG);
  writeFileSync(`${source}/TRIAGE_A_COPY.jpeg`, fixtureJPEG);

  await page.goto('/');
  await page.getByTestId('source-path-input').fill(source);
  await page.getByTestId('source-label-input').fill('triage-fixture');
  await page.getByRole('button', { name: 'Add' }).click();
  await page.getByTestId('source-list').locator('li').filter({ hasText: 'triage-fixture' }).getByRole('button', { name: 'Scan' }).click();
  await tester.step('triage-source-scanned', {
    description: 'The triage fixture source is scanned and assets are available from the dashboard.',
    verifications: [
      { spec: 'Scan job completed', check: async () => await expect(page.getByTestId('job-status')).toContainText('completed') },
      { spec: 'Assets entry point is visible', check: async () => await expect(page.getByTestId('assets-link')).toBeVisible() }
    ]
  });

  await page.getByTestId('assets-link').click();
  await tester.step('asset-grid-defaults', {
    description: 'The asset grid shows duplicated JPEG content as one asset with default triage state.',
    verifications: [
      { spec: 'Assets heading is visible', check: async () => await expect(page.getByRole('heading', { name: 'Assets' })).toBeVisible() },
      { spec: 'Duplicate fixture content appears as one asset card', check: async () => await expect(page.getByTestId('asset-card').filter({ hasText: 'TRIAGE_A.JPG' })).toHaveCount(1) },
      { spec: 'Asset pager reports the current page range', check: async () => await expect(page.getByTestId('asset-page-range')).toHaveText(/^Showing 1-\d+ of \d+$/) },
      { spec: 'Default quality is Unrated', check: async () => await expect(page.getByTestId('asset-card').filter({ hasText: 'TRIAGE_A.JPG' }).getByTestId('asset-quality')).toHaveText('Unrated') },
      { spec: 'Default status is Triage', check: async () => await expect(page.getByTestId('asset-card').filter({ hasText: 'TRIAGE_A.JPG' }).getByTestId('asset-status')).toHaveText('Triage') },
      { spec: 'Default visibility is Normal', check: async () => await expect(page.getByTestId('asset-card').filter({ hasText: 'TRIAGE_A.JPG' }).getByTestId('asset-visibility')).toHaveText('Normal') }
    ]
  });

  await page.getByTestId('asset-card').filter({ hasText: 'TRIAGE_A.JPG' }).click();
  await tester.step('asset-detail-provenance', {
    description: 'The asset detail view shows triage controls and both source occurrences.',
    verifications: [
      { spec: 'Asset detail thumbnail is visible', check: async () => await expect(page.getByTestId('asset-detail-thumbnail')).toBeVisible() },
      { spec: 'Asset source count is two', check: async () => await expect(page.getByTestId('asset-source-count')).toHaveText('2') },
      { spec: 'Source provenance lists original fixture path', check: async () => await expect(page.getByTestId('asset-sources')).toContainText('TRIAGE_A.JPG') },
      { spec: 'Source provenance lists duplicate fixture path', check: async () => await expect(page.getByTestId('asset-sources')).toContainText('TRIAGE_A_COPY.jpeg') }
    ]
  });

  await page.getByTestId('quality-Best').click();
  await page.getByTestId('status-Reviewed').click();
  await page.getByTestId('visibility-Private').click();
  await page.getByTestId('asset-label-input').fill('Family');
  await page.getByTestId('asset-label-add').click();
  await tester.step('asset-triaged', {
    description: 'The asset detail view records quality, status, visibility, and a user-defined label.',
    verifications: [
      { spec: 'Best quality is selected', check: async () => await expect(page.getByTestId('quality-Best')).toHaveClass(/active/) },
      { spec: 'Reviewed status is selected', check: async () => await expect(page.getByTestId('status-Reviewed')).toHaveClass(/active/) },
      { spec: 'Private visibility is selected', check: async () => await expect(page.getByTestId('visibility-Private')).toHaveClass(/active/) },
      { spec: 'Family label is visible', check: async () => await expect(page.getByTestId('asset-detail-labels')).toContainText('Family') }
    ]
  });

  await page.goto('/assets?status=Reviewed');
  await tester.step('asset-status-query-filter', {
    description: 'A direct status query URL filters the asset grid and preserves the active filter state.',
    verifications: [
      { spec: 'Reviewed status filter is active', check: async () => await expect(page.getByTestId('status-filter-Reviewed')).toHaveClass(/active/) },
      { spec: 'Status-filtered grid contains the reviewed asset', check: async () => await expect(page.getByTestId('asset-grid')).toContainText('TRIAGE_A.JPG') },
      { spec: 'Status-filtered pager shows one displayed asset', check: async () => await expect(page.getByTestId('asset-page-range')).toHaveText('Showing 1-1 of 1') }
    ]
  });

  await page.goto('/assets?quality=Best&status=Reviewed&visibility=Private&label=family');
  await tester.step('asset-filters', {
    description: 'The asset grid filters by quality, status, visibility, and user-defined label.',
    verifications: [
      { spec: 'Best filter is active', check: async () => await expect(page.getByTestId('quality-filter-Best')).toHaveClass(/active/) },
      { spec: 'Reviewed filter is active', check: async () => await expect(page.getByTestId('status-filter-Reviewed')).toHaveClass(/active/) },
      { spec: 'Private filter is active', check: async () => await expect(page.getByTestId('visibility-filter-Private')).toHaveClass(/active/) },
      { spec: 'Filtered grid still contains the triaged asset', check: async () => await expect(page.getByTestId('asset-grid')).toContainText('TRIAGE_A.JPG') }
    ]
  });

  await page.getByTestId('asset-card').filter({ hasText: 'TRIAGE_A.JPG' }).click();
  await page.getByTestId('remove-label-family').click();
  await tester.step('asset-label-removed', {
    description: 'A user-defined label can be removed from the asset.',
    verifications: [
      { spec: 'Family label is no longer visible', check: async () => await expect(page.getByTestId('asset-detail-labels')).not.toContainText('Family') }
    ]
  });

  tester.generateDocs();
});
