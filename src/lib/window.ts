// Window controls for the custom title bar.
//
// macOS draws its own traffic lights (AppKit, re-centred by
// center_traffic_lights in src-tauri/src/lib.rs), so nothing here applies
// there. Windows gets `decorations: false` in tauri.windows.conf.json, which
// removes the native frame including its buttons — without these the window
// could not be minimised, maximised or closed at all.
//
// Every call is a no-op outside Tauri (plain `pnpm dev` in a browser tab), so
// the components that use this can be mounted unconditionally.

import { onBeforeUnmount, onMounted, ref } from "vue";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { isTauri } from "./ipc";
import { isMac } from "./platform";

/** Shared maximized state, so the button glyph can follow the window. */
const maximized = ref(false);
let subscribers = 0;
let detach: (() => void) | null = null;

async function syncMaximized() {
  if (!isTauri()) return;
  try {
    maximized.value = await getCurrentWindow().isMaximized();
  } catch {
    maximized.value = false;
  }
}

/**
 * Subscribes to maximize/unmaximize/resize events for the lifetime of the
 * calling component. Returns nothing; the shared ref is updated in place.
 */
export function useWindowState() {
  onMounted(async () => {
    if (!isTauri() || isMac) return;
    subscribers += 1;
    await syncMaximized();
    if (subscribers === 1) {
      // onResized resolves with the unlisten function, so the listener has to
      // be awaited before it can be detached.
      const unlisten = await getCurrentWindow().onResized(() => void syncMaximized());
      detach = () => {
        unlisten();
      };
    }
  });

  onBeforeUnmount(() => {
    if (!isTauri() || isMac) return;
    subscribers -= 1;
    if (subscribers === 0 && detach) {
      detach();
      detach = null;
    }
  });

  return { maximized };
}

/**
 * A no-op mouse handler for the drag region: Tauri's own drag script already
 * turns `mousedown` into a window drag, and on Windows the platform convention
 * is that a double click on the title bar toggles maximize.
 */
export function onTitleBarDoubleClick(event: MouseEvent) {
  if (!isTauri() || isMac) return;
  event.preventDefault();
  void getCurrentWindow().toggleMaximize();
}

export const windowControls = {
  minimize() {
    if (!isTauri()) return;
    void getCurrentWindow().minimize();
  },
  toggleMaximize() {
    if (!isTauri()) return;
    void getCurrentWindow().toggleMaximize();
  },
  close() {
    if (!isTauri()) return;
    void getCurrentWindow().close();
  },
};