# 微信 Pad 导出守护进程接口规范 (API Specification)

> **版本**：v1.0.0-pad  
> **协议规范**：RESTful JSON over HTTP  
> **服务组件**：`wx_daemon` (Go)

---

## 1. 架构总览与网络边界

- **绑定原则**：默认仅监听本地回环 `127.0.0.1:8765`。
- **外部拉取方式**：推荐通过 ADB 端口转发 (`adb forward tcp:8765 tcp:8765`) 或安全的点对点加密隧道（如 WireGuard / Tailscale / SSH 隧道）。
- **鉴权模式**：HTTP Authorization Header 携带 `Bearer <TOKEN>`。

---

## 2. 接口详细说明

### 2.1 健康状态探针 (Health Check)
- **请求方式**：`GET /health` （或 `GET /api/wechat/health`）
- **鉴权**：当开启 `auth: true` 时需要 Bearer Token。
- **响应格式**：`application/json`

#### 响应示例 (200 OK)
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

#### 关键字段含义
| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `status` | string | 服务运行状态，`"ok"` 表示正常 |
| `uptime_seconds` | int | 守护进程自启动以来的运行秒数 |
| `uds_connected` | bool | 当前是否有 Xposed 探针模块连接到 Linux 抽象套接字 |
| `total_messages` | int | 本地 SQLite 存储库（`vault.db`）中的总入库消息数 |

---

### 2.2 消息流只读导出 (Message Export)
- **请求方式**：`GET /api/wechat/export`
- **鉴权**：必需 `Authorization: Bearer <TOKEN>`（除非启动时指定 `-no-auth`）。
- **查询参数**：
  - `since` (string, 可选): ISO-8601 格式起始时间戳（如 `2026-10-13T00:00:00+08:00`），仅返回大于该时间戳的消息。
  - `to` (string, 可选): ISO-8601 格式截止时间戳。
  - `limit` (int, 可选): 单次返回的最大消息数量（默认 500）。

#### 响应示例 (200 OK)
```json
{
  "exported_at": "2026-10-13T08:15:00+08:00",
  "range": {
    "start": "2026-10-13T02:31:54+08:00",
    "end": "2026-10-13T08:15:00+08:00"
  },
  "watermark": "2026-10-13T08:00:06+08:00",
  "chats": [
    {
      "chat_id": "gh_22a4fc732a6e",
      "messages": [
        {
          "id": 182910,
          "sender": "gh_22a4fc732a6e",
          "is_sender": false,
          "content": "早安资讯：精选晨读推送...",
          "type": "text",
          "ts": "2026-10-13T08:00:06+08:00"
        }
      ]
    }
  ],
  "manifest": {
    "message_count": 1,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  }
}
```

#### 关键字段含义
| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `watermark` | string | 本批次导出的最大消息时间戳，消费端可将其作为下一次拉取的起始位点 |
| `chats` | array | 按会话聚合的消息数组 |
| `manifest.message_count` | int | 本次返回的总消息数 |
| `manifest.sha256` | string | 本次导出 JSON 规范化后的防篡改校验哈希 |
