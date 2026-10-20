// Single reactive workspace store.
//
// Deliberately not Pinia: the app has exactly one window and one user, so a
// module-scoped reactive object plus exported actions is less ceremony and keeps
// every transition readable in one file.

import { computed, reactive } from "vue";
import { errorCode, errorText, rpc } from "../lib/ipc";
import type {
  AppConfig,
  FsEntry,
  ListResult,
  Pane,
  PaneKey,
  ResultPaths,
  Session,
  SessionItem,
  Side,
  Toast,
  TransferTask,
  UIState,
} from "../lib/types";
import { isInside, joinPath } from "../lib/format";
import { fileManagerName } from "../lib/platform";

// ------------------------------------------------------------------ defaults

function defaultConfig(): AppConfig {
  return {
    version: 1,
    ui: {
      theme: "system",
      density: "comfortable",
      showHiddenFiles: false,
      sidebarVisible: true,
      transferPanelOpen: true,
      confirmDelete: true,
      dateFormat: "localized",
    },
    transfer: {
      concurrency: 3,
      conflict: "overwrite",
      preserveTimes: true,
      preservePermissions: true,
      bufferSizeKB: 128,
    },
    remote: {
      connectTimeoutSeconds: 15,
      keepAliveSeconds: 30,
      hostKeyPolicy: "tofu",
      maxPacketSize: 32768,
      idleDisconnectMinutes: 15,
    },
  };
}

function emptyPane(side: Side): Pane {
  return {
    side,
    sessionId: null,
    // The starting path is replaced by ui.state (which persists the last
    // location) or fs.home right after boot; "/" is only a placeholder and is
    // always a valid POSIX root, including for a Windows SFTP session.
    path: side === "local" ? "/" : ".",
    parent: "/",
    entries: [],
    loading: false,
    error: null,
    selection: [],
    sortKey: "name",
    sortDesc: false,
    history: [],
    historyIndex: -1,
    anchor: -1,
  };
}

export interface PasswordDialogState {
  sessionId: string;
  title: string;
  subtitle: string;
  remember: boolean;
  error: string;
  busy: boolean;
}

export interface PromptDialogState {
  title: string;
  label: string;
  value: string;
  placeholder: string;
  confirmText: string;
}

export interface ConfirmDialogState {
  title: string;
  message: string;
  detail: string;
  confirmText: string;
  danger: boolean;
}

interface DialogState {
  session: boolean;
  settings: boolean;
  password: PasswordDialogState | null;
  prompt: PromptDialogState | null;
  confirm: ConfirmDialogState | null;
}

// --------------------------------------------------------------------- state

export const state = reactive({
  ready: false,
  bootError: "",
  connected: false,
  connectingId: "",
  backendVersion: "",
  configDir: "",
  paths: null as ResultPaths | null,
  config: defaultConfig(),
  sessions: [] as SessionItem[],
  places: [] as { name: string; path: string }[],
  left: emptyPane("local"),
  right: emptyPane("remote"),
  active: "left" as PaneKey,
  transfers: [] as TransferTask[],
  queueOpen: true,
  sidebarOpen: true,
  splitRatio: 0.5,
  dialog: {
    session: false,
    settings: false,
    password: null,
    prompt: null,
    confirm: null,
  } as DialogState,
  toasts: [] as Toast[],
});

export const activePane = computed(() => state[state.active]);
export const otherPane = computed(() => state[state.active === "left" ? "right" : "left"]);

export function pane(key: PaneKey): Pane {
  return key === "left" ? state.left : state.right;
}

export function otherKey(key: PaneKey): PaneKey {
  return key === "left" ? "right" : "left";
}

export function selectedEntries(key: PaneKey): FsEntry[] {
  const p = pane(key);
  if (!p.selection.length) return [];
  const set = new Set(p.selection);
  return p.entries.filter((entry) => set.has(entry.path));
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

/**
 * Returns the pane's entries in display order (directories first, then the
 * chosen column). Both the table and keyboard navigation use this so the arrow
 * keys always move through exactly what the user sees.
 */
export function sortedEntries(key: PaneKey): FsEntry[] {
  const target = pane(key);
  const direction = target.sortDesc ? -1 : 1;
  const list = [...target.entries];
  list.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    let compare = 0;
    switch (target.sortKey) {
      case "size":
        compare = a.size - b.size;
        break;
      case "modTime":
        compare = new Date(a.modTime).getTime() - new Date(b.modTime).getTime();
        break;
      case "kind":
        compare = collator.compare(a.kind, b.kind);
        break;
      default:
        compare = collator.compare(a.name, b.name);
    }
    if (compare === 0) compare = collator.compare(a.name, b.name);
    return compare * direction;
  });
  return list;
}

