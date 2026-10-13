# 青龙定时任务 `wx_export_check` 自动调度验证报告

验证时间：2026-10-03 18:49 → 19:00 (CST)
手段：OpenAPI 轮询 `last_execution_time` + 家服日志目录观测，全程**未手动触发**、未改动任何任务/脚本/设备。

---

## 1. 认证

- `op read` 取 `client_id` / `client_secret`（不打印明文）
- `id_len=12`, `secret_len=24`
- 经 `GET /open/auth/token` 取得 Bearer token：`token_len=36`
- ✅ 认证成功，后续所有查询均带此 token

---

## 2. `saved:false` 含义核实 + DB 状态

**结论：创建响应里的 `saved:false` 是创建瞬间的瞬时态，不影响调度。当前查询任务 `saved=true`，任务已真实落库并启用。**

OpenAPI 当前完整字段（节选）：
```
"id": 8,
"name": "wx_export_check",
"schedule": "*/15 * * * *",
"saved": true,            # 创建时 false，现已 true
"status": 1,              # 1 = 启用
"isDisabled": 0,          # 0 = 未禁用
"last_execution_time": 1791024303,   # = 2026-10-03 18:45:03
"last_running_time": 1,
"log_path": "wx_export_check/2026-10-03-18-45-03-331.log"
```

DB 直接查证（青龙容器 `database.sqlite`，python3 sqlite 读取）：
```
(8, 'wx_export_check', 'task wx_export_check.py', '*/15 * * * *', 1, 0, 1791024303, 1)
#   id  name                command              schedule       status isDisabled last_execution_time last_running_time
```
- 任务**确实存在**于 DB
- `status=1`（启用）、`isDisabled=0`（未禁用）
- ✅ **调度已真正注册，与 `saved:false` 无关**

---

## 3. 自然调度验证（决定性）

等待窗口 18:49:34 → 19:00:14，每 30s 轮询一次，**全程无手动触发**。

| 时刻 | last_execution_time | 说明 |
|------|---------------------|------|
| 18:49:34 (前) | `1791024303` = **18:45:03** | 手动触发那次 |
| 18:50–18:59 每 30s | 不变 `1791024303` | 等待中 |
| **19:00:14** | **`1791025200` = 19:00:00** | ⭐ **自动推进** |

同时日志目录出现**新文件**（非手动触发的 18:45 那次）：
```
新增: /home/<USER>/Projects/qinglong/data/log/wx_export_check/2026-10-03-19-00-00-766.log
```
- 时间戳 `19-00-00` 正是 `*/15 * * * *` 自然命中的整点周期
- 与 18:45 手动触发的 `18-45-03-331.log` / `18-45-00-766.log` 完全区分

✅ **`last_execution_time` 从 18:45:03 → 19:00:00 发生变化**
✅ **日志目录出现自然周期新 `.log` 文件**

---

## 4. 最新自动运行日志内容

`2026-10-03-19-00-00-766.log`（调度器自动产生，非手动）：
```
## 开始执行... 2026-10-03 19:00:00

=== wx_export_check ===
endpoint : http://<TAILNET_IP>:8765/api/wechat/export
chats    : 16
messages : 168
sha256   : 7ace9e017ee876155166584737803abbae5f9d7627421367495471fe0e574fab
watermark: 2026-10-03T18:59:07+08:00 (age_min=0.9, freshness_limit=180)
last_wm  : 2026-10-03T18:41:05+08:00
RESULT   : advanced

## 完成 ✅... 2026-10-03 19:00:01  耗时 1 秒
```
- 脚本真实执行：拉取 16 chats / 168 messages
- `RESULT: advanced`（watermark 从 18:41:05 推进到 18:59:07，有新数据）
- ✅ 内容证明这是调度器自动跑的真实运行，非手动触发残留

---

## 5. state 文件是否推进

`/home/<USER>/Projects/qinglong/data/scripts/.wx_export_state.json`：
```json
{
  "last_watermark": "2026-10-03T18:59:07+08:00",
  "last_sha256": "7ace9e017ee876155166584737803abbae5f9d7627421367495471fe0e574fab",
  "last_success": "2026-10-03T11:00:01.342179+00:00",
  "last_endpoint": "http://<TAILNET_IP>:8765/api/wechat/export",
  "last_messages": 168
}
```
- `last_success` = `2026-10-03T11:00:01Z` = **本地 19:00:01 CST**（即本次自动运行时刻）
- `last_watermark` 从 `18:41:05` → `18:59:07` 已推进
- ✅ state 文件随自动运行正确更新

---

## 6. 结论

✅ **任务 `wx_export_check` (ID 8) 确实被调度器自动执行，不依赖手动触发。**

- `saved:false` 仅为创建时的瞬时字段语义，对调度**无影响**；当前 `saved=true`、DB 中 `status=1/isDisabled=0`，调度已真实注册。
- `last_execution_time` 在自然周期 **19:00:00** 自动从 `18:45:03` 推进到 `19:00:00`。
- 家服日志目录生成全新自动运行日志 `2026-10-03-19-00-00-766.log`，内容与手动触发区分明确，`RESULT: advanced`。
- state 文件 `last_success`/`last_watermark` 随自动运行推进到 19:00 / 18:59:07。

无根因假设需要——自动调度链路完全正常。
