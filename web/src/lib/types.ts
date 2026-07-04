export type StoreSummary = {
  store_path: string;
  event_count: number;
  source_root_count: number;
  historical_inventory_count: number;
  scan_count: number;
  content_count: number;
  retained_duplicate_bytes: number;
  thumbnail_garbage_bytes: number;
  thumbnail_garbage_files: number;
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

export type AcquiredFile = {
  source_occurrence_id: string;
  stored_object_id: string;
  source_kind: string;
  source_root_id?: string;
  path: string;
  relative_path: string;
  filename: string;
  scan_id: string;
  content_ref: string;
  view_url: string;
  bytes_url: string;
  thumbnail_url: string;
};

export type PhotoDateBucket = {
  bucket_key: string;
  display_label: string;
  photo_count: number;
};

export type PhotoDateBucketResponse = {
  bucket_kind: 'year' | 'month' | 'day';
  bucket_key: string;
  buckets: PhotoDateBucket[];
};

export type DatedPhoto = {
  stored_object_id: string;
  content_ref: string;
  filename: string;
  relative_path: string;
  capture_date?: string;
  capture_time_local?: string;
  utc_offset?: string;
  precision?: string;
  view_url: string;
  bytes_url: string;
  thumbnail_url: string;
};

export type DatedPhotoResponse = {
  bucket_key: string;
  photos: DatedPhoto[];
};

export type Asset = {
  asset_id: string;
  content_ref: string;
  representative_stored_object_id: string;
  filename: string;
  quality: 'Unrated' | 'Best' | 'Good' | 'Poor';
  status: 'Triage' | 'Reviewed';
  visibility: 'Normal' | 'Private';
  labels: string[];
  capture_date?: string;
  capture_time_local?: string;
  camera?: string;
  view_url: string;
  bytes_url: string;
  thumbnail_url: string;
  source_occurrence_count: number;
  created_at_ms: number;
};

export type AssetPage = {
  assets: Asset[];
  total: number;
  limit: number;
  offset: number;
  next_offset?: number;
  prev_offset?: number;
};

export type AssetSource = {
  source_occurrence_id: string;
  stored_object_id: string;
  source_kind: string;
  source_root_id?: string;
  path: string;
  relative_path: string;
  scan_id: string;
};

export type AssetNavigationItem = {
  asset_id: string;
  filename: string;
  view_url: string;
};

export type AssetNavigation = {
  list: string;
  label: string;
  current: AssetNavigationItem;
  previous: AssetNavigationItem | null;
  next: AssetNavigationItem | null;
};

export type AssetDetail = Asset & {
  sources: AssetSource[];
};

export type LabelSummary = {
  normalized_label: string;
  display_label: string;
  asset_count: number;
  last_applied_at_ms: number;
};

export type ObjectMetadata = {
  stored_object_id: string;
  content_ref: string;
  extractor_name: string;
  extractor_version: number;
  metadata_event_id: string;
  fields: Record<string, Record<string, string>>;
};

export type ObjectNavigationItem = {
  stored_object_id: string;
  filename: string;
  view_url: string;
};

export type ObjectNavigation = {
  list: string;
  label: string;
  current: ObjectNavigationItem;
  previous: ObjectNavigationItem | null;
  next: ObjectNavigationItem | null;
};

export type MetadataSummary = {
  content_count: number;
  extracted_count: number;
  failed_count: number;
  missing_count: number;
  extractor_name: string;
  extractor_version: number;
};

export type MetadataPhoto = {
  stored_object_id: string;
  content_ref: string;
  filename: string;
  relative_path: string;
  status: 'failed' | 'missing';
  error_message?: string;
  width?: number;
  height?: number;
  pixel_count?: number;
  view_url: string;
  thumbnail_url: string;
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
  status: 'running' | 'completed' | 'failed' | 'interrupted';
  started_at_ms: number;
  finished_at_ms: number | null;
  result_ref: string | null;
  error: string | null;
  progress: string[];
  progress_current: number | null;
  progress_total: number | null;
};

export type ServerEvent = {
  type: 'job_snapshot' | 'job_started' | 'job_progress' | 'job_finished' | 'projection_changed';
  recorded_at_ms: number;
  job?: Job;
  jobs?: Job[];
};
