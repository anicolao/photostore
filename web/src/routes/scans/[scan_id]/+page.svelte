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
      const [reportResult, filesResult] = await Promise.allSettled([getReport(scanID), getAcquiredFiles(scanID)]);
      if (reportResult.status === 'fulfilled') {
        report = reportResult.value;
      }
      if (filesResult.status === 'fulfilled') {
        files = filesResult.value;
      } else {
        throw filesResult.reason;
      }
    } catch (err) {
      error = String(err);
    } finally {
      loading = false;
    }
  });

  function displayPath(file: AcquiredFile) {
    return file.relative_path || file.path;
  }

  function directoryName(file: AcquiredFile) {
    const display = displayPath(file);
    const filename = file.filename || display.split(/[\\/]/).pop() || display;
    if (display === filename) return '';
    return display.slice(0, Math.max(0, display.length - filename.length)).replace(/[\\/]$/, '');
  }
</script>

<svelte:head>
  <title>Photostore scan {scanID}</title>
</svelte:head>

<main>
  <header>
    <div>
      <a href="/">Dashboard</a>
      <h1>Photos</h1>
      <p><code>{scanID}</code></p>
    </div>
    <div class="count" data-testid="acquired-count">{files.length} files</div>
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
        <div class="photo-grid" data-testid="photo-grid">
          {#each files as file}
            <a class="photo-card" data-testid="photo-card" href={file.view_url}>
              <span class="thumb-wrap">
                <img data-testid="thumbnail-image" src={file.thumbnail_url} alt={file.filename} loading="lazy">
              </span>
              <span class="filename" data-testid="photo-filename">{file.filename}</span>
              {#if directoryName(file)}
                <span class="directory">{directoryName(file)}</span>
              {/if}
            </a>
          {/each}
        </div>
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

  .photo-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 14px;
  }

  .photo-card {
    display: grid;
    grid-template-rows: 132px auto auto;
    gap: 7px;
    color: inherit;
    text-decoration: none;
    min-width: 0;
  }

  .photo-card:hover .filename {
    text-decoration: underline;
  }

  .thumb-wrap {
    display: grid;
    place-items: center;
    overflow: hidden;
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #eef1f5;
  }

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .filename {
    font-weight: 650;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .directory {
    color: #5f6368;
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
