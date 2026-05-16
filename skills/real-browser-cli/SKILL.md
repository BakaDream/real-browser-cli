---
name: real-browser-cli
description: 使用 real-browser-cli 控制用户真实 Chrome/Edge，复用登录态、cookies 和真实浏览器状态。适用于打开网页、观察页面、点击/填写元素、读取内容、调试网络/控制台、处理弹窗、截图、导出 trace 和批处理。
---

# real-browser-cli 使用规范

使用 `real-browser-cli` 时，优先用高层 CLI 命令完成浏览器任务。默认不要直接写 JavaScript，不要使用真实 Chrome tab id，不要输出或复制 runtime token。

## 首选工作流

```bash
real-browser daemon start
real-browser doctor
real-browser open <url>
real-browser snapshot
real-browser click @eN
real-browser fill @eN <text>
real-browser press Enter
real-browser wait --text <text>
real-browser get text
```

## 安装与连接

生成或更新浏览器扩展：

```bash
real-browser plugin update
```

查看扩展目录：

```bash
real-browser plugin path --quiet
```

在 Chrome/Edge 扩展管理页加载该目录。不要直接加载仓库里的 `browser-plugin` 模板目录。

启动和验证：

```bash
real-browser daemon start
real-browser doctor
real-browser tab list
```

如果 `doctor` 显示扩展未连接，确认浏览器已打开、扩展已加载，并重新加载扩展。

## Tab 规则

- 默认使用 `t1`、`t2` 或 label。
- 不要把真实 Chrome tab id 作为工作流的一部分。
- 多 tab 无法推断目标时，先 `tab list`，再 `tab use <tab>`。
- 需要固定目标时，使用 label。

```bash
real-browser tab list
real-browser tab use <tab>
real-browser tab label t1 docs
```

## Element 规则

- 操作元素前先执行 `snapshot`。
- 优先使用 `@eN`，例如 `@e4`。
- CSS selector 是第二选择。
- `text="..."` 和 `role=...` 可以作为可读目标。
- 页面变化、跳转、刷新或报 `stale_ref` / `target_not_found` 后，重新 `snapshot`。

```bash
real-browser snapshot
real-browser click @e4
real-browser fill @e2 "hello"
```

## 等待规则

根据目标选择最具体的等待方式：

```bash
real-browser wait --text "Success"
real-browser wait --selector ".loaded"
real-browser wait --ref @e4
real-browser wait --load networkidle
```

只需要固定暂停时才使用：

```bash
real-browser wait --ms 1000
```

## 读取与调试规则

- 页面内容：`get title`、`get url`、`get text`、`get html`。
- 元素内容：`get value|attr|box|count|styles <target>`。
- 控制台：`console list --level error`。
- 页面异常：`errors list`。
- 网络请求：`network list`、`network get <requestId>`。
- 弹窗：`dialog status`、`dialog accept`、`dialog dismiss`。
- 复现流程：`trace show`、`export ... --out`、`batch --file`。

## 逃生口

只有高层命令无法表达时才使用：

```bash
real-browser eval <js>
real-browser cdp <method> [params-json]
```

优先把逃生口限制在读取状态或小范围操作，不要默认用 JS click 替代 `real-browser click`。

## 完整命令表

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
real-browser plugin path --json
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

Target 示例：

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

Batch 推荐使用 CLI 风格 JSON：

```json
[
  {"cmd": "open", "args": ["https://example.com"]},
  {"cmd": "click", "args": ["@e4"]},
  {"cmd": "fill", "args": ["#name", "Alice"]}
]
```

## 失败恢复

| 错误 | 操作 |
|---|---|
| `bridge_not_connected` | 执行 `doctor`，确认浏览器已打开并重新加载扩展 |
| `target_not_found` | 重新 `snapshot` 或检查 target |
| `stale_ref` | 页面已变化，重新 `snapshot` |
| `ambiguous_tab` | `tab list` 后 `tab use <tab>` |
| `tab_not_found` | `tab list`，确认目标 tab 仍存在 |
| `unsupported_tab` | 切换到普通页面；受限页面只做 tab 管理 |
| `navigation_failed` | 检查 URL、权限或网络阻断规则 |
| `dialog_not_found` | 当前没有可处理的浏览器弹窗 |
| `permission_missing` | 检查扩展权限，必要时重新加载扩展 |

## 注意

- `chrome://`、浏览器扩展商店、未授权 `file://`、未授权 incognito 页面不可完全控制。
- `network list` 和 HAR 默认过滤 `chrome-extension://`，调试扩展请求时加 `--include-extension`。
- `console list --level warn` 会匹配浏览器的 `warning` 级别。
- `trace` 记录高层 CLI 操作，导出脚本中的输入值默认脱敏。
