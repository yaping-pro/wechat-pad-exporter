package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestConfigLoadAndPrecedence tests JSON config parsing and flag overrides.
func TestConfigLoadAndPrecedence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wx_cfg_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgJSON := `{
		"bind": "0.0.0.0",
		"port": 9000,
		"token": "JSON_TOKEN",
		"uds_path": "@json_uds",
		"db_path": "/tmp/json_vault.db",
		"allow_chats": ["allowed_chat"],
		"block_chats": ["blocked_chat"],
		"retain_days": 60,
		"log_max_bytes": 5242880,
		"log_keep": 5
	}`
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0644); err != nil {
		t.Fatalf("failed to write config json: %v", err)
	}

	loaded, err := LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}

	if loaded.Bind != "0.0.0.0" || loaded.Port != 9000 {
		t.Errorf("expected 0.0.0.0:9000, got %s:%d", loaded.Bind, loaded.Port)
	}
	if loaded.Token != "JSON_TOKEN" {
		t.Errorf("expected JSON_TOKEN, got %s", loaded.Token)
	}
	if loaded.RetainDays != 60 {
		t.Errorf("expected retain_days 60, got %d", loaded.RetainDays)
	}
	if len(loaded.AllowChats) != 1 || loaded.AllowChats[0] != "allowed_chat" {
		t.Errorf("allow_chats mismatch: %v", loaded.AllowChats)
	}
	if len(loaded.BlockChats) != 1 || loaded.BlockChats[0] != "blocked_chat" {
		t.Errorf("block_chats mismatch: %v", loaded.BlockChats)
	}
}

// TestChatFilteringAndManifestHash tests allow/block filtering and SHA256 integrity.
func TestChatFilteringAndManifestHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wx_filter_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	store, err := InitStore(dbPath)
	if err != nil {
		t.Fatalf("InitStore failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// Insert 3 messages from different chats
	msg1 := &NormalizedMessage{
		MsgId:       "msg_1",
		ChatId:      "chat_work",
		DisplayName: "Work Group",
		IsGroup:     true,
		Sender:      "<WXID>",
		Kind:        "text",
		Text:        "Important task",
		Ts:          now - 100,
	}
	msg2 := &NormalizedMessage{
		MsgId:       "msg_2",
		ChatId:      "chat_spam",
		DisplayName: "Spam Bot",
		IsGroup:     false,
		Sender:      "<WXID>",
		Kind:        "text",
		Text:        "Click here",
		Ts:          now - 80,
	}
	msg3 := &NormalizedMessage{
		MsgId:       "msg_3",
		ChatId:      "chat_family",
		DisplayName: "Family",
		IsGroup:     true,
		Sender:      "<WXID>",
		Kind:        "text",
		Text:        "Dinner ready",
		Ts:          now - 50,
	}

	_ = store.SaveMessage(msg1)
	_ = store.SaveMessage(msg2)
	_ = store.SaveMessage(msg3)

	// Scenario 1: Block chat_spam
	stateBlock := &ServerState{
		StartTime:   time.Now(),
		Store:       store,
		AuthEnabled: false,
		BlockChats:  []string{"chat_spam"},
	}
	routerBlock := SetupRouter(stateBlock)

	req := httptest.NewRequest(http.MethodGet, "/api/wechat/export", nil)
	rec := httptest.NewRecorder()
	routerBlock.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export failed: %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp ExportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal ExportResponse: %v", err)
	}

	if resp.Manifest.MessageCount != 2 {
		t.Errorf("expected 2 messages after blocking chat_spam, got %d", resp.Manifest.MessageCount)
	}
	for _, c := range resp.Chats {
		if c.ChatId == "chat_spam" {
			t.Errorf("chat_spam should have been blocked")
		}
	}

	// Verify manifest SHA-256 matches canonical bytes of resp.Chats
	canonicalBytes, err := CanonicalJSONBytes(resp.Chats)
	if err != nil {
		t.Fatalf("CanonicalJSONBytes failed: %v", err)
	}
	hasher := sha256.New()
	hasher.Write(canonicalBytes)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	if resp.Manifest.Sha256 != expectedHash {
		t.Errorf("manifest sha256 mismatch! Got %s, expected %s", resp.Manifest.Sha256, expectedHash)
	}

	// Scenario 2: Allowlist only "Family" (by display_name)
	stateAllow := &ServerState{
		StartTime:   time.Now(),
		Store:       store,
		AuthEnabled: false,
		AllowChats:  []string{"Family"},
	}
	routerAllow := SetupRouter(stateAllow)

	rec2 := httptest.NewRecorder()
	routerAllow.ServeHTTP(rec2, req)

	var resp2 ExportResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if resp2.Manifest.MessageCount != 1 {
		t.Errorf("expected 1 message for Family allowlist, got %d", resp2.Manifest.MessageCount)
	}
	if len(resp2.Chats) != 1 || resp2.Chats[0].ChatId != "chat_family" {
		t.Errorf("expected only chat_family in chats, got %+v", resp2.Chats)
	}
}

