<script setup lang="ts">
import { computed, ref } from "vue";
import Icon from "./Icon.vue";
import {
  connectSession,
  disconnectPane,
  selectedEntries,
  sessionById,
  startTransfer,
  state,
  toggleQueue,
  toggleSidebar,
  updateConfig,
} from "../stores/workspace";

const menuOpen = ref(false);

const remotePane = computed(() => {
  if (state.left.side === "remote") return state.left;
  if (state.right.side === "remote") return state.right;
  return null;
});
const remoteSession = computed(() => sessionById(remotePane.value?.sessionId));
const remoteKey = computed(() => (state.left.side === "remote" ? "left" : "right"));

const runningCount = computed(
  () => state.transfers.filter((task) => task.status === "running" || task.status === "queued").length,
);

const activeEntries = computed(() => selectedEntries(state.active));
const canTransferToRight = computed(
  () => state.active === "left" && activeEntries.value.length > 0,
);
const canTransferToLeft = computed(
  () => state.active === "right" && activeEntries.value.length > 0,
);
const canMove = computed(() => activeEntries.value.length > 0);

const isDark = computed(
  () =>
    state.config.ui.theme === "dark" ||
    (state.config.ui.theme === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches),
);

function copyTo(target: "left" | "right") {
  const from = target === "left" ? "right" : "left";
  void startTransfer(from, selectedEntries(from), "copy");
}

function moveToOther() {
  void startTransfer(state.active, activeEntries.value, "move");
}

async function onConnectClick() {
  if (remoteSession.value && remotePane.value?.sessionId) {
    await disconnectPane(remoteKey.value);
    return;
  }
  if (state.sessions.length === 0) {
    state.dialog.session = true;
    return;
  }
  menuOpen.value = true;
}

function chooseSession(id: string) {
  menuOpen.value = false;
  void connectSession(id, undefined, remoteKey.value);
}

async function toggleTheme() {
  const next = isDark.value ? "light" : "dark";
  await updateConfig({ ui: { theme: next } });
}
</script>

<template>
  <!-- `data-tauri-drag-region="deep"` = the whole subtree is a drag handle,
       except interactive elements (button/a/input/select/label/…), which Tauri's
       drag script skips on its own. A bare attribute only matched this element
       itself, so clicks on any child (e.g. the flex-1 spacer) never dragged. -->
  <header
    class="flex h-[52px] shrink-0 items-center gap-2 border-b border-line/70 bg-sidebar/60 pl-[80px] pr-3 backdrop-blur-xl"
    data-tauri-drag-region="deep"
  >
    <!-- brand -->
    <div class="flex items-center gap-2 pr-1">
      <div
        class="flex h-[22px] w-[22px] items-center justify-center rounded-[6px] bg-gradient-to-b from-[#4aa3ff] to-[#0a6cf0] shadow-sm"
      >
        <Icon name="folder" :size="13" :stroke="2" class="text-white" />
      </div>
      <span class="text-[13px] font-semibold tracking-[-0.01em]">file-j</span>
    </div>

    <div class="mx-0.5 h-4 w-px bg-line/70" />

    <!-- session switcher -->
    <div class="relative">
      <button
        class="flex h-7 max-w-[260px] items-center gap-1.5 rounded-md px-2 text-[11.5px] transition-colors hover:bg-ink/[0.06]"
        @click="onConnectClick"
      >
        <span
          class="h-1.5 w-1.5 shrink-0 rounded-full"
          :class="remoteSession ? 'bg-success' : 'bg-muted/50'"
        />
        <span class="truncate font-medium">
          {{ remoteSession ? remoteSession.name : "未连接远程" }}
        </span>
        <span v-if="remoteSession" class="truncate text-[10.5px] text-muted">
          {{ remoteSession.user }}@{{ remoteSession.host }}
        </span>
        <Icon name="chevron-down" :size="10" class="text-muted" />
      </button>

      <div v-if="menuOpen" class="fixed inset-0 z-40" @mousedown="menuOpen = false" />
      <div
        v-if="menuOpen"
        class="absolute left-0 top-8 z-50 w-[290px] rounded-[9px] border border-line/70 bg-elevated/95 p-1 shadow-pop backdrop-blur-xl animate-fade-in"
        data-tauri-drag-region="false"
      >
        <p class="px-2 py-1 text-[10px] font-semibold uppercase tracking-wide text-muted/80">
          远程会话
        </p>
        <button
          v-for="item in state.sessions"
          :key="item.session.id"
          class="menu-item"
          @click="chooseSession(item.session.id)"
        >
          <span class="h-1.5 w-1.5 rounded-full bg-accent/70" />
          <span class="flex-1 truncate">{{ item.session.name }}</span>
          <span class="text-2xs text-muted">{{ item.session.user }}@{{ item.session.host }}</span>
        </button>
        <p v-if="!state.sessions.length" class="px-2 py-2 text-[11px] text-muted">
          还没有保存的会话
        </p>
        <div class="menu-sep" />
        <button
          class="menu-item"
          @click="
            menuOpen = false;
            state.dialog.session = true;
          "
        >
          <Icon name="plus" :size="12" class="text-muted" />
          管理会话…
        </button>
      </div>
    </div>

    <button
      class="btn h-7 gap-1 text-[11.5px]"
      :class="remoteSession ? 'text-danger' : 'text-accent'"
      @click="onConnectClick"
    >
      <Icon :name="remoteSession ? 'plug-off' : 'plug'" :size="12.5" />
      {{ remoteSession ? "断开" : "连接" }}
    </button>

    <!-- transfer actions -->
    <div class="mx-1 h-4 w-px bg-line/70" />
    <div class="flex items-center rounded-[7px] border border-line/70 bg-panel/70 p-0.5">
      <button
        class="flex h-6 w-7 items-center justify-center rounded-[5px] transition-colors disabled:text-muted/35"
        :class="canTransferToLeft ? 'text-ink/80 hover:bg-ink/[0.06]' : 'text-muted/50'"
        :disabled="!canTransferToLeft"
        title="把右侧选中项复制到左侧 (F5)"
        @click="copyTo('left')"
      >
        <Icon name="arrow-left" :size="13" />
      </button>
      <div class="mx-0.5 h-3.5 w-px bg-line/70" />
      <button
        class="flex h-6 w-7 items-center justify-center rounded-[5px] transition-colors disabled:text-muted/35"
        :class="canTransferToRight ? 'text-ink/80 hover:bg-ink/[0.06]' : 'text-muted/50'"
        :disabled="!canTransferToRight"
        title="把左侧选中项复制到右侧 (F5)"
        @click="copyTo('right')"
      >
        <Icon name="arrow-right" :size="13" />
      </button>
    </div>
    <button
      class="btn h-7 gap-1 px-2 text-[11.5px]"
      :disabled="!canMove"
      title="移动当前选中项到对侧 (F6)"
      @click="moveToOther"
    >
      <Icon name="arrow-right" :size="12" class="rotate-[-45deg]" />
      移动
    </button>

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
