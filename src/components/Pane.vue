<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import FileTable from "./FileTable.vue";
import Icon from "./Icon.vue";
import ContextMenu from "./ContextMenu.vue";
import type { MenuAction } from "../lib/menu";
import { modKeyLabel } from "../lib/platform";
import {
  canGoBack,
  canGoForward,
  connectSession,
  createFolder,
  disconnectPane,
  goBack,
  goForward,
  goUp,
  navigate,
  openSessionDialog,
  pane,
  refreshPane,
  selectedEntries,
  sessionById,
  setActive,
  state,
  toggleSidebar,
  updateConfig,
} from "../stores/workspace";
import type { PaneKey } from "../lib/types";
import { formatBytes } from "../lib/format";

const props = defineProps<{ paneKey: PaneKey }>();

const current = computed(() => pane(props.paneKey));
const isRemote = computed(() => current.value.side === "remote");
const session = computed(() => sessionById(current.value.sessionId));
const connected = computed(() => !isRemote.value || Boolean(current.value.sessionId));

const colorDot: Record<string, string> = {
  blue: "bg-[#3b99f7]",
  green: "bg-[#31b56f]",
  purple: "bg-[#a259e6]",
  orange: "bg-[#e89a2b]",
  red: "bg-[#e05555]",
  gray: "bg-muted",
};

const pathInput = ref<HTMLInputElement | null>(null);
const editingPath = ref(false);
const draftPath = ref("");

watch(
  () => current.value.path,
  (value) => {
    if (!editingPath.value) draftPath.value = value;
  },
  { immediate: true },
);

function commitPath() {
  editingPath.value = false;
  const next = draftPath.value.trim();
  if (!next || next === current.value.path) {
    draftPath.value = current.value.path;
    return;
  }
  void navigate(props.paneKey, next);
}

function cancelPath() {
  editingPath.value = false;
  draftPath.value = current.value.path;
  pathInput.value?.blur();
}

const menu = reactive({ open: false, x: 0, y: 0 });

const menuItems = computed<MenuAction[]>(() => {
  const items: MenuAction[] = [
    {
      label: "新建文件夹",
      icon: "folder-plus",
      action: () => createFolder(props.paneKey),
    },
    {
      label: state.config.ui.showHiddenFiles ? "隐藏以点开头的文件" : "显示隐藏文件",
      icon: state.config.ui.showHiddenFiles ? "eye-off" : "eye",
      action: () => {
        void updateConfig({ ui: { showHiddenFiles: !state.config.ui.showHiddenFiles } }).then(() =>
          Promise.all([refreshPane("left"), refreshPane("right")]),
        );
      },
    },
    { label: "", separator: true },
    {
      label: "刷新",
      icon: "refresh",
      hint: `${modKeyLabel}R`,
      action: () => void refreshPane(props.paneKey),
    },
    {
      label: "复制当前路径",
      icon: "copy",
      action: () => navigator.clipboard?.writeText(current.value.path),
    },
  ];
  if (isRemote.value && current.value.sessionId) {
    items.push({ label: "", separator: true });
    items.push({
      label: "断开连接",
      icon: "plug-off",
      danger: true,
      action: () => void disconnectPane(props.paneKey),
    });
  }
  return items;
});

function openMenu(event: MouseEvent) {
  menu.x = event.clientX;
  menu.y = event.clientY;
  menu.open = true;
}

const selectedSize = computed(() =>
  selectedEntries(props.paneKey).reduce((sum, entry) => sum + (entry.isDir ? 0 : entry.size), 0),
);

const remoteSessions = computed(() => state.sessions);

function quickConnect(id: string) {
  void connectSession(id, undefined, props.paneKey);
}

function openSessionManager() {
  openSessionDialog("edit");
}
</script>

