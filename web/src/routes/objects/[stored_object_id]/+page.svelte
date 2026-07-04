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
  $: mapURL = storedObjectID ? `/api/objects/${storedObjectID}/map.svg` : '';
  $: fields = metadata ? Object.entries(metadata.fields).toSorted(([a], [b]) => a.localeCompare(b)) : [];
  $: camera = metadata ? cameraLabel(metadata.fields) : '';
  $: takenAt = metadata ? captureDateLabel(metadata.fields) : '';
  $: location = metadata ? locationLabel(metadata.fields) : '';
  $: coordinates = metadata ? gpsCoordinates(metadata.fields) : null;

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

  function rawField(fields: ObjectMetadata['fields'], name: string) {
    return fields[name]?.raw?.trim() ?? '';
  }

  function cameraLabel(fields: ObjectMetadata['fields']) {
    const make = rawField(fields, 'make');
    const model = rawField(fields, 'model');
    if (make && model && !model.toLowerCase().includes(make.toLowerCase())) {
      return `${make} ${model}`;
    }
    return model || make || 'Unknown camera';
  }

  function captureDateLabel(fields: ObjectMetadata['fields']) {
    const raw = rawField(fields, 'datetime_original');
    if (!raw) return 'No DateTimeOriginal';
    const match = raw.match(/^(\d{4}):(\d{2}):(\d{2}) (\d{2}):(\d{2}):(\d{2})$/);
    if (!match) return raw;
    const [, year, month, day, hour, minute, second] = match;
    return `${year}-${month}-${day} ${hour}:${minute}:${second}`;
  }

  function locationLabel(fields: ObjectMetadata['fields']) {
    const coords = gpsCoordinates(fields);
    if (!coords) {
      return 'No GPS location';
    }
    return `${coords.lat.toFixed(6)}, ${coords.lon.toFixed(6)}`;
  }

  function googleMapsURL(coords: { lat: number; lon: number }) {
    return `https://www.google.com/maps/search/?api=1&query=${coords.lat.toFixed(6)},${coords.lon.toFixed(6)}`;
  }

  function gpsCoordinates(fields: ObjectMetadata['fields']) {
    const lat = gpsCoordinate(fields, 'gps_latitude', 'gps_latitude_ref');
    const lon = gpsCoordinate(fields, 'gps_longitude', 'gps_longitude_ref');
    if (lat === null || lon === null) {
      return null;
    }
    return { lat, lon };
  }

  function gpsCoordinate(fields: ObjectMetadata['fields'], valueKey: string, refKey: string) {
    const raw = rawField(fields, valueKey);
    if (!raw) return null;
    const parts = raw.split(',').map(rationalValue);
    if (parts.length < 3 || parts.some((part) => part === null)) {
      return null;
    }
    const ref = rawField(fields, refKey).toUpperCase();
    const sign = ref === 'S' || ref === 'W' ? -1 : 1;
    return sign * (parts[0]! + parts[1]! / 60 + parts[2]! / 3600);
  }

  function rationalValue(raw: string) {
    const [num, den] = raw.split('/').map(Number);
    if (!Number.isFinite(num) || !Number.isFinite(den) || den === 0) return null;
    return num / den;
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
        <section class="summary-panel" aria-label="Photo information">
          <div>
            <span>Camera</span>
            <strong data-testid="photo-camera">{camera}</strong>
          </div>
          <div>
            <span>Taken</span>
            <strong data-testid="photo-date">{takenAt}</strong>
          </div>
          <div>
            <span>Location</span>
            <strong data-testid="photo-location">
              {#if coordinates}
                <a href={googleMapsURL(coordinates)} target="_blank" rel="noreferrer">{location}</a>
              {:else}
                {location}
              {/if}
            </strong>
          </div>
        </section>
        {#if coordinates}
          <section class="map-panel" aria-label="Photo location map">
            <a href={googleMapsURL(coordinates)} target="_blank" rel="noreferrer">
              <img class="map" data-testid="photo-map" src={mapURL} alt="Map fragment for {location}">
            </a>
          </section>
        {/if}
        <details class="debug-exif" data-testid="raw-exif">
          <summary>Raw EXIF</summary>
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
        </details>
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

  .summary-panel {
    display: grid;
    gap: 12px;
    margin: 16px 0;
  }

  .summary-panel div {
    border: 1px solid #d9dee7;
    border-radius: 8px;
    padding: 12px;
    background: #f8fafc;
  }

  .summary-panel span {
    display: block;
    color: #5f6368;
    font-size: 12px;
    margin-bottom: 4px;
  }

  .summary-panel strong {
    display: block;
    font-size: 16px;
    font-weight: 650;
    overflow-wrap: anywhere;
  }

  .summary-panel a {
    color: #185abc;
  }

  .map-panel {
    border: 1px solid #d9dee7;
    border-radius: 8px;
    overflow: hidden;
    margin: 0 0 16px;
  }

  .map-panel a {
    display: block;
  }

  .map {
    display: block;
    height: 220px;
    width: 100%;
    object-fit: cover;
  }

  .debug-exif {
    margin-top: 14px;
  }

  summary {
    cursor: pointer;
    font-weight: 650;
  }

  dl {
    margin: 12px 0 0;
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
