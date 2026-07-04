<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getAssets, getLabels } from '$lib/api';
  import type { Asset, LabelSummary } from '$lib/types';

  const qualities = ['Unrated', 'Best', 'Good', 'Poor'];
  const statuses = ['Triage', 'Reviewed'];
  const visibilities = ['Normal', 'Private'];
  const sorts = [
    { value: '', label: 'Filename' },
    { value: 'date_asc', label: 'Date ↑' },
    { value: 'date_desc', label: 'Date ↓' }
  ];

  let assets: Asset[] = [];
  let labels: LabelSummary[] = [];
  let loading = true;
  let error = '';
  let total = 0;
  let limit = 60;
  let offset = 0;
  let nextOffset: number | undefined;
  let prevOffset: number | undefined;
  let quality = '';
  let status = '';
  let visibility = '';
  let label = '';
  let hasDate = false;
  let minMegapixels = false;
  let sort = '';
  let currentQuery = '\u0000';
  let loadToken = 0;

  onMount(() => {
    const unsubscribe = page.subscribe(($page) => {
      const nextQuery = $page.url.searchParams.toString();
      if (nextQuery === currentQuery) return;
      currentQuery = nextQuery;
      void loadForURL($page.url);
    });
    return unsubscribe;
  });

  async function loadForURL(url: URL) {
    quality = url.searchParams.get('quality') ?? '';
    status = url.searchParams.get('status') ?? '';
    visibility = url.searchParams.get('visibility') ?? '';
    label = url.searchParams.get('label') ?? '';
    hasDate = url.searchParams.get('has_date') === '1';
    minMegapixels = url.searchParams.get('min_megapixels') === '1';
    sort = url.searchParams.get('sort') ?? '';
    limit = boundedParam(url.searchParams.get('limit'), 60, 1, 200);
    offset = boundedParam(url.searchParams.get('offset'), 0, 0, 1_000_000_000);
    loading = true;
    error = '';
    const token = ++loadToken;
    const params = new URLSearchParams();
    if (quality) params.set('quality', quality);
    if (status) params.set('status', status);
    if (visibility) params.set('visibility', visibility);
    if (label) params.set('label', label);
    if (hasDate) params.set('has_date', '1');
    if (minMegapixels) params.set('min_megapixels', '1');
    if (sort) params.set('sort', sort);
    params.set('limit', String(limit));
    if (offset > 0) params.set('offset', String(offset));
    try {
      const [page, nextLabels] = await Promise.all([getAssets(params), getLabels()]);
      if (token !== loadToken) return;
      assets = page.assets;
      total = page.total;
      limit = page.limit;
      offset = page.offset;
      nextOffset = page.next_offset;
      prevOffset = page.prev_offset;
      labels = nextLabels;
    } catch (err) {
      if (token !== loadToken) return;
      error = String(err);
    } finally {
      if (token !== loadToken) return;
      loading = false;
    }
  }

  function boundedParam(raw: string | null, fallback: number, min: number, max: number) {
    const value = raw === null ? fallback : Number.parseInt(raw, 10);
    if (!Number.isFinite(value)) return fallback;
    if (value < min) return min;
    if (value > max) return max;
    return value;
  }

  function filteredHref(next: { quality?: string; status?: string; visibility?: string; label?: string; hasDate?: boolean; minMegapixels?: boolean; sort?: string }) {
    const params = new URLSearchParams();
    const nextQuality = next.quality ?? quality;
    const nextStatus = next.status ?? status;
    const nextVisibility = next.visibility ?? visibility;
    const nextLabel = next.label ?? label;
    const nextHasDate = next.hasDate ?? hasDate;
    const nextMinMegapixels = next.minMegapixels ?? minMegapixels;
    const nextSort = next.sort ?? sort;
    if (nextQuality) params.set('quality', nextQuality);
    if (nextStatus) params.set('status', nextStatus);
    if (nextVisibility) params.set('visibility', nextVisibility);
    if (nextLabel) params.set('label', nextLabel);
    if (nextHasDate) params.set('has_date', '1');
    if (nextMinMegapixels) params.set('min_megapixels', '1');
    if (nextSort) params.set('sort', nextSort);
    params.set('limit', String(limit));
    const query = params.toString();
    return query ? `/assets?${query}` : '/assets';
  }

  function assetContextParams() {
    const params = new URLSearchParams();
    if (quality) params.set('quality', quality);
    if (status) params.set('status', status);
    if (visibility) params.set('visibility', visibility);
    if (label) params.set('label', label);
    if (hasDate) params.set('has_date', '1');
    if (minMegapixels) params.set('min_megapixels', '1');
    if (sort) params.set('sort', sort);
    return params;
  }

  function pageHref(nextOffset: number | undefined) {
    const params = assetContextParams();
    params.set('limit', String(limit));
    if (nextOffset && nextOffset > 0) params.set('offset', String(nextOffset));
    const query = params.toString();
    return query ? `/assets?${query}` : '/assets';
  }

  function assetHref(assetID: string) {
    const params = assetContextParams();
    const query = params.toString();
    return query ? `/assets/${assetID}?${query}` : `/assets/${assetID}`;
  }

  function formatCapture(asset: Asset) {
    return asset.capture_time_local || asset.capture_date || 'No capture time';
  }

  $: pageStart = total === 0 ? 0 : offset + 1;
  $: pageEnd = Math.min(offset + assets.length, total);
