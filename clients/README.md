# WeChat Pad Stream Exporter - Clients Suite

This directory contains client utilities and integration bridges for consuming the real-time chat stream provided by `wx_daemon`.

## Available Clients

1. **`fetch_export.py`**:
   - Automated incremental consumer with state persistence and freshness monitoring.
   - Evaluates `/health` probe to distinguish quiet hours (`ok(quiet)`) from upstream failures (`STALE`).
   - Supports optional notification callbacks if alerting systems are installed.

2. **`assistant_webhook.py`**:
   - Universal Webhook adapter for forwarding chat events to personal AI assistants.
   - Supports Dify Workflow, Feishu / Lark custom bots, and generic OpenAI-compatible webhook receivers.
   - Provides `--dry-run` mode for testing without sending network requests.

3. **`fetch_phone_wechat.py`**:
   - Quick one-shot CLI utility to dump exported JSON payloads directly to terminal or file.

## Prerequisites & Installation

All scripts in `clients/` use the **Python 3 standard library** with zero required third-party dependencies.

Optional JSON schema validation:
```bash
pip install -r requirements.txt
```

## Quick Start

### 1. Port Forwarding (Recommended for USB / local testing)
Because `wx_daemon` binds to `127.0.0.1:8765` on the Android device for loopback security, map the port to your workstation:
```bash
adb forward tcp:8765 tcp:8765
```

### 2. Pulling Chat Stream
```bash
# If Bearer auth is enabled on wx_daemon:
export WX_EXPORT_TOKEN="your_secure_bearer_token"

# Run incremental consumer
python3 fetch_export.py
```

### 3. Forwarding to Personal Assistant Webhook
```bash
export ASSISTANT_WEBHOOK_URL="https://your-webhook-endpoint/api/v1/webhook"
export ASSISTANT_WEBHOOK_TYPE="dify"  # or 'feishu' or 'custom'

# Test in dry-run mode
python3 assistant_webhook.py --dry-run

# Run daemon polling loop
python3 assistant_webhook.py --poll-interval 10
```
