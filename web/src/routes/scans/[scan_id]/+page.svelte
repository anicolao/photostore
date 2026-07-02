<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getAcquiredFiles, getReport } from '$lib/api';
  import type { AcquiredFile, ScanReport } from '$lib/types';

  let scanID = '';
  let report: ScanReport | null = null;
  let files: AcquiredFile[] = [];
  let loading = true;
  let error = '';

  onMount(async () => {
    scanID = $page.params.scan_id ?? '';
    try {
      [report, files] = await Promise.all([getReport(scanID), getAcquiredFiles(scanID)]);
    } catch (err) {
      error = String(err);
    } finally {
      loading = false;
    }
  });

  function displayPath(file: AcquiredFile) {
    return file.relative_path || file.path;
  }
</script>

<svelte:head>
  <title>Photostore scan {scanID}</title>
</svelte:head>

<main>
  <header>
    <div>
      <a href="/">Dashboard</a>
      <h1>Acquired files</h1>
      <p><code>{scanID}</code></p>
    </div>
    {#if report}
      <div class="count" data-testid="acquired-count">{files.length} files</div>
    {/if}
  </header>

  {#if loading}
    <section>Loading...</section>
  {:else if error}
    <section class="error" data-testid="acquired-error">{error}</section>
  {:else}
    <section>
      {#if files.length === 0}
        <p data-testid="acquired-empty">No acquired files for this scan.</p>
      {:else}
        <table data-testid="acquired-table">
          <thead>
            <tr>
              <th>File</th>
              <th>Source</th>
              <th>Content</th>
            </tr>
          </thead>
          <tbody>
            {#each files as file}
              <tr>
                <td>
                  <a data-testid="acquired-image-link" href={file.view_url} target="_blank" rel="noreferrer">
                    {displayPath(file)}
                  </a>
                </td>
                <td>{file.source_kind}</td>
                <td><code>{file.content_ref}</code></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  {/if}
</main>

<style>
  main {
    max-width: 1180px;
    margin: 0 auto;
    padding: 22px;
  }

  header {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    align-items: flex-start;
    margin-bottom: 18px;
  }

  h1 {
    margin: 8px 0 2px;
    font-size: 26px;
  }

  p {
    margin-top: 0;
    color: #5f6368;
  }

  section {
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #ffffff;
    padding: 16px;
  }

  .count {
    color: #5f6368;
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
    vertical-align: top;
  }

  code {
    word-break: break-all;
  }
</style>
