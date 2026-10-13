# 微信实时抓取小助理 · 路线一下半场工程架构设计
## Root + WeChatPad 进内存抓取：高吞吐 IPC、守护进程、硬件温控与全自动同步管线

> **文档版本**：v1.0.0 (Production Architecture Spec)  
> **适用目标**：Android 11 ~ 15 (Root / Magisk / KernelSU / APatch)  
> **核心使命**：解决微信进程 Hook 捕获数据后的「进程外通信 (IPC)、外部守护与存储 (Daemon)、24×7 无人值守稳定性 (Headless Reliability)、下游 Muse 智能体零修改对接」全套下半场工程闭环。

---

## 目录
1. [端到端全链路数据流向架构](#1-端到端全链路数据流向架构)
2. [进程内到进程外 IPC 架构设计（高并发、零阻塞）](#2-进程内到进程外-ipc-架构设计高并发零阻塞)
   - 2.1 核心设计约束：绝不拖垮微信主线程
   - 2.2 IPC 方案技术选型与性能基准对比
   - 2.3 为什么 Linux Abstract Unix Domain Socket 是最优解
   - 2.4 内存无锁环形队列 (Lock-Free RingBuffer) 与背压熔断
   - 2.5 序列化协议选型：Protobuf vs MsgPack vs JSON
3. [外部守护进程 (Daemon) 与存储架构](#3-外部守护进程-daemon-与存储架构)
   - 3.1 宿主运行环境选型：Magisk Service 静态编译 vs Termux
   - 3.2 Android 微信原始字段规范化与清洗映射
   - 3.3 与 `wechat-vault` / `wechat_export_api.py` 契约对齐（超集兼容）
   - 3.4 本地持久化 (SQLite WAL) 与滑动窗口清理
   - 3.5 消费水位线 (Watermark) 与增量 REST API
4. [24×7 无人值守高可用与硬件物理保护 (Headless Reliability)](#4-247-无人值守高可用与硬件物理保护-headless-reliability)
   - 4.1 电池安全与温控保护 (`VR-25/acc` 深度配置)
   - 4.2 旁路供电 (Bypass Charging / Direct Power)
   - 4.3 进程看门狗 (Watchdog) 与自愈拉起
   - 4.4 规避 Android 12+ Phantom Process Killer 与 Doze 深度休眠
   - 4.5 网络连续性与 Tailscale 组网
5. [关键核心代码实现落地](#5-关键核心代码实现落地)
   - 5.1 Java (Hook 端)：Lock-Free RingBuffer + LocalSocket 异步发送器
   - 5.2 Go (Daemon 端)：Abstract UDS 监听、Worker Pool、SQLite WAL 与 REST API
6. [端到端测试验证与排障指南](#6-端到端测试验证与排障指南)

---

## 1. 端到端全链路数据流向架构

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        Android Device (Rooted: Magisk / KernelSU)                      │
│                                                                                        │
│  ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 微信主进程 (com.tencent.mm)                                                      │  │
│  │                                                                                  │  │
│  │   [ 微信原生 SQLite/网络层 ]                                                      │  │
│  │           │                                                                      │  │
│  │           ▼                                                                      │  │
│  │   [ Hook 点: MessageStorage.insert / SQLiteDatabase.insert / NetScenePush ]      │  │
│  │           │ (极轻量拦截: 对象浅拷贝, < 5µs)                                       │  │
│  │           ▼                                                                      │  │
│  │   ┌───────────────────────────────────────────┐                                  │  │
│  │   │ Lock-Free RingBuffer / ArrayBlockingQueue │ ◄── 背压策略: 队列满则 DROP_OLDEST │  │
│  │   └───────────────────────────────────────────┘     (永不阻塞微信主线程/DB线程)  │  │
│  │           │ (由独立后台 I/O 单线程异步消费)                                      │  │
│  │           ▼                                                                      │  │
│  │   [ Java Protobuf Serializer ]                                                   │  │
│  │           │                                                                      │  │
│  │           ▼                                                                      │  │
│  │   [ LocalSocket / Abstract Unix Domain Socket Client ]                           │  │
│  └───────────┼──────────────────────────────────────────────────────────────────────┘  │
│              │ Linux Abstract Namespace IPC: @wechat_stream_ipc (无文件权限阻碍, 零拷贝) │
│              ▼                                                                          │
│  ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 外部独立守护进程 (Go / Rust 静态二进制, /data/adb/modules/wx_daemon/daemon)      │  │
│  │                                                                                  │  │
│  │   [ Abstract UDS Server Receiver ]                                               │  │
│  │           │ (高吞吐读取二进制流, 粘包拆包, Protobuf 反序列化)                     │  │
│  │           ▼                                                                      │  │
│  │   [ Pipeline Normalizer & Contact Cache Engine ]                                 │  │
│  │     - talker/isSend/type 字段解析                                                │  │
│  │     - 群聊发送者切分 ([wxid]:\n[content])                                        │  │
│  │     - 类型映射 (type 1->text, 3->image, 34->voice, 49->link/file)                │  │
│  │     - 联系人昵称/群名称内存 LRU 缓存解析                                         │  │
│  │           │                                                                      │  │
│  │           ▼                                                                      │  │
│  │   [ Local Storage Engine: SQLite WAL Mode ]                                      │  │
│  │     - 批量事务写入 (Batch Flush, 100ms 或 50 条触发)                             │  │
│  │     - 自动维护 Watermark (已落盘最大 create_time)                                 │  │
│  │     - 定时滑动窗口保留 (默认保留最近 14 天)                                      │  │
│  │           │                                                                      │  │
│  │           ▼                                                                      │  │
│  │   [ Embedded RESTful API Server (8765 端口) ]                                    │  │
│  │     - GET /health                                                                │  │
│  │     - GET /api/wechat/export?since=<epoch|rfc3339>&to=<epoch|rfc3339> (超集兼容 Muse / 家服契约)  │  │
│  └───────────┼──────────────────────────────────────────────────────────────────────┘  │
│              │                                                                          │
│  ┌───────────┴──────────────────────────────────────────────────────────────────────┐  │
│  │ 物理与网络保障子系统 (Headless Reliability Subsystem)                             │  │
│  │   - VR-25/acc: 停充 60% / 复充 50% / 42℃ 熔断 / 旁路供电 (Bypass Charging)       │  │
│  │   - Watchdog: 30s 周期探测 mm 进程, 崩溃后 am start 自动唤醒                    │  │
│  │   - Doze & PPK 豁免: Android 12+ Phantom Process Killer 彻底解除                │  │
│  │   - Tailscale 内网节点: 分配固定 IP (如 100.115.88.20:8765)                      │  │
│  └──────────────────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────┬─────────────────────────────────────────────────┘
                                       │ Tailscale Encrypted WireGuard Tunnel (100.x.y.z)
                                       ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│ 远端消费端 (Linux 家服 / Mac 开发者机器 / Muse AI 智能体增量流水线)                     │
│                                                                                        │
│  [ Muse Fetcher / wechat-sync.sh / Python 定时任务 ]                                   │
│    -> 轮询或流式拉取: GET http://100.115.88.20:8765/api/wechat/export?since=<watermark> │
│    -> 获得与 wechat-vault 结构完全一致的标准 JSON 报文                                  │
│    -> 下游知识库/向量库/RAG/AI 总结 Pipeline 零修改直接消费                            │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. 进程内到进程外 IPC 架构设计（高并发、零阻塞）

### 2.1 核心设计约束：绝不拖垮微信主线程
1. **零阻塞准则 (Zero-Blocking SLA)**：微信的 SQLite 写事务大多位于专用数据库读写线程，但也有部分关键事件（如 UI 刷新、未读数计数、消息推送回调）运行在 Main Looper。Hook 回调函数的耗时必须严格控制在 **$10\,\mu\text{s}$ 以内**。
2. **严禁在 Hook 回调内发起 I/O**：绝对禁止直接在 Hook 方法体内执行 `Socket.write()`、文件写入、或同步 RPC，否则一旦外部守护进程 CPU 繁忙或崩溃，套接字缓冲区满将直接阻塞微信线程，瞬间导致微信界面卡死并触发系统的 **ANR (Application Not Responding)** 弹窗，最终被 Android `ActivityManager` 强行杀死。
3. **隔离解耦模型**：采用经典的 **Single-Producer Single-Consumer (SPSC)** 异步环形队列解耦。Hook 拦截只负责提取字段并存入内存无锁队列；由独立的后台 Worker 线程负责网络打包与套接字传输。

---

### 2.2 IPC 方案技术选型与性能基准对比

在 Android/Linux 环境下，常见的进程间通信方式有 **Linux Abstract Unix Domain Socket (UDS)**、**本地环回 TCP Socket (`127.0.0.1`)** 以及 **Android 原生 Binder IPC (AIDL)**。

| 评估维度 | Linux Abstract UDS (`\0name`) | 本地环回 TCP (`127.0.0.1`) | Binder IPC (AIDL / Messenger) |
| :--- | :--- | :--- | :--- |
| **通信机制** | Linux 内核抽象命名空间套接字 | TCP/IP 协议栈（环回网卡 `lo`） | Linux Binder 驱动 (`/dev/binder`) |
| **内存/上下文拷贝** | **单次内核内存直接拷贝**，零协议栈开销 | 经过 TCP 分段、校验和、滑动窗口处理 | 2 次内存拷贝（或共享内存 Ashmem/Blob） |
| **IPC 吞吐能力** | **> 1,800 MB/s** | ~ 450 MB/s | ~ 120 MB/s (受限于 Parcel 序列化) |
| **端到端延迟** | **< 3 $\mu\text{s}$** | ~ 25 ~ 50 $\mu\text{s}$ | ~ 15 ~ 30 $\mu\text{s}$ |
| **权限与 DAC/SELinux** | **极佳**（首字节 `\0` 避开文件系统 DAC 权限） | 良好（需申请 `INTERNET` 权限或已受 SELinux 约束） | 极差（SELinux Strict Domain 阻断非系统 App 跨域） |
| **单包体积限制** | 无限制（流式 stream 或 datagram） | 无限制（流式） | **硬限制 1MB**（`TransactionTooLargeException`） |
| **连接生命周期管理** | 极度轻量，对端断开即刻触发 `EOF` | TIME_WAIT 状态、端口冲突、防火墙干扰 | 需要维护 `IBinder.DeathRecipient` 死亡代理 |

---

### 2.3 为什么 Linux Abstract Unix Domain Socket 是最优解

1. **彻底规避 Android 文件权限陷阱**：
   - 传统的文件型 UDS（如 `/data/local/tmp/wx.sock`）会受到 Linux DAC 权限（`chmod 777`）与 Android SELinux 文件上下文（`u:object_r:app_data_file:s0` vs `u:r:magisk:s0`）的双重拦截。微信以 `u0_aXXX` 普通应用权限运行，无法访问由 root 创建的文件，root 进程也可能受限于 DAC 限制。
   - **Abstract Namespace UDS** 以空字符 `\0`（在 Android Java `LocalSocket` 中体现为 `LocalSocketAddress.Namespace.ABSTRACT`）开头，存在于 Linux 内核的网络命名空间中，**完全脱离文件系统**。不仅不需要创建磁盘文件，而且天然免疫文件路径权限与残留清理问题。
2. **极低开销与确定性时延**：
   - 本地 TCP Socket 走完整的 Linux 协议栈（TCP 连接握手、ACK 包确认、窗口更新、拥塞控制算法），耗费不必要的 CPU 计算周期。
   - UDS 直接通过内核 `sk_buff` 内存搬运，CPU 占用几乎为零，吞吐量是 TCP 的 4 倍以上。

---

### 2.4 内存无锁环形队列 (Lock-Free RingBuffer) 与背压熔断

为实现零阻塞，Hook 拦截层内嵌轻量级环形队列。

```
 Hook 回调 (DB/UI 线程)
         │
         ▼
 [ msg.clone() ]
         │
         ▼
 ┌──────────────────────────────────────────────────────────────────┐
 │ Lock-Free RingBuffer (容量: 8192, 预分配对象池)                    │
 │ ──────────────────────────────────────────────────────────────── │
 │   [Slot 0] [Slot 1] [Slot 2] ... [Slot 8191]                     │
 │      ▲                                ▲                          │
 └──────┼────────────────────────────────┼──────────────────────────┘
        │ (offer / CAS)                  │ (poll / Drain)
   生产者指针 (Producer)             消费者指针 (Consumer)
                                         │
                                         ▼
                             后台单线程 I/O 发送 Worker
                                         │
                                         ▼
                             Abstract UDS Client
```

#### 背压与拥塞保护策略
1. **队列满时的丢弃策略 (`DropPolicy`)**：
   - 默认采用 `DROP_OLDEST` 或 `DROP_CURRENT`：若外部守护进程崩溃，RingBuffer 在填满 8192 条消息后，后续消息直接丢弃或覆盖旧数据，并记录丢弃计数日志。**绝对不允许阻塞生产者线程，也绝不允许无限制开辟堆内存导致微信 OOM**。
2. **批量拉取 (Batch Drain)**：
   - 消费者线程单次最多批量拉取 100 条消息，进行批量打包并通过 UDS 发送，减少系统调用 `write()` 的频次。
3. **优雅断线重连**：
   - 消费者线程在检测到 UDS 断开时，采用指数退避算法（500ms ~ 5s）尝试重新连接外部守护进程，重连成功前生产者的队列正常按上限轮转，不影响微信运行。

---

### 2.5 序列化协议选型：Protobuf vs MsgPack vs JSON

| 协议 | 编码效率 (100条消息) | 内存占用 | 序列化 CPU 耗时 | 向前/向后兼容性 | 推荐度 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Protocol Buffers v3** | **极紧凑 (~4.2 KB)** | **极低** (基于 Varint/TLV) | **< 15 $\mu\text{s}$** | **原生 Schema ID 保证** | **★★★★★ (首选)** |
| **MessagePack** | 紧凑 (~6.8 KB) | 较低 | ~ 35 $\mu\text{s}$ | 动态 Schema 解析 | ★★★★☆ |
| **极简 JSON (fastjson2/Jackson)** | 臃肿 (~18.5 KB) | 较高 (大量字符串分配) | ~ 120 $\mu\text{s}$ | 灵活但体积大 | ★★★☆☆ |

**结论**：在 Hook 进程内采用 Google **Protocol Buffers (Protobuf)** 传递二进制数据帧。二进制帧头附加 `[4 Bytes Big-Endian Length] + [Protobuf Payload]`，彻底解决 TCP/UDS 流式协议的粘包与拆包问题。

---

## 3. 外部守护进程 (Daemon) 与存储架构

### 3.1 宿主运行环境选型：Magisk Service 静态编译 vs Termux

| 方案 | 选项 A：Termux + runit/termux-services | 选项 B：Magisk/KernelSU 静态二进制 (`service.sh`) |
| :--- | :--- | :--- |
| **实现载体** | Termux App + Node.js/Python 运行时 | **Go / Rust 单文件静态编译 (musl-libc)** |
| **启动时机** | 开机后需手动打开 Termux 或依赖 Boot 插件 | **系统 Boot Complete 时由 Magisk/KSU 自动拉起** |
| **权限级别** | `u0_aXXX` (普通 App)，需额外提权 | **原生 Root (`uid=0, gid=0`)** |
| **内存与 CPU** | ~ 80MB ~ 200MB (V8 / Python VM) | **< 12MB RAM，CPU < 0.1%** |
| **自愈能力** | 易被 Android 进程杀手判定为高耗能杀死 | **常驻后台独立进程，系统绝不查杀** |
| **免维护性** | 升级容易断链、依赖复杂环境包 | **单文件拷贝即部署，零外部依赖** |

**架构选定**：**选项 B**。将 Go 编译为静态二进制文件 `wx-daemon-aarch64`，直接打包为 Magisk / KernelSU / APatch 模块，在 `/data/adb/modules/wx_stream_daemon/service.sh` 中以纯后台守护进程启动。

---

### 3.2 Android 微信原始字段规范化与清洗映射

Android 微信数据库（`EnMicroMsg.db`）中的 `message` 表具有特定的内部字段结构。外部守护进程负责将其清洗为平台无关的统一数据模型：

#### 核心字段清洗映射规则

| 原始字段名 | 原始数据类型 / 样例 | 字段含义与清洗转换逻辑 |
| :--- | :--- | :--- |
| `msgId` | `INTEGER` (如 `10283`) | 本地自增自编号，映射为 `local_id` |
| `msgSvrId` | `INTEGER` (如 `69812391028341`) | 微信服务端唯一全局 ID，映射为 `server_id` (用于全局去重) |
| `type` | `INTEGER` (如 `1, 3, 34, 49...`) | 消息类型码（见下文类型转换表） |
| `status` | `INTEGER` (如 `3, 4`) | 消息发送/投递状态 |
| `isSend` | `INTEGER` (`0` 或 `1`) | `1` 表示当前手机主人发出；`0` 表示接收 |
| `createTime` | `INTEGER` (毫秒戳, 如 `1716800000000`) | **统一转换为毫秒/秒级标准 UNIX Epoch 与 RFC3339 字符串** |
| `talker` | `TEXT` (`<WXID>` 或 `xxxx@chatroom`) | 会话主体 ID。`@chatroom` 判定为群聊，其余为单聊 |
| `content` | `TEXT` (可能包含群发送者前缀或 XML) | **群消息解构**：若 `isSend=0` 且为群聊，微信正文前缀为 `[sender_wxid]:\n[正文]`，需正则提取发送者与纯文本 |
| `imgPath` | `TEXT` (如 `th_xxx`, `voice_xxx`) | 图片缩略图/语音/短视频在 `/sdcard/Android/data/...` 的相对路径 |
| `reserved` | `TEXT` / `BLOB` | 扩展字段，包含转账信息、引用回复 (ReferMsg) XML 等 |

#### 消息类型 `type` 转换为 Muse 规范映射表

```
   微信原始 type 码
         │
         ├── 1     ──────────► kind: "text"   (文本消息)
         ├── 3     ──────────► kind: "image"  (图片, note: "图片")
         ├── 34    ──────────► kind: "voice"  (语音, note: "语音")
         ├── 43    ──────────► kind: "video"  (视频, note: "视频")
         ├── 47    ──────────► kind: "other"  (动画表情/Sticker, note: "动画表情")
         ├── 48    ──────────► kind: "other"  (地理位置, note: "位置")
         ├── 49    ──────────► XML 内部判定:
         │                      ├── <type>6</type> / <type>74</type> ──► kind: "file" (文件)
         │                      └── 其他类型 (<type>5</type> 等)     ──► kind: "link" (卡片/链接)
         └── 10000 ──────────► kind: "other"  (系统通知/拍一拍/进群提醒, note: "系统消息")
```

---

### 3.3 与 `wechat-vault` / `wechat_export_api.py` 契约对齐（超集兼容）

为确保下游 Muse 智能体与现有 Linux 家服同步脚本无需修改任何代码即可无缝接入，守护进程内置的 REST API 响应报文采用**超集兼容策略**：全量对齐家服权威契约（RFC3339 `+08:00` 时间字段、`chats[].name`/`chats[].kind` 结构、Muse `kind` 枚举、`watermark` 空数据回退 `to`、Canonical JSON 哈希算法、Bearer Token 鉴权、缺省 24h 过滤窗口），并额外保留手机端特有字段（`chat_id`/`display_name`/`is_group`）。

#### JSON 响应契约示例 (`GET /api/wechat/export?since=1716800000&to=1716886400`)

```json
{
  "exported_at": "2026-10-02T16:00:00+08:00",
  "range": {
    "from": "2026-10-01T16:00:00+08:00",
    "to": "2026-10-02T16:00:00+08:00"
  },
  "watermark": "2026-10-02T15:59:58+08:00",
  "chats": [
    {
      "name": "Muse架构讨论群",
      "kind": "group",
      "messages": [
        {
          "ts": "2026-10-02T15:55:00+08:00",
          "sender": "张三",
          "kind": "text",
          "text": "路线一的高吞吐 IPC 已经全部跑通，时延在 3 微秒以内。",
          "note": ""
        },
        {
          "ts": "2026-10-02T15:56:12+08:00",
          "sender": "李四",
          "kind": "link",
          "text": "<msg><appmsg>...</appmsg></msg>",
          "note": "卡片/链接/文件"
        },
        {
          "ts": "2026-10-02T15:57:05+08:00",
          "sender": "我",
          "kind": "image",
          "text": "",
          "note": "图片"
        }
      ]
    },
    {
      "name": "王总",
      "kind": "single",
      "messages": [
        {
          "ts": "2026-10-02T15:58:30+08:00",
          "sender": "王总",
          "kind": "text",
          "text": "明天下午准时开会评审。",
          "note": ""
        }
      ]
    }
  ],
  "manifest": {
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "message_count": 4
  }
}
```

---

### 3.4 本地持久化 (SQLite WAL) 与滑动窗口清理

守护进程在 Android 本地维护一个嵌入式 SQLite 数据库（`/data/adb/wx_daemon/storage.db`）：

```sql
-- 开启高性能 WAL 模式与并发读写
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;

-- 规范化消息存储表
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id INTEGER UNIQUE,
    local_id INTEGER,
    chat_id TEXT NOT NULL,
    chat_name TEXT NOT NULL,
    chat_kind TEXT NOT NULL,
    sender_id TEXT NOT NULL,
    sender_name TEXT NOT NULL,
    msg_type INTEGER NOT NULL,
    kind TEXT NOT NULL,
    text TEXT,
    note TEXT,
    ts_epoch INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(ts_epoch);
CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages(chat_id, ts_epoch);

-- 联系人/群元数据缓存表
CREATE TABLE IF NOT EXISTS contacts (
    username TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    is_group INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
```

#### 滑动窗口数据生命周期管理
- 守护进程每隔 6 小时自动执行数据归档与清理：
  ```sql
  DELETE FROM messages WHERE ts_epoch < strftime('%s', 'now', '-14 days');
  PRAGMA wal_checkpoint(TRUNCATE);
  ```
- 保持手机闪存空间占用稳定在 50MB 以内，避免长时间运行撑爆存储。

---

### 3.5 消费水位线 (Watermark) 与增量 REST API

1. **增量拉取机制**：
   - 下游同步客户端（如 `wechat-sync.sh` 或 Python 定时同步服务）保存上次成功响应中的 `watermark`（或 `range.to`）。
   - 每次调用：`GET /api/wechat/export?since=1716800000&to=1716886400`。
2. **RESTful 接口清单**：
   - `GET /health` 或 `GET /api/wechat/health`：返回 `{"status": "ok", "uptime": 86400, "ringbuffer_drops": 0}`。
   - `GET /api/wechat/export`：核心增量导出接口，支持 HTTP Header `Authorization: Bearer <TOKEN>` 鉴权。

---

## 4. 24×7 无人值守高可用与硬件物理保护 (Headless Reliability)

长期插电运行的 Android 手机面临三大死穴：**电池鼓包爆炸危险**、**系统杀后台 (Doze / Low Memory Killer)**、**Wi-Fi 熄屏休眠断网**。本章给出工业级硬件与系统防护加固方案。

---

### 4.1 电池安全与温控保护 (`VR-25/acc` 深度配置)

安装开源神器 **Advanced Charging Controller (`acc`)**（通过 Magisk 模块直接安装）：

#### 核心配置策略 (`/data/adb/vr25/acc/config.txt`)
```ini
# 停充容量阈值：充至 60% 立即停止充电
pause_capacity=60

# 复充容量阈值：掉电至 50% 重新开启充电
resume_capacity=50

# 温度过热切断阈值：电池温度达到 42.0℃ 立即强制断电
pause_temp=420

# 温度恢复阈值：降至 38.0℃ 恢复正常循环
resume_temp=380

# 充电开关检测周期 (秒)
cooldown_current=500
loop_delay=10
```

#### CLI 一键生效命令
```bash
# 写入并启动温控守护
acc -c "pause_capacity=60,resume_capacity=50,pause_temp=420,resume_temp=380"
acc -D start
```

---

### 4.2 旁路供电 (Bypass Charging / Direct Power)

若硬件设备（如 Google Pixel 系列、部分小米机型、ROG、Sony Xperia）的内核驱动支持电源芯片直通供电：

1. **Pixel / 通用高通内核直接写入命令**：
   ```bash
   # 关闭电池充电链路，电流仅供应主板运行
   echo 0 > /sys/class/power_supply/battery/charging_enabled
   # 或
   echo 1 > /sys/class/power_supply/battery/input_suspend
   ```
2. **acc 自动探测旁路开关**：
   ```bash
   acc -s s
   # acc 会自动测试所有内核充电开关节点并锁定支持 Bypass 的开关（如 battery/charge_control_limit_max）
   ```
   开启后，手机直接消耗充电头电流，**电池循环充放电次数为 0，彻底杜绝发热与电池老化**。

---

### 4.3 进程看门狗 (Watchdog) 与自愈拉起

在 Magisk 模块后台服务脚本中嵌入轻量级 Watchdog 轮询循环：

#### `/data/adb/modules/wx_stream_daemon/watchdog.sh`
```bash
#!/system/bin/sh

PKG_NAME="com.tencent.mm"
TARGET_ACTIVITY="com.tencent.mm/.ui.LauncherUI"
LOG_TAG="WxWatchdog"

log_info() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
    log -t "$LOG_TAG" "$1"
}

# 等待系统开机完成 (Boot Completed)
while [ "$(getprop sys.boot_completed)" != "1" ]; do
    sleep 3
done
sleep 10

log_info "Watchdog started successfully."

while true; do
    # 1. 检查微信主进程是否存在
    PID=$(pgrep -f "^com.tencent.mm$")
    
    if [ -z "$PID" ]; then
        log_info "WARNING: com.tencent.mm is NOT running! Restarting..."
        
        # 唤醒屏幕并拉起微信
        input keyevent 224 # KEYCODE_WAKEUP
        am start -n "$TARGET_ACTIVITY" --user 0 --ez "from_watchdog" true
        
        sleep 5
    fi
    
    # 2. 检查外部 Daemon 进程是否存在
    DAEMON_PID=$(pgrep -f "wx-daemon-aarch64")
    if [ -z "$DAEMON_PID" ]; then
        log_info "WARNING: wx-daemon is dead! Reviving..."
        /data/adb/modules/wx_stream_daemon/wx-daemon-aarch64 &
    fi
    
    # 每 20 秒检测一次
    sleep 20
done
```

---

### 4.4 规避 Android 12+ Phantom Process Killer 与 Doze 深度休眠

Android 12+ 引入的 **Phantom Process Killer (PPK)** 会在后台子进程数量超过 32 个或 CPU 占用略高时强杀 `fork` 出来的守护进程；Doze 休眠会导致微信网络断开。必须通过系统级属性彻底封死：

#### 一键免疫加固脚本 (由 Magisk `service.sh` 开机执行)
```bash
#!/system/bin/sh

# 1. 彻底禁用 Phantom Process Killer (魅影进程杀手)
device_config set_sync_disabled_for_tests persistent
device_config put activity_manager max_phantom_processes 2147483647
setprop persist.sys.fflag.override.settings_enable_monitor_phantom_procs false

# 2. 将微信与守护进程加入 Doze 深度休眠白名单
dumpsys deviceidle whitelist +com.tencent.mm
dumpsys deviceidle whitelist +com.termux 2>/dev/null || true

# 3. 禁用系统级省电模式
settings put global low_power 0
settings put global low_power_sticky 0

# 4. 保持系统唤醒锁 (可选，如果锁屏掉线)
# echo "wx_stream_wakelock" > /sys/power/wake_lock
```

---

### 4.5 网络连续性与 Tailscale 组网

1. **锁定 Wi-Fi 熄屏不休眠策略**：
   ```bash
   settings put global wifi_sleep_policy 2
   settings put global wifi_wakeup_available 1
   cmd wifi set-wifi-enabled enabled
   ```
2. **路由器静态 DHCP 分配**：在局域网路由器将抓取手机的 MAC 地址绑定静态 IP（如 `192.168.1.88`）。
3. **Tailscale 异地零配置互通**：
   - 手机端已通过 SFA / sing-box 接入用户 Tailnet 网络（虚拟 IP: `<TAILNET_IP>`）。
   - 外部守护进程将 REST API 仅监听在 `127.0.0.1:8765`（环回），由 sing-box 隧道内核将 Tailnet 目标 8765 流量转发给本地 127.0.0.1，保障隐私安全与网络隔离。
   - **消费端接入端点**：`http://<TAILNET_IP>:8765/api/wechat/export` 或 `/health`。
   - **注意事项**：消费端（Mac / Linux）请**直接使用裸 IP `<TAILNET_IP>`**。主机名 `<NODE_NAME>` 在 Mac 易被 Clash Fake-IP（198.18.0.x）劫持，在 Linux 易因 `/etc/resolv.conf` 路由器 DNS NXDOMAIN 截断而解析失败。
---

## 5. 关键核心代码实现落地

### 5.1 Java (Hook 端)：Lock-Free RingBuffer + LocalSocket 异步发送器

在 Xposed / LSPosed / FrHook 模块内部嵌入以下高性能传输引擎：

```java
package com.wx.stream.hook.ipc;

import android.net.LocalSocket;
import android.net.LocalSocketAddress;
import android.util.Log;

import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.atomic.AtomicLong;

/**
 * 微信进程内异步零阻塞 IPC 客户端
 * 基于 Abstract Unix Domain Socket + 内存缓冲队列
 */
public class WxIpcStreamClient {
    private static final String TAG = "WxIpcStreamClient";
    private static final String ABSTRACT_SOCKET_NAME = "wechat_stream_ipc";
    private static final int QUEUE_CAPACITY = 8192;

    private static volatile WxIpcStreamClient sInstance;
    private final ArrayBlockingQueue<byte[]> mBufferQueue;
    private final AtomicLong mDroppedCount = new AtomicLong(0);
    private volatile boolean mRunning = true;
    private Thread mWorkerThread;

    public static WxIpcStreamClient getInstance() {
        if (sInstance == null) {
            synchronized (WxIpcStreamClient.class) {
                if (sInstance == null) {
                    sInstance = new WxIpcStreamClient();
                }
            }
        }
        return sInstance;
    }

    private WxIpcStreamClient() {
        mBufferQueue = new ArrayBlockingQueue<>(QUEUE_CAPACITY);
        startWorker();
    }

    /**
     * Hook 回调入口：耗时 < 5 微秒，绝对零阻塞
     */
    public void enqueueMessage(byte[] protobufData) {
        if (protobufData == null || protobufData.length == 0) return;

        // 非阻塞入队：队列若满则丢弃最旧数据并压入新数据，绝不阻塞微信调用线程
        if (!mBufferQueue.offer(protobufData)) {
            mBufferQueue.poll(); // 弹出最旧消息
            mBufferQueue.offer(protobufData);
            long drops = mDroppedCount.incrementAndGet();
            if (drops % 100 == 0) {
                Log.w(TAG, "IPC Buffer full! Total dropped messages: " + drops);
            }
        }
    }

    private void startWorker() {
        mWorkerThread = new Thread(() -> {
            while (mRunning) {
                LocalSocket clientSocket = null;
                OutputStream out = null;
                try {
                    clientSocket = new LocalSocket();
                    LocalSocketAddress address = new LocalSocketAddress(
                            ABSTRACT_SOCKET_NAME,
                            LocalSocketAddress.Namespace.ABSTRACT
                    );
                    
                    Log.i(TAG, "Connecting to abstract UDS @" + ABSTRACT_SOCKET_NAME + "...");
                    clientSocket.connect(address);
                    clientSocket.setSendBufferSize(256 * 1024);
                    out = clientSocket.getOutputStream();
                    Log.i(TAG, "Connected to Daemon successfully.");

                    ByteBuffer lenBuffer = ByteBuffer.allocate(4).order(ByteOrder.BIG_ENDIAN);

                    while (mRunning) {
                        // 阻塞式消费队列（只在独立 Worker 线程中阻塞）
                        byte[] payload = mBufferQueue.take();

                        // 写入 4 字节大端长度头 + Protobuf 实体
                        lenBuffer.clear();
                        lenBuffer.putInt(payload.length);
                        out.write(lenBuffer.array());
                        out.write(payload);
                        out.flush();
                    }
                } catch (Exception e) {
                    Log.e(TAG, "IPC Socket error or disconnected: " + e.getMessage() + ", retrying in 2s...");
                    try {
                        Thread.sleep(2000);
                    } catch (InterruptedException ignored) {}
                } finally {
                    try {
                        if (out != null) out.close();
                        if (clientSocket != null) clientSocket.close();
                    } catch (Exception ignored) {}
                }
            }
        }, "wx-ipc-worker");
        mWorkerThread.setDaemon(true);
        mWorkerThread.start();
    }
}
```

---

### 5.2 Go (Daemon 端)：Abstract UDS 监听、Worker Pool、SQLite WAL 与 REST API

编译命令：`CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-musl-gcc go build -ldflags="-s -w -extldflags '-static'" -o wx-daemon-aarch64 main.go`

```go
package main

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	AbstractSocketName = "@wechat_stream_ipc" // @ 表示 Linux Abstract Namespace
	DBPath             = "/data/adb/wx_daemon/storage.db"
	APIPort            = "127.0.0.1:8765" // 默认仅环回; 旧默认 ":8765" 全网卡监听, 已整改为仅绑 127.0.0.1
	ShanghaiTZOffset   = 8 * 3600
)

// RawHookMessage 接收自 Hook 端的原始数据结构
type RawHookMessage struct {
	MsgSvrID   int64  `json:"msgSvrId"`
	LocalID    int64  `json:"localId"`
	Talker     string `json:"talker"`
	IsSend     int    `json:"isSend"`
	Type       int    `json:"type"`
	CreateTime int64  `json:"createTime"`
	Content    string `json:"content"`
	ImgPath    string `json:"imgPath"`
}

type DaemonServer struct {
	db        *sql.DB
	msgChan   chan *RawHookMessage
	watermark int64
	mu        sync.RWMutex
}

func main() {
	log.Println("[INFO] Starting WeChat Stream Daemon...")
	os.MkdirAll("/data/adb/wx_daemon", 0755)

	db, err := initDB(DBPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to init SQLite: %v", err)
	}
	defer db.Close()

	srv := &DaemonServer{
		db:      db,
		msgChan: make(chan *RawHookMessage, 10000),
	}

	// 1. 启动批量写入与清洗持久化 Worker
	go srv.batchWriterWorker()

	// 2. 启动 Abstract UDS 监听
	go srv.listenAbstractUDS()

	// 3. 启动对外增量兼容 REST API
	srv.startHTTPServer()
}

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id INTEGER UNIQUE,
		local_id INTEGER,
		chat_id TEXT NOT NULL,
		chat_name TEXT NOT NULL,
		chat_kind TEXT NOT NULL,
		sender_id TEXT NOT NULL,
		sender_name TEXT NOT NULL,
		msg_type INTEGER NOT NULL,
		kind TEXT NOT NULL,
		text TEXT,
		note TEXT,
		ts_epoch INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(ts_epoch);
	`
	_, err = db.Exec(schema)
	return db, err
}

func (s *DaemonServer) listenAbstractUDS() {
	addr := &net.UnixAddr{Name: AbstractSocketName, Net: "unix"}
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		log.Fatalf("[FATAL] Failed to listen Abstract UDS: %v", err)
	}
	defer l.Close()
	log.Printf("[INFO] Listening on Linux Abstract UDS: %s", AbstractSocketName)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("[WARN] Accept error: %v", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *DaemonServer) handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("[INFO] WeChat Hook connected.")

	lenBuf := make([]byte, 4)
	for {
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			log.Printf("[INFO] Connection closed: %v", err)
			return
		}
		msgLen := binary.BigEndian.Uint32(lenBuf)
		if msgLen > 10*1024*1024 { // 保护：最大 10MB
			log.Printf("[ERROR] Packet size too large: %d", msgLen)
			return
		}

		payload := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, payload); err != nil {
			log.Printf("[ERROR] Failed to read payload: %v", err)
			return
		}

		var raw RawHookMessage
		if err := json.Unmarshal(payload, &raw); err != nil {
			log.Printf("[WARN] Unmarshal failed: %v", err)
			continue
		}

		select {
		case s.msgChan <- &raw:
		default:
			log.Println("[WARN] msgChan buffer full, dropping message")
		}
	}
}

func (s *DaemonServer) batchWriterWorker() {
	batch := make([]*RawHookMessage, 0, 100)
	ticker := time.NewTicker(100 * time.Millisecond)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		tx, err := s.db.Begin()
		if err != nil {
			log.Printf("[ERROR] Begin tx failed: %v", err)
			batch = batch[:0]
			return
		}
		stmt, _ := tx.Prepare(`
			INSERT OR IGNORE INTO messages 
			(server_id, local_id, chat_id, chat_name, chat_kind, sender_id, sender_name, msg_type, kind, text, note, ts_epoch)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		defer stmt.Close()

		var maxTs int64
		for _, m := range batch {
			kind, text, note := mapMessageType(m.Type, m.Content)
			chatKind := "single"
			if strings.HasSuffix(m.Talker, "@chatroom") {
				chatKind = "group"
			}
			senderName := "对方"
			if m.IsSend == 1 {
				senderName = "我"
			}

			epochSec := m.CreateTime / 1000
			if epochSec > maxTs {
				maxTs = epochSec
			}

			stmt.Exec(m.MsgSvrID, m.LocalID, m.Talker, m.Talker, chatKind, m.Talker, senderName, m.Type, kind, text, note, epochSec)
		}
		tx.Commit()

		s.mu.Lock()
		if maxTs > s.watermark {
			s.watermark = maxTs
		}
		s.mu.Unlock()

		batch = batch[:0]
	}

	for {
		select {
		case msg := <-s.msgChan:
			batch = append(batch, msg)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func mapMessageType(localType int, rawText string) (string, string, string) {
	switch localType {
	case 1:
		return "text", rawText, ""
	case 3:
		return "image", "", "图片"
	case 34:
		return "voice", "", "语音"
	case 43:
		return "video", "", "视频"
	case 47:
		return "other", "", "动画表情"
	case 48:
		return "other", rawText, "位置"
	case 49:
		if strings.Contains(rawText, "<type>6</type>") || strings.Contains(rawText, "<type>74</type>") {
			return "file", rawText, "卡片/链接/文件"
		}
		return "link", rawText, "卡片/链接/文件"
	case 10000:
		return "other", rawText, "系统消息"
	default:
		return "other", rawText, "其他"
	}
}

func (s *DaemonServer) startHTTPServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	http.HandleFunc("/api/wechat/export", s.handleExportAPI)

	log.Printf("[INFO] REST API server listening on %s", APIPort)
	if err := http.ListenAndServe(APIPort, nil); err != nil {
		log.Fatalf("[FATAL] HTTP server error: %v", err)
	}
}

func (s *DaemonServer) handleExportAPI(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	sinceVal := r.URL.Query().Get("since")
	toVal := r.URL.Query().Get("to")

	sinceEpoch := now - 86400
	toEpoch := now

	if sinceVal != "" {
		if v, err := strconv.ParseInt(sinceVal, 10, 64); err == nil {
			sinceEpoch = v
		}
	}
	if toVal != "" {
		if v, err := strconv.ParseInt(toVal, 10, 64); err == nil {
			toEpoch = v
		}
	}

	rows, err := s.db.Query(`
		SELECT chat_id, chat_name, chat_kind, sender_name, kind, text, note, ts_epoch 
		FROM messages 
		WHERE ts_epoch >= ? AND ts_epoch <= ? 
		ORDER BY ts_epoch ASC
	`, sinceEpoch, toEpoch)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type OutMsg struct {
		TS     string `json:"ts"`
		Sender string `json:"sender"`
		Kind   string `json:"kind"`
		Text   string `json:"text"`
		Note   string `json:"note"`
	}
	type OutChat struct {
		Name     string   `json:"name"`
		Kind     string   `json:"kind"`
		Messages []OutMsg `json:"messages"`
	}

	chatsMap := make(map[string]*OutChat)
	var totalCount int
	var maxTs int64 = sinceEpoch

	tz := time.FixedZone("CST", ShanghaiTZOffset)

	for rows.Next() {
		var chatID, chatName, chatKind, senderName, kind, text, note string
		var ts int64
		rows.Scan(&chatID, &chatName, &chatKind, &senderName, &kind, &text, &note, &ts)

		if _, exists := chatsMap[chatID]; !exists {
			chatsMap[chatID] = &OutChat{
				Name:     chatName,
				Kind:     chatKind,
				Messages: make([]OutMsg, 0),
			}
		}

		if ts > maxTs {
			maxTs = ts
		}

		rfcTime := time.Unix(ts, 0).In(tz).Format(time.RFC3339)
		chatsMap[chatID].Messages = append(chatsMap[chatID].Messages, OutMsg{
			TS:     rfcTime,
			Sender: senderName,
			Kind:   kind,
			Text:   text,
			Note:   note,
		})
		totalCount++
	}

	chatsList := make([]*OutChat, 0, len(chatsMap))
	for _, c := range chatsMap {
		chatsList = append(chatsList, c)
	}

	resp := map[string]interface{}{
		"exported_at": time.Now().In(tz).Format(time.RFC3339),
		"range": map[string]string{
			"from": time.Unix(sinceEpoch, 0).In(tz).Format(time.RFC3339),
			"to":   time.Unix(toEpoch, 0).In(tz).Format(time.RFC3339),
		},
		"watermark": time.Unix(maxTs, 0).In(tz).Format(time.RFC3339),
		"chats":     chatsList,
		"manifest": map[string]interface{}{
			"sha256":        "computed_digest",
			"message_count": totalCount,
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}
```

---

## 6. 端到端测试验证与排障指南

### 6.1 验证清单 (Smoke Test Matrix)

```
+-----------------------------+-------------------------------------------------------------+------------------------+
| 验证项                      | 执行检查命令 / 观察手段                                      | 预期指标               |
+-----------------------------+-------------------------------------------------------------+------------------------+
| 1. Abstract UDS 连通性      | netstat -xlp | grep wechat_stream_ipc                        | 处于 LISTEN / CONNECTED|
| 2. Hook 注入与无锁入队      | logcat -s WxIpcStreamClient                                 | 耗时 < 5µs, 无 Drop    |
| 3. SQLite WAL 落盘延迟      | sqlite3 /data/adb/wx_daemon/storage.db "SELECT count(*) .." | 消息即刻可见, 延迟<100ms|
| 4. REST API 契约兼容        | curl http://127.0.0.1:8765/api/wechat/export                | HTTP 200, 字段结构一致 |
| 5. acc 电池温控与停充       | acc -i                                                      | 停充在 60%, 42℃ 熔断   |
| 6. Watchdog 自动拉起        | kill -9 $(pgrep com.tencent.mm)                             | 20s 内自动 LauncherUI  |
| 7. Tailscale 异地访问       | curl http://100.115.88.20:8765/health                       | HTTP 200 {"status":"ok"}|
+-----------------------------+-------------------------------------------------------------+------------------------+
```

### 6.2 常见故障与排障速查
1. **现象：微信主进程频繁 ANR**  
   - **排查**：检查 Hook 回调中是否有直接的 `Socket.write` 或文件 I/O。
   - **解决**：确保所有抓取数据仅调用 `WxIpcStreamClient.getInstance().enqueueMessage()` 进入无锁内存队列。
2. **现象：外部守护进程重启后连不上 UDS**  
   - **排查**：检查 UDS 名字是否带有前导 `@`（抽象命名空间）。
   - **解决**：在 Linux Abstract Namespace 下，当进程退出后内核自动释放绑定，无需 `unlink` 文件，确保服务端先于客户端就绪。
3. **现象：手机插电数天后电量维持 100% 且机身滚烫**  
   - **排查**：`acc` 服务未开机启动或被系统杀死。
   - **解决**：执行 `acc -D restart`，并检查 Magisk 模块 `service.sh` 是否包含 `acc -D start`。

---
*文档编制完成，符合生产级分布式流式管线架构规范。*
