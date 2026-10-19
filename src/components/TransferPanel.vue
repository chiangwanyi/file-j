<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
import {
  cancelTask,
  clearFinishedTasks,
  removeTask,
  retryTask,
  state,
  toggleQueue,
} from "../stores/workspace";
import type { TransferTask } from "../lib/types";
import { formatBytes, formatSpeed } from "../lib/format";

const tasks = computed(() => state.transfers);
const activeCount = computed(
  () => tasks.value.filter((task) => task.status === "running" || task.status === "queued").length,
);
const finishedCount = computed(() =>
  tasks.value.filter((task) => !["running", "queued"].includes(task.status)).length,
);

const statusLabel: Record<string, string> = {
  queued: "排队中",
  running: "传输中",
  done: "已完成",
  failed: "失败",
  canceled: "已取消",
  skipped: "已跳过",
};

function percent(task: TransferTask): number {
  if (!task.totalBytes) return task.status === "done" ? 100 : 0;
  return Math.min(100, Math.round((task.bytes / task.totalBytes) * 100));
}

function directionLabel(task: TransferTask): string {
  if (task.direction === "upload") return "上传";
  if (task.direction === "download") return "下载";
  return task.op === "move" ? "移动" : "复制";
}

function directionIcon(task: TransferTask): string {
  if (task.direction === "upload") return "upload";
  if (task.direction === "download") return "download";
  return task.op === "move" ? "arrow-right" : "copy";
}

function statusTone(task: TransferTask): string {
  switch (task.status) {
    case "done":
      return "text-success";
    case "failed":
      return "text-danger";
    case "canceled":
      return "text-muted";
    case "skipped":
      return "text-muted";
    case "running":
      return "text-accent";
    default:
      return "text-muted";
  }
}
</script>

<template>
  <section class="flex shrink-0 flex-col border-t border-line/70 bg-panel/70 backdrop-blur">
    <header class="flex h-[30px] shrink-0 items-center gap-2 px-3">
      <button class="flex items-center gap-1.5 text-[11px] font-semibold" @click="toggleQueue">
        <Icon name="panel-bottom" :size="12" class="text-muted" />
        传输队列
        <span v-if="activeCount" class="chip bg-accent/12 text-accent">{{ activeCount }} 进行中</span>
        <span v-else-if="tasks.length" class="chip bg-ink/[0.06] text-muted">{{ tasks.length }}</span>
      </button>
      <div class="flex-1" />
      <button
        v-if="finishedCount"
        class="text-[10.5px] text-muted transition-colors hover:text-ink"
        @click="clearFinishedTasks"
      >
        清除已完成
      </button>
      <button class="icon-btn h-6 w-6" :title="state.queueOpen ? '收起' : '展开'" @click="toggleQueue">
        <Icon :name="state.queueOpen ? 'chevron-down' : 'chevron-up'" :size="12" />
      </button>
    </header>

    <div v-if="state.queueOpen" class="min-h-0 overflow-y-auto border-t border-line/50" style="max-height: 186px">
      <div
        v-if="!tasks.length"
        class="flex h-[54px] items-center justify-center gap-2 text-[11px] text-muted/80"
      >
        <Icon name="arrow-down" :size="12" class="text-muted/50" />
        暂无传输任务。选中文件后按 F5 复制到对侧窗口。
      </div>

      <div
        v-for="task in tasks"
        :key="task.id"
        class="group flex items-center gap-2.5 border-b border-line/40 px-3 py-1.5 last:border-b-0"
      >
        <div
          class="flex h-6 w-6 shrink-0 items-center justify-center rounded-[5px]"
          :class="{
            'bg-accent/10 text-accent': task.status === 'running',
            'bg-success/10 text-success': task.status === 'done',
            'bg-danger/10 text-danger': task.status === 'failed',
            'bg-ink/[0.05] text-muted': ['queued', 'canceled', 'skipped'].includes(task.status),
          }"
        >
          <Icon
            :name="
              task.status === 'done'
                ? 'check'
                : task.status === 'failed'
                  ? 'alert'
                  : task.status === 'canceled'
                    ? 'x'
                    : directionIcon(task)
            "
            :size="12"
            :stroke="2"
          />
        </div>

        <div class="min-w-0 flex-1">
          <div class="flex items-baseline gap-1.5">
            <span class="truncate text-[11.5px] font-medium leading-[1.3]">{{ task.name }}</span>
            <span class="shrink-0 text-[10px] text-muted">{{ directionLabel(task) }}</span>
            <span class="shrink-0 text-[10px]" :class="statusTone(task)">
              {{ statusLabel[task.status] ?? task.status }}
            </span>
            <span v-if="task.error" class="truncate text-[10px] text-danger">· {{ task.error }}</span>
            <span v-else-if="task.message" class="truncate text-[10px] text-muted">· {{ task.message }}</span>
          </div>

          <div class="mt-1 flex items-center gap-2">
            <div class="h-[3px] flex-1 overflow-hidden rounded-full bg-ink/[0.08]">
              <div
                v-if="task.status === 'running' && !task.totalBytes"
                class="h-full w-1/3 animate-indeterminate rounded-full bg-accent"
              />
              <div
                v-else
                class="h-full rounded-full transition-[width] duration-200"
                :class="task.status === 'failed' ? 'bg-danger' : task.status === 'done' ? 'bg-success' : 'bg-accent'"
                :style="{ width: `${percent(task)}%` }"
              />
            </div>
            <span class="w-[124px] shrink-0 text-right text-[10px] tabular-nums text-muted">
              <template v-if="task.totalBytes">
                {{ formatBytes(task.bytes) }} / {{ formatBytes(task.totalBytes) }}
              </template>
              <template v-else-if="task.bytes">{{ formatBytes(task.bytes) }}</template>
              <template v-else>—</template>
              <span v-if="task.status === 'running' && task.speed" class="ml-1 text-accent">
                {{ formatSpeed(task.speed) }}
              </span>
            </span>
          </div>
        </div>

        <div class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
          <button
            v-if="task.status === 'running' || task.status === 'queued'"
            class="icon-btn h-6 w-6 text-danger"
            title="取消"
            @click="cancelTask(task.id)"
          >
            <Icon name="x" :size="11" />
          </button>
          <template v-else>
            <button
              v-if="task.status === 'failed' || task.status === 'canceled'"
              class="icon-btn h-6 w-6"
              title="重试"
              @click="retryTask(task.id)"
            >
              <Icon name="refresh" :size="11" />
            </button>
            <button class="icon-btn h-6 w-6" title="移除记录" @click="removeTask(task.id)">
              <Icon name="trash" :size="11" />
            </button>
          </template>
        </div>
      </div>
    </div>
  </section>
</template>
