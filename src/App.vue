<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import Toolbar from "./components/Toolbar.vue";
import Sidebar from "./components/Sidebar.vue";
import Pane from "./components/Pane.vue";
import TransferPanel from "./components/TransferPanel.vue";
import StatusBar from "./components/StatusBar.vue";
import SessionDialog from "./components/SessionDialog.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import SimpleDialogs from "./components/SimpleDialogs.vue";
import ToastStack from "./components/ToastStack.vue";
import Icon from "./components/Icon.vue";
import { defaultConfigDir } from "./lib/platform";
import {
  boot,
  closeSessionDialog,
  createFolder,
  deleteSelection,
  goBack,
  goForward,
  goRoot,
  goUp,
  openEntry,
  pane,
  refreshAll,
  refreshPane,
  selectAll,
  selectByPath,
  selectedEntries,
  setSplitRatio,
  sortedEntries,
  startTransfer,
  state,
  transferSelection,
} from "./stores/workspace";

const splitContainer = ref<HTMLElement | null>(null);
const dragging = ref(false);

const leftWidth = computed(() => `${state.splitRatio * 100}%`);
const leftActive = computed(() => selectedEntries("left").length > 0);
const rightActive = computed(() => selectedEntries("right").length > 0);

function startSplitDrag(event: MouseEvent) {
  if (!splitContainer.value) return;
  event.preventDefault();
  dragging.value = true;
  const rect = splitContainer.value.getBoundingClientRect();
  const onMove = (moveEvent: MouseEvent) => {
    const ratio = (moveEvent.clientX - rect.left) / rect.width;
    state.splitRatio = Math.min(0.75, Math.max(0.25, ratio));
  };
  const onUp = () => {
    dragging.value = false;
    window.removeEventListener("mousemove", onMove);
    window.removeEventListener("mouseup", onUp);
    setSplitRatio(state.splitRatio);
  };
  window.addEventListener("mousemove", onMove);
  window.addEventListener("mouseup", onUp);
}

function isTextInput(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null;
  if (!element) return false;
  const tag = element.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || element.isContentEditable;
}

function moveSelection(delta: number) {
  const key = state.active;
  const list = sortedEntries(key);
  if (!list.length) return;
  const current = pane(key).selection;
  let index = current.length ? list.findIndex((entry) => entry.path === current[current.length - 1]) : -1;
  index = Math.min(list.length - 1, Math.max(0, index + delta));
  const target = list[index];
  selectByPath(key, target.path);
  void nextTick(() => {
    const nodes = document.querySelectorAll<HTMLElement>("[data-path]");
    for (const node of nodes) {
      if (node.dataset.path === target.path) {
        node.scrollIntoView({ block: "nearest" });
        break;
      }
    }
  });
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    if (state.dialog.session || state.dialog.settings) {
      closeSessionDialog();
      state.dialog.settings = false;
      return;
    }
  }
  if (isTextInput(event.target)) return;
  if (
    state.dialog.session ||
    state.dialog.settings ||
    state.dialog.prompt ||
    state.dialog.confirm ||
    state.dialog.password
  ) {
    return;
  }

  const key = state.active;
  const meta = event.metaKey || event.ctrlKey;

  if (event.key === "F5") {
    event.preventDefault();
    void transferSelection(key, "copy");
  } else if (event.key === "F6") {
    event.preventDefault();
    void transferSelection(key, "move");
  } else if (event.key === "Enter") {
    const entries = selectedEntries(key);
    if (entries.length === 1) {
      event.preventDefault();
      void openEntry(key, entries[0]);
    }
  } else if (event.key === "Backspace") {
    event.preventDefault();
    if (meta) void deleteSelection(key);
    else void goUp(key);
  } else if (event.key === "Delete") {
    event.preventDefault();
    void deleteSelection(key);
  } else if (event.key === "ArrowDown") {
    event.preventDefault();
    moveSelection(1);
  } else if (event.key === "ArrowUp") {
    event.preventDefault();
    moveSelection(-1);
  } else if (event.key === "ArrowRight" && meta) {
    event.preventDefault();
    void goForward(key);
  } else if (event.key === "ArrowLeft" && meta) {
    event.preventDefault();
    void goBack(key);
  } else if (meta && event.key.toLowerCase() === "r") {
    event.preventDefault();
    void refreshPane(key);
  } else if (meta && event.key.toLowerCase() === "a") {
    event.preventDefault();
    selectAll(key);
  } else if (meta && event.shiftKey && event.key.toLowerCase() === "n") {
    event.preventDefault();
    createFolder(key);
  } else if (meta && event.shiftKey && event.key.toLowerCase() === "r") {
    event.preventDefault();
    void refreshAll();
  } else if (event.key === "Home") {
    event.preventDefault();
    void goRoot(key);
  }
}

onMounted(() => {
  void boot();
  window.addEventListener("keydown", onKeydown);
});

