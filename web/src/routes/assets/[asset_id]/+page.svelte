<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { applyAssetLabel, getAsset, getAssetNavigation, removeAssetLabel, setAssetQuality, setAssetStatus, setAssetVisibility } from '$lib/api';
  import type { Asset, AssetDetail, AssetNavigation } from '$lib/types';

  const qualities: Asset['quality'][] = ['Unrated', 'Best', 'Good', 'Poor'];
  const statuses: Asset['status'][] = ['Triage', 'Reviewed'];
  const visibilities: Asset['visibility'][] = ['Normal', 'Private'];

  let assetID = '';
  let asset: AssetDetail | null = null;
  let navigation: AssetNavigation | null = null;
  let label = '';
  let loading = true;
  let saving = false;
  let error = '';
  let advanceToNext = true;
  let currentRoute = '\u0000';
  let loadToken = 0;

  onMount(() => {
    const unsubscribe = page.subscribe(($page) => {
      const nextRoute = `${$page.params.asset_id ?? ''}?${$page.url.searchParams.toString()}`;
      if (nextRoute === currentRoute) return;
      currentRoute = nextRoute;
      assetID = $page.params.asset_id ?? '';
      void load($page.url.searchParams);
    });
    return unsubscribe;
  });

  async function load(params = new URLSearchParams()) {
    loading = true;
    error = '';
    const token = ++loadToken;
    try {
      const [nextAsset, nextNavigation] = await Promise.all([getAsset(assetID), loadNavigation(params)]);
      if (token !== loadToken) return;
      asset = nextAsset;
      navigation = nextNavigation;
    } catch (err) {
      if (token !== loadToken) return;
      error = String(err);
    } finally {
      if (token !== loadToken) return;
      loading = false;
    }
  }

  async function mutate(fn: () => Promise<unknown>, options: { advance?: boolean } = {}) {
    if (!asset) return;
    saving = true;
    error = '';
    try {
      await fn();
      if (options.advance && advanceToNext && navigation?.next) {
        await goto(navigation.next.view_url);
        return;
      }
      const params = new URLSearchParams($page.url.searchParams);
      asset = await getAsset(assetID);
      navigation = await loadNavigation(params);
    } catch (err) {
      error = String(err);
    } finally {
      saving = false;
    }
  }

  async function addLabel() {
    const next = label.trim();
    if (!next) return;
    await mutate(() => applyAssetLabel(assetID, next));
    label = '';
  }

  function sourceName(path: string, relativePath: string) {
    return relativePath || path;
  }

  async function loadNavigation(params: URLSearchParams) {
    try {
      return await getAssetNavigation(assetID, assetNavigationParams(params));
    } catch {
      return null;
    }
  }

  function assetNavigationParams(params: URLSearchParams) {
    const out = new URLSearchParams();
    for (const key of ['quality', 'status', 'visibility', 'label', 'has_date', 'min_megapixels', 'sort']) {
      for (const value of params.getAll(key)) {
        if (value) out.append(key, value);
      }
    }
    return out;
  }
</script>

<svelte:head>
  <title>Photostore asset {assetID}</title>
</svelte:head>

