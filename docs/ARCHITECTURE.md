# 系统架构与核心工程设计 (System Architecture)

## 1. 架构定位

`wx-pad-exporter` 是专为**个人数据自主权**与**本地 AI 助理**构建的高鲁棒性聊天流只读导出管道。

```text
[ 微信进程 (Tablet 模式已登录) ]
        │
   (运行时 Hook)  DexKitSniffer: 动态操作码定位 WCDB insertWithOnConflict
        ▼
   Linux Abstract UDS: @wechat_stream_ipc (极速非阻塞推流)
        ▼
[ Go 守护进程 wx_daemon (127.0.0.1:8765) ]
        │  ├── SQLite 本地存储 (vault.db, WAL 模式)
        │  ├── 规范化时间戳与 SHA-256 数据签名
        │  └── Bearer Token 鉴权 HTTP API
        ▼
   安全网络边界 (ADB Forward / Tailscale / SSH Tunnel)
        ▼
[ 消费端套件 clients/ ]
        ├── fetch_export.py (增量游标消费 + 静默期与陈旧告警判决)
        └── assistant_webhook.py (转换事件 -> Dify / 飞书机器人 / 自建 Agent)
```

## 2. 核心技术亮点

### 2.1 基于 DexKit 的操作码模式识别 (Robustness via DexKit)
- **挑战**：微信各大版本代码经过重度混淆（ProGuard / R8），类名与方法名在每个子版本变动频繁。
- **解决方案**：探针不依赖任何硬编码方法名，而是利用 **DexKit** 在运行时动态扫描 Dalvik 字节码指令特征，精准锁定底层 SQLite `insertWithOnConflict` 调用点。
- **鲁棒性收益**：WCDB 底层数据库插入签名数年未变，使探针具备极其出色的跨微信版本自适应能力。

### 2.2 极简非阻塞 Linux 抽象 Unix 域套接字 (Abstract UDS)
- Xposed 探针在微信主线程捕获消息后，经由无文件系统权限依赖的抽象套接字（`@wechat_stream_ipc`）单向投递至守护进程。
- 采用环形缓冲区与重连退避，即使 `wx_daemon` 偶发重启，微信主进程亦完全不受干扰。

### 2.3 独占设备端工业级看门狗 (`wx_watchdog.sh`)
- 负责微信主进程存活拉起（`LauncherUI`）、前台/推送进程 OOM 保护（`-800` / `-500` / `-1000`）、守护进程自愈。
- 独占 Tinker 补丁隔离逻辑：发现热更新补丁自动移入隔离区，保障底包纯净度。
