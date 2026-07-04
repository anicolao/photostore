import { expect, test } from '@playwright/test';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { TestStepHelper } from '../helpers/test-step-helper';

const fixtureJPEG = Buffer.from(
  '/9j/2wCEAAUDBAQEAwUEBAQFBQUGBwwIBwcHBw8LCwkMEQ8SEhEPERETFhwXExQaFRERGCEYGh0dHx8fExciJCIeJBweHx4BBQUFBwYHDggIDh4UERQeHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHv/AABEIABIAGAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AM+20/p8taltp/T5a2rbT+ny1p22n9Plr1K2P8zx8tzLbUxbbT+ny1Y/s/8A2a6W20/p8tWP7P8A9mvMnj9dz7fDZl7m5n2wHHArTtgOOBWbbdq07btXJWPyPLehp2wHHAqxgegqvbdqsV5k9z7bD/Af/9k=',
  'base64'
);

test('asset triage labels and filters assets', async ({ page }, testInfo) => {
  const tester = new TestStepHelper(page, testInfo);
  tester.setMetadata('Asset Triage', 'Scan duplicate JPEG content, filter the triage queue, navigate asset decisions, set labels, and verify reviewed assets.');

  const source = '/tmp/photostore-e2e-triage-source';
  rmSync(source, { recursive: true, force: true });
  mkdirSync(source, { recursive: true });
  const triageA = jpegWithEXIF(fixtureJPEG, [
    [0x9003, '2017:07:21 10:11:12'],
    [0xa002, '2000'],
    [0xa003, '1000']
  ]);
  const triageB = jpegWithEXIF(fixtureJPEG, [
    [0x9003, '2018:08:22 11:12:13'],
    [0xa002, '2000'],
    [0xa003, '1000']
  ]);
  const triageSmall = jpegWithEXIF(fixtureJPEG, [
    [0x9003, '2019:09:23 12:13:14'],
    [0xa002, '640'],
    [0xa003, '480']
  ]);
  writeFileSync(`${source}/TRIAGE_A.JPG`, triageA);
  writeFileSync(`${source}/TRIAGE_A_COPY.jpeg`, triageA);
  writeFileSync(`${source}/TRIAGE_B.JPG`, triageB);
  writeFileSync(`${source}/TRIAGE_SMALL.JPG`, triageSmall);
  writeFileSync(`${source}/TRIAGE_NODATE.JPG`, fixtureJPEG);

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

  await page.goto('/assets');
  await page.getByTestId('status-filter-Triage').click();
  await expect(page.getByTestId('status-filter-Triage')).toHaveClass(/active/);
  await page.getByTestId('date-filter-known').click();
  await expect(page.getByTestId('date-filter-known')).toHaveClass(/active/);
  await page.getByTestId('megapixel-filter-large').click();
  await expect(page.getByTestId('megapixel-filter-large')).toHaveClass(/active/);
  await tester.step('triage-queue-filtered', {
    description: 'Filter buttons build a triage queue of dated photos above one megapixel sorted by capture date.',
    verifications: [
      { spec: 'Triage status filter is active', check: async () => await expect(page.getByTestId('status-filter-Triage')).toHaveClass(/active/) },
      { spec: 'Known date filter is active', check: async () => await expect(page.getByTestId('date-filter-known')).toHaveClass(/active/) },
      { spec: 'Large image filter is active', check: async () => await expect(page.getByTestId('megapixel-filter-large')).toHaveClass(/active/) },
      { spec: 'Date ascending sort is active by default', check: async () => await expect(page.getByTestId('sort-filter-date_asc')).toHaveClass(/active/) },
      { spec: 'Large dated triage item A is visible', check: async () => await expect(page.getByTestId('asset-grid')).toContainText('TRIAGE_A.JPG') },
      { spec: 'Large dated triage item B is visible', check: async () => await expect(page.getByTestId('asset-grid')).toContainText('TRIAGE_B.JPG') },
      { spec: 'Small dated item is excluded', check: async () => await expect(page.getByTestId('asset-grid')).not.toContainText('TRIAGE_SMALL.JPG') },
      { spec: 'No-date item is excluded', check: async () => await expect(page.getByTestId('asset-grid')).not.toContainText('TRIAGE_NODATE.JPG') }
    ]
  });

  await page.getByTestId('asset-card').filter({ hasText: 'TRIAGE_A.JPG' }).click();
  await tester.step('asset-detail-provenance', {
    description: 'The asset detail view shows triage controls, navigation, and both source occurrences.',
    verifications: [
      { spec: 'Asset detail shows the full-resolution assessment image', check: async () => {
        await expect(page.getByTestId('asset-detail-image')).toBeVisible();
        await expect(page.getByTestId('asset-detail-image')).toHaveAttribute('src', /\/api\/objects\/.+\/bytes/);
      } },
      { spec: 'Asset detail fits within the viewport without page overflow', check: async () => {
        const overflow = await page.evaluate(() => ({
          x: document.documentElement.scrollWidth - document.documentElement.clientWidth,
          y: document.documentElement.scrollHeight - document.documentElement.clientHeight,
        }));
        expect(overflow.x).toBeLessThanOrEqual(0);
        expect(overflow.y).toBeLessThanOrEqual(0);
      } },
      { spec: 'Asset detail image is fully contained in the assessment stage', check: async () => {
        const fit = await page.evaluate(() => {
          const image = document.querySelector('[data-testid="asset-detail-image"]');
          const stage = document.querySelector('[data-testid="asset-photo-stage"]');
          if (!(image instanceof HTMLElement) || !(stage instanceof HTMLElement)) return null;
          const imageBox = image.getBoundingClientRect();
          const stageBox = stage.getBoundingClientRect();
          return {
            width: imageBox.width <= stageBox.width + 0.5,
            height: imageBox.height <= stageBox.height + 0.5,
          };
        });
        expect(fit).toEqual({ width: true, height: true });
      } },
      { spec: 'Asset source count is two', check: async () => await expect(page.getByTestId('asset-source-count')).toHaveText('2') },
      { spec: 'Source provenance lists original fixture path', check: async () => await expect(page.getByTestId('asset-sources')).toContainText('TRIAGE_A.JPG') },
      { spec: 'Source provenance lists duplicate fixture path', check: async () => await expect(page.getByTestId('asset-sources')).toContainText('TRIAGE_A_COPY.jpeg') },
      { spec: 'Advance to next is checked by default', check: async () => await expect(page.getByTestId('asset-advance-to-next')).toBeChecked() },
      { spec: 'Next asset navigation is available', check: async () => await expect(page.getByTestId('asset-next')).not.toHaveClass(/disabled/) }
    ]
  });

  await page.getByTestId('quality-Best').click();
  await tester.step('asset-quality-advances', {
    description: 'Setting quality marks the asset reviewed in the reducer and advances to the next triage item.',
    verifications: [
      { spec: 'The detail view advanced to TRIAGE_B.JPG', check: async () => await expect(page.getByRole('heading', { name: 'TRIAGE_B.JPG' })).toBeVisible() },
      { spec: 'The reviewed asset left the Triage navigation queue', check: async () => await expect(page.getByTestId('asset-prev')).toHaveClass(/disabled/) },
      { spec: 'The next asset remains in Triage', check: async () => await expect(page.getByTestId('status-Triage')).toHaveClass(/active/) }
    ]
  });

  await page.getByTestId('asset-advance-to-next').uncheck();
  await page.getByTestId('quality-Good').click();
  await tester.step('asset-quality-no-advance', {
    description: 'Turning off advance keeps the current asset visible while quality still marks it reviewed.',
    verifications: [
      { spec: 'The detail view remains on TRIAGE_B.JPG', check: async () => await expect(page.getByRole('heading', { name: 'TRIAGE_B.JPG' })).toBeVisible() },
      { spec: 'Good quality is selected', check: async () => await expect(page.getByTestId('quality-Good')).toHaveClass(/active/) },
      { spec: 'Reviewed status is selected by the quality reducer', check: async () => await expect(page.getByTestId('status-Reviewed')).toHaveClass(/active/) }
    ]
  });

  await page.goto('/assets?status=Reviewed');
  await page.getByTestId('asset-card').filter({ hasText: 'TRIAGE_A.JPG' }).click();
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
      { spec: 'Status-filtered grid also contains the second reviewed asset', check: async () => await expect(page.getByTestId('asset-grid')).toContainText('TRIAGE_B.JPG') },
      { spec: 'Status-filtered pager shows two reviewed assets', check: async () => await expect(page.getByTestId('asset-page-range')).toHaveText('Showing 1-2 of 2') }
    ]
  });

  await page.goto('/assets');
  await page.getByTestId('quality-filter-Best').click();
  await expect(page.getByTestId('quality-filter-Best')).toHaveClass(/active/);
  await page.getByTestId('quality-filter-Good').click();
  await expect(page.getByTestId('quality-filter-Good')).toHaveClass(/active/);
  await page.getByTestId('status-filter-Reviewed').click();
  await expect(page.getByTestId('status-filter-Reviewed')).toHaveClass(/active/);
  await page.getByTestId('visibility-filter-Private').click();
  await expect(page.getByTestId('visibility-filter-Private')).toHaveClass(/active/);
  await page.getByTestId('label-filter-family').click();
  await expect(page.getByTestId('label-filter-family')).toHaveClass(/active/);
  await tester.step('asset-filters', {
    description: 'Filter buttons combine quality disjunction with status, visibility, and label conjunctions.',
    verifications: [
      { spec: 'Best filter is active', check: async () => await expect(page.getByTestId('quality-filter-Best')).toHaveClass(/active/) },
      { spec: 'Good filter is active as a second quality choice', check: async () => await expect(page.getByTestId('quality-filter-Good')).toHaveClass(/active/) },
      { spec: 'Reviewed filter is active', check: async () => await expect(page.getByTestId('status-filter-Reviewed')).toHaveClass(/active/) },
      { spec: 'Private filter is active', check: async () => await expect(page.getByTestId('visibility-filter-Private')).toHaveClass(/active/) },
      { spec: 'Family label filter is active', check: async () => await expect(page.getByTestId('label-filter-family')).toHaveClass(/active/) },
      { spec: 'Filtered grid still contains the triaged asset', check: async () => await expect(page.getByTestId('asset-grid')).toContainText('TRIAGE_A.JPG') },
      { spec: 'Filtered grid excludes the Good asset because it is not private or labelled', check: async () => await expect(page.getByTestId('asset-grid')).not.toContainText('TRIAGE_B.JPG') },
      { spec: 'URL preserves both selected quality values', check: async () => expect(new URL(page.url()).searchParams.getAll('quality')).toEqual(['Best', 'Good']) }
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
  const ifd0Size = 2 + 12 + 4;
  const exifIFDOffset = 8 + ifd0Size;
  const exifIFDSize = 2 + fields.length * 12 + 4;
  const dataStart = exifIFDOffset + exifIFDSize;
  writeUInt32(exifIFDOffset);
  writeUInt32(0);
  writeUInt16(fields.length);

  for (const [tag, text] of fields) {
    writeIFDEntry(tag, Buffer.concat([Buffer.from(text), Buffer.from([0])]), dataStart, tiffParts, dataParts);
  }
  writeUInt32(0);
  return Buffer.concat([Buffer.from('Exif\0\0'), ...tiffParts, ...dataParts]);
}

function writeIFDEntry(tag: number, value: Buffer, dataStart: number, tiffParts: Buffer[], dataParts: Buffer[]) {
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
  writeUInt16(tiffParts, 2);
  writeUInt32(tiffParts, value.length);
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