export function sessionById(id: string | null | undefined): Session | null {
  if (!id) return null;
  return state.sessions.find((item) => item.session.id === id)?.session ?? null;
}

// ------------------------------------------------------------------- toasts

let toastSeq = 0;

export function toast(kind: Toast["kind"], title: string, detail?: string): void {
  const id = ++toastSeq;
  state.toasts.push({ id, kind, title, detail });
  window.setTimeout(() => dismissToast(id), kind === "error" ? 8000 : 4200);
}

export function dismissToast(id: number): void {
  const index = state.toasts.findIndex((item) => item.id === id);
  if (index >= 0) state.toasts.splice(index, 1);
}

// ------------------------------------------------------------------ dialogs

let promptSubmit: ((value: string) => Promise<void>) | null = null;
let confirmRun: (() => Promise<void>) | null = null;

export function openPrompt(options: {
  title: string;
  label: string;
  value?: string;
  placeholder?: string;
  confirmText?: string;
  onSubmit: (value: string) => Promise<void>;
}): void {
  promptSubmit = options.onSubmit;
  state.dialog.prompt = {
    title: options.title,
    label: options.label,
    value: options.value ?? "",
    placeholder: options.placeholder ?? "",
    confirmText: options.confirmText ?? "确定",
  };
}

export async function submitPrompt(value: string): Promise<void> {
  const submit = promptSubmit;
  if (!submit) return;
  try {
    await submit(value);
    closePrompt();
  } catch (error) {
    toast("error", "操作失败", errorText(error));
  }
}

export function closePrompt(): void {
  state.dialog.prompt = null;
  promptSubmit = null;
}

export function openConfirm(options: {
  title: string;
  message: string;
  detail?: string;
  confirmText?: string;
  danger?: boolean;
  onConfirm: () => Promise<void>;
}): void {
  confirmRun = options.onConfirm;
  state.dialog.confirm = {
    title: options.title,
    message: options.message,
    detail: options.detail ?? "",
    confirmText: options.confirmText ?? "确定",
    danger: options.danger ?? false,
  };
}

export async function runConfirm(): Promise<void> {
  const run = confirmRun;
  if (!run) return;
  closeConfirm();
  try {
    await run();
  } catch (error) {
    toast("error", "操作失败", errorText(error));
  }
}

export function closeConfirm(): void {
  state.dialog.confirm = null;
  confirmRun = null;
}

// -------------------------------------------------------------- theme & prefs

let mediaQuery: MediaQueryList | null = null;

export function applyTheme(): void {
  const mode = state.config.ui.theme;
  if (!mediaQuery) {
    mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    mediaQuery.addEventListener("change", () => {
      if (state.config.ui.theme === "system") applyTheme();
    });
  }
  const dark = mode === "dark" || (mode === "system" && mediaQuery.matches);
  document.documentElement.classList.toggle("dark", dark);
}

export async function updateConfig(patch: {
  ui?: Partial<AppConfig["ui"]>;
  transfer?: Partial<AppConfig["transfer"]>;
  remote?: Partial<AppConfig["remote"]>;
}): Promise<void> {
  const next: AppConfig = {
    ...state.config,
    ui: { ...state.config.ui, ...(patch.ui ?? {}) },
    transfer: { ...state.config.transfer, ...(patch.transfer ?? {}) },
    remote: { ...state.config.remote, ...(patch.remote ?? {}) },
  };
  state.config = await rpc.request<AppConfig>("config.set", next);
  applyTheme();
  state.sidebarOpen = state.config.ui.sidebarVisible;
  state.queueOpen = state.config.ui.transferPanelOpen;
}

// ------------------------------------------------------------- pane loading

function applyPaneState(key: PaneKey, saved: UIState["left"]): void {
  const target = pane(key);
  target.side = saved.kind;
  target.path = saved.path;
  target.sessionId = saved.sessionId ?? null;
  target.sortKey = (saved.sortKey as Pane["sortKey"]) || "name";
  target.sortDesc = Boolean(saved.sortDesc);
  target.history = [];
  target.historyIndex = -1;
  target.anchor = -1;
}

