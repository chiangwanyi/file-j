<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import FileIcon from "./FileIcon.vue";
import Icon from "./Icon.vue";
import ContextMenu from "./ContextMenu.vue";
import type { MenuAction } from "../lib/menu";
import {
  deleteKeyLabel,
  modKeyLabel,
  revealLabel,
  shiftKeyLabel,
} from "../lib/platform";
import {
  createFolder,
  deleteEntries,
  isFavorite,
  openEntry,
  pane,
  refreshPane,
  renameEntry,
  revealEntry,
  selectByPath,
  selectRow,
  selectedEntries,
  setActive,
  setSort,
  sortedEntries,
  startTransfer,
  state,
  toast,
  toggleFavorite,
} from "../stores/workspace";
import type { FsEntry, PaneKey } from "../lib/types";
import { basename, formatBytes, formatDate } from "../lib/format";

const props = defineProps<{ paneKey: PaneKey }>();

const current = computed(() => pane(props.paneKey));
const isRemote = computed(() => current.value.side === "remote");
const rowHeight = computed(() => (state.config.ui.density === "compact" ? 23 : 27));

const sorted = computed(() => sortedEntries(props.paneKey));

const columns = computed(() =>
  isRemote.value
    ? "minmax(0, 1fr) 78px 118px 84px"
    : "minmax(0, 1fr) 78px 118px",
);

const menu = reactive({
  open: false,
  x: 0,
  y: 0,
  entry: null as FsEntry | null,
});

const dropTarget = ref<string | null>(null);

function isSelected(entry: FsEntry): boolean {
  return current.value.selection.includes(entry.path);
}

function onRowClick(entry: FsEntry, event: MouseEvent) {
  setActive(props.paneKey);
  selectRow(props.paneKey, entry, event);
}

function onRowDoubleClick(entry: FsEntry) {
  void openEntry(props.paneKey, entry);
}

function openMenu(event: MouseEvent, entry?: FsEntry) {
  event.preventDefault();
  setActive(props.paneKey);
  if (entry) {
    if (!isSelected(entry)) selectByPath(props.paneKey, entry.path);
    menu.entry = entry;
  } else {
    menu.entry = null;
    current.value.selection = [];
  }
  menu.x = event.clientX;
  menu.y = event.clientY;
  menu.open = true;
}

const transferLabel = computed(() => (isRemote.value ? "下载到对侧" : "上传到对侧"));
const transferIcon = computed(() => (isRemote.value ? "download" : "upload"));

const menuItems = computed<MenuAction[]>(() => {
  const entry = menu.entry;
  const items: MenuAction[] = [];
  const targets = entry ? [entry] : selectedEntries(props.paneKey);

  if (entry) {
    items.push({
      label: entry.isDir ? "打开" : isRemote.value ? "下载到对侧" : "打开",
      icon: entry.isDir ? "folder-open" : "corner-down-left",
      action: () => void openEntry(props.paneKey, entry),
    });
    items.push({
      label: transferLabel.value,
      icon: transferIcon.value,
      hint: "F5",
      disabled: targets.length === 0,
      action: () => void startTransfer(props.paneKey, targets, "copy"),
    });
    items.push({
      label: "移动到对侧",
      icon: "arrow-right",
      hint: "F6",
      disabled: targets.length === 0,
      action: () => void startTransfer(props.paneKey, targets, "move"),
    });
    items.push({ label: "", separator: true });
  }

  items.push({
    label: "新建文件夹",
    icon: "folder-plus",
    hint: `${shiftKeyLabel}${modKeyLabel}N`,
    action: () => createFolder(props.paneKey),
  });
  if (entry) {
    items.push({
      label: "重命名",
      icon: "file-text",
      action: () => entry && renameEntry(props.paneKey, entry),
    });
  }
  // Bookmarks are local folders only: an SFTP path is meaningless without its
  // session, and a file has nothing to open.
  if (!isRemote.value && (!entry || entry.isDir)) {
    // With a folder row selected that folder is the target; on empty space it is
    // the folder currently open, which saves a trip to the "更多" menu.
    const target = entry ?? { path: current.value.path, name: basename(current.value.path) };
    const marked = isFavorite(target.path);
    items.push({
      label: marked ? "取消收藏" : entry ? "收藏此文件夹" : "收藏当前文件夹",
      icon: marked ? "star-filled" : "star",
      action: () => void toggleFavorite(target.path, target.name),
    });
  }
  items.push({
    label: targets.length > 1 ? `删除 ${targets.length} 项` : "删除",
    icon: "trash",
    hint: deleteKeyLabel,
    danger: true,
    disabled: targets.length === 0,
    action: () => void deleteEntries(props.paneKey, targets),
  });

  items.push({ label: "", separator: true });
  if (!isRemote.value && entry) {
    items.push({
      label: revealLabel,
      icon: "drive",
      action: () => entry && void revealEntry(entry),
    });
  }
  items.push({
    label: "复制路径",
    icon: "copy",
    disabled: targets.length !== 1,
    action: () => {
      if (!entry) return;
      navigator.clipboard
        ?.writeText(entry.path)
        .then(() => toast("success", "路径已复制"))
        .catch(() => toast("error", "无法复制到剪贴板"));
    },
  });
  items.push({
    label: "刷新",
    icon: "refresh",
    hint: `${modKeyLabel}R`,
    action: () => void refreshPane(props.paneKey),
  });
  return items;
});

