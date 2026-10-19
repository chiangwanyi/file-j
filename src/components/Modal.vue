<script setup lang="ts">
import { onBeforeUnmount, onMounted } from "vue";
import Icon from "./Icon.vue";

const props = withDefaults(
  defineProps<{
    title: string;
    subtitle?: string;
    width?: string;
    icon?: string;
  }>(),
  { width: "520px" },
);

const emit = defineEmits<{ close: [] }>();

function onKey(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.stopPropagation();
    emit("close");
  }
}

onMounted(() => window.addEventListener("keydown", onKey, true));
onBeforeUnmount(() => window.removeEventListener("keydown", onKey, true));
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/25 p-6 animate-fade-in dark:bg-black/45"
      @mousedown.self="emit('close')"
    >
      <div
        class="flex max-h-[86vh] w-full flex-col overflow-hidden rounded-[12px] border border-line/60 bg-elevated shadow-dialog animate-slide-up"
        :style="{ width: props.width }"
        role="dialog"
        aria-modal="true"
      >
        <header class="flex items-start gap-3 border-b border-line/60 px-4 py-3.5">
          <div
            v-if="icon"
            class="mt-0.5 flex h-7 w-7 items-center justify-center rounded-[7px] bg-accent/12 text-accent"
          >
            <Icon :name="icon" :size="15" />
          </div>
          <div class="min-w-0 flex-1">
            <h2 class="truncate text-[13px] font-semibold tracking-[-0.01em]">{{ title }}</h2>
            <p v-if="subtitle" class="mt-0.5 truncate text-[11px] text-muted">{{ subtitle }}</p>
          </div>
          <button class="icon-btn -mr-1 -mt-1" title="关闭" @click="emit('close')">
            <Icon name="x" :size="14" />
          </button>
        </header>

        <div class="min-h-0 flex-1 overflow-auto px-4 py-3.5">
          <slot />
        </div>

        <footer
          v-if="$slots.footer"
          class="flex items-center justify-end gap-2 border-t border-line/60 bg-ink/[0.015] px-4 py-2.5"
        >
          <slot name="footer" />
        </footer>
      </div>
    </div>
  </Teleport>
</template>
