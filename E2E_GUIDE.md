# E2E Guide

This guide defines how Photostore should test the MVP web interface described in
[MVP_UI_DESIGN.md](./MVP_UI_DESIGN.md). It follows the testing strategy used by
`github.com/anicolao/food`: Playwright scenarios are the primary proof that the
browser UI works, screenshots are deterministic, and scenario documentation is
generated from test steps.

## Principles

- E2E tests are the primary correctness tests for user-visible UI behavior.
- Tests should drive the app the way an operator would: click controls, fill
  forms, start scans, and inspect reports.
- Tests should use deterministic fixture stores and source trees.
- Tests should never depend on real user photos, real `/Volumes` paths, or the
  developer's current machine state.
- Screenshots should use zero pixel tolerance.
- Tests should not use arbitrary sleeps. Wait for real UI state, API completion,
  or explicit job status.

## Tooling

Use Playwright with Chromium only for the MVP.

The frontend package should provide:

```json
{
  "scripts": {
    "dev": "vite dev --port 5174 --strictPort",
    "build": "vite build",
    "check": "svelte-kit sync && svelte-check --tsconfig ./tsconfig.json",
    "test:e2e": "playwright test",
    "test:e2e:update-snapshots": "playwright test --update-snapshots"
  }
}
```

The Nix development shell should include Node.js and the system libraries needed
for Playwright's Chromium runtime.

## Directory Layout

Store E2E tests under the frontend tree:

```text
web/tests/e2e/
  helpers/
    fixtures.ts
    test-step-helper.ts
  001-dashboard/
    001-dashboard.spec.ts
    README.md
    screenshots/
  002-source-scan/
    002-source-scan.spec.ts
    README.md
    screenshots/
  003-historical-inventory/
    003-historical-inventory.spec.ts
    README.md
    screenshots/
```

Each scenario directory owns its generated README and baseline screenshots.

## Playwright Configuration

Use a deterministic Playwright config:

```ts
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  retries: 0,
  reporter: 'html',
  timeout: 60_000,
  expect: {
    timeout: 5_000,
    toHaveScreenshot: {
      maxDiffPixels: 0
    }
  },
  use: {
    baseURL: 'http://127.0.0.1:5174',
    trace: 'on-first-retry',
    timezoneId: 'America/New_York',
    locale: 'en-CA',
    viewport: { width: 1280, height: 900 },
    deviceScaleFactor: 1
  },
  snapshotPathTemplate: '{testDir}/{testFileDir}/screenshots/{arg}.png',
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: {
          args: [
            '--disable-gpu',
            '--disable-dev-shm-usage',
            '--disable-font-subpixel-positioning',
            '--disable-lcd-text',
            '--font-render-hinting=none',
            '--use-gl=swiftshader'
          ]
        }
      }
    }
  ],
  webServer: {
    command: 'npm run dev',
    url: 'http://127.0.0.1:5174',
    reuseExistingServer: !process.env.CI
  }
});
```

If the implementation uses a separate Go API server during tests, the
Playwright web server command should run a wrapper script that starts both:

```sh
npm --prefix web run test-server
```

That script should create a temporary Photostore store, start
`photostore serve --api-only`, and start Vite with `/api` proxied to the Go
server.

## Test Step Helper

Use one helper for named steps, screenshots, and generated scenario docs. Tests
should not manually manage screenshot counters.

Expected helper behavior:

- `step(id, options)` runs verifications before taking a screenshot.
- Screenshot names are generated as `000-id.png`, `001-id.png`, and so on.
- Each step records a human-readable description.
- Each scenario writes a README containing the ordered steps and screenshots.
- Optional network or job-state waits are centralized in the helper.

Example shape:

```ts
await helper.step('empty-dashboard', {
  description: 'The dashboard shows an initialized empty store.',
  verifications: [
    async () => await expect(page.getByTestId('store-status')).toBeVisible(),
    async () => await expect(page.getByTestId('source-count')).toHaveText('0')
  ]
});
```

## Fixture Strategy

Each E2E scenario should create its own temporary world:

- A temporary Photostore store.
- One or more temporary source roots.
- Tiny deterministic files with `.jpg` or `.jpeg` extensions.
- Historical `.toc` fixtures when testing inventory flows.

The JPEG fixtures do not need valid image pixels for the ingestion MVP because
the current ingestion contract trusts file extensions. They only need stable
bytes.

Fixture helpers should:

- Initialize the store with the real CLI.
- Register source roots through the UI when the scenario is testing that flow.
- Avoid absolute paths in screenshots when possible by displaying shortened
  names or fixture labels.
- Clean temporary directories after the test unless debugging is enabled.

## Initial Scenarios

### 001 Dashboard

Purpose: prove that `photostore serve` renders an initialized empty store.

Steps:

- Load the dashboard.
- Verify store status is visible.
- Verify source, inventory, scan, and event sections render empty states.
- Capture the empty dashboard screenshot.

### 002 Source Scan

Purpose: prove that the browser can run the basic JPEG source scan.

Fixture:

- One source root with two unique `.jpg` files and one duplicate `.jpeg` file.

Steps:

- Add the source root.
- Start a scan.
- Wait for the scan job to finish.
- Verify the report shows acquired files, duplicate acquisitions, and retained
  duplicate garbage bytes.
- Verify recent events updated.

### 003 Historical Inventory

Purpose: prove that historical inventory scans use trusted hashes to skip work.

Fixture:

- Two `.toc` files that refer to the same JPEG content through trusted hash
  records.
- Resolver roots that can resolve at least one new file.

Steps:

- Acquire the first inventory.
- Scan it and acquire the referenced JPEG.
- Acquire the second inventory.
- Scan it.
- Verify the second scan skips the already seen trusted hash without copying
  duplicate bytes.
- Verify the report references the historical inventory id rather than repeating
  inventory contents.

## Selectors

Prefer accessible roles for stable controls and `data-testid` for precise
status values:

```svelte
<button data-testid="start-source-scan">Scan</button>
<output data-testid="duplicate-garbage-bytes">{bytes}</output>
```

Do not select by incidental CSS classes, generated ids, or full local paths.

## Screenshot Policy

Screenshots are committed artifacts. Before updating them:

1. Run the scenario locally.
2. Inspect the rendered UI and generated README.
3. Update snapshots with `npm --prefix web run test:e2e:update-snapshots`.
4. Review the image diff before committing.

Keep animations disabled or behind `prefers-reduced-motion` so screenshots are
stable.

## CI Commands

The expected CI checks are:

```sh
nix develop --command go test ./...
nix develop --command npm --prefix web ci
nix develop --command npm --prefix web run check
nix develop --command npm --prefix web run build
nix develop --command npm --prefix web run test:e2e
```

The implementation PR that adds the UI should also add GitHub Actions wiring for
these commands.
