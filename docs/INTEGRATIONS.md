# 微信数据导出集成指南 (Integrations & Downstream Recipes)

> **版本**：`v1.0.0` (2026-10-13)  
> **基础契约**：参见 [`docs/DATA_CONTRACT.md`](./DATA_CONTRACT.md)  
> **Schema 校验**：参见 [`schema/export.schema.json`](../schema/export.schema.json)

---

## 1. 概述 (Overview)

本指南汇总了将 `wx-pad-exporter`（或家服 `wechat-export-api`）输出的微信聊天流无缝接入外部智能体的标准实践方案。涵盖以下典型集成场景：
1. **Muse.ai 风格智能体 (Poll & Digest / 早报生成器)**：定时轮询时间切片数据、校验防篡改签名并生成晨间简报。
2. **Grok / 聊天机器人风格 (Webhook Push / 事件流驱动)**：基于 `clients/assistant_webhook.py` 向 Dify、飞书机器人或通用 OpenAI 格式 Webhook 实时分发。
3. **通用编程智能体 (Coding Agent / OMP / Claude Code / Codex)**：智能体在开发或排障会话中通过命令行直接查证微信数据。

---

## 2. 方案一：Muse.ai 风格晨间早报智能体 (Poll & Digest Recipe)

### 2.1 架构原理
家服体系中运行的 Muse AI 早报智能体遵循**低频轮询 + 单调水印增量 + SHA-256 防重**模型：
- **触发时机**：每日早间定时触发（如 07:30 CST）。
- **查询范围**：传入 `?since=<YESTERDAY_WATERMARK>`，拉取自上次早报发布以来的增量消息。
- **校验排重**：校验 `manifest.sha256`。若摘要已存在，说明处于静默窗口，无需重复总结。
- **上下文组装**：将各群聊的最新文本和 AppMsg 文件/链接摘要提取为提示词上下文，投递至大语言模型生成晨读早报。

### 2.2 环境变量配置
```bash
# 目标端点地址（Tailscale 私网或本地 ADB 端口转发）
export WECHAT_EXPORT_URL="http://127.0.0.1:8765/api/wechat/export"
# Bearer Token（若端点开启了鉴权）
export WECHAT_BEARER_TOKEN="your_secure_bearer_token"
# 增量游标持久化文件
export WECHAT_STATE_FILE="$HOME/.local/share/wechat-vault/state/muse_cursor.json"
```

### 2.3 生产级拉取与处理脚本 (`clients/fetch_export.py`)
项目内置的 [`clients/fetch_export.py`](../clients/fetch_export.py) 已原生实现超时指数退避、Tailscale 代理绕过及水印持久化。

直接运行命令：
```bash
python3 clients/fetch_export.py \
  --url "${WECHAT_EXPORT_URL}" \
  --token "${WECHAT_BEARER_TOKEN}" \
  --state-file "${WECHAT_STATE_FILE}" \
  --output "/tmp/morning_brief_input.json"
```

运行输出示例：
```text
[INFO] Loaded watermark: 2026-10-12T07:30:00+08:00
[INFO] Successfully fetched snapshot. Chats: 12, Total Messages: 158
[INFO] Manifest SHA-256 verified: 3758ab9511af67ada30182739c54e54fc7007da3e6dd138692106d34a96de999
[INFO] Updated watermark to: 2026-10-13T07:28:15+08:00
```

---

## 3. 方案二：Grok-bot / 聊天机器人风格实时 Webhook 推送 (Push Recipe)

基于 [`clients/assistant_webhook.py`](../clients/assistant_webhook.py)，可实现近实时的微信消息转发展示与多端联动。

### 3.1 飞书群自定义机器人 (Feishu Bot)
将指定群聊的新消息格式化为飞书卡片推送到团队运营群。

#### 环境变量
```bash
export FEISHU_WEBHOOK_URL="https://open.feishu.cn/open-apis/bot/v2/hook/xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
```

#### 启动指令
```bash
python3 clients/assistant_webhook.py \
  --export-url "http://127.0.0.1:8765/api/wechat/export" \
  --webhook-url "${FEISHU_WEBHOOK_URL}" \
  --webhook-type "feishu" \
  --poll-interval 15 \
  --state-file "/tmp/feishu_bot_cursor.json"
```

### 3.2 Dify 工作流触发 (Dify Workflow)
将微信入站消息作为 Dify 知识库或问答 Agent 的触发源。

#### 环境变量
```bash
export DIFY_API_KEY="app-xxxxxxxxxxxxxxxxxxxxxxxx"
export DIFY_WEBHOOK_URL="https://api.dify.ai/v1/workflows/run"
```

#### 启动指令
```bash
python3 clients/assistant_webhook.py \
  --export-url "http://127.0.0.1:8765/api/wechat/export" \
  --webhook-url "${DIFY_WEBHOOK_URL}" \
  --webhook-type "dify" \
  --poll-interval 10
```

### 3.3 通用 OpenAI / 自定义 HTTP 接收端
投递标准 JSON 载荷（`event: "wechat_message"`, `sender`, `content`, `chat_id`）。

```bash
python3 clients/assistant_webhook.py \
  --export-url "http://127.0.0.1:8765/api/wechat/export" \
  --webhook-url "http://127.0.0.1:9000/webhook/wechat" \
  --webhook-type "custom"
```

---

## 4. 方案三：通用编码智能体消费模式 (Coding Agent: omp / Claude / Codex)

当 AI 工程师或运维智能体（如 `omp`）需要排查或汇总微信数据时，推荐遵循**标准探测三部曲**：

### 步骤 1：健康探针与网络连通性检查
```bash
# 验证 ADB 转发或私网接口可用性
curl -sS --fail http://127.0.0.1:8765/health | jq '{status, total_messages, uds_connected}'
```

### 步骤 2：拉取并执行本地 JSON Schema 验证
```bash
# 拉取最新切片并用 Python 严格校验 Schema
curl -sS -H "Authorization: Bearer ${TOKEN}" "http://127.0.0.1:8765/api/wechat/export?since=$(date -v-1H +%s)" > /tmp/latest_slice.json

python3 -c "
import json, jsonschema
schema = json.load(open('schema/export.schema.json'))
data = json.load(open('/tmp/latest_slice.json'))
jsonschema.validate(instance=data, schema=schema)
print('✅ Snapshot conforms strictly to export.schema.json')
"
```

### 步骤 3：智能体上下文提炼 (提取群聊核心摘要)
```bash
# 仅提取群聊名称和文本行，供智能体理解背景，降低 Token 消耗
jq -r '.chats[] | select(.kind=="group") | "=== " + .name + " ===\n" + (.messages[] | select(.kind=="text") | .sender + ": " + .text)' /tmp/latest_slice.json
```