function onSort(key: "name" | "size" | "modTime" | "kind") {
  setSort(props.paneKey, key);
}

function onDragStart(event: DragEvent, entry: FsEntry) {
  setActive(props.paneKey);
  if (!isSelected(entry)) selectByPath(props.paneKey, entry.path);
  const payload = JSON.stringify({
    key: props.paneKey,
    paths: current.value.selection.length ? current.value.selection : [entry.path],
  });
  event.dataTransfer?.setData("application/x-filej", payload);
  event.dataTransfer?.setData("text/plain", entry.path);
  if (event.dataTransfer) event.dataTransfer.effectAllowed = "copyMove";
}

function onDrop(event: DragEvent, targetEntry?: FsEntry) {
  event.preventDefault();
  dropTarget.value = null;
  const raw = event.dataTransfer?.getData("application/x-filej");
  if (!raw) return;
  let payload: { key: PaneKey; paths: string[] };
  try {
    payload = JSON.parse(raw);
  } catch {
    return;
  }
  if (payload.key === props.paneKey) return;

  const source = pane(payload.key);
  const entries = source.entries.filter((entry) => payload.paths.includes(entry.path));
  if (!entries.length) return;
  const destination =
    targetEntry && targetEntry.isDir && props.paneKey !== payload.key
      ? targetEntry.path
      : undefined;
  void startTransfer(payload.key, entries, event.altKey ? "move" : "copy", destination);
}

function dragOver(event: DragEvent, entry?: FsEntry) {
  event.preventDefault();
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = event.altKey ? "move" : "copy";
  }
  dropTarget.value = entry && entry.isDir ? entry.path : "__pane__";
}

function dragLeave(entry?: FsEntry) {
  if (!entry || dropTarget.value === entry.path) dropTarget.value = null;
}
</script>

