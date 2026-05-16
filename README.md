# real-browser-cli

`real-browser-cli` 是一个面向 agent 的真实 Chrome/Edge 控制 CLI。它通过本地 daemon 和浏览器扩展连接用户已经打开的浏览器，复用登录态、cookies、扩展和真实浏览器环境。

v1.0.0 目标是提供一套稳定的命令行协议，让 agent 可以用可读、可审计、可脚本化的方式完成网页导航、页面观察、元素操作、状态读取、网络/控制台调试和批处理。

## 核心能力

- 连接真实浏览器，复用用户现有登录态和站点状态。
- 使用稳定 tab handle，例如 `t1`、`t2` 或用户设置的 label。
- 使用 `snapshot` 生成元素引用，例如 `@e1`、`@e2`，避免依赖脆弱的真实 Chrome tab id。
- 提供高层动作命令：`click`、`fill`、`press`、`wait`、`get text` 等。
- 支持 cookies、storage、console、errors、network、dialog、screenshot、PDF、trace、export、batch。
- 默认输出面向人阅读；`--json` 输出完整统一响应；`--quiet` 输出脚本友好的关键值。

## 安装

### 从 Release 安装

在 v1.0.0 Release 页面下载与你的平台匹配的文件：

| 系统 | 架构 | Release asset |
|---|---|---|
| macOS | arm64 | `real-browser-darwin-arm64` |
| macOS | x86_64 | `real-browser-darwin-amd64` |
| Linux | x86_64 | `real-browser-linux-amd64` |
| Linux | arm64 | `real-browser-linux-arm64` |
| Windows | x86_64 | `real-browser-windows-amd64.exe` |

Release 同时提供 `checksums.txt`。下载后建议校验 SHA256，再把二进制放到 `PATH` 中并命名为 `real-browser`。

### 从源码构建

```bash
go build -o real-browser ./cmd/real-browser
./real-browser version
```

## 快速开始

1. 释放浏览器扩展文件：

```bash
real-browser plugin update
real-browser plugin path --quiet
```

2. 在 Chrome/Edge 扩展管理页开启开发者模式，加载 `plugin path --quiet` 输出的目录。

3. 启动 daemon 并检查连接：

```bash
real-browser daemon start
real-browser doctor
real-browser tab list
```

4. 执行第一个浏览器任务：

```bash
real-browser open https://example.com
real-browser snapshot
real-browser get title
real-browser get text
```

## Agent 工作流

推荐 agent 按固定流程操作页面：

```bash
real-browser daemon start
real-browser doctor
real-browser open <url>
real-browser snapshot
real-browser click @e1
real-browser fill @e2 "hello@example.com"
real-browser press Enter
real-browser wait --text "Success"
real-browser get text
```

规则：

- 先用 `doctor` 确认 daemon 和扩展连接状态。
- 多 tab 时先 `tab list`，再 `tab use <tab>` 固定目标。
- 操作元素前先 `snapshot`，优先使用 `@eN`。
- `@eN` 失效、页面变化或目标找不到时，重新 `snapshot`。
- 默认使用高层命令；只有高层命令无法表达时才使用 `eval` 或 `cdp`。
- 不输出或复制 runtime token。

## 输出规范

默认输出面向人阅读，只打印摘要、表格、路径或内容本身。

```bash
real-browser get title
real-browser tab list
real-browser screenshot page.png
```

`--quiet` 只输出关键值，适合 shell 脚本：

```bash
real-browser plugin path --quiet
real-browser tab active --quiet
```

`--json` 输出完整统一响应，适合机器解析。成功响应：

```json
{
  "id": "uuid",
  "success": true,
  "data": {
    "title": "Example Domain"
  },
  "meta": {
    "command": "get",
    "durationMs": 12,
    "activeTab": "t1",
    "warnings": []
  }
}
```

失败响应：

```json
{
  "id": "uuid",
  "success": false,
  "error": {
    "code": "target_not_found",
    "message": "target not found: @e4",
    "retryable": false,
    "details": {}
  },
  "meta": {
    "command": "action.click",
    "durationMs": 12,
    "activeTab": "t1",
    "warnings": []
  }
}
```

