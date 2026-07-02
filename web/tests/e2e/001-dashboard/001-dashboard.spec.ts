import { expect, test } from '@playwright/test';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { TestStepHelper } from '../helpers/test-step-helper';

const fixtureJPEG = Buffer.from(
  '/9j/2wCEAAUDBAQEAwUEBAQFBQUGBwwIBwcHBw8LCwkMEQ8SEhEPERETFhwXExQaFRERGCEYGh0dHx8fExciJCIeJBweHx4BBQUFBwYHDggIDh4UERQeHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHv/AABEIABIAGAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AM+20/p8taltp/T5a2rbT+ny1p22n9Plr1K2P8zx8tzLbUxbbT+ny1Y/s/8A2a6W20/p8tWP7P8A9mvMnj9dz7fDZl7m5n2wHHArTtgOOBWbbdq07btXJWPyPLehp2wHHAqxgegqvbdqsV5k9z7bD/Af/9k=',
  'base64'
);
const fixtureJPEGWithEXIF = jpegWithEXIF(fixtureJPEG, [
  [0x9003, '2012:07:04 18:22:11'],
  [0x9011, '-04:00']
]);

test('dashboard loads and scans a source root', async ({ page }, testInfo) => {
  const tester = new TestStepHelper(page, testInfo);
  tester.setMetadata('Dashboard Source Scan', 'Register a source root, scan it, inspect progress, drill into thumbnails, browse photos by date, and trigger metadata refresh.');

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
  writeFileSync(`${source}/A.JPG`, fixtureJPEGWithEXIF);
  writeFileSync(`${source}/B.jpeg`, fixtureJPEGWithEXIF);
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
      { spec: 'Source scan button is re-enabled', check: async () => await expect(page.getByTestId('source-list').getByRole('button', { name: 'Scan' })).toBeEnabled() },
      { spec: 'Scan table shows completed scan', check: async () => await expect(page.getByTestId('scan-table')).toContainText('completed') },
      { spec: 'Duplicate bytes summary is updated', check: async () => await expect(page.getByTestId('duplicate-garbage-bytes')).toHaveText(String(fixtureJPEGWithEXIF.length)) }
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

  await page.getByRole('link', { name: 'Dashboard' }).click();
  await page.getByTestId('photos-by-date-link').click();
  await tester.step('photos-by-date-years', {
    description: 'The date browser lists years derived from raw EXIF metadata.',
    verifications: [
      { spec: 'Photos by date heading is visible', check: async () => await expect(page.getByRole('heading', { name: 'Photos by date' })).toBeVisible() },
      { spec: 'Capture year 2012 is listed', check: async () => await expect(page.getByTestId('date-bucket-grid')).toContainText('2012') },
      { spec: 'Duplicate content is counted once in the year bucket', check: async () => await expect(page.getByTestId('date-bucket-grid')).toContainText('1 photos') }
    ]
  });

  await page.getByTestId('date-bucket').click();
  await tester.step('photos-by-date-months', {
    description: 'Selecting a year lists capture months.',
    verifications: [
      { spec: 'Selected year heading is visible', check: async () => await expect(page.getByRole('heading', { name: '2012' })).toBeVisible() },
      { spec: 'Capture month 2012-07 is listed', check: async () => await expect(page.getByTestId('date-bucket-grid')).toContainText('2012-07') }
    ]
  });

  await page.getByTestId('date-bucket').click();
  await tester.step('photos-by-date-days', {
    description: 'Selecting a month lists capture days.',
    verifications: [
      { spec: 'Selected month heading is visible', check: async () => await expect(page.getByRole('heading', { name: '2012-07' })).toBeVisible() },
      { spec: 'Capture day 2012-07-04 is listed', check: async () => await expect(page.getByTestId('date-bucket-grid')).toContainText('2012-07-04') }
    ]
  });

  await page.getByTestId('date-bucket').click();
  await tester.step('photos-by-date-thumbnails', {
    description: 'Selecting a capture day opens a thumbnail grid for that date.',
    verifications: [
      { spec: 'Selected capture day heading is visible', check: async () => await expect(page.getByRole('heading', { name: '2012-07-04' })).toBeVisible() },
      { spec: 'Date photo grid lists the representative filename', check: async () => await expect(page.getByTestId('date-photo-grid')).toContainText('A.JPG') },
      { spec: 'Date photo grid does not show duplicate content twice', check: async () => await expect(page.getByTestId('date-photo-card')).toHaveCount(1) },
      { spec: 'Date thumbnail is visible', check: async () => await expect(page.getByTestId('date-thumbnail-image')).toBeVisible() }
    ]
  });

  await page.goto('/');
  await page.getByTestId('refresh-metadata').click();
  await tester.step('metadata-refresh-triggered', {
    description: 'The dashboard can trigger a metadata refresh for photos without recorded metadata results.',
    verifications: [
      { spec: 'Metadata refresh job completed', check: async () => await expect(page.getByTestId('job-status')).toContainText('metadata_refresh_missing: completed', { timeout: 10_000 }) },
      { spec: 'Metadata refresh reports no missing work after scan-time extraction', check: async () => await expect(page.getByTestId('job-latest-progress')).toContainText('metadata refresh attempted: 0') }
    ]
  });

  tester.generateDocs();
});

function jpegWithEXIF(base: Buffer, fields: Array<[number, string]>) {
  const payload = exifPayload(fields);
  const segmentLength = payload.length + 2;
  const app1 = Buffer.from([0xff, 0xe1, segmentLength >> 8, segmentLength & 0xff]);
  return Buffer.concat([base.subarray(0, 2), app1, payload, base.subarray(2)]);
}

function exifPayload(fields: Array<[number, string]>) {
  const tiffParts: Buffer[] = [];
  const dataParts: Buffer[] = [];
  const writeUInt16 = (value: number) => {
    const out = Buffer.alloc(2);
    out.writeUInt16LE(value);
    tiffParts.push(out);
  };
  const writeUInt32 = (value: number) => {
    const out = Buffer.alloc(4);
    out.writeUInt32LE(value);
    tiffParts.push(out);
  };

  tiffParts.push(Buffer.from('II'));
  writeUInt16(42);
  writeUInt32(8);
  writeUInt16(1);
  writeUInt16(0x8769);
  writeUInt16(4);
  writeUInt32(1);
  const exifIFDOffset = 8 + 2 + 12 + 4;
  writeUInt32(exifIFDOffset);
  writeUInt32(0);
  writeUInt16(fields.length);

  const dataStart = exifIFDOffset + 2 + fields.length * 12 + 4;
  for (const [tag, text] of fields) {
    const value = Buffer.concat([Buffer.from(text), Buffer.from([0])]);
    writeUInt16(tag);
    writeUInt16(2);
    writeUInt32(value.length);
    if (value.length <= 4) {
      const inline = Buffer.alloc(4);
      value.copy(inline);
      tiffParts.push(inline);
    } else {
      const dataLength = dataParts.reduce((sum, part) => sum + part.length, 0);
      writeUInt32(dataStart + dataLength);
      dataParts.push(value);
    }
  }
  writeUInt32(0);
  return Buffer.concat([Buffer.from('Exif\0\0'), ...tiffParts, ...dataParts]);
}