<template>
  <div class="relative flex min-h-0 flex-1 flex-col">
    <!-- column headers -->
    <div
      class="sticky top-0 z-10 grid h-[25px] shrink-0 items-center gap-2 border-b border-l-2 border-l-transparent border-line/60 bg-panel/95 px-3 text-[10.5px] font-medium text-muted backdrop-blur"
      :style="{ gridTemplateColumns: columns }"
    >
      <button class="flex items-center gap-1 text-left hover:text-ink" @click="onSort('name')">
        名称
        <Icon v-if="current.sortKey === 'name'" :name="current.sortDesc ? 'chevron-down' : 'chevron-up'" :size="10" />
      </button>
      <button class="flex items-center justify-end gap-1 hover:text-ink" @click="onSort('size')">
        大小
        <Icon v-if="current.sortKey === 'size'" :name="current.sortDesc ? 'chevron-down' : 'chevron-up'" :size="10" />
      </button>
      <button class="flex items-center gap-1 hover:text-ink" @click="onSort('modTime')">
        修改日期
        <Icon
          v-if="current.sortKey === 'modTime'"
          :name="current.sortDesc ? 'chevron-down' : 'chevron-up'"
          :size="10"
        />
      </button>
      <button
        v-if="isRemote"
        class="flex items-center gap-1 hover:text-ink"
        @click="onSort('kind')"
      >
        权限
        <Icon v-if="current.sortKey === 'kind'" :name="current.sortDesc ? 'chevron-down' : 'chevron-up'" :size="10" />
      </button>
    </div>

    <!-- rows -->
    <div
      class="min-h-0 flex-1 overflow-auto outline-none"
      :class="{
        'ring-2 ring-inset ring-accent/40': dropTarget === '__pane__',
        'bg-panel': true,
      }"
      tabindex="0"
      @dragover="dragOver($event)"
      @dragleave="dragLeave()"
      @drop="onDrop($event)"
      @contextmenu="openMenu($event)"
      @click.self="setActive(paneKey); current.selection = []"
    >
      <div
        v-for="entry in sorted"
        :key="entry.path"
        :data-path="entry.path"
        class="group grid cursor-default items-center gap-2 border-l-2 px-3 transition-colors"
        :class="[
          isSelected(entry)
            ? state.active === paneKey
              ? 'border-l-accent bg-accent/[0.17]'
              : 'border-l-line bg-ink/[0.07]'
            : 'border-l-transparent hover:bg-ink/[0.045]',
          entry.isHidden ? 'opacity-60' : '',
          dropTarget === entry.path ? 'ring-1 ring-inset ring-accent/60' : '',
        ]"
        :style="{ gridTemplateColumns: columns, height: `${rowHeight}px` }"
        draggable="true"
        @click="onRowClick(entry, $event)"
        @dblclick="onRowDoubleClick(entry)"
        @contextmenu.stop="openMenu($event, entry)"
        @dragstart="onDragStart($event, entry)"
        @dragover.stop="dragOver($event, entry)"
        @dragleave.stop="dragLeave(entry)"
        @drop.stop="onDrop($event, entry)"
      >
        <div class="flex min-w-0 items-center gap-1.5">
          <FileIcon :name="entry.name" :is-dir="entry.isDir" :is-symlink="entry.isSymlink" :size="15" />
          <span class="truncate text-[12px] leading-none">{{ entry.name }}</span>
          <Icon
            v-if="entry.isSymlink"
            name="arrow-right"
            :size="9"
            class="text-muted/60"
          />
        </div>
        <div class="text-right text-[11px] tabular-nums text-muted">
          {{ entry.isDir ? "—" : formatBytes(entry.size) }}
        </div>
        <div class="truncate text-[11px] tabular-nums text-muted">
          {{ formatDate(entry.modTime, state.config.ui.dateFormat) }}
        </div>
        <div v-if="isRemote" class="truncate font-mono text-[10px] text-muted/80">
          {{ entry.mode }}
        </div>
      </div>

      <!-- empty / error / loading states -->
      <div
        v-if="!current.entries.length && !current.loading && !current.error"
        class="flex h-full min-h-[160px] flex-col items-center justify-center gap-1.5 px-6 text-center"
      >
        <Icon name="folder-open" :size="26" :stroke="1.3" class="text-muted/30" />
        <p class="text-[12px] text-muted">此文件夹为空</p>
        <button class="mt-1 text-[11px] font-medium text-accent hover:underline" @click="createFolder(paneKey)">
          新建文件夹
        </button>
      </div>

      <div
        v-if="current.error"
        class="m-3 flex items-start gap-2 rounded-[8px] border border-danger/25 bg-danger/[0.06] px-3 py-2.5"
      >
        <Icon name="alert" :size="14" class="mt-0.5 shrink-0 text-danger" />
        <div class="min-w-0 flex-1">
          <p class="text-[11.5px] font-medium text-danger">无法读取该文件夹</p>
          <p class="mt-0.5 break-words text-[11px] leading-[1.45] text-muted">{{ current.error }}</p>
          <button class="mt-1.5 text-[11px] font-medium text-accent hover:underline" @click="refreshPane(paneKey)">
            重试
          </button>
        </div>
      </div>

      <div v-if="current.loading" class="pointer-events-none absolute inset-x-0 top-[25px] h-0.5 overflow-hidden">
        <div class="h-full w-1/3 animate-indeterminate bg-accent/70" />
      </div>
    </div>

    <ContextMenu
      v-if="menu.open"
      :x="menu.x"
      :y="menu.y"
      :items="menuItems"
      @close="menu.open = false"
    />
  </div>
</template>
