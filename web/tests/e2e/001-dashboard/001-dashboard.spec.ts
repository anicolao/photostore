import { expect, test } from '@playwright/test';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { TestStepHelper } from '../helpers/test-step-helper';

const fixtureJPEG = Buffer.from(
  '/9j/2wCEAAUDBAQEAwUEBAQFBQUGBwwIBwcHBw8LCwkMEQ8SEhEPERETFhwXExQaFRERGCEYGh0dHx8fExciJCIeJBweHx4BBQUFBwYHDggIDh4UERQeHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHv/AABEIABIAGAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AM+20/p8taltp/T5a2rbT+ny1p22n9Plr1K2P8zx8tzLbUxbbT+ny1Y/s/8A2a6W20/p8tWP7P8A9mvMnj9dz7fDZl7m5n2wHHArTtgOOBWbbdq07btXJWPyPLehp2wHHAqxgegqvbdqsV5k9z7bD/Af/9k=',
  'base64'
);

test('dashboard loads and scans a source root', async ({ page }, testInfo) => {
  const tester = new TestStepHelper(page, testInfo);
  tester.setMetadata('Dashboard Source Scan', 'Register a source root, scan it, inspect the compact progress log, and drill into acquired photo thumbnails.');

  await page.goto('/');
  await tester.step('empty-dashboard', {
    description: 'The initialized store dashboard starts empty.',
    verifications: [
      { spec: 'Photostore heading is visible', check: async () => await expect(page.getByRole('heading', { name: 'Photostore' })).toBeVisible() },
      { spec: 'Source count is zero', check: async () => await expect(page.getByTestId('source-count')).toHaveText('0') },
      { spec: 'Recent scans empty state is visible', check: async () => await expect(page.getByTestId('scans-empty')).toBeVisible() }
    ]
  });

  const source = '/tmp/photostore-e2e-source';
  rmSync(source, { recursive: true, force: true });
  mkdirSync(source, { recursive: true });
  writeFileSync(`${source}/A.JPG`, fixtureJPEG);
  writeFileSync(`${source}/B.jpeg`, fixtureJPEG);
  writeFileSync(`${source}/notes.txt`, 'not media');

  await page.getByTestId('source-path-input').fill(source);
  await page.getByTestId('source-label-input').fill('fixture');
  await page.getByRole('button', { name: 'Add' }).click();
  await tester.step('source-registered', {
    description: 'The fixture source root is registered and has never been scanned.',
    verifications: [
      { spec: 'Source count is one', check: async () => await expect(page.getByTestId('source-count')).toHaveText('1') },
      { spec: 'Fixture source is listed', check: async () => await expect(page.getByTestId('source-list')).toContainText('fixture') },
      { spec: 'Source last scan shows Never', check: async () => await expect(page.getByTestId('source-list')).toContainText('Last scan: Never') }
    ]
  });

  await page.getByTestId('source-list').getByRole('button', { name: 'Scan' }).click();
  await tester.step('scan-completed-compact-progress', {
    description: 'The per-source scan completes with compact progress visible.',
    verifications: [
      { spec: 'Scan job completed', check: async () => await expect(page.getByTestId('job-status')).toContainText('completed', { timeout: 10_000 }) },
      { spec: 'Latest progress message is visible', check: async () => await expect(page.getByTestId('job-latest-progress')).toBeVisible() },
      { spec: 'Full job log is hidden by default', check: async () => await expect(page.getByTestId('job-log')).toHaveCount(0) },
      { spec: 'Source last scan is no longer Never', check: async () => await expect(page.getByTestId('source-list')).not.toContainText('Last scan: Never') },
      { spec: 'Scan table shows completed scan', check: async () => await expect(page.getByTestId('scan-table')).toContainText('completed') },
      { spec: 'Duplicate bytes summary is updated', check: async () => await expect(page.getByTestId('duplicate-garbage-bytes')).toHaveText(String(fixtureJPEG.length)) }
    ]
  });

  await page.reload();
  await tester.step('completed-job-restored-after-reload', {
    description: 'Reloading the dashboard restores the latest completed job status and thumbnail summary.',
    verifications: [
      { spec: 'Completed job status is restored', check: async () => await expect(page.getByTestId('job-status')).toContainText('completed') },
      { spec: 'Thumbnail summary remains visible', check: async () => await expect(page.getByTestId('job-latest-progress')).toContainText('thumbnails generated') }
    ]
  });

  await page.getByTestId('scan-table').getByRole('button', { name: 'Status' }).click();
  await tester.step('scan-status-selected-from-table', {
    description: 'A scan row can restore its job status into the status panel.',
    verifications: [
      { spec: 'Selected scan status is visible', check: async () => await expect(page.getByTestId('job-status')).toContainText('completed') },
      { spec: 'Selected scan thumbnail summary is visible', check: async () => await expect(page.getByTestId('job-latest-progress')).toContainText('thumbnails generated') }
    ]
  });

  await page.getByTestId('toggle-job-log').click();
  await tester.step('job-log-opened', {
    description: 'Opening the job log reveals the scrollable acquisition log.',
    verifications: [
      { spec: 'Job log contains acquisition messages', check: async () => await expect(page.getByTestId('job-log')).toContainText('acquiring') },
      { spec: 'Open log button changed to Close log', check: async () => await expect(page.getByTestId('toggle-job-log')).toHaveText('Close log') }
    ]
  });

  await page.getByTestId('scan-acquired-link').click();
  await tester.step('acquired-files-drilldown', {
    description: 'The acquired count opens a thumbnail grid with image links.',
    verifications: [
      { spec: 'Photos heading is visible', check: async () => await expect(page.getByRole('heading', { name: 'Photos' })).toBeVisible() },
      { spec: 'Photo grid lists A.JPG by filename', check: async () => await expect(page.getByTestId('photo-grid')).toContainText('A.JPG') },
      { spec: 'Photo grid does not show the absolute source path', check: async () => await expect(page.getByTestId('photo-grid')).not.toContainText('/tmp/photostore-e2e-source') },
      { spec: 'Generated thumbnails are visible', check: async () => await expect(page.getByTestId('thumbnail-image').first()).toBeVisible() },
      {
        spec: 'First acquired file link serves image/jpeg',
        check: async () => {
          const imageHref = await page.getByTestId('photo-card').first().getAttribute('href');
          expect(imageHref).toBeTruthy();
          const imageResponse = await page.request.get(imageHref!);
          expect(imageResponse.ok()).toBe(true);
          expect(imageResponse.headers()['content-type']).toContain('image/jpeg');
        }
      },
      {
        spec: 'First thumbnail serves image/jpeg',
        check: async () => {
          const thumbnailSrc = await page.getByTestId('thumbnail-image').first().getAttribute('src');
          expect(thumbnailSrc).toBeTruthy();
          const thumbnailResponse = await page.request.get(thumbnailSrc!);
          expect(thumbnailResponse.ok()).toBe(true);
          expect(thumbnailResponse.headers()['content-type']).toContain('image/jpeg');
        }
      }
    ]
  });

  tester.generateDocs();
});
