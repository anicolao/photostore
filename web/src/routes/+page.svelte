<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { addSource, collectThumbnailGarbage, deduplicateDuplicates, getInventories, getJobs, getScans, getSources, getStore, refreshMissingMetadata, resumeScan, startSingleSourceScan, startSourceScan } from '$lib/api';
  import type { HistoricalInventory, Job, ScanProjection, ServerEvent, SourceRoot, StoreSummary } from '$lib/types';

  let store: StoreSummary | null = null;
  let sources: SourceRoot[] = [];
  let scans: ScanProjection[] = [];
  let inventories: HistoricalInventory[] = [];
  let jobs: Job[] = [];
  let activeJob: Job | null = null;
  let sourcePath = '';
  let sourceLabel = '';
  let error = '';
  let loading = true;
  let hydrated = false;
  let jobLogOpen = false;
  let refreshTimer: ReturnType<typeof setInterval> | undefined;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let eventSource: EventSource | undefined;
  let eventsClosed = false;
  $: runningJobActive = activeJob?.status === 'running';

  async function refresh(showLoading = false) {
    if (showLoading) {
      loading = true;
    }
    const [nextStore, nextSources, nextScans, nextInventories, nextJobs] = await Promise.all([
      getStore(),
      getSources(),
      getScans(),
      getInventories(),
      getJobs()
    ]);
    store = nextStore;
    sources = nextSources;
    scans = nextScans;
    inventories = nextInventories;
    jobs = mergeJobs(jobs, nextJobs);
    applyJobs(jobs);
    error = '';
    if (showLoading) {
      loading = false;
    }
  }

  async function load() {
    loading = true;
    error = '';
    try {
      await refresh();
    } catch (err) {
      error = String(err);
    } finally {
      loading = false;
    }
  }

  function connectEvents() {
    if (typeof window === 'undefined') {
      return;
    }
    if (eventSource && eventSource.readyState !== EventSource.CLOSED) {
      return;
    }
    eventSource = new EventSource('/api/events/stream');
    eventSource.onmessage = (message) => {
      applyServerEvent(JSON.parse(message.data) as ServerEvent);
    };
    eventSource.onerror = () => {
      eventSource?.close();
      eventSource = undefined;
      if (!eventsClosed) {
        reconnectTimer = setTimeout(connectEvents, 1000);
      }
    };
  }

  function applyServerEvent(event: ServerEvent) {
    if (event.type === 'job_snapshot') {
      applyJobs(event.jobs ?? []);
      return;
    }
    if (event.job) {
      jobs = upsertJob(jobs, event.job);
      activeJob = event.job;
      if (event.type === 'job_finished') {
        refresh().catch(() => {
          // Keep the streamed job visible if projections lag or a transient read fails.
        });
      }
      return;
    }
    if (event.type === 'projection_changed') {
      refresh().catch(() => {
        // The next projection refresh or event reconnect will catch up.
      });
    }
  }

  function applyJobs(jobs: Job[]) {
    if (activeJob?.status === 'running' && !jobs.some((job) => job.job_id === activeJob?.job_id)) {
      return;
    }
    const runningJob = jobs.find((job) => job.status === 'running');
    if (runningJob) {
      activeJob = runningJob;
      return;
    }
    const currentJob = activeJob ? jobs.find((job) => job.job_id === activeJob?.job_id) : undefined;
    if (currentJob) {
      activeJob = currentJob;
      return;
    }
    activeJob = latestJob(jobs);
  }

  function latestJob(jobs: Job[]) {
    return jobs.toSorted((a, b) => b.started_at_ms - a.started_at_ms)[0] ?? null;
  }

  function upsertJob(existing: Job[], next: Job) {
    return mergeJobs(existing, [next]);
  }

  function mergeJobs(existing: Job[], incoming: Job[]) {
    const byID = new Map<string, Job>();
    for (const job of existing) {
      byID.set(job.job_id, job);
    }
    for (const job of incoming) {
      const current = byID.get(job.job_id);
      byID.set(job.job_id, preferNewerJob(current, job));
    }
    return latestJobs([...byID.values()]);
  }

  function preferNewerJob(current: Job | undefined, incoming: Job) {
    if (!current) {
      return incoming;
    }
    if (current.status !== 'running' && incoming.status === 'running') {
      return current;
    }
    if (incoming.finished_at_ms !== null && (current.finished_at_ms === null || incoming.finished_at_ms >= current.finished_at_ms)) {
      return incoming;
    }
    return incoming.started_at_ms >= current.started_at_ms ? incoming : current;
  }

  function latestJobs(nextJobs: Job[]) {
    return nextJobs.toSorted((a, b) => b.started_at_ms - a.started_at_ms);
  }

  function jobForScan(scanID: string) {
    return jobs.find((job) => job.result_ref === scanID) ?? null;
  }

  function selectScanStatus(scan: ScanProjection) {
    activeJob = jobForScan(scan.scan_id) ?? scanAsJob(scan);
    jobLogOpen = false;
  }

  function scanAsJob(scan: ScanProjection): Job {
    const startedAt = scan.started_at_ms ?? scan.completed_at_ms ?? 0;
    const progress = [`scan ${scan.scan_id}: ${scan.status}`];
    return {
      job_id: `scan_job_${scan.scan_id}`,
      kind: 'scan',
      status: scan.status === 'started' ? 'interrupted' : (scan.status as Job['status']),
      started_at_ms: startedAt,
      finished_at_ms: scan.completed_at_ms,
      result_ref: scan.scan_id,
      error: null,
      progress,
      progress_current: null,
      progress_total: null
    };
  }

  async function submitSource() {
    error = '';
    await addSource(sourcePath, sourceLabel);
    sourcePath = '';
    sourceLabel = '';
    await refresh();
  }

  async function runScan(start: () => Promise<Job>) {
    error = '';
    jobLogOpen = false;
    try {
      activeJob = await start();
      jobs = upsertJob(jobs, activeJob);
      connectEvents();
      await refresh();
    } catch (err) {
      error = String(err);
    }
  }

  async function scanSources() {
    await runScan(startSourceScan);
  }

  async function scanSource(sourceRootID: string) {
    await runScan(() => startSingleSourceScan(sourceRootID));
  }

  async function resume(scanID: string) {
    await runScan(() => resumeScan(scanID));
  }

  async function refreshMetadata() {
    await runScan(refreshMissingMetadata);
  }

  async function deduplicate() {
    await runScan(deduplicateDuplicates);
  }

  async function collectThumbnails() {
    await runScan(collectThumbnailGarbage);
  }

  function formatBytes(bytes: number) {
    return new Intl.NumberFormat('en-CA').format(bytes);
  }

  function formatOptionalNumber(value: number | null | undefined) {
    if (value === null || value === undefined) {
      return 'Unknown';
    }
    return new Intl.NumberFormat('en-CA').format(value);
  }

  function hasKnownAcquiredCount(scan: ScanProjection) {
    return scan.report?.source_files_acquired !== null && scan.report?.source_files_acquired !== undefined;
  }

  function canDrillDown(scan: ScanProjection) {
    return hasKnownAcquiredCount(scan) || scan.status === 'started';
  }

  function canResume(scan: ScanProjection) {
    return scan.status === 'started' && !runningJobActive;
  }

  function scanStatus(scan: ScanProjection) {
    if (scan.status === 'started') {
      return runningJobActive ? 'running' : 'incomplete';
    }
    return scan.status;
  }

  function formatOptionalBytes(value: number | null | undefined) {
    if (value === null || value === undefined) {
      return 'Unknown';
    }
    return formatBytes(value);
  }

  function formatScanTime(value: number | null | undefined) {
    if (value === null || value === undefined) {
      return 'Never';
    }
    return new Intl.DateTimeFormat('en-CA', {
      dateStyle: 'medium',
      timeStyle: 'short'
    }).format(new Date(value));
  }

  function latestProgress(job: Job) {
    return job.progress.at(-1) ?? 'Waiting for progress...';
  }

  function jobStatusMessage(job: Job) {
    return `${job.kind}: ${job.status}`;
  }

  function hasProgressTotal(job: Job) {
    return job.progress_total !== null && job.progress_total > 0 && job.progress_current !== null;
  }

  function progressPercent(job: Job) {
    if (!hasProgressTotal(job)) {
      return 0;
    }
    return Math.max(0, Math.min(100, (job.progress_current! / job.progress_total!) * 100));
  }

  function elideMiddle(value: string, maxLength = 60) {
    if (value.length <= maxLength) {
      return value;
    }
    const marker = '...';
    const remaining = maxLength - marker.length;
    const head = Math.ceil(remaining / 2);
    const tail = Math.floor(remaining / 2);
    return `${value.slice(0, head)}${marker}${value.slice(value.length - tail)}`;
  }

  onMount(() => {
    hydrated = true;
    load();
    connectEvents();
    refreshTimer = setInterval(() => {
      if (activeJob?.status !== 'running') {
        refresh().catch(() => {
          // Keep the last good projection visible if a transient read fails.
        });
      }
    }, 3000);
  });

  onDestroy(() => {
    if (refreshTimer) {
      clearInterval(refreshTimer);
    }
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
    }
    eventsClosed = true;
    eventSource?.close();
  });
