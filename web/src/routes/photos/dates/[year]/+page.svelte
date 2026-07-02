<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { getPhotoMonths } from '$lib/api';
  import type { PhotoDateBucketResponse } from '$lib/types';

  let year = '';
  let response: PhotoDateBucketResponse | null = null;
  let loading = true;
  let error = '';

  onMount(async () => {
    year = $page.params.year ?? '';
    try {
      response = await getPhotoMonths(year);
    } catch (err) {
      error = String(err);
    } finally {
      loading = false;
    }
  });
</script>

<svelte:head>
  <title>Photostore photos {year}</title>
</svelte:head>

<main>
  <header>
    <div>
      <a href="/photos/dates">Photos by date</a>
      <h1>{year}</h1>
    </div>
  </header>

  {#if loading}
    <section>Loading...</section>
  {:else if error}
    <section class="error" data-testid="date-error">{error}</section>
  {:else}
    <section>
      <div class="bucket-grid" data-testid="date-bucket-grid">
        {#each response?.buckets ?? [] as bucket}
          <a class="bucket-card" data-testid="date-bucket" href={`/photos/dates/${bucket.bucket_key.replace('-', '/')}`}>
            <strong>{bucket.display_label}</strong>
            <span>{bucket.photo_count} photos</span>
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

  section {
    border: 1px solid #d9dee7;
    border-radius: 8px;
    background: #ffffff;
    padding: 16px;
  }

  .bucket-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 14px;
  }

  .bucket-card {
    display: grid;
    gap: 6px;
    border: 1px solid #d9dee7;
    border-radius: 8px;
    color: inherit;
    padding: 14px;
    text-decoration: none;
  }

  .bucket-card:hover strong {
    text-decoration: underline;
  }

  .bucket-card span {
    color: #5f6368;
  }

  .error {
    border-color: #d93025;
    color: #a50e0e;
  }
</style>
