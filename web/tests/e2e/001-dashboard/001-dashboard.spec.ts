import { expect, test } from '@playwright/test';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { TestStepHelper } from '../helpers/test-step-helper';

const fixtureJPEG = Buffer.from(
  '/9j/2wCEAAUDBAQEAwUEBAQFBQUGBwwIBwcHBw8LCwkMEQ8SEhEPERETFhwXExQaFRERGCEYGh0dHx8fExciJCIeJBweHx4BBQUFBwYHDggIDh4UERQeHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHv/AABEIABIAGAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AM+20/p8taltp/T5a2rbT+ny1p22n9Plr1K2P8zx8tzLbUxbbT+ny1Y/s/8A2a6W20/p8tWP7P8A9mvMnj9dz7fDZl7m5n2wHHArTtgOOBWbbdq07btXJWPyPLehp2wHHAqxgegqvbdqsV5k9z7bD/Af/9k=',
  'base64'
);
const fixtureJPEGWithEXIF = jpegWithEXIF(fixtureJPEG, [
  [0x010f, 'Canon'],
  [0x0110, 'EOS 5D'],
  [0x9003, '2012:07:04 18:22:11'],
  [0x9011, '-04:00']
], {
  latitudeRef: 'N',
  latitude: [[45, 1], [7, 1], [3367, 100]],
  longitudeRef: 'W',
  longitude: [[79, 1], [38, 1], [2323, 100]]
});
const fixturePosterJPEG = jpegWithoutEXIFDimensions(2400, 1600);
const fixtureBadJPEG = Buffer.from('not really a jpeg');
const expectedDuplicateBytes = fixtureJPEGWithEXIF.length;