// TestLogRotation tests file rotation when max bytes is reached.
func TestLogRotation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wx_log_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "test.log")
	// Max bytes: 200 bytes, keep 2 backups
	rotWriter, err := NewRotatingLogWriter(logFile, 200, 2)
	if err != nil {
		t.Fatalf("failed to create RotatingLogWriter: %v", err)
	}
	defer rotWriter.Close()

	// Write 150 bytes
	line1 := []byte("12345678901234567890123456789012345678901234567890\n") // 51 bytes * 3 = 153 bytes
	for range 3 {
		_, _ = rotWriter.Write(line1)
	}

	// File should not be rotated yet
	if _, err := os.Stat(logFile + ".1"); !os.IsNotExist(err) {
		t.Errorf("expected test.log.1 to not exist yet")
	}

	// Write another 100 bytes -> exceeds 200 bytes, triggers rotate
	for range 2 {
		_, _ = rotWriter.Write(line1)
	}

	if _, err := os.Stat(logFile + ".1"); os.IsNotExist(err) {
		t.Errorf("expected test.log.1 to exist after rotation")
	}
}

// TestPruneRetention tests retention deletion of messages older than cutoff.
func TestPruneRetention(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wx_prune_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")
	store, err := InitStore(dbPath)
	if err != nil {
		t.Fatalf("InitStore failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// Insert old message (100 days ago) and recent message (1 day ago)
	msgOld := &NormalizedMessage{
		MsgId:       "msg_old",
		ChatId:      "chat_test",
		DisplayName: "Test",
		Sender:      "<WXID>",
		Kind:        "text",
		Text:        "Old message",
		Ts:          now - 100*86400,
	}
	msgNew := &NormalizedMessage{
		MsgId:       "msg_new",
		ChatId:      "chat_test",
		DisplayName: "Test",
		Sender:      "<WXID>",
		Kind:        "text",
		Text:        "New message",
		Ts:          now - 86400,
	}

	_ = store.SaveMessage(msgOld)
	_ = store.SaveMessage(msgNew)

	total, _ := store.CountMessages()
	if total != 2 {
		t.Fatalf("expected 2 messages, got %d", total)
	}

	// Prune messages older than 90 days
	cutoff := now - 90*86400
	deleted, err := store.PruneMessages(cutoff)
	if err != nil {
		t.Fatalf("PruneMessages failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 message deleted, got %d", deleted)
	}

	totalAfter, _ := store.CountMessages()
	if totalAfter != 1 {
		t.Errorf("expected 1 message remaining, got %d", totalAfter)
	}

	remaining, _ := store.QueryMessagesByRange(0, now)
	if len(remaining) != 1 || remaining[0].MsgId != "msg_new" {
		t.Errorf("expected msg_new to remain, got %+v", remaining)
	}
}
