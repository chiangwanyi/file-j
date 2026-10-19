<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
import { dismissToast, state } from "../stores/workspace";

// Float the HUD above the transfer queue (whose height depends on whether the
// queue list is expanded), the status bar and the pane footer, so it never
// covers any status text.
const offsetClass = computed(() =>
  state.queueOpen ? "bottom-[148px]" : "bottom-[94px]",
);
</script>

<template>
  <!-- macOS-HUD style notifications: a compact pill at the bottom centre, which
       keeps both file panes and the queue header unobstructed. -->
  <div
    class="pointer-events-none fixed left-1/2 z-[45] flex w-full max-w-[560px] -translate-x-1/2 flex-col items-center gap-1.5 px-4"
    :class="offsetClass"
  >
    <TransitionGroup
      enter-from-class="translate-y-2 opacity-0"
      enter-active-class="transition duration-150 ease-out"
      leave-to-class="translate-y-1 opacity-0"
      leave-active-class="transition duration-120 ease-in"
    >
      <div
        v-for="item in state.toasts"
        :key="item.id"
        class="pointer-events-auto flex max-w-full items-start gap-2 rounded-[10px] border border-white/10 px-3 py-1.5 text-white shadow-pop backdrop-blur-xl"
        :class="item.kind === 'error' ? 'bg-[#a3231c]/95' : 'bg-[#1b1b1e]/93'"
      >
        <span
          class="mt-[1px] flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full"
          :class="{
            'bg-success/20 text-[#5ee08e]': item.kind === 'success',
            'bg-white/15 text-white': item.kind === 'error',
            'bg-accent/25 text-[#7cbcff]': item.kind === 'info',
          }"
        >
          <Icon
            :name="item.kind === 'error' ? 'alert' : item.kind === 'success' ? 'check' : 'clock'"
            :size="9"
            :stroke="2.6"
          />
        </span>
        <p class="min-w-0 text-[11.5px] leading-[1.35]">
          <span class="font-medium">{{ item.title }}</span>
          <span v-if="item.detail" class="ml-1.5 text-white/65">{{ item.detail }}</span>
        </p>
        <button
          class="-mr-1 ml-1 flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-white/50 hover:bg-white/15 hover:text-white"
          @click="dismissToast(item.id)"
        >
          <Icon name="x" :size="9" :stroke="2.4" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
