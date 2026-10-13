# 青龙 (QingLong) OpenAPI 定时任务创建与验证报告

## 1. 凭据用法判定与认证验证

通过源码分析（`/ql/back/services/open.ts` 与 `/ql/back/api/open.ts`）：
- 青龙系统生成 `client_id` 为 12 位随机字符串，`client_secret` 为 24 位随机字符串。
- 用户提供的凭据匹配如下：
  - **`client_id`**：`<QL_ID>`（12 位）
  - **`client_secret`**：`<QL_TOKEN>`（24 位）
- OpenAPI 认证端点为 **`GET /open/auth/token?client_id=<QL_ID>&client_secret=<QL_TOKEN>`**（已列入青龙路由白名单）。
- 认证成功后返回格式为 `{ "code": 200, "data": { "token": "<AUTH_TOKEN>", "token_type": "Bearer", "expiration": ... } }`。后续所有 OpenAPI 请求均在 Header 中携带 `Authorization: Bearer <AUTH_TOKEN>` 访问 `/open/*` 接口。

### 认证交换 HTTP 回显（脱敏）：
```json
{
  "code": 200,
  "data": {
    "token": "<AUTH_TOKEN_UUID>",
    "token_type": "Bearer",
    "expiration": 1793616277
  }
}
```

---

## 2. 定时任务创建结果

- **接口**：`POST http://<TAILNET_IP>:5700/open/crons`
- **请求头**：
  - `Authorization: Bearer <AUTH_TOKEN>`
  - `Content-Type: application/json`
- **请求体**：
  ```json
  {
    "name": "wx_export_check",
    "command": "task wx_export_check.py",
    "schedule": "*/15 * * * *"
  }
  ```
- **创建结果**：
  - **Cron ID**: `8`
  - **Task Name**: `wx_export_check`
  - **Command**: `task wx_export_check.py`
  - **Schedule**: `*/15 * * * *`（每 15 分钟执行一次）
  - **Status / isDisabled**: `status: 1`（空闲就绪）, `isDisabled: 0`（启用）

### API 创建响应回显：
```json
{
  "code": 200,
  "data": {
    "id": 8,
    "name": "wx_export_check",
    "command": "task wx_export_check.py",
    "schedule": "*/15 * * * *",
    "saved": false,
    "status": 1,
    "timestamp": "Sat Oct 03 2026 18:44:58 GMT+0800 (China Standard Time)",
    "isSystem": 0,
    "isDisabled": 0,
    "log_path": "",
    "isPinned": 0,
    "labels": [],
    "last_running_time": 0,
    "last_execution_time": 0,
    "log_name": "wx_export_check",
    "allow_multiple_instances": 0,
    "updatedAt": "2026-10-03T10:44:58.952Z",
    "createdAt": "2026-10-03T10:44:58.952Z"
  }
}
```

---

## 3. 手动触发与执行验证

通过 OpenAPI `PUT /open/crons/run` 传入 `[8]` 触发任务运行，并通过 `/open/logs/detail` 读取任务执行日志。

### 3.1 青龙任务日志原始回显 (`/open/logs/detail?path=wx_export_check&file=2026-10-03-18-45-03-331.log`)
```
## 开始执行... 2026-10-03 18:45:03

=== wx_export_check ===
endpoint : http://<TAILNET_IP>:8765/api/wechat/export
chats    : 16
messages : 165
sha256   : d285377eb1e9c34f6dca004814157e0880e49c1f1fd097ace40633f2ea950086
watermark: 2026-10-03T18:41:05+08:00 (age_min=4.0, freshness_limit=180)
last_wm  : 2026-10-03T18:41:05+08:00
RESULT   : ok(no new)

## 完成 ✅... 2026-10-03 18:45:04  耗时 1 秒
```

### 3.2 容器与状态文件验证
- **脚本退出码**: `EXIT=0`
- **State 文件内容** (`/ql/data/scripts/.wx_export_state.json`):
  ```json
  {"last_watermark": "2026-10-03T18:41:05+08:00", "last_sha256": "d285377eb1e9c34f6dca004814157e0880e49c1f1fd097ace40633f2ea950086", "last_success": "2026-10-03T10:45:27.070688+00:00", "last_endpoint": "http://<TAILNET_IP>:8765/api/wechat/export", "last_messages": 165}
  ```
- **拉取数据指标**: `16 chats` / `165 messages`，数据真实有效。

---

## 4. 任务排期与任务列表确认

通过 OpenAPI `GET /open/crons?limit=50` 确认青龙当前全部 7 个任务状态，新任务 `wx_export_check`（ID 8）处于启用状态且已加入调度器：

```
ID=8   | Name=wx_export_check                          | Schedule=*/15 * * * *    | Status=1 | isDisabled=0
ID=6   | Name=AfterNoise 微信文件归档与同步 (夜间主运行)             | Schedule=55 23 * * *     | Status=1 | isDisabled=0
ID=7   | Name=AfterNoise 微信文件归档与同步 (日间追赶)              | Schedule=0 9,12,18,21 * * * | Status=1 | isDisabled=0
ID=4   | Name=核心网络配置备份 (主路由 + Linux Clash)             | Schedule=30 3 * * 0      | Status=1 | isDisabled=0
ID=3   | Name=VPS 月度流量监控 (GCP e2-micro)                | Schedule=30 9 * * *      | Status=1 | isDisabled=0
ID=2   | Name=kuma_backup                              | Schedule=0 3 * * *       | Status=1 | isDisabled=0
ID=1   | Name=domain_renew                             | Schedule=0 9 1 * *       | Status=1 | isDisabled=0
```

---

## 5. 可复现步骤（供后续调用）

```bash
# 1. 换取 OpenAPI Bearer Token
AUTH_TOKEN=$(curl -s "http://<TAILNET_IP>:5700/open/auth/token?client_id=<QL_ID>&client_secret=<QL_TOKEN>" | jq -r '.data.token')

# 2. 查询任务列表
curl -s "http://<TAILNET_IP>:5700/open/crons" -H "Authorization: Bearer $AUTH_TOKEN"

# 3. 触发任务运行
curl -s -X PUT "http://<TAILNET_IP>:5700/open/crons/run" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '[8]'

# 4. 查看任务日志
curl -s "http://<TAILNET_IP>:5700/open/logs/detail?path=wx_export_check&file=<LOG_FILE>" \
  -H "Authorization: Bearer $AUTH_TOKEN"
```

---

## 6. 结论

**定时任务 `wx_export_check`（ID: 8, Cron: `*/15 * * * *`）已通过青龙 OpenAPI 成功创建、已触发运行并验证跑通，真实数据拉取成功（16 chats / 165 msgs, exit code 0），无需任何人工干预。**
