# 变更日志 (Changelog)

本项目所有显著变更均记录于此文档。
本项目遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/) 语义化版本规范，格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

---

## [Unreleased]
### Added
- 基础开源协作治理规范：Issue Templates (Bug Report / Feature Request / Config)、PR Template、CODEOWNERS、Dependabot 自动依赖更新及分支保护策略。

---

## [v1.0.2] - 2026-10-13
### Added
- **完整端到端架构收口**：集成 Android Hook 探针 (`com.wx.sniffer`)、Go 守护进程 (`wx_daemon`) 与 Python 消费端/Webhook 桥接。
- 跨架构构建产物：Android ARM64 二进制、Linux AMD64 二进制及 Xposed APK 自动化构建分发。
- 严格只读契约规范与 JSON Schema 数据合同。

### Fixed
- Webhook 载荷字段对齐，实现客户端数据拉取条数 limit 限制。
- 守护进程鉴权采用 fail-closed 原则，默认严格拒绝未经授权访问并脱敏日志中的 IP。

---

## [v1.0.1] - 2026-10-13
### Added
- 初始发布多架构构建：`wechat-pad-exporter-module-v1.0.1.apk`、`wx_daemon-android-arm64` 与 `wx_daemon-linux-amd64`。
- 守护进程 UDS 通信与 SQLite WAL 存储核心实现。
