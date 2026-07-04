import type { AcquiredFile, Asset, AssetDetail, AssetPage, AssetSource, DatedPhotoResponse, HistoricalInventory, Job, LabelSummary, MetadataPhoto, MetadataSummary, ObjectMetadata, ObjectNavigation, PhotoDateBucketResponse, ScanProjection, ScanReport, SourceRoot, StoreSummary } from './types';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      'content-type': 'application/json',
      ...(init?.headers ?? {})
    }
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `${res.status} ${res.statusText}`);
  }
  return (await res.json()) as T;
}

export function getStore(): Promise<StoreSummary> {
  return request('/api/store');
}

export function getSources(): Promise<SourceRoot[]> {
  return request('/api/sources');
}

export function addSource(path: string, label: string): Promise<{ source_root_id: string }> {
  return request('/api/sources', {
    method: 'POST',
    body: JSON.stringify({ path, label })
  });
}

export function getScans(): Promise<ScanProjection[]> {
  return request('/api/scans?limit=20');
}

export function startSourceScan(): Promise<Job> {
  return request('/api/scans', {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export function startSingleSourceScan(sourceRootID: string): Promise<Job> {
  return request(`/api/sources/${sourceRootID}/scan`, {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export function getJob(jobID: string): Promise<Job> {
  return request(`/api/jobs/${jobID}`);
}

export function getJobs(): Promise<Job[]> {
  return request('/api/jobs');
}

export function resumeScan(scanID: string): Promise<Job> {
  return request(`/api/scans/${scanID}/resume`, {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export function refreshMissingMetadata(): Promise<Job> {
  return request('/api/metadata/refresh-missing', {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export function deduplicateDuplicates(): Promise<Job> {
  return request('/api/duplicates/deduplicate', {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export function collectThumbnailGarbage(): Promise<Job> {
  return request('/api/thumbnails/gc', {
    method: 'POST',
    body: JSON.stringify({})
  });
}

export function getReport(scanID: string): Promise<ScanReport> {
  return request(`/api/scans/${scanID}/report`);
}

export function getAcquiredFiles(scanID: string): Promise<AcquiredFile[]> {
  return request(`/api/scans/${scanID}/acquired`);
}

export function getObjectMetadata(storedObjectID: string): Promise<ObjectMetadata> {
  return request(`/api/objects/${storedObjectID}/metadata`);
}

export function getObjectNavigation(storedObjectID: string, query: string): Promise<ObjectNavigation> {
  return request(`/api/objects/${storedObjectID}/navigation?${query}`);
}

export function getMetadataSummary(): Promise<MetadataSummary> {
  return request('/api/metadata/summary');
}

export function getMetadataFailures(): Promise<MetadataPhoto[]> {
  return request('/api/metadata/failures');
}

export function getMetadataMissing(): Promise<MetadataPhoto[]> {
  return request('/api/metadata/missing');
}

export function getInventories(): Promise<HistoricalInventory[]> {
  return request('/api/inventories');
}

export function getPhotoYears(): Promise<PhotoDateBucketResponse> {
  return request('/api/photos/dates');
}

export function getPhotoMonths(year: string): Promise<PhotoDateBucketResponse> {
  return request(`/api/photos/dates/${year}`);
}

export function getPhotoDays(year: string, month: string): Promise<PhotoDateBucketResponse> {
  return request(`/api/photos/dates/${year}/${month}`);
}

export function getDatedPhotos(year: string, month: string, day: string): Promise<DatedPhotoResponse> {
  return request(`/api/photos/dates/${year}/${month}/${day}`);
}

export function getUndatedPhotos(): Promise<DatedPhotoResponse> {
  return request('/api/photos/undated');
}

export function getAssets(params = new URLSearchParams()): Promise<AssetPage> {
  const query = params.toString();
  return request(query ? `/api/assets?${query}` : '/api/assets');
}

export function getAsset(assetID: string): Promise<AssetDetail> {
  return request(`/api/assets/${assetID}`);
}

export function getAssetSources(assetID: string): Promise<AssetSource[]> {
  return request(`/api/assets/${assetID}/sources`);
}

export function getLabels(): Promise<LabelSummary[]> {
  return request('/api/labels');
}

export function setAssetQuality(assetID: string, quality: Asset['quality']): Promise<{ ok: boolean }> {
  return request(`/api/assets/${assetID}/quality`, {
    method: 'POST',
    body: JSON.stringify({ quality })
  });
}

export function setAssetStatus(assetID: string, status: Asset['status']): Promise<{ ok: boolean }> {
  return request(`/api/assets/${assetID}/status`, {
    method: 'POST',
    body: JSON.stringify({ status })
  });
}

export function setAssetVisibility(assetID: string, visibility: Asset['visibility']): Promise<{ ok: boolean }> {
  return request(`/api/assets/${assetID}/visibility`, {
    method: 'POST',
    body: JSON.stringify({ visibility })
  });
}

export function applyAssetLabel(assetID: string, label: string): Promise<{ ok: boolean }> {
  return request(`/api/assets/${assetID}/labels`, {
    method: 'POST',
    body: JSON.stringify({ label })
  });
}

export function removeAssetLabel(assetID: string, label: string): Promise<{ ok: boolean }> {
  return request(`/api/assets/${assetID}/labels`, {
    method: 'DELETE',
    body: JSON.stringify({ label })
  });
}