function reloadApp() {
  window.location.reload();
}

onBeforeUnmount(() => window.removeEventListener("keydown", onKeydown));
</script>

<template>
  <!-- splash -->
  <div v-if="!state.ready" class="flex h-full flex-col items-center justify-center gap-4 bg-bg">
    <div
      class="flex h-14 w-14 items-center justify-center rounded-[14px] bg-gradient-to-b from-[#4aa3ff] to-[#0a6cf0] shadow-lg"
    >
      <Icon name="folder" :size="28" :stroke="1.7" class="text-white" />
    </div>
    <div class="text-center">
      <p class="text-[13px] font-semibold">file-j</p>
      <p class="mt-1 text-[11px] text-muted">正在启动 Go 后端服务…</p>
    </div>
    <div class="h-1 w-36 overflow-hidden rounded-full bg-ink/10">
      <div class="h-full w-1/3 animate-indeterminate rounded-full bg-accent" />
    </div>
  </div>

  <!-- boot failure -->
  <div v-else-if="state.bootError" class="flex h-full items-center justify-center bg-bg p-8">
    <div class="w-full max-w-[460px] rounded-[12px] border border-line/60 bg-elevated p-5 shadow-pane">
      <div class="flex items-start gap-3">
        <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-danger/10 text-danger">
          <Icon name="alert" :size="16" />
        </div>
        <div class="min-w-0">
          <p class="text-[13px] font-semibold">后端服务未能启动</p>
          <p class="mt-1 break-words text-[11.5px] leading-[1.5] text-muted">{{ state.bootError }}</p>
        </div>
      </div>
      <div class="mt-4 rounded-[7px] bg-ink/[0.04] px-3 py-2 text-[10.5px] leading-[1.6] text-muted">
        请确认 <code class="font-mono">src-tauri/binaries/file-jd-*</code> 存在（运行
        <code class="font-mono">pnpm backend:build</code>），或查看日志
        <span class="break-all font-mono">{{ state.configDir || defaultConfigDir }}/logs/backend.log</span>
      </div>
      <div class="mt-4 flex justify-end gap-2">
        <button class="btn" @click="refreshAll()">重试连接</button>
        <button class="btn-primary" @click="reloadApp">重新启动</button>
      </div>
    </div>
  </div>

  <!-- main workspace -->
  <div v-else class="flex h-full flex-col bg-bg">
    <Toolbar />

    <div class="flex min-h-0 flex-1">
      <Sidebar v-if="state.sidebarOpen" />

      <div
        ref="splitContainer"
        class="flex min-h-0 min-w-0 flex-1 p-2"
        :class="{ 'select-none': dragging }"
      >
        <div
          class="flex min-w-0 overflow-hidden rounded-[9px] border border-line/60 bg-panel shadow-pane"
          :style="{ width: leftWidth }"
        >
          <Pane pane-key="left" />
        </div>

        <!-- splitter with the classic dual-pane transfer buttons -->
        <div class="group relative w-[13px] shrink-0 cursor-col-resize" @mousedown="startSplitDrag">
          <div
            class="absolute bottom-3 left-1/2 top-3 w-px -translate-x-1/2 bg-line/70 transition-colors group-hover:bg-accent/60"
          />
          <div
            class="absolute left-1/2 top-1/2 z-10 flex -translate-x-1/2 -translate-y-1/2 flex-col gap-1"
          >
            <button
              class="flex h-[21px] w-[21px] items-center justify-center rounded-full border border-line/70 bg-panel text-muted shadow-sm transition-colors hover:border-accent/50 hover:text-accent disabled:opacity-40 disabled:hover:border-line/70 disabled:hover:text-muted"
              title="把左侧选中项复制到右侧"
              :disabled="!leftActive"
              @mousedown.stop
              @click="startTransfer('left', selectedEntries('left'), 'copy')"
            >
              <Icon name="chevron-right" :size="12" :stroke="2.2" />
            </button>
            <button
              class="flex h-[21px] w-[21px] items-center justify-center rounded-full border border-line/70 bg-panel text-muted shadow-sm transition-colors hover:border-accent/50 hover:text-accent disabled:opacity-40 disabled:hover:border-line/70 disabled:hover:text-muted"
              title="把右侧选中项复制到左侧"
              :disabled="!rightActive"
              @mousedown.stop
              @click="startTransfer('right', selectedEntries('right'), 'copy')"
            >
              <Icon name="chevron-left" :size="12" :stroke="2.2" />
            </button>
          </div>
        </div>

        <div
          class="flex min-w-0 flex-1 overflow-hidden rounded-[9px] border border-line/60 bg-panel shadow-pane"
        >
          <Pane pane-key="right" />
        </div>
      </div>
    </div>

    <TransferPanel />
    <StatusBar />

    <SessionDialog />
    <SettingsDialog />
    <SimpleDialogs />
    <ToastStack />
  </div>
</template>
