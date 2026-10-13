# 微信安卓端点接口规范与对接说明 (Phone Endpoint Spec)

> **面向对象**：Muse 智能体研发团队 / 消费端开发者  
> **文档版本**：v1.0.0-pad (2026-10-03)  
> **线路性质**：**独立测试线路（Test Line）**。作为移动端微信消息嗅探的实验性补充线路，与家服生产线路完全解耦。

---

## 1. 核心架构决策 (Architecture Decisions)

1. **零公网暴露 (Zero Public Exposure)**：
   - 服务端监听并运行于安卓本地 (`wx_daemon`)，通过 Tailscale 用户态网络协议栈（sing-box）接入 Tailnet。
   - 外部仅允许 Tailscale 私网 CGNAT 地址 (`<TAILNET_IP>:8765`) 访问，绝不映射公网 IP、绝不开放任何公网入站端口。
2. **Hook 实时拦截 + 动态按需查库 (Hook & Pull)**：
   - 废除传统的"定时导出静态 JSON 中间文件"方案。
   - 微信进程内通过 Xposed/LSPosed 模块 (`com.wx.sniffer`) 拦截 WCDB 插入事件，经由 Linux 抽象命名空间 Unix Domain Socket (`@wechat_stream_ipc`) 实时流式写入本地 SQLite 存储库 (`/data/adb/wx_stream/vault.db`)。
   - 消费端（如 Muse）在需要生成早报或分析时，按需发起 HTTP GET 请求动态查库并结构化导出。
3. **独立测试线路与家服严格解耦 (Line Decoupling)**：
   - 家服线路 (`http://<TAILNET_IP>:8765`) 是当前主力生产线路；安卓手机线路为独立验证线路。
   - 两端数据各自独立采集、独立存储、独立鉴权，手机测试线的启停或弃用对家服生产线零影响。
4. **早报时间对接规范 (Morning Brief Timing)**：
   - 消费端建议在每日早报生成窗口（如 07:00~08:00 CST）通过增量时间戳 (`since` / `to` 或 `watermark`) 定时拉取过去 24 小时的数据切片。

---

## 2. 接口技术参数表 (Technical Parameters)

| 参数项 | 参数值 / 规范 | 说明 |
| :--- | :--- | :--- |
| **基础端点 (Base URL)** | `http://<TAILNET_IP>:8765` | Tailscale 私网裸 IP（Android MagicDNS 在部分客户端受限，请使用裸 IP） |
| **数据导出接口** | `GET /api/wechat/export` | 核心消息导出接口，支持时间范围过滤 |
| **健康检查探针** | `GET /health`<br/>`GET /api/wechat/health` (别名) | 探活与状态探针，免鉴权，返回 UDS 连接数与消息统计 |
| **鉴权方式** | `Authorization: Bearer <PHONE_TOKEN>` | **当前状态**：默认免密放行（`-token` 缺省为空）；预留 Token 鉴权能力 |
| **时区规范** | `Asia/Shanghai (+08:00)` | 所有返回时间戳（如 `exported_at`, `range`, `ts`）均带 `+08:00` 时区 |
| **网络层级** | Tailscale CGNAT (`100.64.0.0/10`) | HTTP 客户端请求时需确保绕过公网代理，直连 Tailscale 隧道 |

---

## 3. 查询参数与行为契约 (Query Parameters & Contract)

### 3.1 导出接口 `GET /api/wechat/export`

| 参数名 | 类型 | 必填 | 默认值 | 格式支持与行为 |
| :--- | :--- | :--- | :--- | :--- |
| `since` | string / integer | 否 | `now - 86400` (当前时间前推 24 小时) | 起始时间戳。支持 **Unix 秒级时间戳** (如 `1790965509`) 或 **RFC3339 字符串** (如 `2026-10-02T08:00:00+08:00`) |
| `to` | string / integer | 否 | `now` (当前请求时间) | 结束时间戳。支持 **Unix 秒级时间戳** 或 **RFC3339 字符串** |

### 3.2 行为与解析细节
1. **时间格式兼容性**：
   - 服务端原生支持 `RFC3339Nano`、`RFC3339`、`2006-01-02T15:04:05+08:00`、`2006-01-02 15:04:05`、`2006-01-02` 及纯数字 Epoch 秒。
   - 针对 URL Query 解码中可能将时区 `+` 转换为空格的情况（如 `2026-10-02T08:00:00 08:00`），服务端内部已做容错修正。
   - 若传入无法识别的非法时间格式，接口直接返回 `HTTP 400 Bad Request`。
