# AI 安装与验证指南

本文档供 AI agent 在用户机器上安装、配置和验证 `real-browser-cli`。默认安装 GitHub latest release，并把二进制放到用户目录下的 `~/.real-agent-cli/bin`。

重要规则：

- 不要输出、复制或读取 runtime token。
- 浏览器扩展需要用户手动加载。agent 必须把插件路径返回给用户，并等待用户确认“已加载”后再继续自检。
- 所有插件命令使用顶层 `real-browser plugin ...`。

## 1. 判断当前系统和架构

### macOS / Linux

```bash
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS:$ARCH" in
  Darwin:arm64)  ASSET="real-browser-darwin-arm64" ;;
  Darwin:x86_64) ASSET="real-browser-darwin-amd64" ;;
  Linux:x86_64)  ASSET="real-browser-linux-amd64" ;;
  Linux:amd64)   ASSET="real-browser-linux-amd64" ;;
  Linux:aarch64) ASSET="real-browser-linux-arm64" ;;
  Linux:arm64)   ASSET="real-browser-linux-arm64" ;;
  *)
    echo "unsupported platform: $OS/$ARCH" >&2
    exit 1
    ;;
esac

echo "$ASSET"
```

### Windows PowerShell

```powershell
$Arch = $env:PROCESSOR_ARCHITECTURE
if ($Arch -eq "AMD64") {
  $Asset = "real-browser-windows-amd64.exe"
} else {
  throw "unsupported Windows architecture: $Arch"
}
$Asset
```

## 2. 拼接 latest release 下载地址

latest release 下载地址固定为：

```text
https://github.com/bakadream/real-browser-cli/releases/latest/download/<asset>
https://github.com/bakadream/real-browser-cli/releases/latest/download/checksums.txt
```

Release asset 映射：

| 系统 | 架构 | asset |
|---|---|---|
| macOS | arm64 | `real-browser-darwin-arm64` |
| macOS | x86_64 | `real-browser-darwin-amd64` |
| Linux | x86_64 / amd64 | `real-browser-linux-amd64` |
| Linux | arm64 / aarch64 | `real-browser-linux-arm64` |
| Windows | x86_64 / AMD64 | `real-browser-windows-amd64.exe` |

## 3. 下载二进制和 checksums.txt

### macOS / Linux

```bash
BASE_URL="https://github.com/bakadream/real-browser-cli/releases/latest/download"
TMP_DIR="$(mktemp -d)"

if command -v curl >/dev/null 2>&1; then
  curl -fL "$BASE_URL/$ASSET" -o "$TMP_DIR/$ASSET"
  curl -fL "$BASE_URL/checksums.txt" -o "$TMP_DIR/checksums.txt"
elif command -v wget >/dev/null 2>&1; then
  wget -O "$TMP_DIR/$ASSET" "$BASE_URL/$ASSET"
  wget -O "$TMP_DIR/checksums.txt" "$BASE_URL/checksums.txt"
else
  echo "curl or wget is required" >&2
  exit 1
fi
```

### Windows PowerShell

```powershell
$BaseUrl = "https://github.com/bakadream/real-browser-cli/releases/latest/download"
$TmpDir = Join-Path $env:TEMP ("real-browser-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $TmpDir | Out-Null

$BinPath = Join-Path $TmpDir $Asset
$ChecksumsPath = Join-Path $TmpDir "checksums.txt"

try {
  curl.exe -fL "$BaseUrl/$Asset" -o $BinPath
  curl.exe -fL "$BaseUrl/checksums.txt" -o $ChecksumsPath
} catch {
  Invoke-WebRequest -Uri "$BaseUrl/$Asset" -OutFile $BinPath
  Invoke-WebRequest -Uri "$BaseUrl/checksums.txt" -OutFile $ChecksumsPath
}
```

## 4. 校验 SHA256

### macOS

```bash
cd "$TMP_DIR"
grep "  $ASSET$" checksums.txt | shasum -a 256 -c -
```

### Linux

```bash
cd "$TMP_DIR"
grep "  $ASSET$" checksums.txt | sha256sum -c -
```

### Windows PowerShell

```powershell
$Expected = (Get-Content $ChecksumsPath | Where-Object { $_ -match "\s+$([regex]::Escape($Asset))$" }).Split()[0]
$Actual = (certutil -hashfile $BinPath SHA256 | Select-String -Pattern "^[0-9a-fA-F]{64}$").Matches.Value.ToLower()
if ($Actual -ne $Expected.ToLower()) {
  throw "checksum mismatch: expected $Expected, got $Actual"
}
```

## 5. 安装到 ~/.real-agent-cli/bin

### macOS / Linux

