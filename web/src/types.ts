export type RecognitionStatus = "recognized" | "unsupported";

export type DownloadStatus =
  | "not_requested"
  | "queued"
  | "submitting"
  | "waiting"
  | "active"
  | "paused"
  | "complete"
  | "error"
  | "unknown";

export interface Session {
  username: string;
  csrfToken: string;
}

export interface Settings {
  aria2Url: string;
  aria2Secret: string;
  hasAria2Secret: boolean;
  proxyUrl: string;
  proxyUsername: string;
  proxyPassword: string;
  hasProxyPassword: boolean;
  defaultDownloadDir: string;
  pollIntervalMinutes: number;
  updatedAt: string;
}

export interface Subscription {
  id: number;
  name: string;
  url: string;
  enabled: boolean;
  downloadDir: string;
  initialSyncDone: boolean;
  initialDownloadExisting: boolean;
  lastCheckedAt: string | null;
  nextCheckAt: string | null;
  lastError: string;
  archivedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface Download {
  id: number;
  aria2Gid: string;
  status: DownloadStatus;
  totalLength: number;
  completedLength: number;
  errorMessage: string;
  retryCount: number;
  nextRetryAt: string | null;
  lastCheckedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface FeedItem {
  id: number;
  subscriptionId: number;
  subscriptionName: string;
  guid: string;
  title: string;
  description: string;
  detailUrl: string;
  enclosureUrl: string;
  enclosureLength: number;
  publishedAt: string | null;
  discoveredAt: string;
  recognitionStatus: RecognitionStatus;
  recognitionError: string;
  downloadId: number | null;
  download?: Download;
}

export interface Page<T> {
  items: T[];
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
}

export interface Dashboard {
  subscriptions: number;
  recognizedItems: number;
  activeDownloads: number;
  completed: number;
  errors: number;
  lastSyncAt: string | null;
}

export interface AriaVersion {
  version: string;
  enabledFeatures: string[];
}
