import { expect, test } from '@playwright/test';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { TestStepHelper } from '../helpers/test-step-helper';

test('dashboard loads and scans a source root', async ({ page }, testInfo) => {
  const tester = new TestStepHelper(page, testInfo);
  tester.setMetadata('Dashboard Source Scan', 'Register a source root, scan it, inspect the compact progress log, and drill into acquired files.');

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
  writeFileSync(`${source}/A.JPG`, 'same');
  writeFileSync(`${source}/B.jpeg`, 'same');
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
      { spec: 'Duplicate bytes summary is updated', check: async () => await expect(page.getByTestId('duplicate-garbage-bytes')).toHaveText('4') }
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
    description: 'The acquired count opens a file list with image links.',
    verifications: [
      { spec: 'Acquired files heading is visible', check: async () => await expect(page.getByRole('heading', { name: 'Acquired files' })).toBeVisible() },
      { spec: 'Acquired table lists A.JPG', check: async () => await expect(page.getByTestId('acquired-table')).toContainText('A.JPG') },
      {
        spec: 'First acquired file link serves image/jpeg',
        check: async () => {
          const imageHref = await page.getByTestId('acquired-image-link').first().getAttribute('href');
          expect(imageHref).toBeTruthy();
          const imageResponse = await page.request.get(imageHref!);
          expect(imageResponse.ok()).toBe(true);
          expect(imageResponse.headers()['content-type']).toContain('image/jpeg');
        }
      }
    ]
  });

  tester.generateDocs();
});
