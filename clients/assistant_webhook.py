#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
assistant_webhook.py - Universal Personal Assistant Webhook Bridge.
Polls WeChat Pad Stream export endpoint, normalizes new messages into structured
events, and delivers them via HTTP POST to AI Assistant Webhooks (Dify, Feishu, Custom).

Zero third-party dependencies (Python 3 stdlib only).
"""

from __future__ import annotations

import argparse
import datetime
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Dict, List, Optional, Tuple

DEFAULT_ENDPOINT = os.environ.get("WX_EXPORT_ENDPOINT", "http://127.0.0.1:8765/api/wechat/export")
DEFAULT_TOKEN = (
    os.environ.get("WX_DEVICE_TOKEN", "").strip()
    or os.environ.get("WX_EXPORT_TOKEN", "").strip()
    or os.environ.get("WXPAD_TOKEN", "").strip()
)
DEFAULT_WEBHOOK_URL = os.environ.get("ASSISTANT_WEBHOOK_URL", "")
DEFAULT_WEBHOOK_TYPE = os.environ.get("ASSISTANT_WEBHOOK_TYPE", "custom").lower()


def parse_iso(ts_str: str) -> Optional[datetime.datetime]:
    if not ts_str:
        return None
    try:
        return datetime.datetime.fromisoformat(ts_str)
    except Exception:
        return None


def fetch_export(
    endpoint: str,
    token: str = "",
    watermark: Optional[str] = None,
    timeout: int = 15,
) -> Tuple[int, Optional[Dict[str, Any]], Optional[str]]:
    url = endpoint
    if watermark:
        parsed = urllib.parse.urlparse(endpoint)
        query = urllib.parse.parse_qs(parsed.query)
        query["since"] = [watermark]
        url = urllib.parse.urlunparse(parsed._replace(query=urllib.parse.urlencode(query, doseq=True)))

    req = urllib.request.Request(
        url,
        headers={"User-Agent": "wechat-pad-exporter-assistant-bridge/1.0"},
    )
    if token:
        req.add_header("Authorization", f"Bearer {token}")

    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status = resp.getcode()
            body = resp.read().decode("utf-8", "replace")
            return status, json.loads(body), None
    except urllib.error.HTTPError as e:
        return e.code, None, f"HTTPError {e.code}"
    except Exception as e:
        return 0, None, f"{type(e).__name__}: {e}"


def format_payload(
    webhook_type: str,
    chat_id: str,
    msg: Dict[str, Any],
) -> Dict[str, Any]:
    sender = msg.get("sender", "")
    ts = msg.get("ts", "")
    kind = msg.get("kind", msg.get("type", "text"))
    text = msg.get("text", msg.get("content", ""))
    note = msg.get("note", "")

    # Backwards-compatible content extraction: if text is empty, fall back to note tag
    content = text if text else (f"[{note}]" if note else "")
    msg_id = msg.get("id", "")
    is_sender = (
        msg.get("is_sender")
        if "is_sender" in msg
        else (sender in ("self", "我"))
    )

    if webhook_type == "feishu":
        # Format as Feishu interactive / text webhook payload
        header = f"[{'Self' if is_sender else sender}] in {chat_id} ({ts})"
        return {
            "msg_type": "text",
            "content": {
                "text": f"{header}\n{content}",
            },
        }

    if webhook_type == "dify":
        # Format as Dify Workflow trigger payload
        return {
            "inputs": {
                "chat_id": chat_id,
                "sender": sender,
                "content": content,
                "msg_type": kind,
                "timestamp": ts,
                "is_sender": is_sender,
                "raw_id": msg_id,
                "kind": kind,
                "text": text,
                "note": note,
            },
            "response_mode": "streaming",
            "user": sender or chat_id,
        }

    # Default 'custom' or OpenAI-compatible format
    return {
        "event": "wechat_message",
        "chat_id": chat_id,
        "sender": sender,
        "is_sender": is_sender,
        "content": content,
        "type": kind,
        "kind": kind,
        "text": text,
        "note": note,
        "timestamp": ts,
        "ts": ts,
        "id": msg_id,
    }
def post_webhook(
    webhook_url: str,
    payload: Dict[str, Any],
    dry_run: bool = False,
    timeout: int = 10,
) -> bool:
    if dry_run or not webhook_url:
        print(f"[DRY-RUN] Webhook POST -> {webhook_url or '(no url)'}")
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        return True

    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        webhook_url,
        data=data,
        headers={
            "Content-Type": "application/json",
            "User-Agent": "wechat-pad-exporter-assistant-bridge/1.0",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return 200 <= resp.getcode() < 300
    except Exception as e:
        print(f"[ERROR] Failed to post webhook: {e}", file=sys.stderr)
        return False


def process_chats(
    chats: List[Dict[str, Any]],
    webhook_url: str,
    webhook_type: str,
    dry_run: bool = False,
    ignore_self: bool = False,
) -> int:
    sent_count = 0
    for chat in chats:
        chat_id = chat.get("chat_id", "unknown")
        messages = chat.get("messages", [])
        for msg in messages:
            is_sender = (
                msg.get("is_sender")
                if "is_sender" in msg
                else (msg.get("sender") in ("self", "我"))
            )
            if ignore_self and is_sender:
                continue
            payload = format_payload(webhook_type, chat_id, msg)
            ok = post_webhook(webhook_url, payload, dry_run=dry_run)
            if ok:
                sent_count += 1
    return sent_count


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Forward real-time WeChat messages to AI Assistant Webhooks (Dify, Feishu, Custom)."
    )
    parser.add_argument(
        "--endpoint",
        default=DEFAULT_ENDPOINT,
        help=f"WeChat Pad Exporter API endpoint (default: {DEFAULT_ENDPOINT})",
    )
    parser.add_argument(
        "--token",
        default=DEFAULT_TOKEN,
        help="Bearer token for daemon authentication",
    )
    parser.add_argument(
        "--webhook-url",
        default=DEFAULT_WEBHOOK_URL,
        help="Target Webhook URL (default: env ASSISTANT_WEBHOOK_URL)",
    )
    parser.add_argument(
        "--webhook-type",
        choices=["custom", "dify", "feishu"],
        default=DEFAULT_WEBHOOK_TYPE,
        help="Webhook format type: custom (default), dify, feishu",
    )
    parser.add_argument(
        "--poll-interval",
        type=int,
        default=0,
        help="Poll interval in seconds (0 = single shot run)",
    )
    parser.add_argument(
        "--ignore-self",
        action="store_true",
        help="Ignore messages sent by self (is_sender == true)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print parsed events without sending HTTP requests to webhook",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    if not args.dry_run and not args.webhook_url:
        print("[WARN] No --webhook-url provided and not in --dry-run mode. Messages will only be logged.", file=sys.stderr)

    current_watermark: Optional[str] = None
    print(f"[assistant_webhook] Starting bridge (endpoint={args.endpoint}, type={args.webhook_type}, dry_run={args.dry_run})")

    while True:
        code, data, err = fetch_export(args.endpoint, token=args.token, watermark=current_watermark)
        if err or code != 200:
            print(f"[poll] Fetch error (code={code}): {err}", file=sys.stderr)
        elif data:
            new_wm = data.get("watermark")
            chats = data.get("chats", [])
            msg_count = data.get("manifest", {}).get("message_count", 0)

            if msg_count > 0:
                count = process_chats(
                    chats,
                    args.webhook_url,
                    args.webhook_type,
                    dry_run=args.dry_run,
                    ignore_self=args.ignore_self,
                )
                print(f"[poll] Forwarded {count} messages to assistant webhook (watermark: {new_wm})")
            if new_wm:
                current_watermark = new_wm

        if args.poll_interval <= 0:
            break
        time.sleep(args.poll_interval)

    return 0


if __name__ == "__main__":
    sys.exit(main())
