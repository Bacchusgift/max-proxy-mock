# AI / 开发者接手指南

此文档用于让新的 AI 或开发者在较少上下文下安全接手。开始修改前，请同时阅读根目录 `README.md`、`docs/architecture.md` 和 `docs/roadmap.md`。

## 1. 一句话状态

这是一个可以实际运行的 macOS Tauri 桌面代理原型：录制和 Mock 已可用；自动契约对比、完整测试体系、schema migration 和团队同步尚未实现。

## 2. 当前主线与遗留代码

- **主线**：`web/` + `src-tauri/`。
- **遗留**：`main.go`、`internal/`、根目录 `index.html`、`data/`。
- Rust 启动时如果桌面数据库为空，会尝试从旧 Go 数据库导入。
- 不要在没有迁移计划的情况下同时修改 Rust 与 Go 两套实现。
- 新功能默认只实现到 Rust/Tauri 主线；如确需维持 Go 兼容，必须明确说明。

## 3. 关键事实

### 运行地址

- 代理：`127.0.0.1:8899`
- PAC/health：`127.0.0.1:8900`
- PAC URL：`http://127.0.0.1:8900/proxy.pac`
- Bundle ID：`dev.maxproxy.mock`

### 数据位置

```text
~/Library/Application Support/dev.maxproxy.mock/max-proxy-mock.db
~/Library/Application Support/dev.maxproxy.mock/certificates/
```

不要把仓库根目录 `data/` 当成当前桌面数据目录。

### UI 与内核通信

React 的 `api()` 在 Tauri 环境调用 `invoke("api_call")`。Rust 在 `commands.rs` 中以 method + path 字符串做内部路由。Rust 更新数据后 emit `data-changed`，UI 收到后重新拉取。

### 代理行为

1. Mock 匹配优先。
2. 命中启用 Mock 后直接返回，不访问后端。
3. 没命中时才转发。
4. 只有录制开启且域名匹配时才保存请求/响应。
5. endpoint 当前按 `project_id + path` 去重，而不是 method + path。

## 4. 已知问题与风险排序

### P0：仓库安全

仓库中存在旧版 `data/certificates/max-proxy-ca.key` 和数据库文件。分享、提交或开源前必须确认这些文件已移除并加入忽略规则。不要读取、展示或复制私钥内容。

### P0：数据库唯一键

当前：

```sql
UNIQUE(project_id, path)
```

这会让相同 Path 的 GET/POST 相互覆盖。不能只改 SQL 创建语句，因为已有用户数据库不会重建；必须先增加 migration 机制，再迁移旧表。

### P0：契约校验尚未实现

产品目标要求真实响应与 Mock 一致时显示 OK，但当前 Mock 会短路后端。推荐先完成仅安全方法的 shadow request + JSON 结构化比较，不要镜像写请求。

### P1：测试不足

Rust 主线几乎没有自动化测试。修改代理匹配、PAC、证书或数据库前先增加可测试边界。

### P1：Body 全量缓冲

存储预览限制 2 MiB，但 request/response body 仍会先全部 `collect()`。大文件和流式协议可能导致高内存或阻塞。

### P1：Mock 查询效率

每个请求都会读取全部 Mock 规则。规则多时应使用内存索引，并在数据库变更时原子刷新。

### P2：前端单文件偏大

`App.tsx` 同时承担页面、状态、API 适配和多个 Modal。继续开发契约视图前应按 feature 拆分，但不要在功能修复中做无关的大规模重构。

## 5. 推荐接手顺序

1. 运行基础检查并确认当前 app 能启动。
2. 阅读本次任务会触及的 Rust 模块和对应 UI。
3. 为目标行为补最小测试或可重复验证步骤。
4. 进行范围最小的修改。
5. 验证 TypeScript、Rust、前端构建和真实 Tauri 窗口。
6. 若涉及代理/PAC，检查退出后的系统代理是否已恢复。
7. 更新 README、架构或路线图中的相关事实。