<main>
  <header>
    <div>
      <a href="/assets">Assets</a>
      <h1>{asset?.filename ?? 'Asset'}</h1>
      <p><code>{assetID}</code></p>
    </div>
    {#if asset}
      <div class="header-actions">
        <a class="button-link" data-testid="asset-open-image" href={asset.view_url}>Open image</a>
        <a class="button-link" data-testid="asset-prev" class:disabled={!navigation?.previous} aria-disabled={!navigation?.previous} href={navigation?.previous?.view_url ?? $page.url.pathname}>Previous</a>
        <a class="button-link" data-testid="asset-next" class:disabled={!navigation?.next} aria-disabled={!navigation?.next} href={navigation?.next?.view_url ?? $page.url.pathname}>Next</a>
      </div>
    {/if}
  </header>

  {#if loading}
    <section>Loading...</section>
  {:else if error}
    <section class="error" data-testid="asset-error">{error}</section>
  {:else if asset}
    <div class="assessment">
      <figure class="photo-stage" data-testid="asset-photo-stage">
        <img data-testid="asset-detail-image" src={asset.bytes_url} alt={asset.filename}>
      </figure>

      <aside class="side-panel" aria-label="Asset assessment">
        <section class="facts">
          <h2>Details</h2>
          <dl>
            <dt>Content</dt>
            <dd data-testid="asset-content-ref">{asset.content_ref}</dd>
            <dt>Capture time</dt>
            <dd data-testid="asset-capture-time">{asset.capture_time_local || asset.capture_date || 'No capture time'}</dd>
            <dt>Camera</dt>
            <dd data-testid="asset-camera">{asset.camera || 'Unknown'}</dd>
            <dt>Sources</dt>
            <dd data-testid="asset-source-count">{asset.source_occurrence_count}</dd>
          </dl>
        </section>

        <section class="controls" aria-label="Triage controls">
          <div class="advance">
            <label>
              <input data-testid="asset-advance-to-next" type="checkbox" bind:checked={advanceToNext}>
              Advance to next
            </label>
            {#if navigation}
              <span data-testid="asset-navigation-label">{navigation.label}</span>
            {/if}
          </div>
          <div>
            <h2>Quality</h2>
            <div class="buttons">
              {#each qualities as value}
                <button data-testid={`quality-${value}`} class:active={asset.quality === value} disabled={saving} on:click={() => mutate(() => setAssetQuality(assetID, value), { advance: value !== 'Unrated' })}>{value}</button>
              {/each}
            </div>
          </div>
          <div>
            <h2>Status</h2>
            <div class="buttons">
              {#each statuses as value}
                <button data-testid={`status-${value}`} class:active={asset.status === value} disabled={saving} on:click={() => mutate(() => setAssetStatus(assetID, value))}>{value}</button>
              {/each}
            </div>
          </div>
          <div>
            <h2>Visibility</h2>
            <div class="buttons">
              {#each visibilities as value}
                <button data-testid={`visibility-${value}`} class:active={asset.visibility === value} disabled={saving} on:click={() => mutate(() => setAssetVisibility(assetID, value))}>{value}</button>
              {/each}
            </div>
          </div>
          <div>
            <h2>Labels</h2>
            <form data-testid="asset-label-form" on:submit|preventDefault={addLabel}>
              <input data-testid="asset-label-input" bind:value={label} placeholder="Label" aria-label="Asset label">
              <button data-testid="asset-label-add" disabled={saving || !label.trim()}>Add</button>
            </form>
            <div class="labels" data-testid="asset-detail-labels">
              {#each asset.labels as item}
                <span>
                  {item}
                  <button data-testid={`remove-label-${item.toLowerCase()}`} disabled={saving} on:click={() => mutate(() => removeAssetLabel(assetID, item))}>Remove</button>
                </span>
              {/each}
            </div>
          </div>
        </section>

        <section>
          <h2>Sources</h2>
          <ul class="sources" data-testid="asset-sources">
            {#each asset.sources as source}
              <li>
                <strong>{source.source_kind}</strong>
                <span>{sourceName(source.path, source.relative_path)}</span>
                <code>{source.scan_id}</code>
              </li>
            {/each}
          </ul>
        </section>
      </aside>
    </div>
  {/if}
</main>

<style>
  main {
    width: 100%;
    min-height: 100vh;
    padding: 14px;
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    background: #f6f7f9;
  }

  header {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    align-items: flex-start;
    margin-bottom: 12px;
  }

  .header-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  h1 {
    margin: 6px 0 2px;
    font-size: 22px;
  }

  h2 {
    margin: 0 0 10px;
    font-size: 16px;
  }

  p {
    margin-top: 0;
    color: #5f6368;
  }

  section {
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #ffffff;
    padding: 12px;
  }

  .button-link {
    border: 1px solid #9aa0a6;
    border-radius: 6px;
    background: #ffffff;
    color: #202124;
    padding: 7px 10px;
    text-decoration: none;
  }

  .button-link.disabled {
    pointer-events: none;
    opacity: 0.45;
  }

  .assessment {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(320px, 380px);
    gap: 12px;
    min-height: 0;
  }

  .photo-stage {
    display: grid;
    place-items: center;
    margin: 0;
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #111418;
    min-width: 0;
    min-height: calc(100vh - 108px);
    max-height: calc(100vh - 108px);
    overflow: hidden;
  }

  .photo-stage img {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }

  .side-panel {
    min-height: 0;
    max-height: calc(100vh - 108px);
    overflow: auto;
    display: grid;
    align-content: start;
    gap: 12px;
  }

  dl {
    display: grid;
    grid-template-columns: 120px minmax(0, 1fr);
    gap: 8px 12px;
  }

  dt {
    color: #5f6368;
  }

  dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .controls {
    display: grid;
    grid-template-columns: 1fr;
    gap: 12px;
  }

  .advance {
    grid-column: 1 / -1;
    display: flex;
    gap: 14px;
    align-items: center;
    color: #5f6368;
    font-size: 13px;
  }

  .advance label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: #202124;
  }

  .buttons,
  .labels {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  button.active {
    background: #202124;
    color: #ffffff;
    border-color: #202124;
  }

  form {
    display: flex;
    gap: 8px;
    margin-bottom: 10px;
  }

  input {
    min-width: 0;
    width: 100%;
  }

  .labels span {
    border: 1px solid #d9dee7;
    border-radius: 6px;
    padding: 4px 6px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .sources {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 8px;
  }

  .sources li {
    display: grid;
    grid-template-columns: 1fr;
    gap: 8px;
  }

  .sources span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .error {
    border-color: #d93025;
    color: #a50e0e;
  }

  @media (max-width: 860px) {
    main {
      padding: 10px;
    }

    header {
      display: grid;
      grid-template-columns: 1fr;
    }

    .header-actions {
      justify-content: flex-start;
    }

    .assessment {
      grid-template-columns: 1fr;
    }

    .photo-stage {
      min-height: 65vh;
      max-height: 65vh;
    }

    .side-panel {
      max-height: none;
      overflow: visible;
    }
  }
</style>