export async function loadPane(
  key: PaneKey,
  path: string,
  options: { push?: boolean; silent?: boolean } = {},
): Promise<void> {
  const target = pane(key);
  if (target.side === "remote" && !target.sessionId) {
    target.entries = [];
    target.loading = false;
    return;
  }
  target.loading = true;
  if (!options.silent) target.error = null;

  try {
    const result = await rpc.request<ListResult>("fs.list", {
      side: target.side,
      sessionId: target.sessionId ?? "",
      path,
      showHidden: state.config.ui.showHiddenFiles,
    });
    target.path = result.path || path;
    target.parent = result.parent;
    target.entries = result.entries;
    target.freeBytes = result.freeBytes;
    target.totalBytes = result.totalBytes;
    target.error = null;
    target.loading = false;
    target.selection = [];
    target.anchor = -1;
    if (options.push !== false) {
      target.history = target.history.slice(0, target.historyIndex + 1);
      target.history.push(target.path);
      target.historyIndex = target.history.length - 1;
    }
    target.lastLoadedAt = Date.now();
    scheduleUIState();
  } catch (error) {
    target.loading = false;
    target.error = errorText(error);
    if (!options.silent) toast("error", "无法打开文件夹", errorText(error));
  }
}

export function navigate(key: PaneKey, path: string): Promise<void> {
  return loadPane(key, path || "/", { push: true });
}

export function goUp(key: PaneKey): Promise<void> {
  const target = pane(key);
  return loadPane(key, target.parent || "/", { push: true });
}

/**
 * Jumps to the top of the pane's tree.
 *
 * The root is asked from the backend instead of being hard-coded: "/" is the
 * answer for macOS local disk and for every SFTP session, while a Windows local
 * pane returns its volume root ("C:\"). Remote panes never need the round trip
 * and are known to be POSIX.
 */
export async function goRoot(key: PaneKey): Promise<void> {
  const target = pane(key);
  if (target.side === "remote") {
    // SFTP paths are POSIX on every server, so there is nothing to ask for.
    if (!target.sessionId) return;
    await loadPane(key, "/", { push: true });
    return;
  }
  try {
    const result = await rpc.request<{ path: string }>("fs.root", {
      side: "local",
      sessionId: "",
      path: target.path,
    });
    await loadPane(key, result.path, { push: true });
  } catch {
    // Older backend without the RPC: "/" is still correct on macOS.
    await loadPane(key, "/", { push: true });
  }
}

export function goBack(key: PaneKey): Promise<void> {
  const target = pane(key);
  if (target.historyIndex <= 0) return Promise.resolve();
  target.historyIndex -= 1;
  return loadPane(key, target.history[target.historyIndex], { push: false });
}

export function goForward(key: PaneKey): Promise<void> {
  const target = pane(key);
  if (target.historyIndex >= target.history.length - 1) return Promise.resolve();
  target.historyIndex += 1;
  return loadPane(key, target.history[target.historyIndex], { push: false });
}

export function canGoBack(key: PaneKey): boolean {
  return pane(key).historyIndex > 0;
}

export function canGoForward(key: PaneKey): boolean {
  const target = pane(key);
  return target.historyIndex < target.history.length - 1;
}

export function refreshPane(key: PaneKey): Promise<void> {
  return loadPane(key, pane(key).path, { push: false });
}

export function refreshAll(): Promise<void> {
  return Promise.all([refreshPane("left"), refreshPane("right")]).then(() => undefined);
}

export function setSort(key: PaneKey, sortKey: Pane["sortKey"]): void {
  const target = pane(key);
  if (target.sortKey === sortKey) {
    target.sortDesc = !target.sortDesc;
  } else {
    target.sortKey = sortKey;
    target.sortDesc = false;
  }
  scheduleUIState();
}

// ---------------------------------------------------------------- selection

export function setActive(key: PaneKey): void {
  if (state.active === key) return;
  state.active = key;
  scheduleUIState();
}

export function clearSelection(key: PaneKey): void {
  pane(key).selection = [];
  pane(key).anchor = -1;
}

