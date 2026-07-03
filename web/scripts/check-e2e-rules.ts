import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const maxExplicitWaitMs = 2000;
const roots = ['tests/e2e', 'playwright.config.ts'];
const violations: string[] = [];

for (const file of roots.flatMap(walk)) {
  const text = readFileSync(file, 'utf8');
  const lines = text.split(/\r?\n/);
  lines.forEach((line, index) => {
    if (line.includes('waitForTimeout')) {
      violations.push(`${file}:${index + 1}: waitForTimeout is forbidden`);
    }
    for (const match of line.matchAll(/timeout\s*:\s*([0-9][0-9_]*)/g)) {
      const value = Number(match[1].replaceAll('_', ''));
      if (isGlobalTestTimeout(file, line)) {
        continue;
      }
      if (value > maxExplicitWaitMs) {
        violations.push(`${file}:${index + 1}: timeout ${value}ms exceeds ${maxExplicitWaitMs}ms`);
      }
    }
  });
}

function isGlobalTestTimeout(file: string, line: string): boolean {
  return file.endsWith('playwright.config.ts') && /^\s*timeout\s*:/.test(line);
}

if (violations.length > 0) {
  console.error(`E2E rule check failed:\n${violations.join('\n')}`);
  process.exit(1);
}

function walk(path: string): string[] {
  const fullPath = join(process.cwd(), path);
  const stat = statSync(fullPath);
  if (stat.isFile()) {
    return [fullPath];
  }
  return readdirSync(fullPath).flatMap((entry) => {
    const child = join(path, entry);
    const childStat = statSync(join(process.cwd(), child));
    if (childStat.isDirectory()) {
      return walk(child);
    }
    return /\.(ts|svelte)$/.test(entry) ? [join(process.cwd(), child)] : [];
  });
}