</script>

<svelte:head>
  <title>Photostore</title>
</svelte:head>

<main>
  <header class="topbar">
    <div>
      <h1>Photostore</h1>
      <p data-testid="store-path">{store?.store_path ?? 'Loading store'}</p>
    </div>
    <div class="topbar-actions">
      <a class="button-link" data-testid="photos-by-date-link" href="/photos/dates">Photos by date</a>
      <a class="button-link" data-testid="metadata-link" href="/metadata">Metadata</a>
      <button data-testid="refresh-metadata" on:click={refreshMetadata} disabled={!hydrated || runningJobActive}>Refresh metadata</button>
      <button data-testid="deduplicate-duplicates" on:click={deduplicate} disabled={!hydrated || runningJobActive || (store?.retained_duplicate_bytes ?? 0) === 0}>Deduplicate</button>
      <button data-testid="collect-thumbnail-garbage" on:click={collectThumbnails} disabled={!hydrated || runningJobActive || (store?.thumbnail_garbage_bytes ?? 0) === 0}>GC thumbnails</button>
      <button on:click={() => refresh(true)} disabled={!hydrated || loading}>Refresh</button>
    </div>
  </header>

  {#if error}
    <section class="error" data-testid="ui-error">{error}</section>
  {/if}

  <section class="summary" aria-label="Store status">
    <div>
      <span>Events</span>
      <strong data-testid="event-count">{store?.event_count ?? 0}</strong>
    </div>
    <div>
      <span>Content</span>
      <strong data-testid="content-count">{store?.content_count ?? 0}</strong>
    </div>
    <div>
      <span>Sources</span>
      <strong data-testid="source-count">{store?.source_root_count ?? 0}</strong>
    </div>
    <div>
      <span>Duplicate bytes</span>
      <strong data-testid="duplicate-garbage-bytes">{formatBytes(store?.retained_duplicate_bytes ?? 0)}</strong>
    </div>
    <div>
      <span>Thumbnail garbage</span>
      <strong data-testid="thumbnail-garbage-bytes">{formatBytes(store?.thumbnail_garbage_bytes ?? 0)}</strong>
      <small data-testid="thumbnail-garbage-files">{formatBytes(store?.thumbnail_garbage_files ?? 0)} files</small>
    </div>
  </section>

  <div class="grid">
    <section aria-labelledby="sources-heading">
      <div class="section-heading">
        <h2 id="sources-heading">Source roots</h2>
        <button class="primary" data-testid="start-source-scan" on:click={scanSources} disabled={!hydrated || runningJobActive}>
          Scan
        </button>
      </div>
      <form data-testid="add-source-form" on:submit|preventDefault={submitSource}>
        <input data-testid="source-path-input" bind:value={sourcePath} placeholder="Path" aria-label="Source path" required>
        <input data-testid="source-label-input" bind:value={sourceLabel} placeholder="Label" aria-label="Source label">
        <button type="submit" disabled={!hydrated}>Add</button>
      </form>
      {#if sources.length === 0}
        <p class="empty" data-testid="sources-empty">No source roots registered.</p>
      {:else}
        <ul class="rows" data-testid="source-list">
          {#each sources as source}
            <li>
              <div>
                <strong>{source.label}</strong>
                <code>{source.path}</code>
                <span>Last scan: {formatScanTime(source.last_scan_completed_at_ms)}</span>
              </div>
              <button data-testid="scan-source-{source.source_root_id}" on:click={() => scanSource(source.source_root_id)} disabled={!hydrated || runningJobActive}>
                Scan
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section aria-labelledby="job-heading" data-testid="job-panel">
      <div class="section-heading">
        <h2 id="job-heading">Job status</h2>
        {#if activeJob && activeJob.progress.length > 0}
          <button data-testid="toggle-job-log" on:click={() => (jobLogOpen = !jobLogOpen)} disabled={!hydrated}>
            {jobLogOpen ? 'Close log' : 'Open log'}
          </button>
        {/if}
      </div>
      {#if activeJob}
        {@const statusMessage = jobStatusMessage(activeJob)}
        <p class="compact-line" data-testid="job-status" title={statusMessage} aria-label={statusMessage}>{elideMiddle(statusMessage)}</p>
        {@const progressMessage = latestProgress(activeJob)}
        <p class="compact-line progress-current" data-testid="job-latest-progress" title={progressMessage} aria-label={progressMessage}>{elideMiddle(progressMessage)}</p>
        {#if hasProgressTotal(activeJob)}
          <div class="job-progress" data-testid="job-progress" aria-label="Job progress">
            <div class="job-progress-bar" role="progressbar" aria-valuemin="0" aria-valuemax={activeJob.progress_total ?? 0} aria-valuenow={activeJob.progress_current ?? 0}>
              <span style={`width: ${progressPercent(activeJob)}%`}></span>
            </div>
            <span data-testid="job-progress-count">{activeJob.progress_current} of {activeJob.progress_total}</span>
          </div>
        {/if}
        {#if activeJob.error}<p class="error">{activeJob.error}</p>{/if}
        {#if jobLogOpen}
          <div class="job-log" data-testid="job-log" role="log" aria-label="Job progress log">
            {#each activeJob.progress as message}
              <div>{message}</div>
            {/each}
          </div>
        {/if}
      {:else}
        <p class="empty" data-testid="job-empty">No jobs yet.</p>
      {/if}
    </section>
  </div>

  <section aria-labelledby="scans-heading">
    <h2 id="scans-heading">Recent scans</h2>
    {#if scans.length === 0}
      <p class="empty" data-testid="scans-empty">No scans yet.</p>
    {:else}
      <table data-testid="scan-table">
        <thead>
          <tr>
            <th>Scan</th>
            <th>Status</th>
            <th>Acquired</th>
            <th>Duplicates</th>
            <th>Duplicate bytes</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each scans as scan}
            <tr>
              <td><code>{scan.scan_id}</code></td>
              <td>{scanStatus(scan)}</td>
              <td>
                {#if canDrillDown(scan)}
                  <a data-testid="scan-acquired-link" href={`/scans/${scan.scan_id}`}>
                    {formatOptionalNumber(scan.report?.source_files_acquired)}
                  </a>
                {:else}
                  <span data-testid="scan-acquired-unknown">Unknown</span>
                {/if}
              </td>
              <td>{formatOptionalNumber(scan.report?.duplicate_acquisitions)}</td>
              <td>{formatOptionalBytes(scan.report?.duplicate_garbage_bytes)}</td>
              <td>
                <button data-testid="scan-status-{scan.scan_id}" on:click={() => selectScanStatus(scan)} disabled={!hydrated}>Status</button>
                {#if canResume(scan)}
                  <button data-testid="resume-scan-{scan.scan_id}" on:click={() => resume(scan.scan_id)} disabled={!hydrated}>Resume</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  <section aria-labelledby="inventories-heading">
    <h2 id="inventories-heading">Historical inventories</h2>
    {#if inventories.length === 0}
      <p class="empty" data-testid="inventories-empty">No historical inventories acquired.</p>
    {:else}
      <ul class="rows">
        {#each inventories as inv}
          <li><strong>{inv.label}</strong><code>{inv.original_path}</code></li>
        {/each}
      </ul>
    {/if}
  </section>
</main>

<style>
  main {
    max-width: 1180px;
    margin: 0 auto;
    padding: 22px;
  }

  .topbar,
  .section-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }

  .topbar-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .button-link {
    border: 1px solid #9aa0a6;
    border-radius: 6px;
    background: #ffffff;
    color: #202124;
    padding: 7px 10px;
    text-decoration: none;
  }

  h1,
  h2,
  p {
    margin-top: 0;
  }

  h1 {
    margin-bottom: 2px;
    font-size: 26px;
  }

  h2 {
    font-size: 18px;
  }

  .topbar p {
    margin-bottom: 0;
    color: #5f6368;
  }

  .summary {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 12px;
    margin: 18px 0;
  }

  .summary div,
  section {
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #ffffff;
  }

  .summary div {
    padding: 14px;
  }

  .summary span {
    display: block;
    color: #5f6368;
    font-size: 12px;
  }

  .summary strong {
    display: block;
    margin-top: 4px;
    font-size: 24px;
  }

  .summary small {
    display: block;
    color: #5f6368;
    font-size: 12px;
  }

  section {
    padding: 16px;
    margin-bottom: 16px;
  }

  .grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 16px;
  }

  .grid > section {
    min-width: 0;
  }

  form {
    display: grid;
    grid-template-columns: 1fr minmax(130px, 180px) auto;
    gap: 8px;
  }

  .rows {
    list-style: none;
    margin: 12px 0 0;
    padding: 0;
  }

  .rows li {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    align-items: center;
    border-top: 1px solid #eef1f5;
    padding: 9px 0;
  }

  .rows li div {
    display: grid;
    gap: 3px;
    min-width: 0;
  }

  .rows span {
    color: #5f6368;
    font-size: 12px;
  }

  .empty {
    color: #5f6368;
    margin-bottom: 0;
  }

  .error {
    border-color: #d93025;
    color: #a50e0e;
  }

  .compact-line {
    box-sizing: border-box;
    display: block;
    width: 100%;
    max-width: none;
    min-width: 0;
    min-height: 18px;
    margin: 8px 0 0;
    overflow: hidden;
    white-space: nowrap;
    color: #202124;
  }

  .progress-current {
    color: #3c4043;
  }

  .job-progress {
    width: 100%;
    max-width: none;
    min-width: 0;
    margin-top: 8px;
    display: grid;
    gap: 4px;
  }

  .job-progress-bar {
    height: 8px;
    overflow: hidden;
    border-radius: 999px;
    background: #e8edf5;
  }

  .job-progress-bar span {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: #1a73e8;
  }

  .job-progress > span {
    color: #5f6368;
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }

  .job-log {
    max-height: 240px;
    margin-top: 12px;
    overflow: auto;
    border: 1px solid #d9dee7;
    border-radius: 6px;
    background: #111827;
    color: #e5e7eb;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 12px;
    line-height: 1.45;
    padding: 10px;
  }

  .job-log div + div {
    margin-top: 4px;
  }

  table {
    width: 100%;
    border-collapse: collapse;
  }

  th,
  td {
    border-top: 1px solid #eef1f5;
    padding: 8px;
    text-align: left;
  }

  @media (max-width: 760px) {
    main {
      padding: 12px;
    }

    .summary,
    .grid,
    form {
      grid-template-columns: 1fr;
    }
  }
</style>