2. **水印 (`watermark`) 机制**：
   - 返回 payload 中的 `watermark` 字段为本次返回消息列表中的**最大消息时间戳**（RFC3339 格式）。
   - 若查询时间段内无任何消息，`watermark` 回退为本次查询的 `to` 时间。
   - 消费端可持久化保存该字段，在下一次同步时作为 `since` 传入以实现连续增量同步。
3. **首次回填建议 (Initial Backfill)**：
   - 安卓端仅记录 Hook 模块启动后实时收到的消息，无端上历史冷库回溯能力。
   - 首次接入时可不传 `since`（默认拉取最近 24 小时），或显式指定测试启动以来的时间戳。

---

## 4. 返回数据格式 (Response Payload & Schema)

### 4.1 导出接口返回 JSON 完整示例

```json
{
  "exported_at": "2026-10-03T08:00:00+08:00",
  "range": {
    "from": "2026-10-02T08:00:00+08:00",
    "to": "2026-10-03T08:00:00+08:00"
  },
  "watermark": "2026-10-03T07:45:12+08:00",
  "chats": [
    {
      "name": "技术预研讨论组",
      "kind": "group",
      "chat_id": "<PHONE>@chatroom",
      "display_name": "技术预研讨论组",
      "is_group": true,
      "messages": [
        {
          "ts": "2026-10-03T07:30:15+08:00",
          "sender": "<WXID>",
          "kind": "text",
          "text": "安卓端实时抓取验证完成，链路正常",
          "note": ""
        },
        {
          "ts": "2026-10-03T07:45:12+08:00",
          "sender": "<WXID>",
          "kind": "image",
          "text": "",
          "note": "图片"
        }
      ]
    },
    {
      "name": "张工",
      "kind": "single",
      "chat_id": "<WXID>",
      "display_name": "张工",
      "is_group": false,
      "messages": [
        {
          "ts": "2026-10-03T07:10:00+08:00",
          "sender": "<WXID>",
          "kind": "file",
          "text": "<msg><appmsg><title>测试报告.pdf</title><type>6</type></appmsg></msg>",
          "note": "卡片/链接/文件"
        }
      ]
    }
  ],
  "manifest": {
    "sha256": "c5f3e9a1b82d4c0e354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945a",
    "message_count": 3
  }
}
```

### 4.2 结构体字段说明

#### 顶层字段 (ExportResponse)
- `exported_at` (string): 导出执行时间（RFC3339 `+08:00`）。
- `range` (object): 查询起止时间范围 `{ "from": "...", "to": "..." }`。
- `watermark` (string): 最新消息时间戳，用于增量游标。
- `chats` (array): 会话聚合列表。
- `manifest` (object): 完整性校验元数据 `{ "sha256": "...", "message_count": N }`。

#### 会话层字段 (ExportChatItem) —— **家服契约的超集**
- `name` (string): 会话展示名称（群名或好友备注名；若为空则回退为 `chat_id`）。
- `kind` (string): 会话类型，枚举值为 `"single"` (私聊) 或 `"group"` (群聊)。
- `chat_id` (string, 可选): **[超集字段]** 原始微信 ID（如 `<WXID>` 或 `xxx@chatroom`）。
- `display_name` (string, 可选): **[超集字段]** 原始展示备注名。
- `is_group` (bool): **[超集字段]** 是否为群聊布尔标记。
- `messages` (array): 该会话内按时间升序排列的消息列表。

#### 消息层字段 (ExportMessageItem) —— **与家服严格一致**
- `ts` (string): 消息发送时间戳（RFC3339 `+08:00`）。
- `sender` (string): 发送者微信 ID（私聊对方为 `wxid`，本机发出统一为 `self` 或当前账号 wxid；群聊为发言人 `wxid`）。
- `kind` (string): 消息类型（标准枚举见 4.3 节）。
- `text` (string): 文本内容或原始结构化 XML（媒体类消息为空字符串）。
- `note` (string): 媒体或类型辅助注释（如 `"图片"`, `"语音"`, `"卡片/链接/文件"`）。
- *注：两端接口均不暴露服务端全局物理 ID (`msg_svr_id`)。*

### 4.3 消息类型 (`kind`) 映射规则