<template>
  <section
    class="relative flex min-w-0 flex-1 flex-col overflow-hidden"
    @mousedown.capture="setActive(paneKey)"
  >
    <!-- pane identity -->
    <header class="flex h-[30px] shrink-0 items-center gap-2 border-b border-line/60 bg-sidebar/30 px-2.5">
      <span
        class="chip"
        :class="isRemote ? 'bg-accent/12 text-accent' : 'bg-ink/[0.06] text-muted'"
      >
        <Icon :name="isRemote ? 'server' : 'hard-drive'" :size="9" :stroke="2.2" />
        {{ isRemote ? "远程" : "本地" }}
      </span>
      <div class="flex min-w-0 items-center gap-1.5">
        <span
          v-if="isRemote && session"
          class="h-1.5 w-1.5 shrink-0 rounded-full"
          :class="colorDot[session.color ?? 'blue'] ?? colorDot.blue"
        />
        <span class="truncate text-[11px] font-medium text-ink/80">
          {{ isRemote ? session?.name ?? "未连接" : "本机磁盘" }}
        </span>
        <span v-if="isRemote && session" class="truncate text-[10.5px] text-muted">
          {{ session.user }}@{{ session.host }}
        </span>
      </div>
      <div class="ml-auto flex items-center gap-0.5">
        <button class="icon-btn h-6 w-6" title="新建文件夹" @click="createFolder(paneKey)">
          <Icon name="folder-plus" :size="13" />
        </button>
        <button class="icon-btn h-6 w-6" title="刷新" @click="refreshPane(paneKey)">
          <Icon name="refresh" :size="12.5" :class="{ 'animate-spin': current.loading }" />
        </button>
        <button class="icon-btn h-6 w-6" title="更多" @click="openMenu">
          <Icon name="more" :size="13" />
        </button>
      </div>
    </header>

    <!-- path bar -->
    <div class="flex h-[34px] shrink-0 items-center gap-0.5 border-b border-line/60 px-1.5">
      <button
        class="icon-btn h-6 w-6"
        title="后退"
        :disabled="!canGoBack(paneKey)"
        @click="goBack(paneKey)"
      >
        <Icon name="chevron-left" :size="13" />
      </button>
      <button
        class="icon-btn h-6 w-6"
        title="前进"
        :disabled="!canGoForward(paneKey)"
        @click="goForward(paneKey)"
      >
        <Icon name="chevron-right" :size="13" />
      </button>
      <button class="icon-btn h-6 w-6" title="上一级" :disabled="!connected" @click="goUp(paneKey)">
        <Icon name="corner-down-left" :size="12.5" class="rotate-180" />
      </button>
      <div class="relative ml-1 min-w-0 flex-1">
        <input
          ref="pathInput"
          v-model="draftPath"
          class="field h-6 border-transparent bg-ink/[0.045] pr-1.5 pl-2 font-mono text-[11px] focus:bg-panel"
          :disabled="!connected"
          spellcheck="false"
          autocomplete="off"
          @focus="editingPath = true"
          @blur="commitPath"
          @keydown.enter.prevent="commitPath"
          @keydown.esc.prevent="cancelPath"
        />
      </div>
    </div>

    <!-- file list or disconnected state -->
    <template v-if="connected">
      <FileTable :pane-key="paneKey" />
    </template>

    <div v-else class="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-6 text-center">
      <div class="flex h-9 w-9 items-center justify-center rounded-full bg-ink/[0.06]">
        <Icon name="server" :size="18" :stroke="1.4" class="text-muted/70" />
      </div>
      <div>
        <p class="text-[12.5px] font-medium">尚未连接远程服务器</p>
        <p class="mt-0.5 text-[11px] text-muted">选择一个已保存的会话，或新建一个 SSH/SFTP 连接</p>
      </div>
      <div v-if="remoteSessions.length" class="mt-1 w-full max-w-[280px] space-y-1">
        <button
          v-for="item in remoteSessions.slice(0, 5)"
          :key="item.session.id"
          class="flex w-full items-center gap-2 rounded-[6px] border border-line/60 bg-panel px-2.5 py-1.5 text-left transition-colors hover:border-accent/40 hover:bg-accent/[0.04]"
          @click="quickConnect(item.session.id)"
        >
          <span
            class="h-1.5 w-1.5 rounded-full"
            :class="colorDot[item.session.color ?? 'blue'] ?? colorDot.blue"
          />
          <span class="min-w-0 flex-1 truncate text-[11.5px] font-medium">{{ item.session.name }}</span>
          <span class="truncate text-[10.5px] text-muted">
            {{ item.session.user }}@{{ item.session.host }}
          </span>
          <Icon name="chevron-right" :size="11" class="text-muted/60" />
        </button>
      </div>
      <div class="mt-2 flex items-center gap-3">
        <button class="btn-primary h-6" @click="openSessionManager">管理会话</button>
        <button class="text-[11px] font-medium text-accent hover:underline" @click="toggleSidebar">
          显示侧边栏
        </button>
      </div>
    </div>

    <!-- footer -->
    <footer
      v-if="connected"
      class="flex h-[24px] shrink-0 items-center gap-3 border-t border-line/60 bg-sidebar/20 px-2.5 text-[10.5px] text-muted"
    >
      <span>{{ current.entries.length }} 项</span>
      <span v-if="current.selection.length" class="text-ink/70">
        已选 {{ current.selection.length }} 项
        <span v-if="selectedSize">· {{ formatBytes(selectedSize) }}</span>
      </span>
      <span class="ml-auto tabular-nums">
        <template v-if="current.freeBytes !== undefined">
          可用 {{ formatBytes(current.freeBytes) }}
        </template>
      </span>
    </footer>

    <ContextMenu
      v-if="menu.open"
      :x="menu.x"
      :y="menu.y"
      :items="menuItems"
      @close="menu.open = false"
    />
  </section>
</template>