顶层字段固定为 `id`、`success`、`data`、`error`、`meta`。业务结果放入 `data`，请求元信息放入 `meta`，错误统一放入 `error`。字段名使用 lowerCamelCase。

## 完整命令

### Root / Update

```bash
real-browser version
real-browser update
real-browser update <tag>
real-browser update --dry-run
real-browser update --force
real-browser doctor
real-browser doctor --json
```

### Daemon

```bash
real-browser daemon start
real-browser daemon stop
real-browser daemon restart
real-browser daemon status
real-browser daemon status --json
real-browser daemon token rotate
```

### Plugin

```bash
real-browser plugin update
real-browser plugin path
real-browser plugin path --quiet
```

### Tab

```bash
real-browser tab list
real-browser tab list --json
real-browser tab active
real-browser tab active --quiet
real-browser tab new
real-browser tab new <url>
real-browser tab new <url> --label <label>
real-browser tab new <url> --background
real-browser tab use <tab>
real-browser tab close
real-browser tab close <tab>
real-browser tab label <tab> <label>
```

### Navigation

```bash
real-browser open <url>
real-browser open <url> --tab <tab>
real-browser open <url> --new-tab
real-browser open <url> --new-tab --background
real-browser back
real-browser forward
real-browser reload
real-browser reload --hard
```

`open` 导航当前 tab；`tab new` 和 `open --new-tab` 新建 tab。浏览器可能拒绝把已有 tab 导航到部分受限 URL，此时会返回 `navigation_failed`。

### Snapshot

```bash
real-browser snapshot
real-browser snapshot --tab <tab>
real-browser snapshot --locators
real-browser snapshot --json
real-browser snapshot --selector <css>
real-browser snapshot --text
```

### Get

```bash
real-browser get title
real-browser get url
real-browser get text
real-browser get html
real-browser get markdown
real-browser get value <target>
real-browser get attr <target> <name>
real-browser get box <target>
real-browser get count <target>
real-browser get styles <target>
```

### Action

```bash
real-browser click <target>
real-browser dblclick <target>
real-browser hover <target>
real-browser focus <target>
real-browser fill <target> <text>
real-browser fill <target> <text> --clear
real-browser type <text>
real-browser press <key>
real-browser select <target> <value>
real-browser check <target>
real-browser uncheck <target>
real-browser scroll
real-browser scroll --x <n>
real-browser scroll --y <n>
real-browser scroll --target <target>
real-browser drag <source> <target>
real-browser upload <target> <path>
```

Targets 支持：

```text
@e4
#submit
input[name=email]
role=button[name="Submit"]
text="Login"
```

### Wait

```bash
real-browser wait --ms <n>
real-browser wait <milliseconds>
real-browser wait --text <text>
real-browser wait --selector <css>
real-browser wait --ref <@eN>
real-browser wait --js <expr>
real-browser wait --load domcontentloaded
real-browser wait --load load
real-browser wait --load networkidle
```

### Eval / CDP

```bash
real-browser eval <js>
real-browser eval --file <file>
real-browser eval --stdin
real-browser eval <js> --wait-js <predicate>
real-browser eval <js> --wait-timeout <seconds>
real-browser eval <js> --wait-interval <seconds>
real-browser cdp <method>
real-browser cdp <method> <params-json>
real-browser cdp <method> --params <params-json>
```

### Cookies

```bash
real-browser cookies list
real-browser cookies list --url <url>
real-browser cookies set --url <url> --name <name> --value <value>
real-browser cookies set --url <url> --name <name> --value <value> --domain <domain> --path <path>
real-browser cookies delete --url <url> --name <name>
real-browser cookies clear
real-browser cookies clear --url <url>
```

### Storage

```bash
real-browser storage local get <key>
real-browser storage local set <key> <value>
real-browser storage local delete <key>
real-browser storage local clear
real-browser storage session get <key>
real-browser storage session set <key> <value>
real-browser storage session delete <key>
real-browser storage session clear
```

### Screenshot / PDF

