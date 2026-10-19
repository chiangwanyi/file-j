<script setup lang="ts">
import { nextTick, ref, watch } from "vue";
import Modal from "./Modal.vue";
import Icon from "./Icon.vue";
import {
  closeConfirm,
  closePasswordDialog,
  closePrompt,
  runConfirm,
  state,
  submitPassword,
  submitPrompt,
} from "../stores/workspace";

// ------------------------------------------------------------- prompt dialog
const promptValue = ref("");
const promptBusy = ref(false);

watch(
  () => state.dialog.prompt,
  async (dialog) => {
    if (!dialog) return;
    promptValue.value = dialog.value;
    await nextTick();
    const input = document.getElementById("prompt-input") as HTMLInputElement | null;
    input?.focus();
    input?.select();
  },
);

async function confirmPrompt() {
  if (promptBusy.value) return;
  promptBusy.value = true;
  try {
    await submitPrompt(promptValue.value);
  } finally {
    promptBusy.value = false;
  }
}

// ----------------------------------------------------------- password dialog
const passwordValue = ref("");

watch(
  () => state.dialog.password,
  async (dialog) => {
    if (!dialog) return;
    passwordValue.value = "";
    await nextTick();
    (document.getElementById("password-input") as HTMLInputElement | null)?.focus();
  },
);

async function confirmPassword() {
  if (!state.dialog.password || state.dialog.password.busy) return;
  await submitPassword(passwordValue.value);
}
</script>

<template>
  <!-- prompt -->
  <Modal
    v-if="state.dialog.prompt"
    :title="state.dialog.prompt.title"
    icon="folder-plus"
    width="380px"
    @close="closePrompt"
  >
    <label class="block">
      <span class="mb-1 block text-[10.5px] font-medium text-muted">
        {{ state.dialog.prompt.label }}
      </span>
      <input
        id="prompt-input"
        v-model="promptValue"
        class="field"
        :placeholder="state.dialog.prompt.placeholder"
        spellcheck="false"
        @keydown.enter.prevent="confirmPrompt"
      />
    </label>
    <template #footer>
      <button class="btn" @click="closePrompt">取消</button>
      <button class="btn-primary" :disabled="promptBusy || !promptValue.trim()" @click="confirmPrompt">
        {{ state.dialog.prompt.confirmText }}
      </button>
    </template>
  </Modal>

  <!-- confirm -->
  <Modal
    v-if="state.dialog.confirm"
    :title="state.dialog.confirm.title"
    icon="alert"
    width="400px"
    @close="closeConfirm"
  >
    <p class="text-[12px] leading-[1.5]">{{ state.dialog.confirm.message }}</p>
    <p
      v-if="state.dialog.confirm.detail"
      class="mt-2 whitespace-pre-line break-words rounded-[7px] bg-ink/[0.04] px-2.5 py-2 text-[11px] leading-[1.55] text-muted"
    >
      {{ state.dialog.confirm.detail }}
    </p>
    <template #footer>
      <button class="btn" @click="closeConfirm">取消</button>
      <button
        class="btn-primary"
        :class="{ '!bg-danger': state.dialog.confirm.danger }"
        @click="runConfirm"
      >
        {{ state.dialog.confirm.confirmText }}
      </button>
    </template>
  </Modal>

  <!-- password -->
  <Modal
    v-if="state.dialog.password"
    :title="state.dialog.password.title"
    :subtitle="state.dialog.password.subtitle"
    icon="key"
    width="390px"
    @close="closePasswordDialog"
  >
    <label class="block">
      <span class="mb-1 block text-[10.5px] font-medium text-muted">密码</span>
      <input
        id="password-input"
        v-model="passwordValue"
        type="password"
        class="field"
        placeholder="输入登录密码"
        autocomplete="off"
        @keydown.enter.prevent="confirmPassword"
      />
    </label>

    <label class="mt-3 flex items-center gap-2 text-[11px] text-muted">
      <input
        v-model="state.dialog.password.remember"
        type="checkbox"
        class="accent-[rgb(var(--c-accent))]"
      />
      保存到系统钥匙串
      <span class="text-muted/70">（sessions.yaml 中只保存引用）</span>
    </label>

    <div
      v-if="state.dialog.password.error"
      class="mt-3 flex items-start gap-2 rounded-[7px] bg-danger/[0.07] px-2.5 py-2 text-[11px] text-danger"
    >
      <Icon name="alert" :size="12" class="mt-0.5 shrink-0" />
      <span class="break-words leading-[1.45]">{{ state.dialog.password.error }}</span>
    </div>

    <template #footer>
      <button class="btn" @click="closePasswordDialog">取消</button>
      <button
        class="btn-primary"
        :disabled="state.dialog.password.busy || !passwordValue"
        @click="confirmPassword"
      >
        {{ state.dialog.password.busy ? "正在连接…" : "连接" }}
      </button>
    </template>
  </Modal>
</template>
