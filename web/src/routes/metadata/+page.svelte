<script lang="ts">
  import { onMount } from 'svelte';
  import { getMetadataFailures, getMetadataMissing, getMetadataSummary } from '$lib/api';
  import type { MetadataPhoto, MetadataSummary } from '$lib/types';

  let summary: MetadataSummary | null = null;
  let failures: MetadataPhoto[] = [];
  let missing: MetadataPhoto[] = [];
  let summaryLoading = true;
  let failuresLoading = true;
  let missingLoading = true;
  let error = '';
  const thumbnailPixelLimit = 1_000_000;

  $: likelySmallFailures = failures.filter(isLikelyThumbnailOrCrop);
  $: reviewFailures = failures.filter((photo) => !isLikelyThumbnailOrCrop(photo));

  onMount(async () => {
    void loadSummary();
    void loadFailures();
  });

  async function loadSummary() {
    try {
      const nextSummary = await getMetadataSummary();
      summary = nextSummary;
      if (nextSummary.missing_count === 0) {
        missing = [];
        missingLoading = false;
      } else {
        void loadMissing();
      }
    } catch (err) {
      error = String(err);
      missingLoading = false;
    } finally {
      summaryLoading = false;
    }
  }

  async function loadFailures() {
    try {
      failures = await getMetadataFailures();
    } catch (err) {
      error = String(err);
    } finally {
      failuresLoading = false;
    }
  }

  async function loadMissing() {
    try {
      missing = await getMetadataMissing();
    } catch (err) {
      error = String(err);
    } finally {
      missingLoading = false;
    }
  }

  function directoryName(photo: MetadataPhoto) {
    const filename = photo.filename || photo.relative_path.split(/[\\/]/).pop() || photo.relative_path;
    if (!photo.relative_path || photo.relative_path === filename) return '';
    return photo.relative_path.slice(0, Math.max(0, photo.relative_path.length - filename.length)).replace(/[\\/]$/, '');
  }

  function isLikelyThumbnailOrCrop(photo: MetadataPhoto) {
    return Boolean(photo.pixel_count && photo.pixel_count > 0 && photo.pixel_count < thumbnailPixelLimit);
  }

  function dimensionsLabel(photo: MetadataPhoto) {
    if (!photo.width || !photo.height) return 'Dimensions unknown';
    const megapixels = photo.pixel_count ? photo.pixel_count / 1_000_000 : (photo.width * photo.height) / 1_000_000;
    return `${photo.width} x ${photo.height} (${megapixels.toFixed(2)} MP)`;
  }
</script>

<svelte:head>
  <title>Photostore metadata</title>
</svelte:head>