| 微信原始 Type | SubType / 特征 | 映射 `kind` | `text` 内容 | `note` 注释 |
| :--- | :--- | :--- | :--- | :--- |
| `1` | - | `text` | 原始文字消息 | `""` |
| `3` | - | `image` | `""` | `"图片"` |
| `34` | - | `voice` | `""` | `"语音"` |
| `43` | - | `video` | `""` | `"视频"` |
| `49` | SubType 6, 74 或包含 `<type>6</type>` | `file` | 原始 AppMsg XML | `"卡片/链接/文件"` |
| `49` | 其他 AppMsg | `link` | 原始 AppMsg XML | `"卡片/链接/文件"` |
| `47` | Emoji 动画表情 | `other` | `""` | `"动画表情"` |
| `48` | 地理位置 | `other` | 原始位置 XML | `"位置"` |
| `10000`, `10002` | 撤回/系统提示 | `other` | 系统提示内容 | `"系统消息"` |
| 其它未定义 | - | `other` | 原始 Content | `"其他"` |

### 4.4 `manifest.sha256` 计算标准

为支持跨端防篡改与完整性校验，安卓端与家服端保持**完全同构的规范化哈希算法**：
1. 取 `chats` 数组结构。
2. 进行规范化 UTF-8 JSON 序列化：键名升序排序 (`sort_keys=True`)、紧凑分隔符（`,` 与 `:` 无额外空格）、保持 UTF-8 原文字符（不转义中文字符 `ensure_ascii=False`）、不转义 HTML 实体。
3. 对序列化后的 UTF-8 字节流计算 SHA-256 哈希并输出 64 位小写十六进制字符串。

Python 验证参考实现：
```python
import hashlib
import json

def compute_manifest_sha256(chats: list) -> str:
    canonical_bytes = json.dumps(
        chats, 
        ensure_ascii=False, 
        sort_keys=True, 
        separators=(",", ":")
    ).encode("utf-8")
    return hashlib.sha256(canonical_bytes).hexdigest()
```

*当 `chats` 为空数组 `[]` 时，其标准 SHA-256 值为：*  
`4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945`

---

## 5. 状态码与错误处理 (Status Codes & Errors)

| HTTP 状态码 | 含义 | 响应体格式示例 | 发生场景与处理策略 |
| :--- | :--- | :--- | :--- |
| `200 OK` | 请求成功 | `ExportResponse` 结构体 | 正常返回查询结果（即使消息列表为空） |
| `400 Bad Request` | 参数错误 | `{"error": "Invalid time format: \"...\""}` | `since` 或 `to` 时间格式无法解析。需检查时间参数格式 |
| `401 Unauthorized` | 鉴权失败 | `{"error": "Unauthorized"}` | 服务端启用了 `-token` 时，请求未携带或携带了错误的 Bearer Token |
| `405 Method Not Allowed` | 方法不支持 | `method not allowed` | 使用了除 `GET` 以外的 HTTP 请求方法（如 POST/PUT） |
| `500 Internal Server Error`| 服务端异常 | `{"error": "failed to query messages: ..."}` | 本地 SQLite 读取故障或 JSON 序列化失败 |

---

## 6. 端到端测试验证命令 (Verification Commands)

在具备 Tailscale 访问权限的节点（如 Muse 所在环境或工作站）执行以下验证：

### 6.1 健康探针检查 (`/health`)
```bash
# 探测存活状态、UDS 连接与基础指标
curl -sS --noproxy "*" "http://<TAILNET_IP>:8765/health" | jq .
```
*预期回显示例：*
```json
{
  "status": "ok",
  "uptime_seconds": 3600,
  "uds_connected": true,
  "active_uds_connections": 1,
  "total_messages": 74,
  "total_received_stream": 74,
  "memory_bytes": 1048576,
  "version": "1.0.0-pad"
}
```

### 6.2 默认时间窗口导出拉取 (最近 24 小时)
```bash
# 默认拉取最近 24 小时聊天记录
curl -sS --noproxy "*" "http://<TAILNET_IP>:8765/api/wechat/export" | jq .manifest
```

### 6.3 指定精确时间范围拉取 (RFC3339 格式)
```bash
# 查询指定时间区间
curl -sS --noproxy "*" "http://<TAILNET_IP>:8765/api/wechat/export?since=2026-10-02T00:00:00%2B08:00&to=2026-10-03T00:00:00%2B08:00" | jq .range
```

### 6.4 鉴权拦截测试 (预留 Token 启用后)
```bash
# 若守护进程启动时配置了 -token <PHONE_TOKEN>，未带 Token 应拦截返回 401：
curl -i -sS --noproxy "*" "http://<TAILNET_IP>:8765/api/wechat/export"

# 带 Bearer Token 正常请求：
curl -sS --noproxy "*" \
  -H "Authorization: Bearer <PHONE_TOKEN>" \
  "http://<TAILNET_IP>:8765/api/wechat/export" | jq .manifest
```

