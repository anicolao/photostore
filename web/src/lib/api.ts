import type { HistoricalInventory, Job, ScanProjection, ScanReport, SourceRoot, StoreSummary } from './types';

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

export function getReport(scanID: string): Promise<ScanReport> {
  return request(`/api/scans/${scanID}/report`);
}

export function getInventories(): Promise<HistoricalInventory[]> {
  return request('/api/inventories');
}
