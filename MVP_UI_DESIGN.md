# MVP UI Design

This document defines the first web interface for the ingestion MVP. It builds on
[MVP_IMPLEMENTATION_PLAN.md](./MVP_IMPLEMENTATION_PLAN.md) and
[MVP_USAGE.md](./MVP_USAGE.md). The goal is to expose the existing local ingestion
workflows through `photostore serve` without changing the event-sourced storage
model.

## Scope

The MVP UI is an operator console for local ingestion. It should help a user see
the current store, register source roots, run scans, acquire historical
inventories, scan those inventories, and inspect recent reports.

The MVP UI includes:

- A `photostore serve` command that serves a local web application.
- Read-only projection views for store status, source roots, inventories, scans,
  reports, and recent events.
- Command actions that call the same ingestion paths as the CLI.
- Progress reporting for long-running scans.
- E2E coverage for the primary ingestion workflows.

The MVP UI excludes:

- Thumbnail generation, image preview, editing, albums, labels, and sharing.
- Authentication or public network serving.
- Remote stores, cloud sync, or Google Drive integration.
- Verification and safe deduplication controls beyond reporting retained
  duplicate bytes.

## Architecture

Use a small SvelteKit/Vite frontend, following the architecture style used by
`github.com/anicolao/food`: TypeScript components, Playwright E2E tests, stable
test helpers, and checked builds. The Go binary remains the owner of storage,
events, projections, and ingestion commands.

Suggested layout:

```text
web/
  package.json
  svelte.config.js
  vite.config.ts
  src/
    app.css
    routes/
      +layout.svelte
      +page.svelte
    lib/
      api.ts
      types.ts
      stores.ts
      components/
  tests/
    e2e/
      helpers/
      001-dashboard/
      002-source-scan/
      003-historical-inventory/
```

The development shell should provide Go, Bun, Node.js, and Playwright browser
dependencies. The existing Nix flake is the right place to add those tools when
the UI is implemented.

## `photostore serve`

Add a new command:

```sh
photostore serve --store ./photostore-data --addr 127.0.0.1:8080
```

Behavior:

- Open the configured store exactly as other commands do.
- Bind to `127.0.0.1:8080` by default.
- Serve JSON APIs under `/api/*`.
- Serve the built frontend for all non-API paths.
- Print the URL on startup.
- Refuse non-loopback addresses unless the user passes an explicit
  `--allow-public-bind` flag.

The implementation can use Go `embed` for the built frontend:

```text
go generate or release build:
  cd web && bun install --frozen-lockfile
  cd web && bun run build
  go build ./cmd/photostore
```

For local frontend development, run Vite and Go separately:

```sh
photostore serve --store ./photostore-data --addr 127.0.0.1:8080 --api-only
cd web && bun run dev
```

In dev mode, Vite should proxy `/api` to `http://127.0.0.1:8080`.

## API Shape

The UI must not write event records directly. It sends commands to the Go server,
and the server emits the same events the CLI would emit. All displayed state is
read from projections, reports, or event tails.

Initial endpoints:

```text
GET  /api/health
GET  /api/store

GET  /api/sources
POST /api/sources
POST /api/sources/{source_root_id}/scan

GET  /api/scans
POST /api/scans
GET  /api/scans/{scan_id}
GET  /api/scans/{scan_id}/report
GET  /api/scans/{scan_id}/acquired
GET  /api/objects/{stored_object_id}/bytes

GET  /api/inventories
POST /api/inventories/acquire
POST /api/inventories/{historical_inventory_id}/scan

GET  /api/events?limit=100
GET  /api/events/stream
GET  /api/jobs
GET  /api/jobs/{job_id}
```

Command endpoints should return `202 Accepted` with a `job_id` when work runs
asynchronously. The job projection should expose:

```json
{
  "job_id": "job_...",
  "kind": "source_scan",
  "status": "running",
  "started_at_ms": 1783012345000,
  "finished_at_ms": null,
  "result_ref": null,
  "error": null
}
```

`/api/events/stream` is a Server-Sent Events stream. It sends an initial
`job_snapshot` event followed by job progress and projection-change events.
The MVP UI also polls projection endpoints periodically so changes made by CLI
commands appear without a manual refresh.

## UI Surface

The first screen should be the actual console, not a landing page. The interface
should be dense, quiet, and optimized for repeated local operation.

Primary regions:

- Store status: store path, event count, content count, retained duplicate bytes,
  and last scan time.
- Source roots: registered roots, labels, enabled state, and an add-source form.
- Scan controls: start all-source scans or one source-root scan, show progress,
  and link to the resulting report.
- Historical inventories: acquired `.toc` files, labels, groups, and scan
  actions with resolver roots and extension filters.
- Recent scan reports: discovered candidates, acquired objects, skipped trusted
  duplicates, duplicate acquisitions, duplicate garbage bytes, and errors.
- Acquired-file drill-down: clicking a scan's acquired count opens a route that
  lists acquired files and links each file to its stored JPEG bytes.
- Recent events: compact event tail for debugging and confidence.

Use stable `data-testid` attributes for all controls and status values covered
by E2E tests.

## Command Semantics

The UI should preserve the ingestion MVP's semantics:

- Source scans copy JPEG bytes into retained acquired objects.
- The incoming hash is computed while copying.
- Newly seen content is materialized into the CAS.
- Existing content may still leave retained duplicate bytes until a later
  verifier/deduplicator removes them.
- Historical inventory scans first deduplicate against the projection of already
  seen trusted historical hashes before resolving and copying bytes.
- CAS storage keys are not exposed as persisted facts in the UI.

The UI may display derived paths for operator convenience, but it must label
them as current projection values.

## Job Model

`photostore serve` needs a small in-process job runner for long operations.
The runner should:

- Limit concurrent ingestion jobs to one for the MVP.
- Keep command output in structured progress records rather than terminal text.
- Persist final scan reports in the same report format used by the CLI.
- Expose current progress through `/api/jobs/{job_id}` and `/api/events/stream`.
- Recover gracefully if the server exits during a job by leaving durable events
  and reports as the source of truth.

The job runner is process-local in the MVP. It is not a durable queue.

## Security

The MVP is local-first:

- Bind to loopback by default.
- Do not implement accounts, sessions, or remote access.
- Do not allow browser-submitted arbitrary shell commands.
- Validate all filesystem paths server-side before running ingestion commands.
- Show a visible warning if the server is intentionally bound to a non-loopback
  address.

## Implementation Phases

1. Add `photostore serve` with `/api/health`, `/api/store`, embedded static
   serving, and dev proxy support.
2. Add read-only projection APIs and render the dashboard.
3. Add source registration and source scan jobs.
4. Add historical inventory acquisition and scan jobs.
5. Add Playwright E2E coverage and screenshot documentation.

## Acceptance Criteria

The UI MVP is complete when:

- `photostore serve --store ./photostore-data` starts a local web interface.
- The dashboard renders store, source, scan, inventory, report, and event state.
- A user can add a source root and run a JPEG scan from the browser.
- A user can acquire and scan a historical `.toc` inventory from the browser.
- Scan reports include duplicate acquisition and duplicate garbage counts.
- A user can click a scan's acquired count, inspect acquired file paths, and open
  the underlying JPEG bytes in the browser.
- `go test ./...`, `cd web && bun run check`, frontend build, and Playwright
  E2E tests pass in the Nix development shell.
