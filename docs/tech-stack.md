# 技术栈与关键决策

## 1. 技术栈总览

| 层级 | 技术 | 当前用途 |
| --- | --- | --- |
| 桌面容器 | Tauri 2 | macOS 窗口、WebView、IPC、事件、应用目录、打包 |
| 核心语言 | Rust 2021 | 代理、证书、PAC、持久化和系统集成 |
| 异步运行时 | Tokio | 启动 MITM Proxy 与 PAC Server |
| 代理内核 | hudsucker（仓库内 vendor） | HTTP/HTTPS 代理、CONNECT/TLS 拦截、请求响应 Hook |
| TLS/证书 | rustls + rcgen | 根 CA 与目标站点证书动态签发 |
| 本地 HTTP | Axum 0.8 | 提供 PAC 文件与健康检查 |
| 数据库 | SQLite + rusqlite bundled | 项目、接口、Mock 和本地设置持久化 |
| 序列化 | Serde / serde_json | Rust 模型、IPC payload、Headers JSON |
| 标识/时间 | uuid / chrono | 实体 ID 与时间戳 |
| 前端 | React 19 + TypeScript | 桌面管理界面 |
| 构建 | Vite 8 | 前端开发与静态资源打包 |
| UI primitives | Radix UI | Select、Tooltip、Separator 等无样式可访问组件 |
| 图标 | Lucide React | 统一线性图标体系 |
| 旧实现 | Go | 迁移参考与旧数据来源，不是当前主线 |

精确 Rust 版本以 `src-tauri/Cargo.lock` 为准，前端版本以 `package-lock.json` 为准。

## 2. 为什么选择 Tauri + Rust

### 对本项目合适的原因

- 代理、TLS、证书与系统网络设置天然适合由本地原生进程处理。
- Rust 可以在同一进程中安全承载异步代理、SQLite 和桌面 IPC。
- Tauri 使用系统 WebView，应用体积和常驻内存通常低于完整 Chromium 桌面壳。
- React 让复杂管理界面可以复用成熟的组件、状态和前端工程生态。
- 本地执行面未来可与远端团队控制面分离，不需要重写代理核心。

### 代价

- Tauri WebView 行为依赖操作系统版本，需要真实桌面环境验收。
- Rust 代理与 TLS 调试门槛高于纯 Node 方案。
- macOS 证书和网络配置涉及系统权限、钥匙串与浏览器缓存。
- `hudsucker` 当前以源码 vendor 固定，升级需要人工合并并做完整回归。

## 3. Rust 依赖职责

### `tauri`

负责窗口生命周期、Tauri command、event、路径解析和应用 bundle。UI 不通过公网管理 API 访问内核，而是调用 `api_call` command。

### `hudsucker`

提供 HTTP/HTTPS 代理骨架和 `HttpHandler` Hook。项目使用：

- `should_intercept_connect` / `should_intercept_tls` 选择性 MITM。
- `handle_request` 做 Mock 匹配和请求录制准备。
- `handle_response` 保存真实响应并原样返回。

源码位于 `src-tauri/vendor/hudsucker/`。不要在未说明原因时直接替换为 crates.io 版本。

### `rustls` / `rcgen`

由 hudsucker 重新导出。`RcgenAuthority` 根据项目 CA 动态为目标域名签发证书；`ring` provider 用于 TLS 加密实现。

### `rusqlite` with `bundled`

将 SQLite 编译进应用，避免依赖用户机器的系统 SQLite 版本。当前为单连接 + Mutex，适合单机早期版本。

### `axum`

只暴露 loopback PAC 与健康检查。管理业务不应该继续堆进 Axum；桌面版的管理操作应优先使用 typed Tauri commands。

## 4. 前端技术决策

### React + TypeScript

当前 UI 集中在 `web/src/App.tsx`，便于原型快速推进，但已经接近需要拆分的临界点。推荐逐步拆为：

```text
web/src/
├── app/                 # App shell、providers、路由/页面状态
├── features/
│   ├── projects/
│   ├── recording/
│   ├── endpoints/
│   ├── mocks/
│   └── proxy-setup/
├── components/          # 通用 UI primitives
├── lib/tauri-api.ts     # typed invoke client
└── styles/              # tokens、components、pages
```

### Radix UI

Radix 提供交互语义、键盘操作和 Portal，不决定视觉风格。目前使用：

- `@radix-ui/react-select`
- `@radix-ui/react-tooltip`
- `@radix-ui/react-separator`

新增弹层、菜单、开关等复杂控件时优先继续使用 Radix primitive，不要手写不具备焦点管理的 div 模拟控件。

### CSS

当前使用单个 `styles.css` 和深色桌面主题。后续应先抽离设计 token，再按组件拆分；避免引入第二套完整组件库导致样式与交互不一致。

## 5. 数据与接口约定

### 前端 API 适配层

`App.tsx` 中的 `api<T>()`：

- Tauri 环境：`invoke("api_call", { method, path, body })`。
- 普通浏览器环境：使用相同 Path 调用 HTTP，主要为旧版/开发兼容。

这种方式便于迁移，但字符串路径缺少编译期保护。推荐演进为独立的 typed client，例如：

```ts
projects.list()
projects.create(input)
recording.start(input)
mocks.update(id, patch)
```

### SQLite

- WAL 模式提高读写并发体验。
- Foreign keys 开启，删除项目会级联删除 endpoints 和 mock_rules。
- Headers 以 JSON 字符串保存，便于早期迭代；需要检索/脱敏时应在领域层处理。
- 目前没有 schema version。引入任何表结构修改前必须先实现迁移表或使用专用迁移方案。

## 6. 构建与运行

| 命令 | 作用 |
| --- | --- |
| `npm run tauri dev` | 启动 Vite 与 Tauri 开发应用 |
| `npm run tauri build` | 构建正式桌面应用 |
| `npm run tauri build -- --debug` | 构建可调试应用包 |
| `npm run build` | 仅构建 React 静态资源 |
| `npx tsc --noEmit` | TypeScript 类型检查 |
| `cargo check --manifest-path src-tauri/Cargo.toml --locked` | Rust 编译检查 |
| `make check` | 执行前端与 Rust 基础检查 |
| `make legacy-test` | 运行旧 Go 实现测试 |

`scripts/tauri.mjs` 会在 `dev` 前检查 `8899/8900`。它只结束属于当前项目的旧实例，不会擅自关闭其他应用。

## 7. 版本与依赖策略

当前 `package.json` 对 React、Vite、Lucide 等使用了 `latest`，锁文件可以保证本机复现，但这不是稳定的长期策略。进入 Beta 前应：

1. 把直接依赖改为明确 semver 范围。
2. 在 CI 中使用 `npm ci` 和 `cargo ... --locked`。
3. 每次 Tauri、Vite、React 或 hudsucker 升级单独提交并记录回归项。
4. 对代理/TLS 依赖保留已知可工作的锁定版本。

## 8. 不建议的技术方向

- 不要让前端 WebView 直接修改系统代理；系统集成必须留在 Rust command。
- 不要为了团队版把流量代理迁移到云端；本地代理应作为安全隔离的执行面。
- 不要默认镜像 POST/PUT/PATCH/DELETE 做契约校验，避免产生重复副作用。
- 不要用字符串替换比较 JSON；应先规范化，再按结构、类型和可配置规则比较。
- 不要把 CA 私钥、Cookie、Authorization 或完整生产响应同步到团队服务。
