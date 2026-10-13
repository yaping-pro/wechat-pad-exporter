# 微信导出数据契约规范 (Data Egress Contract)

> **版本**：`v1.0.0` (2026-10-13)  
> **适用范围**：`wx-pad-exporter` (安卓 Pad 守护进程) 及 Linux 家服 (`wechat-export-api`)  
> **Schema 定义**：[`schema/export.schema.json`](../schema/export.schema.json)

---

## 1. 概述与核心哲学 (Overview & Core Philosophy)

微信导出数据契约定义了从采集端（无论是运行于 Android 的 `wx-pad-exporter` 守护进程，还是部署于 Linux 家服的 PC 端微信数据库解密导出服务）向各类下游智能体（AI Assistant、早报生成器、知识库检索系统）传输聊天数据的标准只读协议。

### 核心设计原则
1. **快照不可变与零读撕裂 (Immutable Snapshot & Zero Read Tear)**：
   - 导出的每一批数据均为确定性、时间切片绑定的只读快照。
   - 底层存储采用只读不可变模式（家服端 `mode=ro&immutable=1`；安卓端动态 WAL 提交帧实时入库），彻底杜绝并发锁冲突与未完成事务导致的 `database is malformed` 脏读。
2. **规范化完整性与防篡改 (Canonical Hash Verification)**：
   - 响应体必须附带 `manifest.sha256`。该摘要由 `chats` 数组经过严格的规范化 JSON（Canonical JSON: 字典序键名、紧凑分隔符、UTF-8 字符保持）序列化后计算得出，下游消费端可实现亚毫秒级的数据完整性校验与重复抓取排重。
3. **单调游标与增量语义 (Monotonic Cursor & Watermark)**：
   - 响应包含唯一的 `watermark` 游标（RFC3339 时间戳），代表本批次所含消息的最大物理时间点。消费端持久化此游标后即可在下一次请求中作为 `?since=<watermark>` 传入，实现无缝断点续传。
4. **超集兼容与平滑降级 (Superset Compatibility & Graceful Fallback)**：
   - 移动端 `wx-pad-exporter` 输出是家服 PC 导出契约的**超集**。基础消费者仅需读取 `name`, `kind`, `messages` 三个核心字段；高级消费者则可提取 `chat_id`, `display_name`, `is_group` 开展更细粒度的实体关联。

---

## 2. 接口端点契约 (Standard Interface Contract)

### 2.1 基础网络与安全边界

| 参数项 | 手机端 (`wx-pad-exporter`) | 家服端 (`wechat-export-api`) |
| :--- | :--- | :--- |
| **默认监听地址** | `127.0.0.1:8765` (本地回环) | `<TAILNET_IP>:8765` (Tailscale 私网裸 IP) |
| **推荐访问通道** | `adb forward tcp:8765 tcp:8765`<br/>或 Tailscale 私网隧道 | Tailscale 点对点加密通道 (`100.64.0.0/10`) |
| **鉴权机制** | `Authorization: Bearer <TOKEN>`<br/>*(默认 Fail-Closed 强制要求 Token；可通过 `-no-auth` 或 `-insecure-no-auth` 显式免密)* | **强制必须** `Authorization: Bearer <TOKEN>` |
| **时区规范** | `Asia/Shanghai (+08:00)` | `Asia/Shanghai (+08:00)` |
| **公网策略** | **严禁公网暴露**，零端口映射 | **严禁公网暴露**，零端口映射 |

### 2.2 端点规范

#### (1) `GET /health` (或 `GET /api/wechat/health`)
- **功能**：服务存活、运行指标与采集连接健康探针。
- **状态码**：`200 OK`
- **响应体示例**：
```json
{
  "status": "ok",
  "uptime_seconds": 167204,
  "uds_connected": true,
  "active_uds_connections": 1,
  "total_messages": 1718,
  "total_received_stream": 477,
  "memory_bytes": 641264,
  "version": "1.0.0-pad"
}
```

#### (2) `GET /api/wechat/export`
- **功能**：增量/全量拉取结构化消息快照。
- **请求参数**：
  | 参数名 | 类型 | 必填 | 默认值 | 说明与示例 |
  | :--- | :--- | :--- | :--- | :--- |
  | `since` | string/int | 否 | `now - 86400` (前推 24h) | 起始时间戳。支持 RFC3339 字符串（如 `2026-10-13T00:00:00+08:00`）或 Unix Epoch 秒（如 `1791849600`） |
  | `to` | string/int | 否 | `now` (请求发起时刻) | 结束时间戳。支持 RFC3339 或 Unix Epoch 秒 |
  | `limit` | int | 否 | 500 (手机端可按需配置) | 保护性单次返回上限 |

---

## 3. 标准快照 Payload 规范 (Standard Snapshot Schema)

### 3.1 真实数据结构示例 (脱敏)

