package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeRawMessage(t *testing.T) {
	// 1. Test group message with sender prefix
	rawGroup := &RawHookMessage{
		MsgId:       1001,
		MsgSvrId:    99990001,
		Talker:      "<PHONE>@chatroom",
		DisplayName: "Project Alpha Group",
		Type:        1,
		IsSend:      0,
		Content:     "<WXID>:\nHello team, test message!",
		CreateTime:  1727800000000,
	}

	norm := NormalizeRawMessage(rawGroup)
	if !norm.IsGroup {
		t.Errorf("expected is_group=true, got false")
	}
	if norm.Sender != "<WXID>" {
		t.Errorf("expected sender '<WXID>', got '%s'", norm.Sender)
	}
	if norm.Text != "Hello team, test message!" {
		t.Errorf("expected trimmed content, got '%s'", norm.Text)
	}
	if norm.Kind != "text" {
		t.Errorf("expected kind 'text', got '%s'", norm.Kind)
	}
	if norm.Ts != 1727800000 {
		t.Errorf("expected ts 1727800000, got %d", norm.Ts)
	}

	// 2. Test self sent message (image)
	rawSelf := &RawHookMessage{
		MsgId:       1002,
		MsgSvrId:    99990002,
		Talker:      "<WXID>",
		DisplayName: "Alice",
		Type:        3, // image
		IsSend:      1,
		SelfWxid:    "<WXID>",
		Content:     "",
		CreateTime:  1727800100,
	}
	normSelf := NormalizeRawMessage(rawSelf)
	if normSelf.IsGroup {
		t.Errorf("expected is_group=false, got true")
	}
	if normSelf.Sender != "<WXID>" {
		t.Errorf("expected sender '<WXID>', got '%s'", normSelf.Sender)
	}
	if normSelf.Kind != "image" {
		t.Errorf("expected kind 'image', got '%s'", normSelf.Kind)
	}
	if normSelf.Text != "" {
		t.Errorf("expected empty text for image, got '%s'", normSelf.Text)
	}
	if normSelf.Note != "图片" {
		t.Errorf("expected note '图片', got '%s'", normSelf.Note)
	}

	// 3. Test appmsg file (sub_type 6)
	rawFile := &RawHookMessage{
		MsgId:      1003,
		Talker:     "<WXID>",
		Type:       49,
		SubType:    6,
		Content:    "<msg><appmsg><title>doc.pdf</title><type>6</type></appmsg></msg>",
		CreateTime: 1727800150,
	}
	normFile := NormalizeRawMessage(rawFile)
	if normFile.Kind != "file" {
		t.Errorf("expected kind 'file', got '%s'", normFile.Kind)
	}
	if normFile.Note != "卡片/链接/文件" {
		t.Errorf("expected note '卡片/链接/文件', got '%s'", normFile.Note)
	}

	// 4. Test emoji (type 47 -> kind 'other', note '动画表情')
	rawEmoji := &RawHookMessage{
		MsgId:      1004,
		Talker:     "<WXID>",
		Type:       47,
		Content:    "<emoji></emoji>",
		CreateTime: 1727800200,
	}
	normEmoji := NormalizeRawMessage(rawEmoji)
	if normEmoji.Kind != "other" {
		t.Errorf("expected kind 'other', got '%s'", normEmoji.Kind)
	}
	if normEmoji.Note != "动画表情" {
		t.Errorf("expected note '动画表情', got '%s'", normEmoji.Note)
	}
}

func TestStoreOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wx_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_vault.db")
	store, err := InitStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer store.Close()

	msg1 := &NormalizedMessage{
		MsgId:       "msg_001",
		ChatId:      "room1@chatroom",
		DisplayName: "Room 1",
		IsGroup:     true,
		Sender:      "<WXID>",
		Kind:        "text",
		Text:        "msg 1",
		Ts:          1000,
	}
	msg2 := &NormalizedMessage{
		MsgId:       "msg_002",
		ChatId:      "room1@chatroom",
		DisplayName: "Room 1",
		IsGroup:     true,
		Sender:      "<WXID>",
		Kind:        "link",
		Text:        "https://example.com",
		Note:        "article",
		Ts:          2000,
	}

	if err := store.SaveMessage(msg1); err != nil {
		t.Fatalf("failed to save msg1: %v", err)
	}
	if err := store.SaveMessage(msg2); err != nil {
		t.Fatalf("failed to save msg2: %v", err)
	}

	// Duplicate insert should be ignored without error
	if err := store.SaveMessage(msg1); err != nil {
		t.Fatalf("saving duplicate msg1 failed: %v", err)
	}

	count, err := store.CountMessages()
	if err != nil {
		t.Fatalf("failed to count messages: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}

	res, err := store.QueryMessagesByRange(500, 1500)
	if err != nil {
		t.Fatalf("failed to query range: %v", err)
	}
	if len(res) != 1 || res[0].MsgId != "msg_001" {
		t.Fatalf("query range mismatch: %+v", res)
	}
}

func TestApiExportContract(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wx_test_api_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_api.db")
	store, err := InitStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer store.Close()

	_ = store.SaveMessage(&NormalizedMessage{
		MsgId:       "msg_100",
		ChatId:      "chat_x",
		DisplayName: "Chat X",
		IsGroup:     false,
		Sender:      "chat_x",
		Kind:        "text",
		Text:        "Hello API test",
		Ts:          1727800050,
	})

	state := &ServerState{
		StartTime: time.Now(),
		Store:     store,
	}

	router := SetupRouter(state)
	if router == nil {
		t.Fatalf("router is nil")
	}
}