</script>

<svelte:head>
  <title>Photostore assets</title>
</svelte:head>

<main>
  <header>
    <div>
      <a href="/">Dashboard</a>
      <h1>Assets</h1>
      <p data-testid="asset-count">{total} assets</p>
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
    <div>
      <span>Metadata</span>
      <a data-testid="date-filter-all" class:active={!hasDate} href={filteredHref({ hasDate: false })}>All dates</a>
      <a data-testid="date-filter-known" class:active={hasDate} href={filteredHref({ hasDate: true })}>Has date</a>
      <a data-testid="megapixel-filter-all" class:active={!minMegapixels} href={filteredHref({ minMegapixels: false })}>All sizes</a>
      <a data-testid="megapixel-filter-large" class:active={minMegapixels} href={filteredHref({ minMegapixels: true })}>&gt; 1MP</a>
    </div>
    <div>
      <span>Sort</span>
      {#each sorts as item}
        <a data-testid={`sort-filter-${item.value || 'filename'}`} class:active={sort === item.value} href={filteredHref({ sort: item.value })}>{item.label}</a>
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
      <div class="pager" data-testid="asset-pager">
        <span data-testid="asset-page-range">Showing {pageStart}-{pageEnd} of {total}</span>
        <div>
          <a class:disabled={prevOffset === undefined} aria-disabled={prevOffset === undefined} data-testid="asset-prev-page" href={prevOffset === undefined ? pageHref(offset) : pageHref(prevOffset)}>Previous</a>
          <a class:disabled={nextOffset === undefined} aria-disabled={nextOffset === undefined} data-testid="asset-next-page" href={nextOffset === undefined ? pageHref(offset) : pageHref(nextOffset)}>Next</a>
        </div>
      </div>
      <div class="asset-grid" data-testid="asset-grid">
        {#each assets as asset}
          <a class="asset-card" data-testid="asset-card" href={assetHref(asset.asset_id)}>
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

  .pager {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 14px;
    color: #5f6368;
    font-size: 13px;
  }

  .pager div {
    display: flex;
    gap: 8px;
  }

  .pager a {
    border: 1px solid #d9dee7;
    border-radius: 6px;
    padding: 5px 9px;
    color: inherit;
    text-decoration: none;
  }

  .pager a.disabled {
    pointer-events: none;
    opacity: 0.45;
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
