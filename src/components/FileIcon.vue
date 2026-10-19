<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
import { extensionOf, isArchive, isCode, isDocument, isImage, isMedia } from "../lib/format";

const props = defineProps<{ name: string; isDir: boolean; isSymlink?: boolean; size?: number }>();

const descriptor = computed(() => {
  if (props.isDir) return { icon: "folder", tone: "text-[#3b99f7]", bg: "bg-[#3b99f7]/10" };
  const ext = extensionOf(props.name);
  if (isImage(props.name)) return { icon: "image", tone: "text-[#a259e6]", bg: "bg-[#a259e6]/10" };
  if (isArchive(props.name)) return { icon: "archive", tone: "text-[#c98a20]", bg: "bg-[#c98a20]/12" };
  if (isCode(props.name)) return { icon: "code", tone: "text-[#21a06a]", bg: "bg-[#21a06a]/10" };
  if (isMedia(props.name))
    return ext.match(/mp4|mov|m4v|avi|mkv/)
      ? { icon: "film", tone: "text-[#e0568a]", bg: "bg-[#e0568a]/10" }
      : { icon: "music", tone: "text-[#e0568a]", bg: "bg-[#e0568a]/10" };
  if (isDocument(props.name)) return { icon: "file-text", tone: "text-[#5b7fbf]", bg: "bg-[#5b7fbf]/10" };
  return { icon: "file", tone: "text-muted", bg: "bg-ink/5" };
});
</script>

<template>
  <span class="relative inline-flex" :style="{ width: `${size ?? 16}px`, height: `${size ?? 16}px` }">
    <Icon :name="descriptor.icon" :size="size ?? 16" :stroke="1.7" :class="descriptor.tone" />
    <Icon
      v-if="isSymlink"
      name="link"
      :size="9"
      :stroke="2.6"
      class="absolute -bottom-0.5 -left-0.5 rounded-full bg-panel p-[1px] text-muted"
    />
  </span>
</template>
