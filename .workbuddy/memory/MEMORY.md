# file-j 项目长期约定

## 顶栏（Toolbar）
- 顶栏高度是唯一约定值：`Toolbar.vue` 的 `h-[52px]` ↔ `src-tauri/src/lib.rs` 的 `TITLE_BAR_HEIGHT`，
  改一边就要改另一边，否则 macOS 红绿灯会偏离中线。
- 左内边距 `pl-[80px]` 是给系统红绿灯留的位置，不要压缩。
- 顶栏内所有控件高度 ≤28px（`h-7`），靠 `items-center` 对齐到中线；不要再往顶栏塞品牌 logo/应用名。

## 分层
- 业务只在 Go（`backend/`）；Rust 仅窗口/进程/原生外观微调；Vue 只做展示与交互编排。
