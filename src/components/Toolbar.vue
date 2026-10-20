<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
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
       of the same 52px, so keep `h-[52px]` and `TITLE_BAR_HEIGHT` in sync. -->
  <header
    class="flex h-[52px] shrink-0 items-center gap-2 border-b border-line/70 bg-sidebar/60 pl-[80px] pr-3 backdrop-blur-xl"
    data-tauri-drag-region="deep"
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
  </header>
</template>