export function selectRow(key: PaneKey, entry: FsEntry, event: MouseEvent | KeyboardEvent): void {
  const target = pane(key);
  const index = target.entries.findIndex((item) => item.path === entry.path);
  if (index < 0) return;

  if (event.metaKey || event.ctrlKey) {
    const set = new Set(target.selection);
    if (set.has(entry.path)) set.delete(entry.path);
    else set.add(entry.path);
    target.selection = [...set];
    target.anchor = index;
    return;
  }

  if (event.shiftKey && target.anchor >= 0) {
    const [from, to] = [target.anchor, index].sort((a, b) => a - b);
    target.selection = target.entries.slice(from, to + 1).map((item) => item.path);
    return;
  }

  target.selection = [entry.path];
  target.anchor = index;
}

export function selectAll(key: PaneKey): void {
  const target = pane(key);
  target.selection = target.entries.map((entry) => entry.path);
}

export function selectByPath(key: PaneKey, path: string): void {
  const target = pane(key);
  const index = target.entries.findIndex((entry) => entry.path === path);
  if (index >= 0) {
    target.selection = [path];
    target.anchor = index;
  }
}

// ------------------------------------------------------------------ sessions

export async function loadSessions(): Promise<void> {
  state.sessions = await rpc.request<SessionItem[]>("session.list");
}

function paneKeyForRemote(): PaneKey {
  if (state.right.side === "remote") return "right";
  if (state.left.side === "remote") return "left";
  return "right";
}

export function openPasswordDialog(sessionId: string, error = ""): void {
  const session = sessionById(sessionId);
  if (!session) return;
  state.dialog.password = {
    sessionId,
    title: "需要密码",
    subtitle: `${session.user}@${session.host}:${session.port}`,
    remember: Boolean(session.credentialRef),
    error,
    busy: false,
  };
}

export function closePasswordDialog(): void {
  state.dialog.password = null;
}

export async function submitPassword(password: string): Promise<void> {
  const dialog = state.dialog.password;
  if (!dialog) return;
  dialog.busy = true;
  dialog.error = "";
  try {
    if (dialog.remember) {
      const session = sessionById(dialog.sessionId);
      if (session) {
        const result = await rpc.request<{ session: Session; warning?: string }>("session.save", {
          session,
          password,
        });
        if (result.warning) toast("info", "密码未持久化", result.warning);
        await loadSessions();
      }
    }
    const ok = await connectSession(dialog.sessionId, password);
    if (ok) state.dialog.password = null;
    else {
      dialog.busy = false;
      dialog.error = "认证失败，请检查密码";
    }
  } catch (error) {
    dialog.busy = false;
    dialog.error = errorText(error);
  }
}

export async function connectSession(
  sessionId: string,
  password?: string,
  targetKey?: PaneKey,
  silent = false,
): Promise<boolean> {
  const item = state.sessions.find((entry) => entry.session.id === sessionId);
  if (!item) {
    toast("error", "会话不存在");
    return false;
  }
  const key = targetKey ?? paneKeyForRemote();
  const target = pane(key);
  state.connectingId = sessionId;
  try {
    const result = await rpc.request<{ home?: string }>("session.connect", {
      id: sessionId,
      password: password ?? "",
    });
    item.connected = true;
    target.side = "remote";
    target.sessionId = sessionId;
    target.history = [];
    target.historyIndex = -1;
    target.entries = [];
    target.error = null;
    const preferred =
      item.session.remotePath && item.session.remotePath !== "."
        ? item.session.remotePath
        : result.home ?? ".";
    await loadPane(key, preferred, { push: true });
    if (!silent) toast("success", `已连接 ${item.session.name}`);
    scheduleUIState();
    return true;
  } catch (error) {
    const code = errorCode(error);
    const message = errorText(error);
    if (code === "AUTH_FAILED" || message.includes("需要密码")) {
      if (state.dialog.password) {
        state.dialog.password.error = message;
      } else {
        openPasswordDialog(sessionId, message);
      }
    } else if (code === "HOST_KEY") {
      openConfirm({
        title: "主机密钥异常",
        message: "无法验证远程主机的身份。",
        detail: message,
        confirmText: "仍然关闭",
        danger: true,
        onConfirm: async () => undefined,
      });
    } else {
      toast("error", "连接失败", message);
    }
    return false;
  } finally {
    state.connectingId = "";
  }
}

export async function disconnectPane(key: PaneKey): Promise<void> {
  const target = pane(key);
  if (target.side !== "remote" || !target.sessionId) return;
  const id = target.sessionId;
  try {
    await rpc.request("session.disconnect", { id });
  } catch {
    /* the connection may already be gone */
  }
  const item = state.sessions.find((entry) => entry.session.id === id);
  if (item) item.connected = false;
  target.entries = [];
  target.selection = [];
  target.sessionId = null;
  target.path = ".";
  target.parent = "/";
  toast("info", "已断开远程连接");
  scheduleUIState();
}

