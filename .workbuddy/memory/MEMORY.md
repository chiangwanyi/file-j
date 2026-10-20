# file-j 项目长期约定

## 目标平台
- **macOS 12.0+ 与 Windows 11 x64 双平台**，同一套代码，不分叉。
- 平台差异只允许出现在三处：Go 的 `//go:build` 成对文件、Rust 的 `#[cfg]` 与
  `tauri.<platform>.conf.json`、前端的 `src/lib/platform.ts`。
  **业务代码里不允许出现 `runtime.GOOS` 分支。**
- 完整的平台能力对照表、路径方言规则、窗口外壳差异见 ARCHITECTURE.md 第 6 节。

## 顶栏（Toolbar）
- 顶栏高度是唯一约定值：`Toolbar.vue` 的 `h-[52px]` ↔ `src-tauri/src/lib.rs` 的 `TITLE_BAR_HEIGHT`，
  改一边就要改另一边，否则 macOS 红绿灯会偏离中线。
- 左内边距 **按平台分叉**：macOS `pl-[80px]` 给系统红绿灯；Windows 无红绿灯，改 `pl-3`，
  窗口按钮由前端自绘在右侧。不要把 `pl-[80px]` 改成两平台通用。
- 顶栏内所有控件高度 ≤28px（`h-7`），靠 `items-center` 对齐到中线；不要再往顶栏塞品牌 logo/应用名。
- 键位提示、系统名（Finder / 文件资源管理器）一律取 `src/lib/platform.ts` 的常量，
  不要在组件里硬编码 `⌘` 或 `Ctrl`。

## 路径
- 后端同时处理两种方言：**本地路径**跟随宿主系统，**SFTP 路径**永远是 POSIX。
  `fsx` 的助手函数按**输入本身**判断方言（盘符或 `\\` 前缀 = Windows 风格），不要改成按平台判断。
- 前端 `src/lib/format.ts` 的 `basename/direname/joinPath/isInside` 用同一套判定。
- 「回到根目录」问后端 `fs.root`，不要硬编码 `/`（Windows 上应返回 `C:\`）。

## 分层
- 业务只在 Go（`backend/`）；Rust 仅窗口/进程/原生外观微调；Vue 只做展示与交互编排。

## 构建
- sidecar 构建入口是 `node scripts/build-backend.mjs`（`pnpm backend:build`）。
  `scripts/build-backend.sh` 只是转发壳，因为 Windows 上不保证有 bash。
- 新增脚本一律用 Node 或跨平台命令，不要引入 `.sh` 作为唯一实现。
- 全仓 LF（`.gitattributes`），只有 `*.bat/*.cmd/*.ps1` 例外。
