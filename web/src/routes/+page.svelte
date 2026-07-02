<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { addSource, getInventories, getJob, getScans, getSources, getStore, startSingleSourceScan, startSourceScan } from '$lib/api';
  import type { HistoricalInventory, Job, ScanProjection, SourceRoot, StoreSummary } from '$lib/types';

  let store: StoreSummary | null = null;
  let sources: SourceRoot[] = [];
  let scans: ScanProjection[] = [];
  let inventories: HistoricalInventory[] = [];
  let activeJob: Job | null = null;
  let sourcePath = '';
  let sourceLabel = '';
  let error = '';
  let loading = true;
  let refreshTimer: ReturnType<typeof setInterval> | undefined;

  async function refresh(showLoading = false) {
    if (showLoading) {
      loading = true;
    }
    [store, sources, scans, inventories] = await Promise.all([
      getStore(),
      getSources(),
      getScans(),
      getInventories()
    ]);
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

  async function submitSource() {
    error = '';
    await addSource(sourcePath, sourceLabel);
    sourcePath = '';
    sourceLabel = '';
    await refresh();
  }

  async function runScan(start: () => Promise<Job>) {
    error = '';
    try {
      activeJob = await start();
      while (activeJob.status === 'running') {
        await sleep(150);
        activeJob = await getJob(activeJob.job_id);
      }
      for (let attempt = 0; attempt < 20; attempt += 1) {
        await refresh();
        if (!activeJob.result_ref || scans.some((scan) => scan.scan_id === activeJob?.result_ref)) {
          return;
        }
        await sleep(150);
      }
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

  function sleep(ms: number) {
    return new Promise((resolve) => setTimeout(resolve, ms));
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

  onMount(() => {
    load();
    refreshTimer = setInterval(() => {
      if (activeJob?.status !== 'running') {
        refresh().catch((err) => {
          error = String(err);
        });
      }
    }, 3000);
  });

  onDestroy(() => {
    if (refreshTimer) {
      clearInterval(refreshTimer);
    }
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
    <button on:click={() => refresh(true)} disabled={loading}>Refresh</button>
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
  </section>

  <div class="grid">
    <section aria-labelledby="sources-heading">
      <div class="section-heading">
        <h2 id="sources-heading">Source roots</h2>
        <button class="primary" data-testid="start-source-scan" on:click={scanSources} disabled={activeJob?.status === 'running'}>
          Scan
        </button>
      </div>
      <form data-testid="add-source-form" on:submit|preventDefault={submitSource}>
        <input data-testid="source-path-input" bind:value={sourcePath} placeholder="Path" aria-label="Source path" required>
        <input data-testid="source-label-input" bind:value={sourceLabel} placeholder="Label" aria-label="Source label">
        <button type="submit">Add</button>
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
              <button data-testid="scan-source-{source.source_root_id}" on:click={() => scanSource(source.source_root_id)} disabled={activeJob?.status === 'running'}>
                Scan
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section aria-labelledby="job-heading">
      <h2 id="job-heading">Active job</h2>
      {#if activeJob}
        <p data-testid="job-status">{activeJob.kind}: {activeJob.status}</p>
        {#if activeJob.error}<p class="error">{activeJob.error}</p>{/if}
        <ul class="rows">
          {#each activeJob.progress as message}
            <li>{message}</li>
          {/each}
        </ul>
      {:else}
        <p class="empty" data-testid="job-empty">No job running.</p>
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
          </tr>
        </thead>
        <tbody>
          {#each scans as scan}
            <tr>
              <td><code>{scan.scan_id}</code></td>
              <td>{scan.status}</td>
              <td>{formatOptionalNumber(scan.report?.source_files_acquired)}</td>
              <td>{formatOptionalNumber(scan.report?.duplicate_acquisitions)}</td>
              <td>{formatOptionalBytes(scan.report?.duplicate_garbage_bytes)}</td>
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
    grid-template-columns: repeat(4, minmax(0, 1fr));
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

  section {
    padding: 16px;
    margin-bottom: 16px;
  }

  .grid {
    display: grid;
    grid-template-columns: 1.2fr 0.8fr;
    gap: 16px;
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
