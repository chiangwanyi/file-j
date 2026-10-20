<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
import { modKeyLabel } from "../lib/platform";
import { pane, selectedEntries, state } from "../stores/workspace";

const active = computed(() => pane(state.active));
const selection = computed(() => selectedEntries(state.active));
const running = computed(
  () => state.transfers.filter((task) => task.status === "running" || task.status === "queued").length,
);
</script>

<template>
  <footer
    class="flex h-[23px] shrink-0 items-center gap-2.5 border-t border-line/70 bg-sidebar/50 px-3 text-[10px] text-muted"
  >
    <span class="flex items-center gap-1.5">
      <span
        class="h-[6px] w-[6px] rounded-full"
        :class="state.connected ? 'bg-success' : 'bg-danger'"
      />
      {{ state.connected ? "已连接后端" : "后端未连接" }}
    </span>
    <span class="text-muted/40">|</span>
    <span class="flex items-center gap-1">
      <Icon :name="active.side === 'remote' ? 'server' : 'hard-drive'" :size="9" />
      {{ active.side === "remote" ? "远程" : "本地" }}
    </span>
    <span v-if="selection.length" class="text-ink/70">
      已选 {{ selection.length }} 项
    </span>
    <span v-if="running" class="flex items-center gap-1 text-accent">
      <Icon name="refresh" :size="9" class="animate-spin" />
      {{ running }} 个任务传输中
    </span>
    <span class="flex-1" />
    <span class="hidden lg:inline text-muted/70">F5 复制 · F6 移动 · {{ modKeyLabel }}R 刷新</span>
    <span class="text-muted/40">|</span>
    <span class="tabular-nums">file-j {{ state.backendVersion || "0.1.0" }}</span>
  </footer>
</template>
