# file-j

WinSCP 式双栏文件管理器，现代 macOS 风格，支持本地文件管理与 SSH/SFTP 远程传输。

- **界面**：Vue 3 + TypeScript + Tailwind CSS
- **外壳**：Tauri 2（Rust 仅负责窗口与 sidecar 生命周期）
- **业务**：Go 1.26.9（文件系统、SSH/SFTP、传输引擎、YAML 配置）
- **持久化**：纯 YAML，无数据库
- **系统要求**：macOS 12.0 及以上

## 功能

| 功能 | 说明 |
| --- | --- |
| 双栏浏览 | 左右面板可各自为本地或远程，路径导航、历史前进后退、排序、多选 |
| 本地管理 | 复制、移动、重命名、新建文件夹、删除（进废纸篓）、在 Finder 中显示 |
| 远程管理 | SSH/SFTP 连接复用、密码/私钥/ssh-agent、主机密钥 TOFU 校验、权限与容量展示 |
| 文件传输 | 本地↔本地复制移动、上传、下载，队列并发、实时进度与速度、取消、重试、四种冲突策略 |
| 操作方式 | 拖拽跨面板传输（按住 ⌥ 为移动）、F5 复制、F6 移动、右键菜单 |
| 外观 | 跟随系统/浅色/深色，紧凑/宽松密度 |

## 快速开始

```bash
pnpm install

# 完整桌面应用
pnpm tauri dev

# 仅前端调试：先起后端，再起 Vite
pnpm backend:run     # ws://127.0.0.1:47121, token=dev, 配置写入 ./.devconfig
pnpm dev             # http://localhost:1420
```

打包：

```bash
pnpm tauri build     # 生成 .app 与 .dmg
```

## 测试

```bash
pnpm backend:test    # Go 测试，包含进程内真实 SSH/SFTP 服务器的端到端用例
pnpm build           # vue-tsc 类型检查 + 生产构建
```

## 本地演示服务器

没有可用的 SSH 主机时，可以启动内置演示服务器（仅用于开发，会以当前用户权限暴露本机文件）：

```bash
cd backend
go run ./cmd/demo-sshd --addr 127.0.0.1:2222 --user demo --password demo
```

然后在应用中新建会话：主机 `127.0.0.1`、端口 `2222`、用户 `demo`、密码 `demo`。

## 架构

分层职责、WebSocket 协议、YAML 结构、各功能实现边界与 macOS 12 兼容性约束，见
[ARCHITECTURE.md](./ARCHITECTURE.md)。

## 配置位置

```
~/Library/Application Support/file-j/
├── config.yaml
├── sessions.yaml
├── ui-state.yaml
├── known_hosts.yaml
└── logs/backend.log
```
