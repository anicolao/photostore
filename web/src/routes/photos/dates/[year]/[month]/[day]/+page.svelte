<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getDatedPhotos } from '$lib/api';
  import { objectURLWithContext } from '$lib/navigation';
  import type { DatedPhoto, DatedPhotoResponse } from '$lib/types';

  let year = '';
  let month = '';
  let day = '';
  let response: DatedPhotoResponse | null = null;
  let loading = true;
  let error = '';

  onMount(async () => {
    year = $page.params.year ?? '';
    month = $page.params.month ?? '';
    day = $page.params.day ?? '';
    try {
      response = await getDatedPhotos(year, month, day);
    } catch (err) {
      error = String(err);
    } finally {
      loading = false;
    }
  });

  function directoryName(photo: DatedPhoto) {
    const filename = photo.filename || photo.relative_path.split(/[\\/]/).pop() || photo.relative_path;
    if (!photo.relative_path || photo.relative_path === filename) return '';
    return photo.relative_path.slice(0, Math.max(0, photo.relative_path.length - filename.length)).replace(/[\\/]$/, '');
  }
</script>

<svelte:head>
  <title>Photostore photos {year}-{month}-{day}</title>
</svelte:head>

<main>
  <header>
    <div>
      <a href={`/photos/dates/${year}/${month}`}>Month</a>
      <h1>{year}-{month}-{day}</h1>
      <p>{response?.photos.length ?? 0} photos</p>
    </div>
  </header>

  {#if loading}
    <section>Loading...</section>
  {:else if error}
    <section class="error" data-testid="date-error">{error}</section>
  {:else}
    <section>
      <div class="photo-grid" data-testid="date-photo-grid">
        {#each response?.photos ?? [] as photo}
          <a class="photo-card" data-testid="date-photo-card" href={objectURLWithContext(photo.view_url, { list: 'date', date: `${year}-${month}-${day}` })}>
            <span class="thumb-wrap">
              <img data-testid="date-thumbnail-image" src={photo.thumbnail_url} alt={photo.filename} loading="lazy">
            </span>
            <span class="filename" data-testid="date-photo-filename">{photo.filename}</span>
            {#if directoryName(photo)}
              <span class="directory">{directoryName(photo)}</span>
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

  .error {
    border-color: #d93025;
    color: #a50e0e;
  }
</style>
