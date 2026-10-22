<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
import {
  connectSession,
  navigate,
  openSessionDialog,
  sessionById,
  state,
  toast,
} from "../stores/workspace";

const localKey = computed(() => (state.left.side === "local" ? "left" : "right"));
const localPath = computed(() => state[localKey.value].path);

const placeIcon: Record<string, string> = {
  主目录: "home",
  桌面: "desktop",
  文稿: "file-text",
  下载: "download",
  根目录: "hard-drive",
};

// macOS contributes a single "根目录" entry; Windows contributes one entry per
// mounted drive ("C:\", "D:\"), so those fall back to the drive glyph instead
// of the generic folder one.
function iconFor(place: { name: string; path: string }): string {
  return placeIcon[place.name] ?? (/^[A-Za-z]:\\?$/.test(place.path) ? "hard-drive" : "folder");
}

const colorDot: Record<string, string> = {
  blue: "bg-[#3b99f7]",
  green: "bg-[#31b56f]",
  purple: "bg-[#a259e6]",
  orange: "bg-[#e89a2b]",
  red: "bg-[#e05555]",
  gray: "bg-muted",
};

const remoteKey = computed(() => (state.right.side === "remote" ? "right" : "left"));
const remotePane = computed(() => state[remoteKey.value]);

function openPlace(path: string) {
  void navigate(localKey.value, path);
}

function openSession(id: string) {
  const existing = remotePane.value;
  if (existing.sessionId === id) return;
  if (state.sessions.find((item) => item.session.id === id)?.connected) {
    const pane = remotePane.value;
    pane.side = "remote";
    pane.sessionId = id;
    pane.history = [];
    pane.historyIndex = -1;
    void navigate(remoteKey.value, sessionById(id)?.remotePath ?? ".");
    return;
  }
  void connectSession(id, undefined, remoteKey.value);
}

function newSession() {
  // "create" matters: opening the manager in edit mode would select the session
  // used last time, so the connection the user just typed in would be written
  // over instead of added.
  openSessionDialog("create");
}

function revealConfig() {
  toast("info", "配置文件位于", state.configDir || "");
}
</script>

<template>
  <aside class="flex w-[196px] shrink-0 flex-col border-r border-line/70 bg-sidebar/40">
    <div class="min-h-0 flex-1 overflow-y-auto px-2 pb-2 pt-3">
      <p class="px-2 pb-1 text-[10px] font-semibold uppercase tracking-[0.05em] text-muted/80">
        本机
      </p>
      <button
        v-for="place in state.places"
        :key="place.path"
        class="flex h-[27px] w-full items-center gap-2 rounded-[6px] px-2 text-[11.5px] transition-colors"
        :class="
          localPath === place.path
            ? 'bg-accent/[0.12] font-medium text-accent'
            : 'text-ink/85 hover:bg-ink/[0.05]'
        "
        @click="openPlace(place.path)"
      >
        <Icon :name="iconFor(place)" :size="13" :class="localPath === place.path ? 'text-accent' : 'text-muted'" />
        <span class="truncate">{{ place.name }}</span>
      </button>

      <div class="mt-4 flex items-center justify-between px-2 pb-1">
        <p class="text-[10px] font-semibold uppercase tracking-[0.05em] text-muted/80">会话</p>
        <button class="text-muted transition-colors hover:text-accent" title="新建会话" @click="newSession">
          <Icon name="plus" :size="12" />
        </button>
      </div>

      <button
        v-for="item in state.sessions"
        :key="item.session.id"
        class="group flex h-[30px] w-full items-center gap-2 rounded-[6px] px-2 text-left transition-colors hover:bg-ink/[0.05]"
        :class="
          remotePane.sessionId === item.session.id
            ? 'bg-accent/[0.12]'
            : ''
        "
        @click="openSession(item.session.id)"
      >
        <span
          class="h-[6px] w-[6px] shrink-0 rounded-full"
          :class="[
            colorDot[item.session.color ?? 'blue'] ?? colorDot.blue,
            item.connected ? '' : 'opacity-40',
          ]"
        />
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[11.5px] leading-[1.2]">{{ item.session.name }}</span>
          <span class="block truncate text-[10px] leading-[1.25] text-muted">
            {{ item.session.user }}@{{ item.session.host }}
          </span>
        </span>
        <Icon
          v-if="state.connectingId === item.session.id"
          name="refresh"
          :size="11"
          class="animate-spin text-accent"
        />
        <Icon
          v-else-if="!item.connected"
          name="plug"
          :size="11"
          class="text-muted/0 transition-colors group-hover:text-muted"
        />
        <span v-else class="h-1 w-1 rounded-full bg-success" />
      </button>

      <div v-if="!state.sessions.length" class="px-2 py-2 text-[10.5px] leading-[1.5] text-muted/80">
        还没有会话，点击右上角 + 新建 SSH/SFTP 连接。
      </div>

      <button
        class="mt-1 flex h-[27px] w-full items-center gap-2 rounded-[6px] px-2 text-[11.5px] text-muted transition-colors hover:bg-ink/[0.05] hover:text-ink"
        @click="newSession"
      >
        <Icon name="plus" :size="12" />
        新建会话…
      </button>
    </div>

    <div class="border-t border-line/70 px-2 py-2">
      <button
        class="flex w-full items-center gap-2 rounded-[6px] px-2 py-1 text-left text-[10.5px] text-muted transition-colors hover:bg-ink/[0.05]"
        title="配置文件位置"
        @click="revealConfig"
      >
        <Icon name="settings" :size="11" />
        <span class="truncate">YAML 配置</span>
      </button>
    </div>
  </aside>
</template>