## 6. 常用命令

```bash
npm install
npm run tauri dev
make check
npm run build
npm run tauri build -- --debug
make legacy-test
```

GitHub Release 由 `.github/workflows/release.yml` 负责。推送与应用版本一致的 `v*` 标签会构建 macOS arm64 DMG 和 Windows x64 NSIS EXE。不要在应用版本未更新时重复使用旧标签；已推送的发布标签不应被强制移动。

开发脚本会清理属于本项目的旧端口进程。不要使用宽泛的 `killall`，也不要终止无法确认归属的端口进程。

## 7. 修改规则

### 代理与 Mock

- 保持非目标域名直连。
- 不破坏原响应状态、Headers 和 Body。
- 修改 Body 时处理 Content-Length、Content-Encoding 和 Content-Type。
- 对流式、压缩、大文件和错误响应明确行为。
- 不自动重放具有副作用的方法。

### 证书

- 不提交或输出 CA 私钥。
- 证书“已存在”不等于“已受 SSL 信任”；状态必须验证本地证书与钥匙串证书完全匹配。
- 自动安装/删除证书属于安全敏感动作，UI 应要求明确用户操作。

### 系统代理

- 修改前保存原状态。
- 恢复时只恢复本工具保存的配置。
- 不把 Wi-Fi 写死为唯一网络服务；当前代码会根据默认路由查找服务。
- 测试失败或退出时确认 PAC 未意外保持开启。

### 数据库

- 所有 schema 变更必须有版本化 migration。
- migration 必须支持已有数据，先备份/事务化，再替换约束。
- 保持 foreign keys 和级联删除语义。

### UI

- 使用 Radix primitives 承载 Select、Tooltip、Dialog/Menu 等复杂交互语义。
- 保持 macOS overlay titlebar 与左侧栏的一体化外观。
- 字体不要退回早期的小字号；在最小窗口 `1080 × 700` 验证。
- 每次修复点击问题后，用真实 Tauri WebView 验收，而不仅看普通浏览器。

## 8. 最小验收清单

### 静态检查

- [ ] `npx tsc --noEmit`
- [ ] `npm run build`
- [ ] `cargo check --manifest-path src-tauri/Cargo.toml --locked`
- [ ] 新增 Rust 测试时执行 `cargo test --manifest-path src-tauri/Cargo.toml --locked`

### 桌面行为

- [ ] 应用启动，窗口不是空白。
- [ ] 新建/切换项目有效。
- [ ] Modal、Select、Tooltip 可点击且键盘可用。
- [ ] 录制状态能开始和停止。
- [ ] 接口列表会因新流量更新。
- [ ] Mock 保存后下一次请求命中。
- [ ] 应用图标在 Finder/Dock 正常。

### 网络与安全

- [ ] `8900/health` 返回 `ok`。
- [ ] PAC 只包含项目域名。
- [ ] 其他域名保持 DIRECT。
- [ ] HTTPS CA 状态与钥匙串真实状态一致。
- [ ] 测试结束后系统 PAC 已恢复。
- [ ] 日志与提交内容没有证书私钥或敏感 Header。

## 9. 契约功能建议数据结构

实现前应通过 migration 新增独立实体，不要把 Diff 塞回 endpoint 的 response body：

```text
contract_versions
  id, endpoint_id, version, expected_status, expected_headers,
  expected_body, rules, created_at

verification_runs
  id, endpoint_id, contract_version_id, source, actual_status,
  actual_headers, actual_body_preview, result, diff, duration_ms, created_at
```

建议 `result`：`compatible | drift | backend_error | skipped`。状态计算应基于最新契约版本与最新有效验证，不要用 UI 临时状态代替持久化结论。

## 10. 完成任务时的交接格式

建议每次交付说明：

1. 实现了什么用户结果。
2. 修改了哪些模块。
3. 运行了哪些检查及结果。
4. 是否影响 PAC、证书、数据 schema 或兼容性。
5. 尚未覆盖的边界和下一步建议。