export async function saveSession(
  session: Session,
  password = "",
  clearPassword = false,
): Promise<Session> {
  const result = await rpc.request<{ session: Session; warning?: string }>("session.save", {
    session,
    password,
    clearPassword,
  });
  await loadSessions();
  if (result.warning) toast("info", "密码未持久化", result.warning);
  return result.session;
}

export async function deleteSession(id: string): Promise<void> {
  await rpc.request("session.delete", { id });
  await loadSessions();
  for (const key of ["left", "right"] as PaneKey[]) {
    const target = pane(key);
    if (target.side === "remote" && target.sessionId === id) {
      target.sessionId = null;
      target.entries = [];
      target.path = ".";
    }
  }
  scheduleUIState();
}

export async function testSession(
  session: Session,
  password = "",
): Promise<{ home: string; serverVersion: string }> {
  return rpc.request("session.test", { session, password });
}

// ------------------------------------------------------------------ transfer

const paneRefreshTimers = new Map<PaneKey, number>();

/** Coalesces "task finished" refreshes when a batch of files lands at once. */
function schedulePaneRefresh(key: PaneKey): void {
  const existing = paneRefreshTimers.get(key);
  if (existing) window.clearTimeout(existing);
  const timer = window.setTimeout(() => {
    paneRefreshTimers.delete(key);
    void loadPane(key, pane(key).path, { push: false, silent: true });
  }, 350);
  paneRefreshTimers.set(key, timer);
}

function upsertTask(task: TransferTask): void {
  const index = state.transfers.findIndex((item) => item.id === task.id);
  const previous = index >= 0 ? state.transfers[index] : null;
  if (index >= 0) state.transfers[index] = task;
  else state.transfers.unshift(task);

  // A finished transfer invalidates the folder listing it wrote into (and, for
  // a move, the folder it removed the source from). Refreshing here means the
  // panes stay truthful without the user pressing Cmd+R.
  const finished = task.status === "done" && previous?.status !== "done";
  if (!finished) return;
  for (const key of ["left", "right"] as PaneKey[]) {
    const target = pane(key);
    const sameSession = (task.to.sessionId ?? "") === (target.sessionId ?? "");
    if (target.side === task.to.side && sameSession && isInside(task.to.path, target.path)) {
      schedulePaneRefresh(key);
    }
    if (
      task.op === "move" &&
      target.side === task.from.side &&
      (task.from.sessionId ?? "") === (target.sessionId ?? "")
    ) {
      schedulePaneRefresh(key);
    }
  }
}

export async function startTransfer(
  fromKey: PaneKey,
  entries: FsEntry[],
  op: "copy" | "move",
  toPath?: string,
): Promise<void> {
  const source = pane(fromKey);
  const target = pane(otherKey(fromKey));
  const destinationPath = toPath ?? target.path;
  if (!entries.length) {
    toast("info", "请先选择文件");
    return;
  }
  if (!state.connected) {
    toast("error", "后端未连接");
    return;
  }
  if (source.side === "remote" && !source.sessionId) {
    toast("error", "请先连接远程会话");
    return;
  }
  if (target.side === "remote" && !target.sessionId) {
    toast("error", "对侧窗口尚未连接远程会话");
    return;
  }
  if (source.side === "remote" && target.side === "remote" && source.sessionId !== target.sessionId) {
    toast("error", "暂不支持在两个远程会话之间直接传输");
    return;
  }
  for (const entry of entries) {
    if (entry.isDir && source.side === target.side && isInside(entry.path, destinationPath)) {
      toast("error", "无法将文件夹复制到其自身内部", entry.name);
      return;
    }
  }

  const items = entries.map((entry) => ({
    from: { side: source.side, sessionId: source.sessionId ?? "", path: entry.path },
    to: { side: target.side, sessionId: target.sessionId ?? "", path: destinationPath },
  }));

  try {
    const tasks = await rpc.request<TransferTask[]>("transfer.create", {
      op,
      conflict: state.config.transfer.conflict,
      items,
    });
    for (const task of tasks) upsertTask(task);
    if (!state.queueOpen) toggleQueue();
    else state.queueOpen = true;
    const verb = op === "move" ? "移动" : "复制";
    const direction =
      source.side === "local" && target.side === "remote"
        ? "上传"
        : source.side === "remote" && target.side === "local"
          ? "下载"
          : verb;
    toast("info", `已加入队列：${direction} ${entries.length} 项`);
  } catch (error) {
    toast("error", "无法开始传输", errorText(error));
  }
}