```bash
real-browser screenshot
real-browser screenshot <path>
real-browser screenshot <path> --full
real-browser screenshot <path> --annotate
real-browser pdf <path>
```

### Console / Errors

```bash
real-browser console list
real-browser console list --level log
real-browser console list --level warn
real-browser console list --level warning
real-browser console list --level error
real-browser console clear
real-browser errors list
real-browser errors clear
```

`console list` 和 `errors list` 会为目标 tab 启动观测；后续页面产生的 console 和异常会进入 daemon 缓冲区。

### Network

```bash
real-browser network list
real-browser network list --status 2xx
real-browser network list --status 4xx
real-browser network list --type fetch
real-browser network list --type xhr
real-browser network list --type document
real-browser network list --include-extension
real-browser network get <requestId>
real-browser network clear
real-browser network har start
real-browser network har stop
real-browser network har save <path>
real-browser network har save <path> --include-extension
real-browser network block <pattern>
real-browser network unblock <pattern>
```

`network list` 会为目标 tab 启动网络观测；默认过滤 `chrome-extension://` 请求，调试扩展请求时使用 `--include-extension`。`network har start` 会清空当前 tab 的 network buffer 并开始录制，`network har save` 输出 HAR 1.2 JSON。

### Dialog

```bash
real-browser dialog status
real-browser dialog accept
real-browser dialog dismiss
real-browser dialog accept --prompt <text>
```

### Trace / Export / Batch

```bash
real-browser trace show
real-browser trace clear
real-browser export playwright
real-browser export playwright --out <path>
real-browser export drissionpage
real-browser export drissionpage --out <path>
real-browser batch --stdin
real-browser batch --stdin --bail
real-browser batch --file <path>
real-browser batch --file <path> --bail
```

`trace show` 展示高层 CLI 操作记录；`export` 根据 trace 生成脚本草稿，输入值默认脱敏。

`batch` 推荐使用 CLI 风格 JSON：

```json
[
  {"cmd": "open", "args": ["https://example.com"]},
  {"cmd": "click", "args": ["@e4"]},
  {"cmd": "fill", "args": ["#name", "Alice"]}
]
```

也兼容底层 RPC 风格：

```json
[
  {"command": "action.click", "params": {"target": "@e4"}}
]
```

## HTTP API

CLI 与 daemon 使用统一 RPC：

```text
GET  /v1/health
POST /v1/rpc
POST /v1/shutdown
```

旧 HTTP 路径仍保留：`GET /tabs`、`POST /scan`、`POST /exec`、`POST /open`、`POST /shutdown`。除 `GET /` 返回纯文本外，API 都返回统一 JSON 包络。

请求：

```json
{
  "id": "uuid",
  "command": "action.click",
  "target": {"tab": "t1"},
  "params": {"target": "@e4"},
  "options": {"timeoutMs": 30000}
}
```

HTTP 状态码按错误类型返回：参数错误 `400`、未授权 `401`、目标不存在 `404`、超时 `408`、扩展未连接 `503`、内部错误 `500`。

## 常见错误

| 错误码 | 处理 |
|---|---|
| `bridge_not_connected` | 执行 `doctor`，检查扩展是否加载 |
| `tab_not_found` | 执行 `tab list` |
| `target_not_found` | 重新 `snapshot` 或检查 selector/ref |
| `unsupported_tab` | 该 tab 可管理但不可注入，切换到普通页面或只使用 tab 管理命令 |
| `navigation_failed` | 检查 URL、权限或 `network block` 规则 |
| `dialog_not_found` | 当前没有可处理的 alert/confirm/prompt |
| `ambiguous_tab` | 执行 `tab list` 后 `tab use <tab>` |
| `stale_ref` | 重新 `snapshot` |
| `permission_missing` | 检查扩展权限 |

## 限制

- `chrome://`、浏览器扩展商店等受限页面可管理但通常不可注入。
- `file://` 需要用户在扩展详情页允许文件访问。
- incognito 需要用户允许扩展在无痕模式运行。
- 扩展 service worker 可能休眠，daemon 会处理断线和重连。
