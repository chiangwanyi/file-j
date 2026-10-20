<script setup lang="ts">
const props = defineProps<{ modelValue: boolean; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();

function toggle() {
  if (props.disabled) return;
  emit("update:modelValue", !props.modelValue);
}
</script>

<template>
  <!-- The knob is pinned with `left-[2px]` on purpose. Relying on the static
       position (`absolute` + only `top`) makes the knob start at the button's
       centred text origin, so `translate-x` pushes it outside the 30px track. -->
  <button
    type="button"
    role="switch"
    :aria-checked="modelValue"
    :disabled="disabled"
    class="relative box-border h-[18px] w-[30px] shrink-0 cursor-pointer appearance-none rounded-full border-0 p-0 transition-colors duration-150 disabled:cursor-default disabled:opacity-40"
    :class="modelValue ? 'bg-accent' : 'bg-ink/25'"
    @click="toggle"
  >
    <span
      class="pointer-events-none absolute left-[2px] top-[2px] block h-[14px] w-[14px] rounded-full bg-white shadow-sm transition-transform duration-150"
      :class="modelValue ? 'translate-x-[12px]' : 'translate-x-0'"
    />
  </button>
</template>
