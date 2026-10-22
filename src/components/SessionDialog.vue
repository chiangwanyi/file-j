<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import Modal from "./Modal.vue";
import Icon from "./Icon.vue";
import {
  closePasswordDialog,
  closeSessionDialog,
  connectSession,
  deleteSession,
  loadSessions,
  openConfirm,
  saveSession,
  state,
  testSession,
  toast,
} from "../stores/workspace";
import type { AuthMethod, Session } from "../lib/types";
import { errorText } from "../lib/ipc";

const COLORS = ["blue", "green", "purple", "orange", "red", "gray"];

function blank(): Session {
  return {
    id: "",
    name: "",
    protocol: "sftp",
    host: "",
    port: 22,
    user: "",
    authMethod: "password",
    keyPath: "~/.ssh/id_ed25519",
    remotePath: ".",
    localPath: "",
    color: "blue",
    note: "",
  };
}

const form = reactive<Session>(blank());
const password = ref("");
const clearPassword = ref(false);
const selectedId = ref<string | null>(null);
const testing = ref(false);
const saving = ref(false);
const testResult = ref<{ ok: boolean; text: string } | null>(null);

const isNew = computed(() => !form.id);
const hasStoredPassword = computed(() => {
  const current = state.sessions.find((item) => item.session.id === form.id);
  return Boolean(current?.session.credentialRef) && Boolean(current?.hasCredential);
});

function fill(session: Session) {
  Object.assign(form, blank(), JSON.parse(JSON.stringify(session)));
  password.value = "";
  clearPassword.value = false;
  testResult.value = null;
}

function selectSession(id: string) {
  const item = state.sessions.find((entry) => entry.session.id === id);
  if (!item) {
    // The session disappeared (deleted elsewhere): fall back to a blank form
    // rather than leaving stale values in the fields.
    newSession();
    return;
  }
  selectedId.value = id;
  fill(item.session);
}

function newSession() {
  selectedId.value = null;
  fill(blank());
  if (state.sessions.length === 0) form.name = "新会话";
}

watch(
  () => state.dialog.session,
  (open) => {
    if (!open) return;
    // The mode is consumed once: every open starts from a known state instead
    // of inheriting whatever the component was showing last time.
    const mode = state.dialog.sessionMode;
    state.dialog.sessionMode = "edit";
    if (mode === "create" || state.sessions.length === 0) {
      newSession();
      return;
    }
    const preferred = selectedId.value ?? state.sessions[0].session.id;
    selectSession(preferred);
  },
  { immediate: true },
);

function validate(): boolean {
  if (!form.host.trim()) {
    toast("error", "请填写主机地址");
    return false;
  }
  if (!form.user.trim()) {
    toast("error", "请填写用户名");
    return false;
  }
  return true;
}

async function persist(): Promise<Session | null> {
  if (!validate()) return null;
  saving.value = true;
  try {
    const payload: Session = {
      ...form,
      name: form.name.trim() || `${form.user.trim()}@${form.host.trim()}`,
      host: form.host.trim(),
      user: form.user.trim(),
      port: Number(form.port) || 22,
      remotePath: form.remotePath?.trim() || ".",
    };
    const saved = await saveSession(payload, password.value, clearPassword.value);
    Object.assign(form, saved);
    selectedId.value = saved.id;
    password.value = "";
    clearPassword.value = false;
    testResult.value = { ok: true, text: "已保存到 sessions.yaml" };
    return saved;
  } catch (error) {
    toast("error", "保存失败", errorText(error));
    return null;
  } finally {
    saving.value = false;
  }
}

async function onTest() {
  if (!validate()) return;
  testing.value = true;
  testResult.value = null;
  try {
    const result = await testSession(form, password.value);
    testResult.value = {
      ok: true,
      text: `连接成功 · ${result.serverVersion || "SSH"} · 主目录 ${result.home}`,
    };
  } catch (error) {
    testResult.value = { ok: false, text: errorText(error) };
  } finally {
    testing.value = false;
  }
}

async function onSave() {
  const saved = await persist();
  if (saved) {
    toast(
      "success",
      "会话已保存",
      `${saved.user}@${saved.host}:${saved.port} · 共 ${state.sessions.length} 个会话`,
    );
  }
}