export function transferSelection(fromKey: PaneKey, op: "copy" | "move"): Promise<void> {
  return startTransfer(fromKey, selectedEntries(fromKey), op);
}

export async function cancelTask(id: string): Promise<void> {
  try {
    await rpc.request("transfer.cancel", { id });
  } catch (error) {
    toast("error", "无法取消任务", errorText(error));
  }
}

export async function retryTask(id: string): Promise<void> {
  try {
    await rpc.request("transfer.retry", { id });
  } catch (error) {
    toast("error", "无法重试任务", errorText(error));
  }
}

export async function removeTask(id: string): Promise<void> {
  await rpc.request("transfer.remove", { id });
  const index = state.transfers.findIndex((item) => item.id === id);
  if (index >= 0) state.transfers.splice(index, 1);
}

export async function clearFinishedTasks(): Promise<void> {
  await rpc.request("transfer.clear");
  state.transfers = state.transfers.filter(
    (task) => task.status === "running" || task.status === "queued",
  );
}

export function toggleQueue(): void {
  state.queueOpen = !state.queueOpen;
  state.config.ui.transferPanelOpen = state.queueOpen;
  void updateConfig({ ui: { transferPanelOpen: state.queueOpen } }).catch(() => undefined);
}

export function toggleSidebar(): void {
  state.sidebarOpen = !state.sidebarOpen;
  state.config.ui.sidebarVisible = state.sidebarOpen;
  void updateConfig({ ui: { sidebarVisible: state.sidebarOpen } }).catch(() => undefined);
}

// ------------------------------------------------------------- file actions

export async function openEntry(key: PaneKey, entry: FsEntry): Promise<void> {
  if (entry.isDir) {
    await navigate(key, entry.path);
    return;
  }
  if (entry.isSymlink && entry.kind === "symlink") {
    // Let the backend follow the link; Stat reports the target type.
    await navigate(key, entry.path).catch(() => undefined);
    return;
  }
  const target = pane(key);
  if (target.side === "local") {
    try {
      await rpc.request("fs.open", { path: entry.path });
    } catch (error) {
      toast("error", "无法打开文件", errorText(error));
    }
  } else {
    await startTransfer(key, [entry], "copy");
  }
}

export async function revealEntry(entry: FsEntry): Promise<void> {
  try {
    await rpc.request("fs.reveal", { path: entry.path });
  } catch (error) {
    toast("error", `无法在 ${fileManagerName} 中显示`, errorText(error));
  }
}

export function createFolder(key: PaneKey): void {
  const target = pane(key);
  if (target.side === "remote" && !target.sessionId) {
    toast("error", "请先连接远程会话");
    return;
  }
  openPrompt({
    title: "新建文件夹",
    label: "名称",
    value: "新建文件夹",
    confirmText: "创建",
    onSubmit: async (value) => {
      const name = value.trim();
      if (!name) return;
      await rpc.request("fs.mkdir", {
        side: target.side,
        sessionId: target.sessionId ?? "",
        path: target.path,
        name,
      });
      await refreshPane(key);
      selectByPath(key, joinPath(target.path, name));
    },
  });
}

export function renameEntry(key: PaneKey, entry: FsEntry): void {
  openPrompt({
    title: "重命名",
    label: "新名称",
    value: entry.name,
    confirmText: "重命名",
    onSubmit: async (value) => {
      const name = value.trim();
      if (!name || name === entry.name) return;
      await rpc.request("fs.rename", {
        side: pane(key).side,
        sessionId: pane(key).sessionId ?? "",
        path: entry.path,
        newName: name,
      });
      await refreshPane(key);
    },
  });
}

