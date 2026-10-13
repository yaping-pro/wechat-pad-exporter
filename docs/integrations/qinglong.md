# 青龙面板定时消费集成指引 (QingLong Integration)

青龙面板（QingLong）是一款常用的定时任务管理面板。本项目可无缝作为青龙定时任务运行，实现每日定时归档与监控。

## 1. 添加定时任务

在青龙面板 Web 控制台创建任务：
- **名称**：`微信 Pad 消息增量拉取`
- **命令**：`task clients/fetch_export.py`
- **定时规则**：`*/15 * * * *` (每 15 分钟执行一次)

## 2. 环境变量配置

在青龙「环境变量」中配置以下选项：
- `WX_EXPORT_ENDPOINT`: `http://<TARGET_HOST>:8765/api/wechat/export`
- `WX_DEVICE_TOKEN`: 守护进程的 Bearer Token
- `WX_FRESHNESS_MINUTES`: `180` (超时告警时间阈值，默认 3 小时)
- `WX_STATE_FILE`: `/ql/data/scripts/.wx_export_state.json` (游标持久化路径)

## 3. 静默期与自愈机制说明
`fetch_export.py` 内置了 `/health` 上游健康校验。当深夜数小时无新微信消息到达时，脚本将自动判定为 `ok(quiet)` 并正常退出，避免误触报警通知。
