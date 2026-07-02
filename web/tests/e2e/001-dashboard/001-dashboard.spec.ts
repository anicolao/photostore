import { expect, test } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

test('dashboard loads and scans a source root', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Photostore' })).toBeVisible();
  await expect(page.getByTestId('source-count')).toHaveText('0');
  await expect(page.getByTestId('scans-empty')).toBeVisible();

  const source = join(tmpdir(), `photostore-e2e-source-${Date.now()}`);
  mkdirSync(source, { recursive: true });
  writeFileSync(join(source, 'A.JPG'), 'same');
  writeFileSync(join(source, 'B.jpeg'), 'same');
  writeFileSync(join(source, 'notes.txt'), 'not media');

  await page.getByTestId('source-path-input').fill(source);
  await page.getByTestId('source-label-input').fill('fixture');
  await page.getByRole('button', { name: 'Add' }).click();
  await expect(page.getByTestId('source-count')).toHaveText('1');
  await expect(page.getByTestId('source-list')).toContainText('fixture');
  await expect(page.getByTestId('source-list')).toContainText('Last scan: Never');

  await page.getByTestId('source-list').getByRole('button', { name: 'Scan' }).click();
  await expect(page.getByTestId('job-status')).toContainText('completed', { timeout: 10_000 });
  await expect(page.getByTestId('source-list')).not.toContainText('Last scan: Never');
  await expect(page.getByTestId('scan-table')).toContainText('completed');
  await expect(page.getByTestId('scan-table')).toContainText('2');
  await expect(page.getByTestId('duplicate-garbage-bytes')).toHaveText('4');
});
