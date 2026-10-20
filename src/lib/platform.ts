// Platform facts the UI needs, isolated in one module.
//
// The app ships a custom title bar (see Toolbar.vue) so the window chrome is
// ours on both platforms. That means the front-end also has to know which
// platform it is on for three things:
//
//   1. keyboard hints (⌘R vs Ctrl+R) in menus and the status bar,
//   2. the wording of OS-owned actions (Finder vs 文件资源管理器),
//   3. which window buttons to draw, since macOS keeps the native traffic
//      lights while Windows has none once `decorations: false` is applied.
//
// Detection is deliberately synchronous and dependency-free: `navigator` is
// available before the first render, and waiting for Tauri's async OS plugin
// would make the title bar flash the wrong layout on boot. macOS keeps the
// previous behaviour exactly, because `isMac` is true there and every label
// below is unchanged.

export type Platform = "mac" | "windows" | "other";

function detect(): Platform {
  if (typeof navigator === "undefined") return "other";
  // `userAgentData.platform` is the modern field; the UA string is the fallback
  // for WebView2 on Windows 11, which does not implement the newer API.
  const nav = navigator as Navigator & {
    userAgentData?: { platform?: string };
  };
  const hint = `${nav.userAgentData?.platform ?? ""} ${navigator.platform ?? ""} ${navigator.userAgent}`;
  if (/mac/i.test(hint) && !/iphone|ipad|ipod/i.test(hint)) return "mac";
  if (/win/i.test(hint)) return "windows";
  if (/mac/i.test(hint)) return "mac";
  return "other";
}

export const platform: Platform = detect();

/** macOS: native traffic lights are drawn by AppKit; no custom buttons. */
export const isMac = platform === "mac";

/** Windows: the window is undecorated, so we draw our own control buttons. */
export const isWindows = platform === "windows";

/**
 * Primary modifier key as it should be *printed* in hints and menus.
 * macOS uses the Command symbol, Windows/Linux spell out Ctrl.
 */
export const modKeyLabel = isMac ? "⌘" : "Ctrl";

/** Shift label, matching the platform's own typography. */
export const shiftKeyLabel = isMac ? "⇧" : "Shift";

/**
 * Key that deletes the current selection, as printed in the context menu.
 * macOS keyboards have a dedicated Delete (⌫) that maps to Backspace; Windows
 * keyboards do not, so the forward Delete is the primary one there.
 */
export const deleteKeyLabel = isMac ? "⌫" : "Del";

/** Name of the OS file manager, used by "reveal in file manager" actions. */
export const fileManagerName = isMac ? "Finder" : isWindows ? "文件资源管理器" : "文件管理器";

/** Full label for the reveal action, e.g. "在 Finder 中显示". */
export const revealLabel = `在 ${fileManagerName} 中显示`;

/**
 * Key that returns the pane to its root.
 *
 * "/" is a real POSIX root but meaningless on Windows, where the equivalent is
 * the Home key. The backend's fs.list already resolves the pane root per
 * platform, so the front-end only needs the hint text.
 */
export const rootKeyLabel = "/";

/**
 * Fallback config directory shown in the boot-failure screen before the backend
 * has reported its real one. Mirrors what backend/cmd/file-jd resolves from
 * os.UserConfigDir: ~/Library/Application Support/file-j on macOS,
 * %APPDATA%\file-j on Windows.
 */
export const defaultConfigDir = isMac
  ? "~/Library/Application Support/file-j"
  : isWindows
    ? "%APPDATA%\\file-j"
    : "~/.config/file-j";