```json
{
  "exported_at": "2026-10-13T08:15:00+08:00",
  "range": {
    "from": "2026-10-12T08:15:00+08:00",
    "to": "2026-10-13T08:15:00+08:00"
  },
  "watermark": "2026-10-13T08:00:06+08:00",
  "chats": [
    {
      "name": "技术研发交流群",
      "kind": "group",
      "chat_id": "23984719283@chatroom",
      "display_name": "技术研发交流群",
      "is_group": true,
      "messages": [
        {
          "ts": "2026-10-13T07:45:10+08:00",
          "sender": "wxid_dev_alice",
          "kind": "text",
          "text": "早安！今天的服务发布请注意先检查 Tailscale 节点连通性。",
          "note": ""
        },
        {
          "ts": "2026-10-13T07:50:22+08:00",
          "sender": "wxid_dev_bob",
          "kind": "image",
          "text": "",
          "note": "图片"
        }
      ]
    },
    {
      "name": "李工",
      "kind": "single",
      "chat_id": "wxid_arch_li",
      "display_name": "李工",
      "is_group": false,
      "messages": [
        {
          "ts": "2026-10-13T08:00:06+08:00",
          "sender": "wxid_arch_li",
          "kind": "file",
          "text": "<msg><appmsg><title>架构演进方案_v2.pdf</title><type>6</type></appmsg></msg>",
          "note": "卡片/链接/文件"
        }
      ]
    }
  ],
  "manifest": {
    "sha256": "3758ab9511af67ada30182739c54e54fc7007da3e6dd138692106d34a96de999",
    "message_count": 3
  }
}
```

### 3.2 字段定义表

#### 顶层字段
| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `exported_at` | string (RFC3339) | 导出快照生成的时间点（服务器上海时区） |
| `range.from` | string (RFC3339) | 实际查询范围起始时间点 |
| `range.to` | string (RFC3339) | 实际查询范围结束时间点 |
| `watermark` | string (RFC3339) | **增量同步游标**。当前批次所有消息中的最大 `ts`；若无新消息则退化为 `range.to` |
| `chats` | array | 按会话聚合的消息数组 |
| `manifest.sha256` | string (hex, 64) | 依据规范化 JSON 规则计算的 `chats` 数组 SHA-256 校验和 |
| `manifest.message_count` | int | 整个快照包含的消息物理总条数 |

#### 会话层字段 (`chats[]`)
| 字段 | 类型 | 规范属性 | 说明 |
| :--- | :--- | :--- | :--- |
| `name` | string | **核心基础** | 会话展示名称（群名、好友备注名；若为空则自动回退为 `chat_id`） |
| `kind` | string | **核心基础** | 会话类型枚举：`"single"`（私聊 / 单聊）或 `"group"`（群聊） |
| `chat_id` | string | *手机超集* | 微信原始会话标识符（如 `wxid_xxx` 或 `xxx@chatroom`） |
| `display_name`| string | *手机超集* | 原始展示名称（未经 fallback 处理） |
| `is_group` | boolean | *手机超集* | 是否为群聊布尔标记 |
| `messages` | array | **核心基础** | 该会话内按发送时间递增排序的消息列表 |

#### 消息层字段 (`chats[].messages[]`)
| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `ts` | string (RFC3339) | 消息创建/接收时间戳，精确至秒，带时区偏移 |
| `sender` | string | 发送者身份。私聊为发言人标识；群聊为发言人 `wxid` 或展示昵称；本机发送为 `"我"` 或 `"self"` |
| `kind` | string | 归一化消息类型枚举：`"text"`, `"image"`, `"voice"`, `"video"`, `"file"`, `"link"`, `"other"` |
| `text` | string | 纯文本内容或卡片/文件/链接的原始 XML 内容（媒体类消息为空字符串） |
| `note` | string | 消息类型的辅助注释标签（如 `"图片"`, `"语音"`, `"卡片/链接/文件"`） |

---

## 4. 完整性校验：Canonical JSON 与 SHA-256 (Integrity Algorithm)

为保证在不同语言（Python, Go, Node.js, Rust）实现下校验和完全一致，`manifest.sha256` 采用确定性规范化 JSON 序列化规范：

1. **目标对象**：仅对 `chats` 数组（即 `payload["chats"]`）进行摘要计算。
2. **序列化规则**：
   - 字典/对象键名按 Unicode 码点**升序排序** (`sort_keys=True`)。
   - 紧凑分隔符：键值对之间无多余空格，元素之间仅用单逗号分隔（`separators=(",", ":")`）。
   - 保留原生 UTF-8 字符，严禁转义非 ASCII 字符（`ensure_ascii=False`）。
   - 禁用 HTML 实体转义（如 `<` 不得转义为 `\u003c`）。
3. **哈希计算**：将上述生成的 UTF-8 字节流送入标准 SHA-256 计算引擎，输出 64 位小写十六进制文本。

### 各语言实现参考

