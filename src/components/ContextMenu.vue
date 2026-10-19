<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import Icon from "./Icon.vue";
import type { MenuAction } from "../lib/menu";

const props = defineProps<{ x: number; y: number; items: MenuAction[] }>();
const emit = defineEmits<{ close: [] }>();

const root = ref<HTMLElement | null>(null);
const position = ref({ left: props.x, top: props.y });

function clampPosition() {
  const el = root.value;
  if (!el) return;
  const rect = el.getBoundingClientRect();
  const margin = 8;
  const left = Math.min(props.x, window.innerWidth - rect.width - margin);
  const top = Math.min(props.y, window.innerHeight - rect.height - margin);
  position.value = { left: Math.max(margin, left), top: Math.max(margin, top) };
}

function onPointerDown(event: MouseEvent) {
  if (root.value && !root.value.contains(event.target as Node)) emit("close");
}

function onKey(event: KeyboardEvent) {
  if (event.key === "Escape") emit("close");
}

function run(item: MenuAction) {
  if (item.disabled || !item.action) return;
  emit("close");
  item.action();
}

watch(
  () => [props.x, props.y, props.items],
  async () => {
    await nextTick();
    clampPosition();
  },
);

onMounted(async () => {
  window.addEventListener("mousedown", onPointerDown, true);
  window.addEventListener("keydown", onKey, true);
  window.addEventListener("resize", clampPosition);
  await nextTick();
  clampPosition();
});

onBeforeUnmount(() => {
  window.removeEventListener("mousedown", onPointerDown, true);
  window.removeEventListener("keydown", onKey, true);
  window.removeEventListener("resize", clampPosition);
});
</script>

<template>
  <Teleport to="body">
    <div
      ref="root"
      class="fixed z-[70] min-w-[196px] rounded-[8px] border border-line/70 bg-elevated/95 p-1 shadow-pop backdrop-blur-xl animate-fade-in"
      :style="{ left: `${position.left}px`, top: `${position.top}px` }"
      @contextmenu.prevent
    >
      <template v-for="(item, index) in items" :key="index">
        <div v-if="item.separator" class="menu-sep" />
        <button
          v-else
          class="menu-item group"
          :class="{ 'text-danger hover:bg-danger': item.danger }"
          :disabled="item.disabled"
          @click="run(item)"
        >
          <Icon v-if="item.icon" :name="item.icon" :size="13" :class="item.danger ? '' : 'text-muted group-hover:text-white'" />
          <span class="flex-1 truncate">{{ item.label }}</span>
          <span v-if="item.hint" class="menu-hint text-2xs text-muted/70">{{ item.hint }}</span>
        </button>
      </template>
    </div>
  </Teleport>
</template>
