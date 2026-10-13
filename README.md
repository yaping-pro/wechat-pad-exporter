# WeChat Pad Exporter (`wechat-pad-exporter`)

> **Pad 版微信实时聊天流只读导出守护进程 + 通用 Webhook / 个人 AI 助理数据管道**  
> Read-only realtime chat stream exporter daemon for Tablet-mode WeChat, paired with universal Webhook and personal AI assistant consumer pipelines.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/yaping-pro/wechat-pad-exporter?color=brightgreen)](https://github.com/yaping-pro/wechat-pad-exporter/releases/latest)
[![Go Report Card](https://img.shields.io/badge/Go-1.23-00ADD8.svg?logo=go)](daemon/)
[![Android Hook](https://img.shields.io/badge/Hook-DexKit%20%2B%20LSPosed-green.svg)](android-module/)

[中文](#中文) | [English](#english)

---

## 中文

### 目录
- [💡 项目定位与核心特性](#-项目定位与核心特性)
- [⚠️ 前置要求与只读边界声明](#-前置要求与只读边界声明)
- [⚡ 30 秒极速上手 (Quickstart)](#-30-秒极速上手-quickstart)
- [🛠️ 从源码构建](#️-从源码构建)
- [⚙️ 守护进程配置与命令行参数全景](#️-守护进程配置与命令行参数全景)
- [🤖 接入个人 AI 助理 (Webhook Bridge)](#-接入个人-ai-助理-webhook-bridge)
- [❓ 常见问题与排错指南 (FAQ)](#-常见问题与排错指南-faq)
- [📚 详细文档导航](#-详细文档导航)
- [🔒 安全政策与免责声明](#-安全政策与免责声明)
- [📄 开源许可证](#-开源许可证)

---

### 💡 项目定位与核心特性

`wechat-pad-exporter` 是一个专为**个人数据自主权**与**本地个人 AI 助理**打造的高性能、工业级数据导出套件：
1. **只读 Xposed 探针 (`com.wx.sniffer`)**：运行于 Android 微信进程内部，借助 DexKit 动态匹配 Dalvik 字节码指令特征，无感拦截底层 WCDB SQLite 插入事件，并通过 Linux 抽象命名空间域套接字（`@wechat_stream_ipc`）极速单向投递。
2. **本地 Go 守护进程 (`wx_daemon`)**：轻量级单二进制，默认安全绑定于回环地址 `127.0.0.1:8765`。内置 Bearer Token 强鉴权、SQLite WAL 增量存储、日志自动轮转与 SHA-256 防篡改导出摘要。
3. **自愈看门狗守护 (`deploy/`)**：工业级常驻脚本（`service.sh` / `wx_autostart.sh`），支持 Magisk / KernelSU 开机自动拉起、OOM 保护与防多实例踩踏。
4. **即插即用消费客户端 (`clients/`)**：提供标准游标拉取器（`fetch_export.py`）与通用 AI Webhook 桥接网关（`assistant_webhook.py`，原生适配 Dify、飞书机器人、OpenAI 兼容 Agent）。

---

### ⚠️ 前置要求与只读边界声明

- **前置登录依赖**：本项目假定微信**已处于登录就绪状态**。支持两种运行场景：
  - **实体安卓平板**：原生支持与手机微信同时在线；
  - **安卓手机 + 平板伪装伴侣**：配合开源伴侣项目 [`wx-tablet-mode`](https://github.com/yaping-pro/wx-tablet-mode)（或等效平板多登方案）实现副设备双登。
- **100% 严格只读承诺**：本仓库**严禁且绝不包含任何写库、发送消息、好友请求或自动化点击能力**，完全杜绝因主动发信导致的风控隐患。
- **环境解耦与安全性**：默认仅监听本地回环地址，无需暴露至公网，支持纯离线本地数据闭环。

---

### ⚡ 30 秒极速上手 (Quickstart)

> **极速路径**：直接使用预编译 Release 产物，省去配置 Android NDK 与 Go 交叉编译环境。

#### 步骤 1：下载预编译发布产物
前往 [GitHub Releases 最新发布页](https://github.com/yaping-pro/wechat-pad-exporter/releases/latest) 下载：
- `wechat-pad-exporter-module-v1.0.1.apk` (Xposed 探针模块)
- `wx_daemon-android-arm64` (Android 设备端守护进程二进制)

#### 步骤 2：安装探针并加入 LSPosed / Vector 作用域
```bash
# 安装 Xposed 探针 APK
adb install -r wechat-pad-exporter-module-v1.0.1.apk
```
打开 LSPosed / Vector 管理器：
- 启用 `com.wx.sniffer` (WeChat Pad Exporter)；
- 将微信 `com.tencent.mm` 添加到模块作用域 (Scope)。

#### 步骤 3：在设备端配置并启动守护进程
生成高熵鉴权令牌，创建配置文件并启动守护进程：
```bash
# 1. 生成 32 字节高熵随机 Token (请记下输出的 Token)
TOKEN=$(openssl rand -hex 32)
echo "生成的 Token: $TOKEN"

# 2. 推送守护进程并赋予执行权限
adb push wx_daemon-android-arm64 /data/local/tmp/wx_daemon
adb shell chmod 755 /data/local/tmp/wx_daemon

# 3. 创建配置目录与配置文件 (仅 root 可读写 0700)
adb shell "su -c 'mkdir -p /data/adb/wx_stream && chmod 700 /data/adb/wx_stream'"

# 4. 写入标准配置文件 config.json
adb shell "su -c 'cat <<EOF > /data/adb/wx_stream/config.json
{
  \"bind\": \"127.0.0.1\",
  \"port\": 8765,
  \"uds_path\": \"@wechat_stream_ipc\",
  \"db_path\": \"/data/adb/wx_stream/vault.db\",
  \"token\": \"$TOKEN\",
  \"auth\": true,
  \"log_file\": \"/data/adb/wx_stream/daemon.log\"
}
EOF'"

# 5. 启动守护进程
adb shell "su -c '/data/local/tmp/wx_daemon -config /data/adb/wx_stream/config.json &'"
```

#### 步骤 4：本地端口转发与健康检查 (Zero Exposure)
在您的电脑工作站上，利用 ADB 端口转发建立安全链路：
```bash
# 将设备本地 8765 端口安全映射到工作站
adb forward tcp:8765 tcp:8765

# 调用健康检查接口 (替换为您刚生成的 Token)
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8765/health
```

返回示例：
```json
{"status":"ok","uptime_seconds":5,"uds_connected":false,"active_uds_connections":0,"total_messages":0,"version":"1.0.0-pad"}
```
*(注：首次启动微信前 `uds_connected` 为 `false` 属完全正常现象，见 [FAQ](#-常见问题与排错指南-faq)。)*

#### 步骤 5：只读拉取增量消息
```bash
# 导出当前批次消息
curl -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8765/api/wechat/export?limit=50"
```

---

### 🛠️ 从源码构建

如果您需要自行编译各组件：

#### 1. 编译 Android 探针模块
```bash
cd android-module
./gradlew assembleRelease
# 产物：android-module/build/outputs/apk/release/android-module-release.apk
```

#### 2. 编译 Go 守护进程 (交叉编译为 Android ARM64)
```bash
cd daemon
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o wx_daemon .
# 产物：daemon/wx_daemon
```

---

### ⚙️ 守护进程配置与命令行参数全景

守护进程支持通过 `-config` 指定 JSON 配置文件，也支持通过命令行 flags 进行精准覆盖。

#### 1. 真实命令行 Flags 列表 (来自 `daemon/main.go`)
| Flag 参数 | 类型 | 默认值 | 作用说明 |
| :--- | :--- | :--- | :--- |
| `-config` | string | `""` | JSON 配置文件绝对路径 (如 `/data/adb/wx_stream/config.json`) |
| `-http` | string | `127.0.0.1:8765` | HTTP 服务监听地址及端口 (生产环境务必保持回环) |
| `-db` | string | `/data/adb/wx_stream/vault.db` | SQLite 存储库路径 (WAL 模式持久化) |
| `-uds` | string | `@wechat_stream_ipc` | Unix Domain Socket 地址 (以 `@` 开头代表 Linux 抽象命名空间) |
| `-token` | string | `""` | API 调用的 Bearer 鉴权 Token |
| `-no-auth` | bool | `false` | 显式禁用 Token 鉴权 (仅限封闭受信任调试环境) |
| `-retain-days` | int | `-1` | 消息落库保留天数 (超过则自动剪裁；`0` 表示永久保留) |
| `-log-file` | string | `""` | 日志输出路径 (留空则仅向 stderr 输出) |
| `-log-max-bytes` | int64 | `-1` | 单个日志文件最大字节数，触发自动切割轮转 |
| `-log-keep` | int | `-1` | 保留的历史日志文件数量 |

#### 2. 完整配置文件范例 (`daemon/config.example.json`)
```json
{
  "bind": "127.0.0.1",
  "port": 8765,
  "uds_path": "@wechat_stream_ipc",
  "db_path": "/data/adb/wx_stream/vault.db",
  "token": "REPLACE_WITH_SECURE_TOKEN",
  "auth": true,
  "allow_chats": [],
  "block_chats": [],
  "log_file": "/data/adb/wx_stream/daemon.log",
  "log_max_bytes": 10485760,
  "log_keep": 3,
  "retain_days": 90
}
```

---

### 🤖 接入个人 AI 助理 (Webhook Bridge)

本仓库提供了一键网关 `clients/assistant_webhook.py`，能以毫秒级将增量微信消息推送至 Dify 或飞书机器人：

```bash
# 1. 设置鉴权与回调环境变量
export WX_DEVICE_TOKEN="$TOKEN"
export ASSISTANT_WEBHOOK_URL="https://your-dify-or-agent-endpoint/webhook"
export ASSISTANT_WEBHOOK_TYPE="dify"  # 可选: 'dify', 'feishu', 'custom'

# 2. 启动轮询同步网关
python3 clients/assistant_webhook.py --poll-interval 5
```

---

### ❓ 常见问题与排错指南 (FAQ)

#### Q1: 健康检查返回 `"uds_connected": false`，是不是探针没挂上？
**A**: **完全正常**。出于节电与抗脆弱性设计，探针采用**延迟/按需重连**机制。微信刚刚冷启动或守护进程刚刚重启时，UDS 套接字处于空闲态；一旦微信产生下一条真实的收发消息，探针会瞬间完成 IPC 握手并持久化连接，随后 `uds_connected` 即会转为 `true`。

#### Q2: 守护进程重启后，探针会自动重新连接吗？
**A**: 会。探针在写入失败时会自动捕捉断开异常并标记为非连接态，在下一次消息事件触发时自动重新拨号 `@wechat_stream_ipc`，无需重启微信。

#### Q3: 为什么深夜或熄屏很长时间后收不到新消息推送？
**A**: Android 系统的 Doze 激进省电机制或 MIUI/Aurogon 可能会将后台网络挂起并整夜批量对齐唤醒（"Quiet ≠ Broken"）。  
**解决方法**：请将微信和探针加入电池无限制白名单：
```bash
dumpsys deviceidle whitelist +com.tencent.mm
dumpsys deviceidle whitelist +com.wx.sniffer
```

#### Q4: 守护进程端口是否可以直接绑定到 `0.0.0.0` 方便远程访问？
**A**: **极度不建议！** 导出的聊天记录包含明文个人隐私。将端口暴露在公网或开放局域网存在巨大安全风险。强烈建议：
1. 默认保持 `127.0.0.1:8765`；
2. 跨主机访问时，使用 `adb forward tcp:8765 tcp:8765`、SSH 本地端口转发或加密点对点组网（WireGuard / Tailscale）。

#### Q5: 多用户/双开微信（如 MIUI 分身）下执行 `pm install-existing` 报错？
**A**: `pm install-existing --user <UID> com.wx.sniffer` 依赖于主用户空间（User 0）已存在安装底包。必须先在主空间执行 `adb install`，然后再执行分身映射。

#### Q6: 如何准确查询双开微信的分配 UID，而不是盲目硬编码 999？
**A**: 不要硬编码 `999`，通过 Android 官方 dumpsys 动态抓取分配的 UID：
```bash
TARGET_USER=999 # 目标分身用户号
DETECTED_UID=$(dumpsys package --user "$TARGET_USER" com.tencent.mm 2>/dev/null | grep "userId=" | head -1 | awk -F= '{print $2}' | tr -d ' \r\n')
echo "微信真实 UID: $DETECTED_UID"
```

#### Q7: 为什么探针能长期免受微信频繁版本升级的影响？
**A**: 传统 Xposed 模块依赖硬编码混淆类名（如 `com.tencent.mm.model.ax`），每周微信发版即失效。本项目使用 **DexKit** 在运行时动态识别 Dalvik 操作码模式，锚定的是微信最底层的 SQLite 核心入库接口 `insertWithOnConflict`。由于底层数据库驱动多年未变，实现了极高的跨版本通用性。

---

### 📚 详细文档导航

- [RESTful 接口详细规范 (API Specification)](docs/API_SPEC.md)
- [系统架构与高可用设计 (Architecture)](docs/ARCHITECTURE.md)
- [安全组网与端口暴露防护指南 (Networking)](docs/NETWORKING.md)
- [设备端初始化与配置全书 (Setup Guide)](docs/SETUP_GUIDE.md)
- [Dify / 飞书机器人对接指南](docs/integrations/personal_assistant.md)
- [青龙面板定时调度指南](docs/integrations/qinglong.md)

---

### 🔒 安全政策与免责声明

- [开源免责声明 (DISCLAIMER.md)](DISCLAIMER.md)
- [安全策略与漏洞披露 (SECURITY.md)](SECURITY.md)
- [贡献指南 (CONTRIBUTING.md)](CONTRIBUTING.md)

---

### 📄 开源许可证

本项目基于 [MIT License](LICENSE) 开源。

---

## English

### Table of Contents
- [💡 Overview & Core Capabilities](#-overview--core-capabilities)
- [⚠️ Prerequisites & Read-Only Boundaries](#-prerequisites--read-only-boundaries)
- [⚡ 30-Second Quickstart](#-30-second-quickstart-1)
- [🛠️ Build from Source](#️-build-from-source-1)
- [⚙️ Daemon Configuration & CLI Flag Reference](#️-daemon-configuration--cli-flag-reference)
- [🤖 Personal AI Assistant Integration (Webhook Bridge)](#-personal-ai-assistant-integration-webhook-bridge)
- [❓ FAQ & Troubleshooting](#-faq--troubleshooting-1)
- [📚 Documentation Index](#-documentation-index-1)
- [🔒 Security & Disclaimer](#-security--disclaimer-1)
- [📄 License](#-license-1)

---

### 💡 Overview & Core Capabilities

`wechat-pad-exporter` is an industrial-grade, open-source pipeline engineered for **personal data sovereignty** and **local AI personal assistants**:
1. **Read-Only Xposed Probe (`com.wx.sniffer`)**: Injected into the Android WeChat process, dynamically matching Dalvik bytecode opcode signatures via DexKit to intercept low-level WCDB SQLite insertions, emitting data through an abstract Linux Unix Domain Socket (`@wechat_stream_ipc`).
2. **Local Go Daemon (`wx_daemon`)**: Lightweight self-contained binary safely bound to loopback `127.0.0.1:8765` by default. Features Bearer token authentication, WAL-backed SQLite incremental persistence, automatic log rotation, and SHA-256 tamper-evident export manifests.
3. **Self-Healing Watchdog (`deploy/`)**: Production-ready watchdog scripts (`service.sh` / `wx_autostart.sh`) ensuring daemon uptime across reboots on Magisk / KernelSU, OOM priority protection, and multi-instance concurrency isolation.
4. **Consumer Client Suite (`clients/`)**: Includes incremental cursor fetchers (`fetch_export.py`) and an out-of-the-box AI Webhook bridge (`assistant_webhook.py`, with built-in adapters for Dify, Feishu bots, and OpenAI-compatible agents).

---

### ⚠️ Prerequisites & Read-Only Boundaries

- **Login Prerequisite**: Assumes WeChat is **already logged in and operational**. Compatible with:
  - **Physical Android Tablet**: Natively supports concurrent login alongside your primary phone.
  - **Android Phone + Tablet Disguise Companion**: Paired with companion project [`wx-tablet-mode`](https://github.com/yaping-pro/wx-tablet-mode) (or equivalent tablet emulation setup) for secondary coexistence.
- **100% Read-Only Guarantee**: This repository **strictly prohibits and contains zero write, message sending, automated tapping, or injection features**. Pure passive observation eliminates risk of active operation triggers.
- **Environment Decoupled**: Free of third-party cloud ties or hardcoded networks. Binds strictly to local loopback.

---

### ⚡ 30-Second Quickstart

> **Fast Track**: Skip local Android NDK and Go cross-compilation setups by using precompiled Release binaries.

#### Step 1: Download Release Artifacts
Visit the [GitHub Releases Latest Page](https://github.com/yaping-pro/wechat-pad-exporter/releases/latest) and download:
- `wechat-pad-exporter-module-v1.0.1.apk` (Xposed probe module)
- `wx_daemon-android-arm64` (Android device daemon binary)

#### Step 2: Install Probe & Enable in LSPosed / Vector
```bash
# Install module APK
adb install -r wechat-pad-exporter-module-v1.0.1.apk
```
Open LSPosed / Vector Manager:
- Enable module `com.wx.sniffer` (WeChat Pad Exporter);
- Check WeChat `com.tencent.mm` in the module Scope.

#### Step 3: Configure and Start Daemon on Device
Generate a high-entropy secret token, write the config file, and start the daemon:
```bash
# 1. Generate a 32-byte high-entropy token (record this token)
TOKEN=$(openssl rand -hex 32)
echo "Generated Token: $TOKEN"

# 2. Push binary and set execution permissions
adb push wx_daemon-android-arm64 /data/local/tmp/wx_daemon
adb shell chmod 755 /data/local/tmp/wx_daemon

# 3. Create root-only configuration directory (0700)
adb shell "su -c 'mkdir -p /data/adb/wx_stream && chmod 700 /data/adb/wx_stream'"

# 4. Write configuration file config.json
adb shell "su -c 'cat <<EOF > /data/adb/wx_stream/config.json
{
  \"bind\": \"127.0.0.1\",
  \"port\": 8765,
  \"uds_path\": \"@wechat_stream_ipc\",
  \"db_path\": \"/data/adb/wx_stream/vault.db\",
  \"token\": \"$TOKEN\",
  \"auth\": true,
  \"log_file\": \"/data/adb/wx_stream/daemon.log\"
}
EOF'"

# 5. Launch daemon in background
adb shell "su -c '/data/local/tmp/wx_daemon -config /data/adb/wx_stream/config.json &'"
```

#### Step 4: Local Port Forwarding & Health Check (Zero Exposure)
On your workstation, forward port 8765 via ADB:
```bash
# Safely forward local port 8765
adb forward tcp:8765 tcp:8765

# Probe daemon health (replace $TOKEN with your generated token)
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8765/health
```

Expected output:
```json
{"status":"ok","uptime_seconds":5,"uds_connected":false,"active_uds_connections":0,"total_messages":0,"version":"1.0.0-pad"}
```
*(Note: `uds_connected: false` is expected right after startup until WeChat receives the next message; see [FAQ](#-faq--troubleshooting-1).)*

#### Step 5: Fetch Messages
```bash
curl -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8765/api/wechat/export?limit=50"
```

---

### 🛠️ Build from Source

#### 1. Compile Android Probe APK
```bash
cd android-module
./gradlew assembleRelease
# Output: android-module/build/outputs/apk/release/android-module-release.apk
```

#### 2. Cross-Compile Go Daemon (Android ARM64)
```bash
cd daemon
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o wx_daemon .
# Output: daemon/wx_daemon
```

---

### ⚙️ Daemon Configuration & CLI Flag Reference

The daemon accepts a JSON configuration file via `-config` as well as direct command-line flags.

#### 1. Command-Line Flags (from `daemon/main.go`)
| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `-config` | string | `""` | Path to JSON config file (e.g. `/data/adb/wx_stream/config.json`) |
| `-http` | string | `127.0.0.1:8765` | HTTP listen address and port |
| `-db` | string | `/data/adb/wx_stream/vault.db` | Path to SQLite storage file (WAL mode) |
| `-uds` | string | `@wechat_stream_ipc` | Unix Domain Socket address (`@` prefix denotes Linux abstract socket) |
| `-token` | string | `""` | Bearer token for API authentication |
| `-no-auth` | bool | `false` | Explicitly disables API token authorization |
| `-retain-days` | int | `-1` | Message retention window in days (`0` disables pruning) |
| `-log-file` | string | `""` | Log file destination (empty logs to stderr) |
| `-log-max-bytes` | int64 | `-1` | Max log bytes before triggering log rotation |
| `-log-keep` | int | `-1` | Number of rotated log archives to retain |

#### 2. Configuration File Schema (`daemon/config.example.json`)
```json
{
  "bind": "127.0.0.1",
  "port": 8765,
  "uds_path": "@wechat_stream_ipc",
  "db_path": "/data/adb/wx_stream/vault.db",
  "token": "REPLACE_WITH_SECURE_TOKEN",
  "auth": true,
  "allow_chats": [],
  "block_chats": [],
  "log_file": "/data/adb/wx_stream/daemon.log",
  "log_max_bytes": 10485760,
  "log_keep": 3,
  "retain_days": 90
}
```

---

### 🤖 Personal AI Assistant Integration (Webhook Bridge)

Use `clients/assistant_webhook.py` to forward new chat messages in real time to Dify or a Feishu bot:

```bash
export WX_DEVICE_TOKEN="$TOKEN"
export ASSISTANT_WEBHOOK_URL="https://your-dify-or-agent-endpoint/webhook"
export ASSISTANT_WEBHOOK_TYPE="dify"  # Options: 'dify', 'feishu', 'custom'

# Start polling bridge
python3 clients/assistant_webhook.py --poll-interval 5
```

---

### ❓ FAQ & Troubleshooting

#### Q1: Health check returns `"uds_connected": false`. Is the hook broken?
**A**: **No, this is completely normal.** The probe uses an on-demand lazy reconnection design. Right after daemon boot or WeChat startup, the socket remains idle. As soon as WeChat receives or sends the next real message, the hook connects to `@wechat_stream_ipc` in milliseconds and `uds_connected` flips to `true`.

#### Q2: What happens if the daemon restarts?
**A**: The probe catches socket write errors upon disconnection and automatically reconnects on the next message event. You do not need to restart WeChat.

#### Q3: Why are no messages received overnight or when the screen is locked?
**A**: Aggressive Android Doze or vendor battery optimization (MIUI / Aurogon) may freeze network processes and batch wakeups ("Quiet ≠ Broken").  
**Fix**: Whitelist WeChat and the sniffer:
```bash
dumpsys deviceidle whitelist +com.tencent.mm
dumpsys deviceidle whitelist +com.wx.sniffer
```

#### Q4: Can I bind `-http` to `0.0.0.0` for remote access?
**A**: **Strongly discouraged.** Plaintext chat logs must never be exposed on open public or LAN networks. Always keep it bound to `127.0.0.1:8765` and use `adb forward tcp:8765 tcp:8765`, SSH tunnels, or encrypted peer-to-peer overlays (WireGuard / Tailscale).

#### Q5: `pm install-existing --user <UID>` fails on secondary clone profile?
**A**: `pm install-existing` requires the APK to be installed in User 0 first. Install via `adb install` in the primary profile before mapping to clone users.

#### Q6: How to locate the exact UID in dual-app environments rather than hardcoding 999?
**A**: Use dumpsys to query the actual assigned UID dynamically:
```bash
TARGET_USER=999 # Target secondary profile ID
DETECTED_UID=$(dumpsys package --user "$TARGET_USER" com.tencent.mm 2>/dev/null | grep "userId=" | head -1 | awk -F= '{print $2}' | tr -d ' \r\n')
echo "Detected UID: $DETECTED_UID"
```

#### Q7: Why is DexKit opcode matching resilient across WeChat releases?
**A**: Traditional hooks target obfuscated Java class names that change every week. DexKit inspects bytecode opcode patterns at runtime to locate WCDB's core SQLite `insertWithOnConflict` method, which has remained architecturally stable for years.

---

### 📚 Documentation Index

- [RESTful API Specification](docs/API_SPEC.md)
- [System Architecture & Reliability](docs/ARCHITECTURE.md)
- [Networking & Secure Port Forwarding](docs/NETWORKING.md)
- [Device Setup & Provisioning Guide](docs/SETUP_GUIDE.md)
- [Personal Assistant Webhook Integration (Dify & Feishu)](docs/integrations/personal_assistant.md)
- [QingLong Panel Scheduled Dispatch](docs/integrations/qinglong.md)

---

### 🔒 Security & Disclaimer

- [Disclaimer (DISCLAIMER.md)](DISCLAIMER.md)
- [Security Policy (SECURITY.md)](SECURITY.md)
- [Contributing Guidelines (CONTRIBUTING.md)](CONTRIBUTING.md)

---

### 📄 License

Distributed under the [MIT License](LICENSE).