---

## 7. 与家服端点的差异对照与解耦保证 (Differences & Decoupling)

### 7.1 双端特性全维度对照表

| 维度 | 家服端点 (Homelab Line - 生产主力) | 安卓手机端点 (Phone Line - 测试线路) |
| :--- | :--- | :--- |
| **端点地址** | `http://<TAILNET_IP>:8765` | `http://<TAILNET_IP>:8765` |
| **承载架构** | Linux1 原生 `wechat-export-api.service` | Redmi Pad 本地 `wx_daemon` (Go 静态二进制) |
| **数据捕获机理** | 按需**热解密 PC 端微信 SQLite 本地库** (`mode=ro&immutable=1`) | Xposed 模块在**微信进程内实时拦截 WCDB 插入**，UDS 串流落库 |
| **数据覆盖特性** | 覆盖**全量历史与群聊归档** (实测 182 会话 / 11,317+ 条) | 仅捕获**该安卓设备自启动后实时收到的消息** (实测 13 会话 / 74+ 条起) |
| **两端数据关系** | 各自仅包含本端接收的消息；实测**重叠度为 0%**，属于**互补关系**而非副本 | 各自仅包含本端接收的消息；实测**重叠度为 0%**，属于**互补关系**而非副本 |
| **字段契约** | `name`, `kind`, `messages` | **超集**：包含家服全部字段，额外扩展 `chat_id`, `display_name`, `is_group` |
| **鉴权状态** | **强制 Bearer Token** 鉴权 | **当前默认免密放行**；支持通过 `-token` 启用可选 Bearer 鉴权 |
| **常驻自持机制** | systemd 托管，服务器硬件常驻 | WiFi + sing-box Tailscale + Magisk 启动脚本 (`/data/adb/service.d/wx_autostart.sh`) |
| **线路定位** | **核心生产主力线路**，承担正式早报生成 | **实验性测试线路**，用于验证移动端实时嗅探 |

### 7.2 弃用与不影响家服线的保证 (Decommission Guarantee)

为确保测试线路的任何变动不反噬生产系统，系统遵循以下工程保证：

1. **家服零足迹 (Zero Footprint on Homelab)**：
   - 手机线路的所有代码、Hook 模块、守护进程与配置文件均严格收敛在安卓设备与本地工程仓库中，**绝不在家服 Linux1 部署任何测试脚本或常驻配置**。
2. **下游消费建议 (Muse Integration Rule)**：
   - **请勿将 Muse 早报的主力生产流程强绑定在安卓端点上**。
   - 若 Muse 需同时观察双端数据，建议使用可选客户端聚合脚本 `clients/merge_sources.py`。该脚本已内置故障隔离与降级机制（当安卓端断网或离线时，自动标记 `degraded: ["phone"]` 并平滑返回家服完整数据，不阻断早报产出）。
3. **极简弃用路径 (Zero-Impact Decommissioning)**：
   - 若后续安卓测试结束并决定停用该线路，仅需在手机侧执行清理（停止 `wx_daemon`、停用 sing-box、卸载或在 LSPosed 中禁用模块）。
   - **家服生产侧零改动、零操作、零清理**，Muse 原生直连家服的链路保持 100% 连续稳定运行。

### 7.3 自持机制运维前置条件（user 999 模块 / FBE 原理）

`wx_autostart.sh`（Magisk `service.d`）的 Watchdog 已实现 USB-free 自持。当前部署（2026-10-03 实测）的运行前提如下：
- **补丁隔离由 watchdog 自动执行**：`wx_autostart.sh` 拉起的 Watchdog（`deploy/wx_watchdog.sh` H 段）每 30s 扫描 `tinker/` 目录，发现微信自动下发的 Tinker 热更补丁（`patch-*` / `patch_meta.info`）即隔离到 `/data/adb/wx_stream/tinker_quarantine/`（只移动、绝不杀微信）并回正 `tinker_patch_version_key` 为底包版本，防止"登录突然失效"问题回潮。判据与处置详见 `docs/tablet-login-sop.md` 对应章节。

