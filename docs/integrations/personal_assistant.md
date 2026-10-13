# 对接个人 AI 助理指南 (Personal Assistant Integrations)

通过 `clients/assistant_webhook.py`，您可以轻松将实时微信聊天流对接到主流 AI 助理平台或自建 Agent 工作流。

---

## 1. 对接 Dify 知识库 / Workflow

Dify 提供了灵活的 HTTP Webhook 触发器。

### 步骤：
1. 在 Dify 中创建 **Workflow** 应用。
2. 添加 **Webhook 触发器** 节点，配置输入参数：`chat_id`, `sender`, `content`, `timestamp`。
3. 启动 `assistant_webhook.py` 并指定类型为 `dify`：
```bash
export ASSISTANT_WEBHOOK_URL="https://dify.your-domain.com/v1/workflows/run"
export ASSISTANT_WEBHOOK_TYPE="dify"
export WX_DEVICE_TOKEN="your_daemon_token"

python3 clients/assistant_webhook.py --poll-interval 15
```

---

## 2. 对接飞书 / Lark 自定义机器人

支持将特定联系人或群组的微信消息实时转发到飞书群话题群。

### 步骤：
1. 在飞书群中添加 **自定义机器人**，获取 Webhook 地址。
2. 启动转发器：
```bash
export ASSISTANT_WEBHOOK_URL="https://open.feishu.cn/open-apis/bot/v2/hook/xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
export ASSISTANT_WEBHOOK_TYPE="feishu"

python3 clients/assistant_webhook.py --poll-interval 10 --ignore-self
```

---

## 3. 自定义 Webhook / OpenAI-Compatible Agent

默认 `custom` 模式会向指定的 HTTP POST 端点发送标准化 JSON 载荷：
```json
{
  "event": "wechat_message",
  "chat_id": "contact_id_or_roomid",
  "sender": "sender_display_name",
  "is_sender": false,
  "content": "消息正文内容",
  "type": "text",
  "timestamp": "2026-10-13T08:00:06+08:00",
  "id": 182910
}
```

测试命令：
```bash
python3 clients/assistant_webhook.py --dry-run
```