async function onConnect() {
  const saved = await persist();
  if (!saved) return;
  const ok = await connectSession(saved.id, password.value);
  if (ok) {
    closeSessionDialog();
    closePasswordDialog();
  }
}

function onDelete() {
  if (!form.id) return;
  openConfirm({
    title: "删除会话",
    message: `确定删除“${form.name}”吗？`,
    detail: "只会移除本地保存的连接信息，不会影响远程服务器上的文件。",
    confirmText: "删除",
    danger: true,
    onConfirm: async () => {
      await deleteSession(form.id);
      await loadSessions();
      newSession();
      toast("success", "会话已删除");
    },
  });
}

const authOptions: { value: AuthMethod; label: string }[] = [
  { value: "password", label: "密码" },
  { value: "key", label: "私钥" },
  { value: "agent", label: "ssh-agent" },
];
</script>

<template>
  <Modal
    v-if="state.dialog.session"
    title="会话管理"
    subtitle="SSH / SFTP 连接保存在 sessions.yaml，密码存入系统钥匙串"
    icon="server"
    width="720px"
    @close="closeSessionDialog()"
  >
    <div class="flex gap-4" style="min-height: 380px">
      <!-- list -->
      <div class="flex w-[190px] shrink-0 flex-col">
        <div class="mb-1 flex items-center justify-between px-1">
          <span class="text-[10px] font-semibold uppercase tracking-wide text-muted">
            已保存 {{ state.sessions.length }}
          </span>
          <button class="icon-btn h-5 w-5" title="新建会话" @click="newSession">
            <Icon name="plus" :size="12" />
          </button>
        </div>
        <div class="min-h-0 flex-1 space-y-0.5 overflow-y-auto rounded-[7px] border border-line/60 bg-panel/60 p-1">
          <button
            v-for="item in state.sessions"
            :key="item.session.id"
            class="flex w-full items-center gap-2 rounded-[5px] px-2 py-1.5 text-left transition-colors"
            :class="
              selectedId === item.session.id ? 'bg-accent/[0.13]' : 'hover:bg-ink/[0.05]'
            "
            @click="selectSession(item.session.id)"
          >
            <span class="h-1.5 w-1.5 shrink-0 rounded-full bg-accent/70" />
            <span class="min-w-0 flex-1">
              <span class="block truncate text-[11.5px] font-medium">{{ item.session.name }}</span>
              <span class="block truncate text-[10px] text-muted">{{ item.session.host }}</span>
            </span>
            <span v-if="item.connected" class="h-1 w-1 rounded-full bg-success" />
          </button>
          <p v-if="!state.sessions.length" class="px-2 py-3 text-[10.5px] leading-[1.5] text-muted">
            还没有保存的会话。
          </p>
        </div>
        <button class="mt-1.5 btn h-6 justify-center text-[11px]" @click="newSession">
          <Icon name="plus" :size="11" />
          新建会话
        </button>
      </div>

      <!-- form -->
      <div class="min-w-0 flex-1">
        <div class="mb-3 flex items-center gap-2">
          <span class="text-[12px] font-semibold">{{ isNew ? "新建会话" : form.name }}</span>
          <span class="chip bg-ink/[0.06] text-muted">SFTP</span>
        </div>

        <div class="grid grid-cols-2 gap-x-3 gap-y-2.5">
          <label class="col-span-2 block">
            <span class="mb-1 block text-[10.5px] font-medium text-muted">显示名称</span>
            <input v-model="form.name" class="field" placeholder="例如：生产服务器" spellcheck="false" />
          </label>

          <label class="block">
            <span class="mb-1 block text-[10.5px] font-medium text-muted">主机</span>
            <input v-model="form.host" class="field" placeholder="server.example.com" spellcheck="false" />
          </label>
          <div class="grid grid-cols-[72px_1fr] gap-2">
            <label class="block">
              <span class="mb-1 block text-[10.5px] font-medium text-muted">端口</span>
              <input v-model.number="form.port" type="number" class="field" min="1" max="65535" />
            </label>
            <label class="block">
              <span class="mb-1 block text-[10.5px] font-medium text-muted">用户名</span>
              <input v-model="form.user" class="field" placeholder="root" spellcheck="false" />
            </label>
          </div>

          <label class="block">
            <span class="mb-1 flex h-[14px] items-center text-[10.5px] font-medium text-muted">认证方式</span>
            <select v-model="form.authMethod" class="field">
              <option v-for="option in authOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </label>

          <label v-if="form.authMethod === 'password'" class="block">
            <span class="mb-1 flex h-[14px] items-center text-[10.5px] font-medium text-muted">密码</span>
            <input
              v-model="password"
              type="password"
              class="field"
              :placeholder="hasStoredPassword ? '已保存，留空则不修改' : '输入密码'"
              autocomplete="off"
            />
          </label>

          <label v-else-if="form.authMethod === 'key'" class="col-span-2 block">
            <span class="mb-1 block text-[10.5px] font-medium text-muted">私钥路径</span>
            <input v-model="form.keyPath" class="field font-mono" placeholder="~/.ssh/id_ed25519" spellcheck="false" />
            <span class="mt-1 block text-[10px] text-muted/80">
              支持 OpenSSH / PEM 格式。私钥有口令时，把口令填入下方密码框。
            </span>
          </label>

          <div v-else class="col-span-2 rounded-[7px] border border-line/60 bg-ink/[0.02] px-2.5 py-2 text-[10.5px] leading-[1.5] text-muted">
            将通过 <code class="font-mono">SSH_AUTH_SOCK</code> 使用当前登录会话的 ssh-agent 密钥。
          </div>

          <label v-if="form.authMethod === 'key'" class="block">
            <span class="mb-1 flex h-[14px] items-center text-[10.5px] font-medium text-muted">私钥口令（可选）</span>
            <input v-model="password" type="password" class="field" placeholder="未加密则留空" autocomplete="off" />
          </label>

          <label class="block">
            <span class="mb-1 flex h-[14px] items-center text-[10.5px] font-medium text-muted">远程起始目录</span>
            <input v-model="form.remotePath" class="field font-mono" placeholder="." spellcheck="false" />
          </label>

          <div class="col-span-2 flex items-center gap-3">
            <span class="text-[10.5px] font-medium text-muted">标签颜色</span>
            <button
              v-for="color in COLORS"
              :key="color"
              class="h-3.5 w-3.5 rounded-full border-2 transition-transform"
              :class="[
                {
                  blue: 'bg-[#3b99f7]',
                  green: 'bg-[#31b56f]',
                  purple: 'bg-[#a259e6]',
                  orange: 'bg-[#e89a2b]',
                  red: 'bg-[#e05555]',
                  gray: 'bg-muted',
                }[color],
                form.color === color ? 'border-ink/70 scale-110' : 'border-transparent',
              ]"
              :title="color"
              @click="form.color = color"
            />
            <label v-if="hasStoredPassword" class="ml-2 flex items-center gap-1.5 text-[10.5px] text-muted">
              <input v-model="clearPassword" type="checkbox" class="accent-[rgb(var(--c-accent))]" />
              清除已保存密码
            </label>
          </div>
        </div>

        <div
          v-if="testResult"
          class="mt-3 flex items-start gap-2 rounded-[7px] px-2.5 py-2 text-[11px]"
          :class="testResult.ok ? 'bg-success/[0.08] text-success' : 'bg-danger/[0.07] text-danger'"
        >
          <Icon :name="testResult.ok ? 'check' : 'alert'" :size="12" class="mt-0.5 shrink-0" />
          <span class="break-words leading-[1.45]">{{ testResult.text }}</span>
        </div>
      </div>
    </div>

    <template #footer>
      <button v-if="!isNew" class="btn-ghost-danger mr-auto" @click="onDelete">
        <Icon name="trash" :size="12" />
        删除
      </button>
      <button class="btn" :disabled="testing" @click="onTest">
        <Icon name="plug" :size="12" :class="{ 'animate-pulse': testing }" />
        {{ testing ? "正在连接…" : "测试连接" }}
      </button>
      <button class="btn" :disabled="saving" @click="onSave">保存</button>
      <button class="btn-primary" :disabled="saving" @click="onConnect">保存并连接</button>
    </template>
  </Modal>
</template>