1. **【已规避】重启后人工解锁（FBE 原理，当前部署不再触发）**：带 PIN 锁屏时，冷启动处于 Direct Boot（`RUNNING_LOCKED`），user 999 的 CE（凭据加密）存储与微信在首次解锁屏幕前不可访问，Watchdog 靠 `while true; sleep 30` 持续尝试，只能在用户解锁后才成功拉起微信与 Hook 才能恢复线路。为获得 **100% 无人值守**，本专用采集终端**已主动移除锁屏凭据**——此后冷启动直接进 `RUNNING_UNLOCKED`，整机无需任何人工介入即自动恢复（实测数据见下）。**若设备日后恢复锁屏凭据，则回到"每次重启后需人工解锁一次（之后可再锁屏）"的前置约束。** 该运维选择的安全含义见 §7.4。
   - **当前实测全链路自动恢复数据（无锁屏凭据，2026-10-03）**：
     - 冷启动到 `sys.boot_completed=1`：约 **53s**
     - 到 `wx_daemon` 就绪 + user 999 已 `RUNNING_UNLOCKED` + LauncherUI 自动拉起 + watchdog 启动：约 **65s**
     - 到微信 PID 拉起且 `uds_connected:true`：约 **67s**（boot 后仅 **14s**）
     - **人工介入次数：0**；全程零 force-stop、`autoauth.cfg`=1114B 完好、模块注入 `maps`=3、3 分钟 PID 恒定。
   - 典型时间线：
     ```text
     17:08:36  adb reboot
     17:09:29  sys.boot_completed=1            (53s)
     17:09:41  wx_daemon 就绪 + u999 已 RUNNING_UNLOCKED + LauncherUI 自动拉起 + watchdog 启动
     17:09:48  微信 PID 5613, uds_connected:true  (boot 后仅 14 秒)
     ```
2. **模块必须装到 user 999**：`com.wx.sniffer` / `com.roro.tablethook` 必须 `pm install-existing --user 999`；若只装在 user 0，Vector 按 user 查包对 user 999 查不到 → 静默跳过注入 → daemon `uds_connected:false`、无新 消息。判据：`grep -icE "sniffer" /proc/<u999微信PID>/maps` = 0。
3. **自启脚本内 `am`/`pm` 输出禁止重定向到 root-only 目录（Binder DAC 陷阱）**：`am`/`pm`/`settings`/`svc`/`dumpsys` 是 `/system/bin/cmd` 封装，会把 stdout/stderr 的 fd 经 Binder 交给 `system_server`(uid 1000) 去写；若重定向到 `/data/adb/`(0700 root-only)，`system_server` 被 DAC 拒绝 → 整个 Binder 事务失败并静默 `Failed transaction (2147483646)`，导致所有 `am`/`pm` 调用成为 no-op（user 999 模块装不上、微信拉不起来）。处置与判据详见 `docs/tablet-login-sop.md` 排错表「`Failed transaction (2147483646)`」一行：所有框架命令必须 `>/dev/null 2>&1` 或重定向到 `/data/local/tmp/`(0771)。

### 7.4 ⚠️ 无锁屏凭据的安全权衡 (Security Trade-off)

为获得 100% 无人值守，本部署**移除了设备锁屏凭据**。这带来明确的安全含义：

- **利**：彻底消除 FBE 限制，冷启动后无需人工介入，线路自动恢复（实测 ~67s）。
- **弊**：设备**物理失窃或丢失时，数据不再有锁屏保护**。任何人拿到设备即可访问已登录的微信会话与 `/data/adb/wx_stream/vault.db` 中的聊天记录。
- **适用前提**：该设备为**专用采集终端**（不承载日常通讯、不存放敏感个人数据），且放置在**物理受控环境**。
- **不适用**：若设备用于日常使用或放置在非受控位置，**应恢复锁屏凭据**，并接受"每次重启后需人工解锁一次"的运维成本。
- **缓解措施（建议）**：
  - 全盘加密仍生效（FBE 在无凭据时用默认密钥，防的是离线拆机读取）
  - 限制 tailnet 访问范围（仅需消费的节点可达）
  - 定期检查 `/data/adb/wx_stream/vault.db` 的保留窗口
  - 设备放置在物理上不可随意取走的位置
## 8. 运维安全边界 (Operational Safety Boundary)

- **严禁任何自动化脚本猜测/枚举/清除设备锁屏凭据（`locksettings verify/set/clear`）**：本项目的代码与自持脚本一律不得触碰锁屏凭据。当前部署的"无锁屏凭据"状态是**运维人员人工在设备设置中主动移除**的（纯手动、非脚本），属于 §7.4 所述、且只适用于专用采集终端的运维选择。
- 设备锁屏与凭据文件（`locksettings.db` / `gatekeeper` / `spblob`）不在本项目的可操作范围内。若设备恢复锁屏凭据，则回到 §7.3 约束：每次重启后需人工解锁一次（之后可再锁屏）。
- 若自持流程需要设备处于解锁态且设备带锁屏，应停下来由人工解锁，不得自动化绕过。