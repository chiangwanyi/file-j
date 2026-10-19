<script setup lang="ts">
import { computed } from "vue";
import Modal from "./Modal.vue";
import Icon from "./Icon.vue";
import Toggle from "./Toggle.vue";
import { state, toast, updateConfig } from "../stores/workspace";
import { rpc } from "../lib/ipc";

const config = computed(() => state.config);

async function setUI(key: string, value: unknown) {
  try {
    await updateConfig({ ui: { [key]: value } as never });
  } catch (error) {
    toast("error", "保存设置失败", String(error));
  }
}

async function setTransfer(key: string, value: unknown) {
  try {
    await updateConfig({ transfer: { [key]: value } as never });
  } catch (error) {
    toast("error", "保存设置失败", String(error));
  }
}

async function setRemote(key: string, value: unknown) {
  try {
    await updateConfig({ remote: { [key]: value } as never });
  } catch (error) {
    toast("error", "保存设置失败", String(error));
  }
}

async function revealConfig() {
  const path = state.paths?.configDir;
  if (!path) return;
  try {
    await rpc.request("fs.reveal", { path });
  } catch (error) {
    toast("error", "无法打开配置目录", String(error));
  }
}
</script>

<template>
  <Modal
    v-if="state.dialog.settings"
    title="设置"
    subtitle="所有选项即时写入 config.yaml"
    icon="settings"
    width="620px"
    @close="state.dialog.settings = false"
  >
    <div class="space-y-5">
      <!-- appearance -->
      <section>
        <h3 class="mb-2 text-[11px] font-semibold text-ink/80">外观</h3>
        <div class="divide-y divide-line/50 rounded-[8px] border border-line/60 bg-panel/50">
          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">主题</p>
              <p class="text-[10.5px] text-muted">跟随系统或强制浅色 / 深色</p>
            </div>
            <div class="segmented">
              <button
                v-for="option in [
                  { value: 'system', label: '系统' },
                  { value: 'light', label: '浅色' },
                  { value: 'dark', label: '深色' },
                ]"
                :key="option.value"
                :data-active="config.ui.theme === option.value"
                @click="setUI('theme', option.value as 'system' | 'light' | 'dark')"
              >
                {{ option.label }}
              </button>
            </div>
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">列表密度</p>
              <p class="text-[10.5px] text-muted">紧凑模式可在一屏显示更多文件</p>
            </div>
            <div class="segmented">
              <button
                :data-active="config.ui.density === 'comfortable'"
                @click="setUI('density', 'comfortable')"
              >
                宽松
              </button>
              <button :data-active="config.ui.density === 'compact'" @click="setUI('density', 'compact')">
                紧凑
              </button>
            </div>
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">显示隐藏文件</p>
              <p class="text-[10.5px] text-muted">包括以 . 开头的文件</p>
            </div>
            <Toggle :model-value="config.ui.showHiddenFiles" @update:model-value="setUI('showHiddenFiles', $event)" />
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">删除前确认</p>
              <p class="text-[10.5px] text-muted">本地删除会移到废纸篓，远程删除不可恢复</p>
            </div>
            <Toggle :model-value="config.ui.confirmDelete" @update:model-value="setUI('confirmDelete', $event)" />
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">日期格式</p>
              <p class="text-[10.5px] text-muted">影响文件列表的修改时间列</p>
            </div>
            <div class="segmented">
              <button :data-active="config.ui.dateFormat === 'localized'" @click="setUI('dateFormat', 'localized')">
                本地化
              </button>
              <button :data-active="config.ui.dateFormat === 'iso'" @click="setUI('dateFormat', 'iso')">
                ISO
              </button>
            </div>
          </div>
        </div>
      </section>

      <!-- transfer -->
      <section>
        <h3 class="mb-2 text-[11px] font-semibold text-ink/80">传输</h3>
        <div class="divide-y divide-line/50 rounded-[8px] border border-line/60 bg-panel/50">
          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">并发任务数</p>
              <p class="text-[10.5px] text-muted">同时进行的传输任务，连接复用同一条 SSH</p>
            </div>
            <select
              class="field w-[72px]"
              :value="config.transfer.concurrency"
              @change="setTransfer('concurrency', Number(($event.target as HTMLSelectElement).value))"
            >
              <option v-for="n in 8" :key="n" :value="n">{{ n }}</option>
            </select>
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">目标已存在时</p>
              <p class="text-[10.5px] text-muted">对所有传输任务生效</p>
            </div>
            <select
              class="field w-[116px]"
              :value="config.transfer.conflict"
              @change="setTransfer('conflict', ($event.target as HTMLSelectElement).value as never)"
            >
              <option value="overwrite">覆盖</option>
              <option value="skip">跳过</option>
              <option value="keepboth">保留两者</option>
              <option value="fail">中止任务</option>
            </select>
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">保留修改时间</p>
              <p class="text-[10.5px] text-muted">复制后同步源文件的 mtime</p>
            </div>
            <Toggle :model-value="config.transfer.preserveTimes" @update:model-value="setTransfer('preserveTimes', $event)" />
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">保留权限位</p>
              <p class="text-[10.5px] text-muted">尽量沿用源文件的 rwx 权限</p>
            </div>
            <Toggle
              :model-value="config.transfer.preservePermissions"
              @update:model-value="setTransfer('preservePermissions', $event)"
            />
          </div>
        </div>
      </section>

      <!-- ssh -->
      <section>
        <h3 class="mb-2 text-[11px] font-semibold text-ink/80">SSH</h3>
        <div class="divide-y divide-line/50 rounded-[8px] border border-line/60 bg-panel/50">
          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">主机密钥校验</p>
              <p class="text-[10.5px] text-muted">strict 仅信任 known_hosts.yaml 中已有的指纹</p>
            </div>
            <select
              class="field w-[116px]"
              :value="config.remote.hostKeyPolicy"
              @change="setRemote('hostKeyPolicy', ($event.target as HTMLSelectElement).value as never)"
            >
              <option value="tofu">首次信任 (TOFU)</option>
              <option value="strict">严格校验</option>
              <option value="insecure">不校验</option>
            </select>
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">连接超时</p>
              <p class="text-[10.5px] text-muted">秒</p>
            </div>
            <input
              class="field w-[72px] text-right"
              type="number"
              min="3"
              max="120"
              :value="config.remote.connectTimeoutSeconds"
              @change="setRemote('connectTimeoutSeconds', Number(($event.target as HTMLInputElement).value))"
            />
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">心跳间隔</p>
              <p class="text-[10.5px] text-muted">秒，0 表示关闭 keepalive</p>
            </div>
            <input
              class="field w-[72px] text-right"
              type="number"
              min="0"
              max="600"
              :value="config.remote.keepAliveSeconds"
              @change="setRemote('keepAliveSeconds', Number(($event.target as HTMLInputElement).value))"
            />
          </div>

          <div class="flex items-center justify-between px-3 py-2">
            <div>
              <p class="text-[11.5px]">空闲断开</p>
              <p class="text-[10.5px] text-muted">分钟，0 表示保持连接</p>
            </div>
            <input
              class="field w-[72px] text-right"
              type="number"
              min="0"
              max="240"
              :value="config.remote.idleDisconnectMinutes"
              @change="setRemote('idleDisconnectMinutes', Number(($event.target as HTMLInputElement).value))"
            />
          </div>
        </div>
      </section>

      <!-- config files -->
      <section>
        <h3 class="mb-2 text-[11px] font-semibold text-ink/80">配置文件</h3>
        <div class="rounded-[8px] border border-line/60 bg-panel/50 px-3 py-2.5">
          <div class="mb-2 flex items-center gap-2">
            <Icon name="file-text" :size="12" class="text-muted" />
            <span class="flex-1 truncate font-mono text-[10.5px] text-muted">
              {{ state.paths?.configDir ?? state.configDir }}
            </span>
            <button class="btn h-6 text-[11px]" @click="revealConfig">在 Finder 中显示</button>
          </div>
          <ul class="space-y-0.5 font-mono text-[10px] text-muted/80">
            <li>config.yaml · 外观 / 传输 / SSH 参数</li>
            <li>sessions.yaml · 会话定义（不含密码）</li>
            <li>ui-state.yaml · 双栏路径与排序</li>
            <li>known_hosts.yaml · 主机密钥指纹</li>
          </ul>
        </div>
      </section>

      <p class="text-[10px] text-muted/70">
        file-j {{ state.backendVersion || "0.1.0" }} · Go 后端 + Tauri 2 外壳 · 配置不使用数据库
      </p>
    </div>

    <template #footer>
      <button class="btn-primary" @click="state.dialog.settings = false">完成</button>
    </template>
  </Modal>
</template>
