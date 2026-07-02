<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getObjectMetadata } from '$lib/api';
  import type { ObjectMetadata } from '$lib/types';

  let storedObjectID = '';
  let metadata: ObjectMetadata | null = null;
  let metadataError = '';
  let infoOpen = false;

  $: bytesURL = storedObjectID ? `/api/objects/${storedObjectID}/bytes` : '';
  $: fields = metadata ? Object.entries(metadata.fields).toSorted(([a], [b]) => a.localeCompare(b)) : [];

  onMount(async () => {
    storedObjectID = $page.params.stored_object_id ?? '';
    try {
      metadata = await getObjectMetadata(storedObjectID);
    } catch (err) {
      metadataError = String(err);
    }
  });

  function fieldValue(field: Record<string, string>) {
    return field.raw ?? '';
  }
</script>

<svelte:head>
  <title>Photostore image {storedObjectID}</title>
</svelte:head>

<main class:info-open={infoOpen}>
  <header>
    <div>
      <a href="/">Dashboard</a>
      <h1>Image</h1>
      <p><code>{storedObjectID}</code></p>
    </div>
    <div class="actions">
      <a class="button-link" data-testid="open-original" href={bytesURL} target="_blank" rel="noreferrer">Open original</a>
      <button data-testid="toggle-exif" on:click={() => (infoOpen = !infoOpen)}>{infoOpen ? 'Hide info' : 'Info'}</button>
    </div>
  </header>

  <section class="viewer">
    {#if bytesURL}
      <img data-testid="object-image" src={bytesURL} alt={storedObjectID}>
    {/if}
  </section>

  {#if infoOpen}
    <aside data-testid="exif-panel" aria-label="EXIF data">
      <div class="panel-heading">
        <h2>EXIF</h2>
        <button aria-label="Close info" on:click={() => (infoOpen = false)}>Close</button>
      </div>
      {#if metadata}
        <p class="extractor">{metadata.extractor_name} v{metadata.extractor_version}</p>
        <dl>
          {#each fields as [name, field]}
            <div data-testid="exif-row">
              <dt>{name}</dt>
              <dd>
                {#if fieldValue(field)}
                  <strong>{fieldValue(field)}</strong>
                {/if}
                <span>{field.ifd} {field.tag} {field.type} x{field.count}</span>
              </dd>
            </div>
          {/each}
        </dl>
      {:else if metadataError}
        <p class="error" data-testid="exif-error">{metadataError}</p>
      {:else}
        <p>Loading...</p>
      {/if}
    </aside>
  {/if}
</main>

<style>
  main {
    min-height: 100vh;
    padding: 22px;
  }

  header {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    align-items: flex-start;
    margin: 0 auto 18px;
    max-width: 1180px;
  }

  h1 {
    margin: 8px 0 2px;
    font-size: 26px;
  }

  p {
    margin-top: 0;
    color: #5f6368;
  }

  .actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .button-link {
    border: 1px solid #9aa0a6;
    border-radius: 6px;
    background: #ffffff;
    color: #202124;
    padding: 7px 10px;
    text-decoration: none;
  }

  .viewer {
    display: grid;
    place-items: center;
    max-width: 1180px;
    min-height: 620px;
    margin: 0 auto;
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #ffffff;
    padding: 16px;
  }

  .viewer img {
    max-width: 100%;
    max-height: 76vh;
    object-fit: contain;
    display: block;
  }

  aside {
    position: fixed;
    top: 0;
    right: 0;
    width: min(420px, 100vw);
    height: 100vh;
    overflow: auto;
    box-sizing: border-box;
    border-left: 1px solid #d9dee7;
    background: #ffffff;
    padding: 18px;
    box-shadow: -8px 0 24px rgba(60, 64, 67, 0.18);
  }

  .panel-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  h2 {
    margin: 0;
    font-size: 18px;
  }

  .extractor {
    margin: 8px 0 14px;
  }

  dl {
    margin: 0;
  }

  dl div {
    border-top: 1px solid #edf0f4;
    padding: 10px 0;
  }

  dt {
    font-weight: 650;
    overflow-wrap: anywhere;
  }

  dd {
    display: grid;
    gap: 4px;
    margin: 5px 0 0;
    color: #5f6368;
    overflow-wrap: anywhere;
  }

  dd strong {
    color: #202124;
    font-weight: 500;
  }

  .error {
    color: #a50e0e;
  }
</style>
