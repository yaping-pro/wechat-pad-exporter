# 变更日志 (Changelog)

本项目所有显著变更均记录于此文档。
本项目遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/) 语义化版本规范，格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

---
## [v1.0.5] - 2026-10-14
### Added
- **社区持久备用频道**：在 README 交流群章节与 Issue 模板 `contact_links` 中新增 Telegram 群组 [`t.me/WeChatExporter`](https://t.me/WeChatExporter)，作为微信群二维码过期后的稳定备用交流渠道。

### Changed
- **Android 模块构建工具链升级（AGP 9.4.1 + Gradle 9.8.1 + 内置 Kotlin）**：升级 Android Gradle Plugin 至 `9.4.1`，Gradle Wrapper 至 `9.8.1`；移除独立的 `org.jetbrains.kotlin.android` 插件，改用 AGP 9 内置 Kotlin 支持，配置迁移为顶层 `kotlin { compilerOptions { jvmTarget.set(JvmTarget.JVM_17) } }`；升级 `kotlinx-coroutines-android` 至 `1.11.0`；清理并收敛所有 Dependabot 依赖 PR。

### Fixed
- **CI 发布资产命名清洗与 Go 工具链版本解耦**：发布工作流对 `GITHUB_REF_NAME` 资产文件名执行字符清洗，避免非标准引用（如 PR 分支 `4/merge`）破坏文件复制；`build-daemon.yml` 与 `release.yml` 改用 `go-version-file: daemon/go.mod` 统一解析 Go 版本，替代写死的 Go 版本号。
## [v1.0.4] - 2026-10-13
### Security & Correctness
- **S-001 (P1, security)**: 抽象 UDS / 文件系统 UDS 连接引入底层 peer UID 校验机制 (`SO_PEERCRED` / `LOCAL_PEERCRED`)。仅允许白名单 UID (root 0 或微信 UID) 接入，拒绝未授权本地对端注入；支持 `-allow-uid` 与 `allowed_peer_uids` 配置。
- **S-004 (P1, privacy)**: 修复 JSON 解析失败日志泄漏私聊明文问题，仅打印错误类型、字节长度与 SHA-256 前缀哈希。
- **O-003 (P1, performance)**: SQLite 查询下推安全上限门禁 (`LIMIT limit*K + 1`)，杜绝大时间窗口 OOM，并在命中上限时返回 `truncated: true`。
- **S-003 (P1, hardening)**: 移除 `setup_phone.sh` 对 `/data/local/tmp` 的 `chmod 777` 操作，维持 Android 默认权限 `0771` 并创建专用 `0700` 工作区；全局休眠与进程杀手修改收归显式 `--enable-global-tuning` 门禁，并新增 `deploy/teardown_phone.sh` 原生回滚脚本。
- **P0-a (P0, reliability)**: 修复 `wx_watchdog.sh` 与 `wx_autostart.sh` 在健康探测中未携带 Bearer 令牌导致探活死循环并频繁报 401 的问题。
- **P0-b (P0, robustness)**: 修复 `deploy/service.sh` 在缺少 `config.json` 时裸启导致守护进程 fail-closed 崩溃退出，添加前置友好校验与报错引导。
- **R-001 & R-004 (P1, release truth)**: 发布 CI 中增加证书公钥指纹 (`76:AD:A6:29:...`) 强比对与 `zipalign -c 4` 门禁；在 `SECURITY.md` 正式公示官方签名指纹。
- **R-003 & R-005 (P1, build consistency)**: Go 守护进程版本通过 `-ldflags -X main.Version=${TAG}` 动态注入；tag 触发构建流与 Release 发布收敛为单一构建链，杜绝二次构建漂移。
- **CI / Fork PR**: 为 fork PRs 提供瞬时 debug keystore 自动降级构建，保证开源贡献者 PR 绿灯。
- **P2 (DevOps)**: 将 `scripts/selfcheck.sh` 改造为环境无关的自适应脚本，未配置外部主机时优雅跳过。

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
