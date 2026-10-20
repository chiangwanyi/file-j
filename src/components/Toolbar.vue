<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
import { isMac, isWindows } from "../lib/platform";
import { onTitleBarDoubleClick, useWindowState, windowControls } from "../lib/window";
import {
  state,
  toggleQueue,
  toggleSidebar,
  updateConfig,
} from "../stores/workspace";

const runningCount = computed(
  () => state.transfers.filter((task) => task.status === "running" || task.status === "queued").length,
);

const isDark = computed(
  () =>
    state.config.ui.theme === "dark" ||
    (state.config.ui.theme === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches),
);

async function toggleTheme() {
  const next = isDark.value ? "light" : "dark";
  await updateConfig({ ui: { theme: next } });
}

// Windows only: the window is undecorated, so it has no native buttons.
const { maximized } = useWindowState();
</script>

<template>
  <!-- `data-tauri-drag-region="deep"` = the whole subtree is a drag handle,
       except interactive elements (button/a/input/select/label/…), which Tauri's
       drag script skips on its own. A bare attribute only matched this element
       itself, so clicks on any child (e.g. the flex-1 spacer) never dragged.

       One 52px row, no brand block: `pl-[80px]` is the traffic-light gutter and
       every child stays ≤28px tall so `items-center` puts the whole bar on one
       centre line. That centre line is shared with the native window buttons —
       `center_traffic_lights` in src-tauri/src/lib.rs moves them to the midpoint
       of the same 52px, so keep `h-[52px]` and `TITLE_BAR_HEIGHT` in sync.

       The gutter is macOS-only: AppKit owns the traffic lights there. Windows
       runs undecorated and draws its own buttons at the right edge instead, so
       it drops the left padding entirely. -->
  <header
    class="flex h-[52px] shrink-0 items-center gap-2 border-b border-line/70 bg-sidebar/60 pr-3 backdrop-blur-xl"
    :class="isMac ? 'pl-[80px]' : 'pl-3'"
    data-tauri-drag-region="deep"
    @dblclick="onTitleBarDoubleClick"
  >
    <div class="flex-1" />

    <!-- right side -->
    <button class="icon-btn" :title="state.sidebarOpen ? '隐藏侧边栏' : '显示侧边栏'" @click="toggleSidebar">
      <Icon name="panel-left" :size="14" />
    </button>
    <button class="icon-btn relative" title="传输队列" @click="toggleQueue">
      <Icon name="panel-bottom" :size="14" />
      <span
        v-if="runningCount"
        class="absolute -right-0.5 -top-0.5 flex h-[13px] min-w-[13px] items-center justify-center rounded-full bg-accent px-1 text-[9px] font-semibold text-white"
      >
        {{ runningCount }}
      </span>
    </button>
    <button class="icon-btn" :title="isDark ? '切换到浅色外观' : '切换到深色外观'" @click="toggleTheme">
      <Icon :name="isDark ? 'eye' : 'eye-off'" :size="13.5" />
    </button>
    <button class="icon-btn" title="设置" @click="state.dialog.settings = true">
      <Icon name="settings" :size="14" />
    </button>

    <!-- Window buttons: Windows only. Same 52px centre line as everything
         else, 46x28 hit areas matching the Windows 11 caption buttons, and
         `-webkit-app-region: no-drag` is unnecessary because Tauri's drag
         script already skips <button>. -->
    <div v-if="isWindows" class="-mr-3 ml-1 flex h-[52px] items-stretch">
      <button
        class="flex w-[46px] items-center justify-center text-ink/70 transition-colors hover:bg-ink/[0.06]"
        title="最小化"
        @click="windowControls.minimize()"
      >
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
          <path d="M0 5h10" stroke="currentColor" stroke-width="1" />
        </svg>
      </button>
      <button
        class="flex w-[46px] items-center justify-center text-ink/70 transition-colors hover:bg-ink/[0.06]"
        :title="maximized ? '向下还原' : '最大化'"
        @click="windowControls.toggleMaximize()"
      >
        <svg v-if="!maximized" width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
          <rect x="0.5" y="0.5" width="9" height="9" fill="none" stroke="currentColor" stroke-width="1" />
        </svg>
        <svg v-else width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
          <rect x="0.5" y="2.5" width="7" height="7" fill="none" stroke="currentColor" stroke-width="1" />
          <path d="M2.5 2.5V0.5h7v7h-2" fill="none" stroke="currentColor" stroke-width="1" />
        </svg>
      </button>
      <button
        class="flex w-[46px] items-center justify-center text-ink/70 transition-colors hover:bg-danger hover:text-white"
        title="关闭"
        @click="windowControls.close()"
      >
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
          <path d="M0 0l10 10M10 0L0 10" stroke="currentColor" stroke-width="1" />
        </svg>
      </button>
    </div>
  </header>
</template>
