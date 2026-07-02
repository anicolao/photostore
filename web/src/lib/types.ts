export type StoreSummary = {
  store_path: string;
  event_count: number;
  source_root_count: number;
  historical_inventory_count: number;
  scan_count: number;
  content_count: number;
  retained_duplicate_bytes: number;
  last_scan_completed_at_ms: number | null;
};

export type SourceRoot = {
  source_root_id: string;
  path: string;
  label: string;
  last_scan_id?: string | null;
  last_scan_completed_at_ms?: number | null;
};

export type ScanReport = {
  scan_id: string;
  candidate_files_seen?: number | null;
  source_files_acquired?: number | null;
  duplicate_acquisitions?: number | null;
  duplicate_garbage_bytes?: number | null;
  historical_jpeg_entries_loaded?: number | null;
  historical_entries_already_seen?: number | null;
};

export type ScanProjection = {
  scan_id: string;
  status: string;
  started_at_ms: number | null;
  completed_at_ms: number | null;
  report?: ScanReport;
};

export type HistoricalInventory = {
  historical_inventory_id: string;
  original_path: string;
  label: string;
  group: string;
};

export type Job = {
  job_id: string;
  kind: string;
  status: 'running' | 'completed' | 'failed';
  started_at_ms: number;
  finished_at_ms: number | null;
  result_ref: string | null;
  error: string | null;
  progress: string[];
};