export async function deleteEntries(key: PaneKey, entries: FsEntry[]): Promise<void> {
  if (!entries.length) return;
  const target = pane(key);
  const run = async () => {
    const result = await rpc.request<{
      trashed: number;
      deleted: number;
      failures: { path: string; message: string }[];
    }>("fs.delete", {
      side: target.side,
      sessionId: target.sessionId ?? "",
      paths: entries.map((entry) => entry.path),
      permanent: false,
    });
    await refreshPane(key);
    if (result.failures.length) {
      toast("error", `${result.failures.length} 项删除失败`, result.failures[0]?.message);
    } else if (result.trashed) {
      toast("success", `已将 ${result.trashed} 项移到废纸篓`);
    } else {
      toast("success", `已删除 ${result.deleted} 项`);
    }
  };

  if (!state.config.ui.confirmDelete) {
    await run().catch((error) => toast("error", "删除失败", errorText(error)));
    return;
  }
  const remote = target.side === "remote";
  openConfirm({
    title: "确认删除",
    message: `确定要删除选中的 ${entries.length} 项吗？`,
    detail: `${entries
      .slice(0, 5)
      .map((entry) => entry.name)
      .join("、")}${entries.length > 5 ? " 等" : ""}${
      remote ? "\n\n远程文件将被永久删除，无法恢复。" : "\n\n本地项目会被移到废纸篓。"
    }`,
    confirmText: "删除",
    danger: true,
    onConfirm: run,
  });
}

export function deleteSelection(key: PaneKey): Promise<void> {
  return deleteEntries(key, selectedEntries(key));
}

// ------------------------------------------------------------------ ui state

let uiTimer: number | undefined;

function scheduleUIState(): void {
  if (uiTimer) window.clearTimeout(uiTimer);
  uiTimer = window.setTimeout(() => {
    const snapshot: UIState = {
      version: 1,
      left: {
        kind: state.left.side,
        path: state.left.path,
        sessionId: state.left.sessionId ?? undefined,
        sortKey: state.left.sortKey,
        sortDesc: state.left.sortDesc,
      },
      right: {
        kind: state.right.side,
        path: state.right.path,
        sessionId: state.right.sessionId ?? undefined,
        sortKey: state.right.sortKey,
        sortDesc: state.right.sortDesc,
      },
      splitRatio: state.splitRatio,
      activePane: state.active,
    };
    void rpc.request("ui.state.set", snapshot).catch(() => undefined);
  }, 500);
}

export function setSplitRatio(ratio: number): void {
  state.splitRatio = Math.min(0.75, Math.max(0.25, ratio));
  scheduleUIState();
}

// --------------------------------------------------------------------- boot

export async function boot(): Promise<void> {
  rpc.onStateChange = (connection) => {
    state.connected = connection === "open";
  };
  rpc.on("transfer.task", (payload) => {
    const task = (payload as { task?: TransferTask }).task;
    if (task) upsertTask(task);
  });

  try {
    await rpc.connect();
    const [info, config, sessions, uiState, tasks, places] = await Promise.all([
      rpc.request<{ version: string; configDir: string }>("app.info"),
      rpc.request<{ config: AppConfig; paths: ResultPaths }>("config.get"),
      rpc.request<SessionItem[]>("session.list"),
      rpc.request<UIState>("ui.state.get"),
      rpc.request<TransferTask[]>("transfer.list"),
      rpc.request<{ name: string; path: string }[]>("fs.places"),
    ]);

    state.backendVersion = info.version;
    state.configDir = info.configDir;
    state.config = config.config;
    state.paths = config.paths;
    state.sessions = sessions;
    state.transfers = tasks;
    state.places = places;
    state.splitRatio = uiState.splitRatio;
    state.sidebarOpen = config.config.ui.sidebarVisible;
    state.queueOpen = config.config.ui.transferPanelOpen;

    applyPaneState("left", uiState.left);
    applyPaneState("right", uiState.right);
    state.active = uiState.activePane;
    applyTheme();

    await Promise.all([
      loadPane("left", state.left.path, { push: false, silent: true }),
      loadPane("right", state.right.path, { push: false, silent: true }),
    ]);

    if (state.left.side === "remote" && state.left.sessionId) {
      state.active = "left";
    }

    const remembered = [state.left, state.right].find(
      (p) => p.side === "remote" && p.sessionId,
    );
    if (remembered?.sessionId) {
      const item = state.sessions.find((entry) => entry.session.id === remembered.sessionId);
      if (item?.hasCredential) {
        const key: PaneKey = state.left === remembered ? "left" : "right";
        await connectSession(remembered.sessionId, undefined, key, true).catch(() => undefined);
      }
    }
  } catch (error) {
    state.bootError = errorText(error);
  } finally {
    state.ready = true;
  }
}