<main>
  <header>
    <div>
      <a href="/">Dashboard</a>
      <h1>Metadata</h1>
      {#if summary}
        <p>{summary.extractor_name} v{summary.extractor_version}</p>
      {/if}
    </div>
  </header>

  {#if error}
    <section class="error" data-testid="metadata-error">{error}</section>
  {/if}

  {#if summaryLoading}
    <section data-testid="metadata-summary-loading">Loading...</section>
  {:else}
    <section class="summary" aria-label="Metadata status">
      <div>
        <span>Content</span>
        <strong data-testid="metadata-content-count">{summary?.content_count ?? 0}</strong>
      </div>
      <div>
        <span>Extracted</span>
        <strong data-testid="metadata-extracted-count">{summary?.extracted_count ?? 0}</strong>
      </div>
      <div>
        <span>No metadata found</span>
        <strong data-testid="metadata-failed-count">{summary?.failed_count ?? 0}</strong>
      </div>
      <div>
        <span>Not scanned</span>
        <strong data-testid="metadata-missing-count">{summary?.missing_count ?? 0}</strong>
      </div>
    </section>
  {/if}

  <section aria-labelledby="failed-heading">
    <h2 id="failed-heading">No metadata found</h2>
    {#if failuresLoading}
      <p class="empty" data-testid="metadata-failures-loading">Loading...</p>
    {:else if failures.length === 0}
      <p class="empty" data-testid="metadata-failures-empty">No metadata failures.</p>
    {:else}
      <div class="photo-list" data-testid="metadata-failures-list">
        {#each reviewFailures as photo}
          <a class="photo-row" data-testid="metadata-failure" href={photo.view_url}>
            <span class="thumb-wrap">
              <img src={photo.thumbnail_url} alt={photo.filename} loading="lazy">
            </span>
            <span class="details">
              <strong>{photo.filename}</strong>
              <span>{dimensionsLabel(photo)}</span>
              {#if directoryName(photo)}
                <span>{directoryName(photo)}</span>
              {/if}
              {#if photo.error_message}
                <span class="reason">{photo.error_message}</span>
              {/if}
            </span>
          </a>
        {/each}
      </div>
      {#if likelySmallFailures.length > 0}
        <details class="small-failures" data-testid="metadata-small-failures">
          <summary>
            <span>Likely thumbnails or crops</span>
            <strong data-testid="metadata-small-failures-count">{likelySmallFailures.length}</strong>
          </summary>
          <div class="photo-list" data-testid="metadata-small-failures-list">
            {#each likelySmallFailures as photo}
              <a class="photo-row" data-testid="metadata-small-failure" href={photo.view_url}>
                <span class="thumb-wrap">
                  <img src={photo.thumbnail_url} alt={photo.filename} loading="lazy">
                </span>
                <span class="details">
                  <strong>{photo.filename}</strong>
                  <span>{dimensionsLabel(photo)}</span>
                  {#if directoryName(photo)}
                    <span>{directoryName(photo)}</span>
                  {/if}
                  {#if photo.error_message}
                    <span class="reason">{photo.error_message}</span>
                  {/if}
                </span>
              </a>
            {/each}
          </div>
        </details>
      {/if}
    {/if}
  </section>

  <section aria-labelledby="missing-heading">
    <h2 id="missing-heading">Not scanned by current extractor</h2>
    {#if missingLoading}
      <p class="empty" data-testid="metadata-missing-loading">Loading...</p>
    {:else if missing.length === 0}
      <p class="empty" data-testid="metadata-missing-empty">No unscanned photos.</p>
    {:else}
      <div class="photo-list" data-testid="metadata-missing-list">
        {#each missing as photo}
          <a class="photo-row" data-testid="metadata-missing" href={photo.view_url}>
            <span class="thumb-wrap">
              <img src={photo.thumbnail_url} alt={photo.filename} loading="lazy">
            </span>
            <span class="details">
              <strong>{photo.filename}</strong>
              {#if directoryName(photo)}
                <span>{directoryName(photo)}</span>
              {/if}
            </span>
          </a>
        {/each}
      </div>
    {/if}
  </section>
</main>

<style>
  main {
    max-width: 1180px;
    margin: 0 auto;
    padding: 22px;
  }

  header {
    margin-bottom: 18px;
  }

  h1 {
    margin: 8px 0 2px;
    font-size: 26px;
  }

  h2,
  p {
    margin-top: 0;
  }

  p,
  .empty {
    color: #5f6368;
  }

  section {
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #ffffff;
    padding: 16px;
    margin-bottom: 16px;
  }

  .summary {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
    border: 0;
    background: transparent;
    padding: 0;
  }

  .summary div {
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #ffffff;
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

  .photo-list {
    display: grid;
    gap: 10px;
  }

  .photo-row {
    display: grid;
    grid-template-columns: 72px 1fr;
    gap: 12px;
    align-items: center;
    color: inherit;
    text-decoration: none;
    border-top: 1px solid #eef1f5;
    padding-top: 10px;
  }

  .photo-row:hover strong {
    text-decoration: underline;
  }

  .thumb-wrap {
    display: grid;
    place-items: center;
    width: 72px;
    height: 54px;
    overflow: hidden;
    border: 1px solid #d9dee7;
    border-radius: 6px;
    background: #eef1f5;
  }

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .details {
    display: grid;
    gap: 3px;
    min-width: 0;
  }

  .details strong,
  .details span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .details span {
    color: #5f6368;
    font-size: 12px;
  }

  .details .reason {
    color: #a50e0e;
  }

  .small-failures {
    margin-top: 14px;
    border-top: 1px solid #eef1f5;
    padding-top: 12px;
  }

  summary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    cursor: pointer;
    color: #3c4043;
  }

  summary strong {
    border-radius: 999px;
    background: #eef1f5;
    padding: 2px 8px;
    font-size: 12px;
  }

  .error {
    border-color: #d93025;
    color: #a50e0e;
  }

  @media (max-width: 760px) {
    main {
      padding: 12px;
    }

    .summary {
      grid-template-columns: 1fr;
    }
  }
</style>