#### Python
```python
import hashlib
import json

def verify_manifest(payload: dict) -> bool:
    chats = payload.get("chats", [])
    expected_hash = payload.get("manifest", {}).get("sha256", "")
    
    canonical_bytes = json.dumps(
        chats,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":")
    ).encode("utf-8")
    
    actual_hash = hashlib.sha256(canonical_bytes).hexdigest()
    return actual_hash == expected_hash
```

#### Go
```go
func VerifyManifest(chats []ExportChatItem, expectedSha string) bool {
    b, err := json.Marshal(chats)
    if err != nil {
        return false
    }
    var generic any
    _ = json.Unmarshal(b, &generic)

    var buf bytes.Buffer
    enc := json.NewEncoder(&buf)
    enc.SetEscapeHTML(false)
    _ = enc.Encode(generic)
    
    res := buf.Bytes()
    if len(res) > 0 && res[len(res)-1] == '\n' {
        res = res[:len(res)-1]
    }
    hasher := sha256.New()
    hasher.Write(res)
    actual := hex.EncodeToString(hasher.Sum(nil))
    return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedSha)) == 1
}
```

*边界基准：当 `chats` 为空数组 `[]` 时，标准的 SHA-256 值为：*  
`4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945`

---

## 5. 增量消费算法伪代码 (Incremental Consumption Algorithm)

消费端（例如 Muse 早报轮询器、知识库同步 Daemon、Webhook 转发器）应当遵循以下标准循环算法：

```text
Algorithm IncrementalWeChatSync:
    Input: EndpointURL, BearerToken, StorageState (watermark_cursor, seen_sha256_set)
    Output: Processed messages stream

    1. Load cursor from local persistent storage:
       watermark = StorageState.get("last_watermark", default=NOW() - 24_HOURS)
       
    2. Build request:
       URL = EndpointURL + "/api/wechat/export?since=" + UrlEncode(watermark)
       Headers = { "Authorization": "Bearer " + BearerToken }
       
    3. Execute HTTP GET with exponential backoff retry:
       Response = HTTP_GET(URL, Headers)
       If Response.Status != 200:
           HandleError(Response)
           Wait(RetryDelay)
           Return
           
    4. Parse Payload:
       Payload = JSON_DECODE(Response.Body)
       
    5. Integrity Check:
       CalculatedSHA = CanonicalSHA256(Payload.chats)
       If CalculatedSHA != Payload.manifest.sha256:
           Raise IntegrityError("Checksum mismatch! Data corrupted in transit.")
           
    6. Deduplication & Ingestion:
       If Payload.manifest.sha256 in StorageState.seen_sha256_set:
           // Quiet batch; no newly written chats despite watermark tick
           AdvanceCursor(Payload.watermark)
           Return
           
       For each Chat in Payload.chats:
           For each Msg in Chat.messages:
               // Compose unique message dedupe identity
               MsgUID = SHA256(Chat.name + "|" + Msg.ts + "|" + Msg.sender + "|" + Msg.text)
               If MsgUID not in StorageState.seen_messages:
                   IngestIntoAssistant(Chat, Msg)
                   StorageState.seen_messages.add(MsgUID)
                   
    7. Commit State Atomically:
       StorageState.seen_sha256_set.add(Payload.manifest.sha256)
       StorageState.set("last_watermark", Payload.watermark)
       StorageState.persist()
```

---

## 6. 双端实现差异与对齐状态 (Phone vs Homelab Divergence)

在对接消费端前，请明确掌握手机端 (`wx-pad-exporter`) 与家服端 (`wechat-export-api`) 的技术差异。系统在接口层已完成协议归一，但在数据源特征上存在如下已知事实：

| 维度 | 家服 PC 微信端点 (`linux`) | 手机 Pad 微信端点 (`wx_daemon`) |
| :--- | :--- | :--- |
| **数据捕获机理** | 周期性热解密桌面微信本地 SQLite 数据库（WAL 帧内存校验合并） | Xposed 模块在微信进程内实时拦截 WCDB 插入，UDS 串流落库 |
| **历史数据厚度** | 拥有**全量历史与群聊归档**（支持数月甚至数年前的冷数据回溯） | 仅能捕获**该设备安装并启动 Hook 以来实时接收到的新消息**，无历史冷库回溯能力 |
| **发送者标识 (sender)** | 经过联系人反查解析后的**展示名称**（群聊为昵称，私聊为对方备注，本机为 `"我"`） | 原始微信 ID（如 `wxid_xxx` 或 `self` / `"我"`） |
| **会话层结构** | 仅含核心 3 字段：`name`, `kind`, `messages` | **超集**：除上述 3 字段外，扩展提供 `chat_id`, `display_name`, `is_group` |
| **鉴权默认值** | **强制 Bearer Token** 鉴权 | **强制 Bearer Token** 鉴权（缺省空 Token 拒绝启动，需配 `-token` 或显式 `-no-auth`） |
| **两端数据交集** | 实测两端消息重叠度为 0%（各自独立捕获所属设备接收的消息流），为**互补拓扑**而非互为主备 | 同左 |
