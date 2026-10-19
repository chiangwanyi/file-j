<script setup lang="ts">
const props = defineProps<{ modelValue: boolean; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();

function toggle() {
  if (props.disabled) return;
  emit("update:modelValue", !props.modelValue);
}
</script>

<template>
  <button
    class="relative h-[18px] w-[30px] shrink-0 rounded-full transition-colors duration-150"
    :class="modelValue ? 'bg-accent' : 'bg-ink/25'"
    role="switch"
    :aria-checked="modelValue"
    @click="toggle"
  >
    <span
      class="absolute top-[2px] h-[14px] w-[14px] rounded-full bg-white shadow-sm transition-transform duration-150"
      :class="modelValue ? 'translate-x-[14px]' : 'translate-x-[2px]'"
    />
  </button>
</template>
