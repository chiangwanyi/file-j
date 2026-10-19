// Types mirror the JSON produced by the Go backend (internal/config,
// internal/fsx, internal/transfer). They are hand written on purpose: the
// contract is small and stable, and keeping it explicit makes protocol changes
// visible in code review.

export type Side = "local" | "remote";
export type PaneKey = "left" | "right";

export interface Endpoint {
  side: Side;
  sessionId?: string;
  path: string;
}

export interface FsEntry {
  name: string;
  path: string;
  isDir: boolean;
  isSymlink: boolean;
  isHidden: boolean;
  size: number;
  modTime: string;
  mode: string;
  kind: string;
  owner?: string;
  group?: string;
  linkTarget?: string;
  readOnly?: boolean;
}

export interface ListResult {
  path: string;
  parent: string;
  entries: FsEntry[];
  freeBytes?: number;
  totalBytes?: number;
}

export type AuthMethod = "password" | "key" | "agent";

export interface Session {
  id: string;
  name: string;
  protocol: string;
  host: string;
  port: number;
  user: string;
  authMethod: AuthMethod;
  keyPath?: string;
  credentialRef?: string;
  remotePath?: string;
  localPath?: string;
  color?: string;
  group?: string;
  note?: string;
  lastUsedAt?: string;
  createdAt?: string;
}

export interface SessionItem {
  session: Session;
  connected: boolean;
  hasCredential: boolean;
}

export interface UIConfig {
  theme: "system" | "light" | "dark";
  density: "comfortable" | "compact";
  showHiddenFiles: boolean;
  sidebarVisible: boolean;
  transferPanelOpen: boolean;
  confirmDelete: boolean;
  dateFormat: "iso" | "localized";
}

export interface TransferConfig {
  concurrency: number;
  conflict: ConflictPolicy;
  preserveTimes: boolean;
  preservePermissions: boolean;
  bufferSizeKB: number;
}

export interface RemoteConfig {
  connectTimeoutSeconds: number;
  keepAliveSeconds: number;
  hostKeyPolicy: "strict" | "tofu" | "insecure";
  maxPacketSize: number;
  idleDisconnectMinutes: number;
}

export interface AppConfig {
  version: number;
  ui: UIConfig;
  transfer: TransferConfig;
  remote: RemoteConfig;
}

export interface UIState {
  version: number;
  left: PaneState;
  right: PaneState;
  splitRatio: number;
  activePane: PaneKey;
}

export interface PaneState {
  kind: Side;
  path: string;
  sessionId?: string;
  sortKey: string;
  sortDesc: boolean;
}

export type ConflictPolicy = "overwrite" | "skip" | "keepboth" | "fail";

export type TaskStatus =
  | "queued"
  | "running"
  | "done"
  | "failed"
  | "canceled"
  | "skipped";

export interface TransferTask {
  id: string;
  op: "copy" | "move";
  direction: "local-local" | "upload" | "download";
  name: string;
  from: Endpoint;
  to: Endpoint;
  destPath: string;
  status: TaskStatus;
  bytes: number;
  totalBytes: number;
  files: number;
  filesDone: number;
  speed: number;
  message?: string;
  error?: string;
  createdAt: string;
  startedAt?: string | null;
  finishedAt?: string | null;
}

export interface SessionStatus {
  sessionId: string;
  connected: boolean;
  home?: string;
  since?: string;
}

export interface KnownHost {
  host: string;
  algorithm: string;
  fingerprint: string;
  addedAt: string;
}

export interface BackendInfo {
  port: number;
  token: string;
  pid: number;
  version: string;
  configDir: string;
  logFile: string;
}

export interface ResultPaths {
  configDir: string;
  config: string;
  sessions: string;
  uiState: string;
  knownHosts: string;
}

export interface Pane {
  side: Side;
  sessionId: string | null;
  path: string;
  parent: string;
  entries: FsEntry[];
  loading: boolean;
  error: string | null;
  selection: string[];
  sortKey: "name" | "size" | "modTime" | "kind";
  sortDesc: boolean;
  history: string[];
  historyIndex: number;
  anchor: number;
  freeBytes?: number;
  totalBytes?: number;
  lastLoadedAt?: number;
}

export interface Toast {
  id: number;
  kind: "info" | "success" | "error";
  title: string;
  detail?: string;
}
