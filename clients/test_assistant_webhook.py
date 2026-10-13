#!/usr/bin/env python3
"""
Unit test for assistant_webhook.py payload formatting.
Verifies that schema-conforming message items produce non-empty pushed payloads
under feishu, dify, and custom formats.
"""

import unittest
from assistant_webhook import format_payload, process_chats


class TestAssistantWebhookPayload(unittest.TestCase):
    def setUp(self):
        # A schema-conforming message from export.schema.json / daemon API
        self.schema_msg = {
            "ts": "2026-10-13T10:00:00+08:00",
            "sender": "wxid_friend123",
            "kind": "text",
            "text": "Hello world from WeChat Pad!",
            "note": "",
        }
        self.self_msg = {
            "ts": "2026-10-13T10:01:00+08:00",
            "sender": "self",
            "kind": "text",
            "text": "Replying from my phone",
            "note": "",
        }
        self.image_msg = {
            "ts": "2026-10-13T10:02:00+08:00",
            "sender": "wxid_friend123",
            "kind": "image",
            "text": "",
            "note": "图片",
        }

    def test_feishu_format_non_empty(self):
        payload = format_payload("feishu", "chat_room_001", self.schema_msg)
        self.assertEqual(payload["msg_type"], "text")
        text = payload["content"]["text"]
        self.assertIn("Hello world from WeChat Pad!", text)
        self.assertIn("wxid_friend123", text)
        self.assertIn("chat_room_001", text)

    def test_feishu_self_message(self):
        payload = format_payload("feishu", "chat_room_001", self.self_msg)
        text = payload["content"]["text"]
        self.assertIn("[Self]", text)
        self.assertIn("Replying from my phone", text)

    def test_feishu_media_fallback_note(self):
        payload = format_payload("feishu", "chat_room_001", self.image_msg)
        text = payload["content"]["text"]
        self.assertIn("[图片]", text)

    def test_dify_format_non_empty(self):
        payload = format_payload("dify", "chat_room_001", self.schema_msg)
        inputs = payload["inputs"]
        self.assertEqual(inputs["content"], "Hello world from WeChat Pad!")
        self.assertEqual(inputs["text"], "Hello world from WeChat Pad!")
        self.assertEqual(inputs["msg_type"], "text")
        self.assertEqual(inputs["sender"], "wxid_friend123")
        self.assertFalse(inputs["is_sender"])

    def test_custom_format_non_empty(self):
        payload = format_payload("custom", "chat_room_001", self.schema_msg)
        self.assertEqual(payload["event"], "wechat_message")
        self.assertEqual(payload["content"], "Hello world from WeChat Pad!")
        self.assertEqual(payload["text"], "Hello world from WeChat Pad!")
        self.assertEqual(payload["type"], "text")
        self.assertEqual(payload["kind"], "text")
        self.assertEqual(payload["sender"], "wxid_friend123")
        self.assertFalse(payload["is_sender"])

    def test_process_chats_dry_run(self):
        chats = [
            {
                "chat_id": "room1",
                "messages": [self.schema_msg, self.self_msg],
            }
        ]
        # Ignore self should filter self_msg
        count = process_chats(chats, "", "custom", dry_run=True, ignore_self=True)
        self.assertEqual(count, 1)

        # Do not ignore self should count both
        count2 = process_chats(chats, "", "custom", dry_run=True, ignore_self=False)
        self.assertEqual(count2, 2)


if __name__ == "__main__":
    unittest.main()
