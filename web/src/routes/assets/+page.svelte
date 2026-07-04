<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getAssets, getLabels } from '$lib/api';
  import type { Asset, LabelSummary } from '$lib/types';

  const qualities = ['Unrated', 'Best', 'Good', 'Poor'];
  const statuses = ['Triage', 'Reviewed'];
  const visibilities = ['Normal', 'Private'];

  let assets: Asset[] = [];
  let labels: LabelSummary[] = [];
  let loading = true;
  let error = '';
  let quality = '';
  let status = '';
  let visibility = '';
  let label = '';

  onMount(async () => {
    quality = $page.url.searchParams.get('quality') ?? '';
    status = $page.url.searchParams.get('status') ?? '';
    visibility = $page.url.searchParams.get('visibility') ?? '';
    label = $page.url.searchParams.get('label') ?? '';
    await load();
  });

  async function load() {
    loading = true;
    error = '';
    const params = new URLSearchParams();
    if (quality) params.set('quality', quality);
    if (status) params.set('status', status);
    if (visibility) params.set('visibility', visibility);
    if (label) params.set('label', label);
    try {
      [assets, labels] = await Promise.all([getAssets(params), getLabels()]);
    } catch (err) {
      error = String(err);
    } finally {
      loading = false;
    }
  }

  function filteredHref(next: { quality?: string; status?: string; visibility?: string; label?: string }) {
    const params = new URLSearchParams();
    const nextQuality = next.quality ?? quality;
    const nextStatus = next.status ?? status;
    const nextVisibility = next.visibility ?? visibility;
    const nextLabel = next.label ?? label;
    if (nextQuality) params.set('quality', nextQuality);
    if (nextStatus) params.set('status', nextStatus);
    if (nextVisibility) params.set('visibility', nextVisibility);
    if (nextLabel) params.set('label', nextLabel);
    const query = params.toString();
    return query ? `/assets?${query}` : '/assets';
  }

  function formatCapture(asset: Asset) {
    return asset.capture_time_local || asset.capture_date || 'No capture time';
  }
</script>

<svelte:head>
  <title>Photostore assets</title>
</svelte:head>

<main>
  <header>
    <div>
      <a href="/">Dashboard</a>
      <h1>Assets</h1>
      <p data-testid="asset-count">{assets.length} assets</p>
    </div>
  </header>

  <section class="filters" aria-label="Asset filters">
    <div>
      <span>Quality</span>
      <a data-testid="quality-filter-all" class:active={!quality} href={filteredHref({ quality: '' })}>All</a>
      {#each qualities as value}
        <a data-testid={`quality-filter-${value}`} class:active={quality === value} href={filteredHref({ quality: value })}>{value}</a>
      {/each}
    </div>
    <div>
      <span>Status</span>
      <a data-testid="status-filter-all" class:active={!status} href={filteredHref({ status: '' })}>All</a>
      {#each statuses as value}
        <a data-testid={`status-filter-${value}`} class:active={status === value} href={filteredHref({ status: value })}>{value}</a>
      {/each}
    </div>
    <div>
      <span>Visibility</span>
      <a data-testid="visibility-filter-all" class:active={!visibility} href={filteredHref({ visibility: '' })}>All</a>
      {#each visibilities as value}
        <a data-testid={`visibility-filter-${value}`} class:active={visibility === value} href={filteredHref({ visibility: value })}>{value}</a>
      {/each}
    </div>
    <div>
      <span>Labels</span>
      <a data-testid="label-filter-all" class:active={!label} href={filteredHref({ label: '' })}>All</a>
      {#each labels as item}
        <a data-testid={`label-filter-${item.normalized_label}`} class:active={label === item.normalized_label} href={filteredHref({ label: item.normalized_label })}>{item.display_label}</a>
      {/each}
    </div>
  </section>

  {#if loading}
    <section>Loading...</section>
  {:else if error}
    <section class="error" data-testid="assets-error">{error}</section>
  {:else if assets.length === 0}
    <section class="empty" data-testid="assets-empty">No assets match these filters.</section>
  {:else}
    <section>
      <div class="asset-grid" data-testid="asset-grid">
        {#each assets as asset}
          <a class="asset-card" data-testid="asset-card" href={`/assets/${asset.asset_id}`}>
            <span class="thumb-wrap">
              <img data-testid="asset-thumbnail" src={asset.thumbnail_url} alt={asset.filename} loading="lazy">
            </span>
            <span class="filename" data-testid="asset-filename">{asset.filename}</span>
            <span class="meta">{formatCapture(asset)}</span>
            <span class="badges">
              <span data-testid="asset-quality">{asset.quality}</span>
              <span data-testid="asset-status">{asset.status}</span>
              <span data-testid="asset-visibility">{asset.visibility}</span>
            </span>
            {#if asset.labels.length > 0}
              <span class="labels" data-testid="asset-labels">{asset.labels.join(', ')}</span>
            {/if}
          </a>
        {/each}
      </div>
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
    margin-bottom: 16px;
  }

  .filters {
    display: grid;
    gap: 10px;
  }

  .filters div {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .filters span {
    min-width: 72px;
    color: #5f6368;
    font-size: 13px;
  }

  .filters a,
  .badges span {
    border: 1px solid #d9dee7;
    border-radius: 6px;
    padding: 4px 8px;
    color: inherit;
    text-decoration: none;
    font-size: 13px;
  }

  .filters a.active {
    background: #202124;
    color: #ffffff;
    border-color: #202124;
  }

  .asset-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
    gap: 14px;
  }

  .asset-card {
    display: grid;
    grid-template-rows: 132px auto auto auto auto;
    gap: 7px;
    color: inherit;
    text-decoration: none;
    min-width: 0;
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

  .meta,
  .labels {
    color: #5f6368;
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badges {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }

  .badges span {
    background: #f8fafc;
  }

  .error {
    border-color: #d93025;
    color: #a50e0e;
  }
</style>