```bash
INSTALL_DIR="$HOME/.real-agent-cli/bin"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMP_DIR/$ASSET" "$INSTALL_DIR/real-browser"
```

### Windows PowerShell

```powershell
$InstallDir = Join-Path $env:USERPROFILE ".real-agent-cli\bin"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item $BinPath (Join-Path $InstallDir "real-browser.exe") -Force
```

## 6. 添加 bin 目录到 PATH

### zsh

```bash
INSTALL_DIR="$HOME/.real-agent-cli/bin"
LINE='export PATH="$HOME/.real-agent-cli/bin:$PATH"'
touch "$HOME/.zshrc"
grep -F "$LINE" "$HOME/.zshrc" >/dev/null 2>&1 || printf '\n%s\n' "$LINE" >> "$HOME/.zshrc"
```

### bash

```bash
INSTALL_DIR="$HOME/.real-agent-cli/bin"
LINE='export PATH="$HOME/.real-agent-cli/bin:$PATH"'
touch "$HOME/.bashrc"
grep -F "$LINE" "$HOME/.bashrc" >/dev/null 2>&1 || printf '\n%s\n' "$LINE" >> "$HOME/.bashrc"
```

### Windows PowerShell

```powershell
$InstallDir = Join-Path $env:USERPROFILE ".real-agent-cli\bin"
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($UserPath -split ";") -notcontains $InstallDir) {
  [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
}
```

## 7. 刷新 PATH 并验证 real-browser

### zsh

```bash
source "$HOME/.zshrc"
real-browser version
```

### bash

```bash
source "$HOME/.bashrc"
real-browser version
```

### 当前 shell 临时刷新

如果不确定用户使用 zsh 还是 bash，当前会话可以先临时刷新：

```bash
export PATH="$HOME/.real-agent-cli/bin:$PATH"
real-browser version
```

### Windows PowerShell

```powershell
$env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")
real-browser.exe version
```

成功判据：`real-browser version` 能正常输出版本信息。latest release 通常会输出当前 release tag。

## 8. 释放插件并等待用户加载

释放或更新浏览器扩展文件：

```bash
real-browser plugin update
PLUGIN_DIR="$(real-browser plugin path --quiet)"
printf '%s\n' "$PLUGIN_DIR"
```

Windows PowerShell：

```powershell
real-browser.exe plugin update
$PluginDir = real-browser.exe plugin path --quiet
$PluginDir
```

把 `PLUGIN_DIR` 原样返回给用户，并指导用户：

1. 打开 Chrome/Edge 扩展管理页。
2. 开启开发者模式。
3. 点击“加载已解压的扩展程序”。
4. 选择上面输出的插件目录。
5. 加载完成后回复“已加载”。

agent 必须在这里停止自动继续，等待用户确认扩展已加载。

如果需要控制 `file://` 页面，让用户在扩展详情页允许文件访问。如果需要控制无痕窗口，让用户允许扩展在无痕模式运行。

## 9. 极简自检

用户确认扩展已加载后，执行：

```bash
real-browser daemon start
real-browser doctor
real-browser open https://github.com/bakadream/real-browser-cli
real-browser wait --text real-browser-cli
real-browser get title
```

成功判据：

- `doctor` 显示 daemon 正常，并且浏览器插件已连接。
- 浏览器打开 `https://github.com/bakadream/real-browser-cli`。
- `get title` 能输出 GitHub 仓库页面标题。

如果失败：

| 现象或错误 | 处理 |
|---|---|
| `bridge_not_connected` | 确认浏览器已打开、扩展已加载，重新加载扩展后再执行 `doctor` |
| `tab_not_found` | 执行 `real-browser tab list`，确认存在可用 tab |
| `navigation_failed` | 检查网络、浏览器权限或 URL 是否被阻断 |
| `target_not_found` / `stale_ref` | 执行 `real-browser snapshot` 后重试 |
| token 轮换后扩展断连 | 执行 `real-browser daemon token rotate` 后必须重新 `real-browser plugin update` 并重新加载扩展 |

## 10. 安装完成后常用命令

```bash
real-browser doctor
real-browser tab list
real-browser open <url>
real-browser snapshot
real-browser click @e1
real-browser fill @e2 "hello@example.com"
real-browser press Enter
real-browser wait --text "Success"
real-browser get text
```

使用规则：

- 默认使用 `t1`、`t2` 或 label，不使用真实 Chrome tab id。
- 默认使用 `@e1`、`@e2` 等 snapshot ref。
- 页面变化后重新执行 `real-browser snapshot`。
- 多 tab 无法推断时，先 `real-browser tab list`，再 `real-browser tab use <tab>`。
- 只有高层命令无法表达时，才使用 `real-browser eval` 或 `real-browser cdp`。