test('dashboard loads and scans a source root', async ({ page }, testInfo) => {
  const tester = new TestStepHelper(page, testInfo);
  tester.setMetadata('Dashboard Source Scan', 'Register a source root, scan it, inspect progress, drill into thumbnails, browse photos by date, and trigger metadata refresh.');
  let jobPanelWidth = 0;

  await page.goto('/');
  await tester.step('empty-dashboard', {
    description: 'The initialized store dashboard starts empty.',
    verifications: [
      { spec: 'Photostore heading is visible', check: async () => await expect(page.getByRole('heading', { name: 'Photostore' })).toBeVisible() },
      { spec: 'UI build hash is visible', check: async () => await expect(page.getByTestId('ui-build-hash')).toHaveText('UI e2e-build') },
      { spec: 'Source count is zero', check: async () => await expect(page.getByTestId('source-count')).toHaveText('0') },
      { spec: 'Thumbnail garbage starts at zero', check: async () => await expect(page.getByTestId('thumbnail-garbage-bytes')).toHaveText('0') },
      { spec: 'Recent scans empty state is visible', check: async () => await expect(page.getByTestId('scans-empty')).toBeVisible() }
    ]
  });

  const source = '/tmp/photostore-e2e-source';
  rmSync(source, { recursive: true, force: true });
  mkdirSync(source, { recursive: true });
  writeFileSync(`${source}/A.JPG`, fixtureJPEGWithEXIF);
  writeFileSync(`${source}/B.jpeg`, fixtureJPEGWithEXIF);
  writeFileSync(`${source}/poster.JPG`, fixturePosterJPEG);
  writeFileSync(`${source}/crop.JPG`, fixtureJPEG);
  writeFileSync(`${source}/bad.JPG`, fixtureBadJPEG);
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
      { spec: 'Scan job completed', check: async () => await expect(page.getByTestId('job-status')).toContainText('completed') },
      { spec: 'Latest progress message is visible', check: async () => await expect(page.getByTestId('job-latest-progress')).toBeVisible() },
      {
        spec: 'Latest progress message is capped at 60 visible characters',
        check: async () => {
          const text = await page.getByTestId('job-latest-progress').innerText();
          expect(text.length).toBeLessThanOrEqual(60);
        }
      },
      {
        spec: 'Job panel has a stable reserved width',
        check: async () => {
          const box = await page.getByTestId('job-panel').boundingBox();
          expect(box).not.toBeNull();
          jobPanelWidth = Math.round(box!.width);
          expect(jobPanelWidth).toBeGreaterThan(0);
        }
      },
      { spec: 'Full job log is hidden by default', check: async () => await expect(page.getByTestId('job-log')).toHaveCount(0) },
      { spec: 'Source last scan is no longer Never', check: async () => await expect(page.getByTestId('source-list')).not.toContainText('Last scan: Never') },
      { spec: 'Source scan button is re-enabled', check: async () => await expect(page.getByTestId('source-list').getByRole('button', { name: 'Scan' })).toBeEnabled() },
      { spec: 'Scan table shows completed scan', check: async () => await expect(page.getByTestId('scan-table')).toContainText('completed') },
      { spec: 'Duplicate bytes summary is updated', check: async () => await expect(page.getByTestId('duplicate-garbage-bytes')).toHaveText(new Intl.NumberFormat('en-CA').format(expectedDuplicateBytes)) }
    ]
  });

  await page.reload();
  await tester.step('completed-job-restored-after-reload', {
    description: 'Reloading the dashboard restores the latest completed job status and thumbnail summary.',
    verifications: [
      { spec: 'Completed job status is restored', check: async () => await expect(page.getByTestId('job-status')).toContainText('completed') },
      { spec: 'Thumbnail summary remains available on the compact progress line', check: async () => await expect(page.getByTestId('job-latest-progress')).toHaveAttribute('title', /thumbnails generated/) }
    ]
  });

  await page.getByTestId('deduplicate-duplicates').click();
  await tester.step('duplicates-deduplicated', {
    description: 'The dashboard verifies retained duplicates and releases duplicate bytes.',
    verifications: [
      { spec: 'Deduplication job completed', check: async () => await expect(page.getByTestId('job-status')).toContainText('duplicate_deduplication: completed') },
      { spec: 'Deduplication progress reports released bytes', check: async () => await expect(page.getByTestId('job-latest-progress')).toHaveAttribute('title', /bytes released/) },
      {
        spec: 'Job panel width does not change for deduplication progress',
        check: async () => {
          const box = await page.getByTestId('job-panel').boundingBox();
          expect(box).not.toBeNull();
          expect(Math.round(box!.width)).toBe(jobPanelWidth);
        }
      },
      { spec: 'Retained duplicate bytes drop to zero', check: async () => await expect(page.getByTestId('duplicate-garbage-bytes')).toHaveText('0') },
      { spec: 'Deduplicate button disables when no duplicate bytes remain', check: async () => await expect(page.getByTestId('deduplicate-duplicates')).toBeDisabled() }
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

  const staleThumbnail = '/tmp/photostore-e2e-store/thumbnails/jpeg/240/old-renderer/aa/bb/stale.jpg';
  mkdirSync('/tmp/photostore-e2e-store/thumbnails/jpeg/240/old-renderer/aa/bb', { recursive: true });
  writeFileSync(staleThumbnail, 'stale thumbnail');
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByTestId('thumbnail-garbage-bytes')).toHaveText('15');
  await page.getByTestId('collect-thumbnail-garbage').click();
  await tester.step('thumbnail-garbage-collected', {
    description: 'The dashboard reports stale thumbnail renderer output and removes it through explicit garbage collection.',
    verifications: [
      { spec: 'Thumbnail garbage collection job completed', check: async () => await expect(page.getByTestId('job-status')).toContainText('thumbnail_gc: completed') },
      { spec: 'Thumbnail garbage progress reports removed bytes', check: async () => await expect(page.getByTestId('job-latest-progress')).toHaveAttribute('title', /thumbnail garbage bytes removed/) },
      { spec: 'Thumbnail garbage byte counter drops to zero', check: async () => await expect(page.getByTestId('thumbnail-garbage-bytes')).toHaveText('0') },
      { spec: 'Thumbnail garbage button disables after collection', check: async () => await expect(page.getByTestId('collect-thumbnail-garbage')).toBeDisabled() }
    ]
  });

  await page.getByTestId('scan-table').getByRole('button', { name: 'Status' }).click();
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
      { spec: 'First acquired file opens the image view', check: async () => await expect(page.getByTestId('photo-card').first()).toHaveAttribute('href', /\/objects\//) },
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

  await page.getByTestId('photo-card').first().click();
  await page.getByTestId('toggle-exif').click();
  await tester.step('image-exif-side-panel', {
    description: 'The image view shows the original image and a readable information side panel.',
    verifications: [
      { spec: 'Image view renders the photo', check: async () => await expect(page.getByTestId('object-image')).toBeVisible() },
      { spec: 'Open original serves image/jpeg', check: async () => {
        const originalHref = await page.getByTestId('open-original').getAttribute('href');
        expect(originalHref).toBeTruthy();
        const imageResponse = await page.request.get(originalHref!);
        expect(imageResponse.ok()).toBe(true);
        expect(imageResponse.headers()['content-type']).toContain('image/jpeg');
      } },
      { spec: 'EXIF panel is visible', check: async () => await expect(page.getByTestId('exif-panel')).toBeVisible() },
      { spec: 'Camera summary is visible', check: async () => await expect(page.getByTestId('photo-camera')).toHaveText('Canon EOS 5D') },
      { spec: 'Capture date summary is visible', check: async () => await expect(page.getByTestId('photo-date')).toHaveText('2012-07-04 18:22:11') },
      { spec: 'Location summary is visible', check: async () => await expect(page.getByTestId('photo-location')).toHaveText('45.126019, -79.639786') },
      { spec: 'Raw EXIF debug section is available', check: async () => await expect(page.getByTestId('raw-exif')).toContainText('Raw EXIF') }
    ]
  });

  await page.goto('/');
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
  await page.getByTestId('metadata-link').click();
  await tester.step('metadata-review', {
    description: 'The metadata review page shows extraction results and photos where no metadata was found.',
    verifications: [
      { spec: 'Metadata heading is visible', check: async () => await expect(page.getByRole('heading', { name: 'Metadata', exact: true })).toBeVisible() },
      { spec: 'One unique content item has extracted metadata', check: async () => await expect(page.getByTestId('metadata-extracted-count')).toHaveText('1') },
      { spec: 'Three photos have no metadata found', check: async () => await expect(page.getByTestId('metadata-failed-count')).toHaveText('3') },
      { spec: 'No current extractor work remains unscanned', check: async () => await expect(page.getByTestId('metadata-missing-count')).toHaveText('0') },
      { spec: 'Failed metadata list identifies bad.JPG', check: async () => await expect(page.getByTestId('metadata-failures-list')).toContainText('bad.JPG') },
      { spec: 'Main metadata failure list keeps the large no-EXIF photo visible', check: async () => await expect(page.getByTestId('metadata-failures-list')).toContainText('poster.JPG') },
      { spec: 'Main metadata failure list hides the small crop by default', check: async () => await expect(page.getByTestId('metadata-failures-list')).not.toContainText('crop.JPG') },
      { spec: 'Small metadata failures are collapsed with a count', check: async () => await expect(page.getByTestId('metadata-small-failures-count')).toHaveText('1') },
      { spec: 'Failed metadata list shows the extraction error', check: async () => await expect(page.getByTestId('metadata-failures-list')).toContainText('not a JPEG file') },
      { spec: 'Opening likely thumbnails or crops reveals crop.JPG', check: async () => {
        await page.getByTestId('metadata-small-failures').click();
        await expect(page.getByTestId('metadata-small-failures-list')).toContainText('crop.JPG');
      } },
      { spec: 'Unscanned metadata empty state is visible', check: async () => await expect(page.getByTestId('metadata-missing-empty')).toBeVisible() }
    ]
  });

  await page.goto('/');
  await page.getByTestId('refresh-metadata').click();
  await tester.step('metadata-refresh-triggered', {
    description: 'The dashboard can trigger a metadata refresh for photos without recorded metadata results.',
    verifications: [
      { spec: 'Metadata refresh job completed', check: async () => await expect(page.getByTestId('job-status')).toContainText('metadata_refresh_missing: completed') },
      { spec: 'Metadata refresh reports no missing work after scan-time extraction', check: async () => await expect(page.getByTestId('job-latest-progress')).toHaveAttribute('title', /metadata refresh attempted: 0/) }
    ]
  });

  tester.generateDocs();
});

type GPSFixture = {
  latitudeRef: 'N' | 'S';
  latitude: Array<[number, number]>;
  longitudeRef: 'E' | 'W';
  longitude: Array<[number, number]>;
};

function jpegWithEXIF(base: Buffer, fields: Array<[number, string]>, gps?: GPSFixture) {
  const payload = exifPayload(fields, gps);
  const segmentLength = payload.length + 2;
  const app1 = Buffer.from([0xff, 0xe1, segmentLength >> 8, segmentLength & 0xff]);
  return Buffer.concat([base.subarray(0, 2), app1, payload, base.subarray(2)]);
}

function jpegWithoutEXIFDimensions(width: number, height: number) {
  const sof = Buffer.from([
    0xff, 0xc0,
    0x00, 0x11,
    0x08,
    (height >> 8) & 0xff, height & 0xff,
    (width >> 8) & 0xff, width & 0xff,
    0x03,
    0x01, 0x11, 0x00,
    0x02, 0x11, 0x00,
    0x03, 0x11, 0x00
  ]);
  const sos = Buffer.from([
    0xff, 0xda,
    0x00, 0x0c,
    0x03,
    0x01, 0x00,
    0x02, 0x11,
    0x03, 0x11,
    0x00, 0x3f, 0x00
  ]);
  return Buffer.concat([Buffer.from([0xff, 0xd8]), sof, sos, Buffer.from([0x00, 0xff, 0xd9])]);
}

function exifPayload(fields: Array<[number, string]>, gps?: GPSFixture) {
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
  writeUInt16(gps ? 2 : 1);
  writeUInt16(0x8769);
  writeUInt16(4);
  writeUInt32(1);
  const ifd0Size = 2 + (gps ? 2 : 1) * 12 + 4;
  const exifIFDOffset = 8 + ifd0Size;
  const exifIFDSize = 2 + fields.length * 12 + 4;
  const gpsFields = gps ? gpsIFDFields(gps) : [];
  const gpsIFDOffset = exifIFDOffset + exifIFDSize;
  const gpsIFDSize = gps ? 2 + gpsFields.length * 12 + 4 : 0;
  const dataStart = gpsIFDOffset + gpsIFDSize;
  writeUInt32(exifIFDOffset);
  if (gps) {
    writeUInt16(0x8825);
    writeUInt16(4);
    writeUInt32(1);
    writeUInt32(gpsIFDOffset);
  }
  writeUInt32(0);
  writeUInt16(fields.length);

  for (const [tag, text] of fields) {
    writeIFDEntry(tag, 2, Buffer.concat([Buffer.from(text), Buffer.from([0])]), dataStart, tiffParts, dataParts);
  }
  writeUInt32(0);

  if (gps) {
    writeUInt16(gpsFields.length);
    for (const field of gpsFields) {
      writeIFDEntry(field.tag, field.type, field.value, dataStart, tiffParts, dataParts);
    }
    writeUInt32(0);
  }
  return Buffer.concat([Buffer.from('Exif\0\0'), ...tiffParts, ...dataParts]);
}

function gpsIFDFields(gps: GPSFixture) {
  return [
    { tag: 0x0001, type: 2, value: asciiValue(gps.latitudeRef) },
    { tag: 0x0002, type: 5, value: rationalArray(gps.latitude) },
    { tag: 0x0003, type: 2, value: asciiValue(gps.longitudeRef) },
    { tag: 0x0004, type: 5, value: rationalArray(gps.longitude) }
  ];
}

function writeIFDEntry(tag: number, type: number, value: Buffer, dataStart: number, tiffParts: Buffer[], dataParts: Buffer[]) {
  const writeUInt16 = (target: Buffer[], next: number) => {
    const out = Buffer.alloc(2);
    out.writeUInt16LE(next);
    target.push(out);
  };
  const writeUInt32 = (target: Buffer[], next: number) => {
    const out = Buffer.alloc(4);
    out.writeUInt32LE(next);
    target.push(out);
  };
  writeUInt16(tiffParts, tag);
  writeUInt16(tiffParts, type);
  writeUInt32(tiffParts, exifValueCount(type, value));
  if (value.length <= 4) {
    const inline = Buffer.alloc(4);
    value.copy(inline);
    tiffParts.push(inline);
  } else {
    const dataLength = dataParts.reduce((sum, part) => sum + part.length, 0);
    writeUInt32(tiffParts, dataStart + dataLength);
    dataParts.push(value);
  }
}

function exifValueCount(type: number, value: Buffer) {
  if (type === 5) return value.length / 8;
  return value.length;
}

function asciiValue(value: string) {
  return Buffer.concat([Buffer.from(value), Buffer.from([0])]);
}

function rationalArray(values: Array<[number, number]>) {
  const out = Buffer.alloc(values.length * 8);
  values.forEach(([num, den], index) => {
    out.writeUInt32LE(num, index * 8);
    out.writeUInt32LE(den, index * 8 + 4);
  });
  return out;
}
