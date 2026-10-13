# WeChat Pad Exporter (`wechat-pad-exporter`)

> **Pad 版微信实时聊天流只读导出守护进程 + 通用 Webhook / 个人 AI 助理数据管道**  
> Read-only realtime chat stream exporter daemon for Tablet-mode WeChat, paired with universal Webhook and personal AI assistant consumer pipelines.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://img.shields.io/badge/Go-1.23-00ADD8.svg?logo=go)](daemon/)
[![Android Hook](https://img.shields.io/badge/Hook-DexKit%20%2B%20LSPosed-green.svg)](android-module/)

---

## 💡 项目定位 (What It Is)

`wechat-pad-exporter` 是一个面向**个人数据自主权**与**本地 AI 助理**的基础设施套件：
1. **只读 Xposed 探针 (`DexKitSniffer`)**：在 Android 微信进程内通过 DexKit 动态识别 Dalvik 操作码，无感拦截底层 WCDB SQLite 插入事件，并通过极速 Linux 抽象 Unix 域套接字（`@wechat_stream_ipc`）单向投递数据。
2. **本地 Go 守护进程 (`wx_daemon`)**：默认安全绑定于 `127.0.0.1:8765`，提供 Bearer 鉴权、WAL 增量落库与 SHA-256 防篡改签名的 HTTP 导出接口。
3. **设备端自愈看门狗 (`wx_watchdog.sh`)**：工业级常驻守护脚本，自动拉起主进程、设置 OOM Protection 并持久化隔离回潮补丁。
4. **通用消费端套件 (`clients/`)**：提供游标消费器（`fetch_export.py`）与通用个人助理 Webhook 桥梁（`assistant_webhook.py`，支持 Dify、飞书机器人、OpenAI 兼容 Agent）。

### ⚠️ 前置要求与项目边界 (Prerequisites & What It Is NOT)
- **前置条件**：假定微信**已处于登录状态**。微信可以通过以下两种方式之一运行：
  - **实体安卓平板 (Physical Android Tablet)**：原生支持与手机微信同时在线。
  - **安卓手机 + 平板伪装伴侣**：配合伴侣开源项目 [`wx-tablet-mode`](https://github.com/yaping-pro/wx-tablet-mode)（或同类平板伪装方案）以平板模式共存登录。
- **严格只读承诺**：本仓库**严禁且不包含任何写库、发消息或自动化点击外挂能力**，100% 只读。
- **环境解耦**：不绑定任何特定私有 VPN、内网穿透或多用户环境（默认支持标准 Android 主用户 `0`）。

---

## 🛡️ 基于 DexKit 的操作码模式识别 (Robustness via DexKit)

> 传统 Xposed 微信模块极易随微信每周版本迭代而失效。  
> 本项目核心探针使用 **DexKit**，在运行时直接识别 Dalvik 字节码指令模式以定位底层 WCDB SQLite `insertWithOnConflict`。由于 WCDB 底层数据库插入接口数年未变，导出管道具备极强的跨微信版本兼容性，免去频繁适配的烦恼。

---

## 🚀 快速上手 (Quick Start)

### 1. 配置守护进程并生成安全令牌
在 Android 目标设备（需 Root 权限）上准备配置：
```bash
# 生成 32 字节高熵令牌
openssl rand -hex 32
```
创建 `/data/adb/wx_stream/config.json`（参考 `daemon/config.example.json`）：
```json
{
  "bind": "127.0.0.1",
  "port": 8765,
  "uds_path": "@wechat_stream_ipc",
  "db_path": "/data/adb/wx_stream/vault.db",
  "token": "你的高熵随机Token",
  "auth": true
}
```

### 2. 编译与部署组件
- **探针 APK**：在 `android-module/` 下执行 `./gradlew assembleRelease`，在设备上安装并于 LSPosed / Vector 中启用。
- **Go 守护进程**：进入 `daemon/` 交叉编译为 Android ARM64：
  ```bash
  CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags="-s -w" -o wx_daemon .
  ```
  推送至 `/data/local/tmp/wx_daemon`。
- **启停守护**：
  ```bash
  su -c "/data/local/tmp/wx_daemon -config /data/adb/wx_stream/config.json &"
  ```
  或者将 `deploy/wx_autostart.sh` 与 `deploy/wx_watchdog.sh` 安装至 `/data/adb/service.d/` 实现开机常驻。

### 3. 本地端口映射 (Zero Exposure via ADB Forward)
由于守护进程绑定于 `127.0.0.1:8765`，在工作站端执行端口转发即可安全访问：
```bash
adb forward tcp:8765 tcp:8765
```

测试健康探针：
```bash
curl -H "Authorization: Bearer 你的高熵随机Token" http://127.0.0.1:8765/health
```

### 4. 接入个人 AI 助理 (Webhook Bridge)
配置环境变量并启动通用 Webhook 转发器：
```bash
export WX_DEVICE_TOKEN="你的高熵随机Token"
export ASSISTANT_WEBHOOK_URL="https://your-dify-or-agent-webhook-url"
export ASSISTANT_WEBHOOK_TYPE="dify"  # 支持 'dify', 'feishu', 'custom'

# 启动轮询并转发
python3 clients/assistant_webhook.py --poll-interval 10
```

---

## 📚 详细文档导航 (Documentation)

- [完整 API 规范文档 (API Specification)](docs/API_SPEC.md)
- [系统架构与鲁棒性设计 (Architecture)](docs/ARCHITECTURE.md)
- [网络暴露与安全隧道指引 (Networking & Port Forwarding)](docs/NETWORKING.md)
- [设备部署与初始化指引 (Setup Guide)](docs/SETUP_GUIDE.md)
- [对接 Dify / 飞书机器人指南 (Personal Assistant Integration)](docs/integrations/personal_assistant.md)
- [可选方案：青龙面板定时调度 (QingLong Integration)](docs/integrations/qinglong.md)

---

## 🔒 免责声明与安全政策 (Disclaimer & Security)

本项目仅供个人数据便携性研究与技术交流使用。详细条款请参阅：
- [免责声明 (DISCLAIMER.md)](DISCLAIMER.md)
- [安全策略与漏洞披露 (SECURITY.md)](SECURITY.md)

---

## 📄 许可证 (License)

本项目采用 [MIT License](LICENSE) 开源。
