package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var rfc3339CSTRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\+08:00$`)

func setupTestDBAndServer(t *testing.T, token string) (*Store, *http.ServeMux, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "wx_contract_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "vault.db")
	store, err := InitStore(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to init store: %v", err)
	}

	state := &ServerState{
		StartTime:   time.Now(),
		Store:       store,
		Token:       token,
		AuthEnabled: token != "",
	}

	router := SetupRouter(state)
	return store, router, tmpDir
}

// 1. Test health endpoints (/health and /api/wechat/health)
func TestContractHealthEndpoints(t *testing.T) {
	store, router, tmpDir := setupTestDBAndServer(t, "")
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	endpoints := []string{"/health", "/api/wechat/health"}
	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("[%s] expected status 200, got %d", ep, rec.Code)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("[%s] invalid JSON: %v", ep, err)
		}

		if body["status"] != "ok" {
			t.Errorf("[%s] expected status 'ok', got %v", ep, body["status"])
		}
	}
}

// 2. Test Bearer authentication contract
func TestContractBearerAuth(t *testing.T) {
	// A. When Token is set to "SECRET_TOKEN"
	store, router, tmpDir := setupTestDBAndServer(t, "SECRET_TOKEN")
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	// 1. Missing Authorization header -> 401 on /api/wechat/export and /health
	req1 := httptest.NewRequest(http.MethodGet, "/api/wechat/export", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth on export, got %d", rec1.Code)
	}

	reqHealthUnauth := httptest.NewRequest(http.MethodGet, "/health", nil)
	recHealthUnauth := httptest.NewRecorder()
	router.ServeHTTP(recHealthUnauth, reqHealthUnauth)
	if recHealthUnauth.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth on /health, got %d", recHealthUnauth.Code)
	}

	// 2. Wrong Bearer token -> 401
	req2 := httptest.NewRequest(http.MethodGet, "/api/wechat/export", nil)
	req2.Header.Set("Authorization", "Bearer WRONG_TOKEN")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", rec2.Code)
	}

	// 3. Correct Bearer token -> 200 on export and health
	req3 := httptest.NewRequest(http.MethodGet, "/api/wechat/export", nil)
	req3.Header.Set("Authorization", "Bearer SECRET_TOKEN")
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("expected 200 with valid token on export, got %d", rec3.Code)
	}

	reqHealthAuth := httptest.NewRequest(http.MethodGet, "/health", nil)
	reqHealthAuth.Header.Set("Authorization", "Bearer SECRET_TOKEN")
	recHealthAuth := httptest.NewRecorder()
	router.ServeHTTP(recHealthAuth, reqHealthAuth)
	if recHealthAuth.Code != http.StatusOK {
		t.Errorf("expected 200 with valid token on /health, got %d", recHealthAuth.Code)
	}

	// B. When Token is empty and AuthEnabled is false (opt-out)
	store2, router2, tmpDir2 := setupTestDBAndServer(t, "")
	defer store2.Close()
	defer os.RemoveAll(tmpDir2)

	req4 := httptest.NewRequest(http.MethodGet, "/api/wechat/export", nil)
	rec4 := httptest.NewRecorder()
	router2.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Errorf("expected 200 when token is not configured and auth disabled, got %d", rec4.Code)
	}

	req5 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec5 := httptest.NewRecorder()
	router2.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Errorf("expected 200 on /health when auth disabled, got %d", rec5.Code)
	}
}

// 3. Test time parameter parsing & default since window
func TestContractTimeParamsAndDefaults(t *testing.T) {
	store, router, tmpDir := setupTestDBAndServer(t, "")
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	// 1. Default since should be approximately now - 86400 (within 5 seconds)
	before := time.Now().Unix() - 86400
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/export", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	after := time.Now().Unix() - 86400

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ExportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	fromEpoch, err := ParseTimeParam(resp.Range.From, 0)
	if err != nil {
		t.Fatalf("failed to parse range.from (%s): %v", resp.Range.From, err)
	}
	if fromEpoch < before-5 || fromEpoch > after+5 {
		t.Errorf("range.from (%d) not within expected 24h default [%d, %d]", fromEpoch, before, after)
	}

	// 2. Invalid since should return 400 Bad Request
	reqInvalid := httptest.NewRequest(http.MethodGet, "/api/wechat/export?since=garbage_time", nil)
	recInvalid := httptest.NewRecorder()
	router.ServeHTTP(recInvalid, reqInvalid)
	if recInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid since, got %d", recInvalid.Code)
	}

	// 3. RFC3339 query params should be accepted
	reqRFC := httptest.NewRequest(http.MethodGet, "/api/wechat/export?since=2026-10-01T00:00:00%2B08:00&to=2026-10-02T00:00:00%2B08:00", nil)
	recRFC := httptest.NewRecorder()
	router.ServeHTTP(recRFC, reqRFC)
	if recRFC.Code != http.StatusOK {
		t.Errorf("expected 200 for RFC3339 query params, got %d: %s", recRFC.Code, recRFC.Body.String())
	}
}

// 4. Test complete data contract schema, time formats, kind enums, and canonical SHA-256
func TestContractFullPayloadSchemaAndEnums(t *testing.T) {
	store, router, tmpDir := setupTestDBAndServer(t, "")
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	nowEpoch := time.Now().Unix()
	baseTs := nowEpoch - 3600 // 1 hour ago

	// Seed test messages covering all kinds per Muse home service spec
	testMsgs := []*NormalizedMessage{
		{
			MsgId:       "msg_text",
			ChatId:      "<PHONE>@chatroom",
			DisplayName: "Project Alpha Group",
			IsGroup:     true,
			Sender:      "<WXID>",
			Kind:        "text",
			Text:        "Hello text message <tag>&test",
			Note:        "",
			Ts:          baseTs + 10,
		},
		{
			MsgId:       "msg_file",
			ChatId:      "<PHONE>@chatroom",
			DisplayName: "Project Alpha Group",
			IsGroup:     true,
			Sender:      "<WXID>",
			Kind:        "file",
			Text:        "<msg><appmsg><title>doc.pdf</title><type>6</type></appmsg></msg>",
			Note:        "卡片/链接/文件",
			Ts:          baseTs + 20,
		},
		{
			MsgId:       "msg_image",
			ChatId:      "<WXID>",
			DisplayName: "Single Friend",
			IsGroup:     false,
			Sender:      "我",
			Kind:        "image",
			Text:        "",
			Note:        "图片",
			Ts:          baseTs + 30,
		},
		{
			MsgId:       "msg_voice",
			ChatId:      "<WXID>",
			DisplayName: "Single Friend",
			IsGroup:     false,
			Sender:      "<WXID>",
			Kind:        "voice",
			Text:        "",
			Note:        "语音",
			Ts:          baseTs + 40,
		},
		{
			MsgId:       "msg_other",
			ChatId:      "<WXID>",
			DisplayName: "Single Friend",
			IsGroup:     false,
			Sender:      "<WXID>",
			Kind:        "other",
			Text:        "",
			Note:        "动画表情",
			Ts:          baseTs + 50,
		},
	}

	for _, m := range testMsgs {
		if err := store.SaveMessage(m); err != nil {
			t.Fatalf("failed to save test message: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/wechat/export?since="+FormatRFC3339Epoch(baseTs)+"&to="+FormatRFC3339Epoch(nowEpoch), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ExportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// 1. Verify Top-Level Time Formats (+08:00)
	if !rfc3339CSTRegex.MatchString(resp.ExportedAt) {
		t.Errorf("exported_at '%s' does not match RFC3339 +08:00", resp.ExportedAt)
	}
	if !rfc3339CSTRegex.MatchString(resp.Range.From) {
		t.Errorf("range.from '%s' does not match RFC3339 +08:00", resp.Range.From)
	}
	if !rfc3339CSTRegex.MatchString(resp.Range.To) {
		t.Errorf("range.to '%s' does not match RFC3339 +08:00", resp.Range.To)
	}
	if !rfc3339CSTRegex.MatchString(resp.Watermark) {
		t.Errorf("watermark '%s' does not match RFC3339 +08:00", resp.Watermark)
	}

	// 2. Verify Watermark matches latest message timestamp
	expectedWatermark := FormatRFC3339Epoch(baseTs + 50)
	if resp.Watermark != expectedWatermark {
		t.Errorf("expected watermark '%s', got '%s'", expectedWatermark, resp.Watermark)
	}

	// 3. Verify Chats structure
	allowedKinds := map[string]bool{
		"text": true, "image": true, "voice": true, "video": true, "link": true, "file": true, "other": true,
	}

	if len(resp.Chats) != 2 {
		t.Fatalf("expected 2 chats, got %d", len(resp.Chats))
	}

	totalMsgCount := 0
	for _, chat := range resp.Chats {
		if chat.Name == "" {
			t.Errorf("chat.name must not be empty")
		}
		if chat.Kind != "group" && chat.Kind != "single" {
			t.Errorf("chat.kind must be 'group' or 'single', got '%s'", chat.Kind)
		}
		// Check superset compatibility fields
		if chat.ChatId == "" {
			t.Errorf("chat.chat_id must be populated in superset")
		}

		for _, msg := range chat.Messages {
			totalMsgCount++
			if !rfc3339CSTRegex.MatchString(msg.Ts) {
				t.Errorf("msg.ts '%s' does not match RFC3339 +08:00", msg.Ts)
			}
			if !allowedKinds[msg.Kind] {
				t.Errorf("msg.kind '%s' is not in allowed home service enum", msg.Kind)
			}
			if msg.Sender == "" {
				t.Errorf("msg.sender must not be empty")
			}
		}
	}

	if totalMsgCount != 5 {
		t.Errorf("expected 5 total messages, got %d", totalMsgCount)
	}
	if resp.Manifest.MessageCount != 5 {
		t.Errorf("expected manifest.message_count=5, got %d", resp.Manifest.MessageCount)
	}

	// 4. Verify Canonical SHA-256 Manifest Hash
	canonicalBytes, err := CanonicalJSONBytes(resp.Chats)
	if err != nil {
		t.Fatalf("failed to serialize canonical bytes: %v", err)
	}
	h := sha256.Sum256(canonicalBytes)
	expectedHash := hex.EncodeToString(h[:])
	if resp.Manifest.Sha256 != expectedHash {
		t.Errorf("manifest.sha256 mismatch: got %s, expected %s", resp.Manifest.Sha256, expectedHash)
	}

	// 5. Cross-validate with Python canonical_json_bytes if python3 is available
	if pyPath, err := exec.LookPath("python3"); err == nil {
		cmd := exec.Command(pyPath, "-c", `
import sys, json, hashlib
raw = sys.stdin.read()
chats = json.loads(raw)
b = json.dumps(chats, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode('utf-8')
print(hashlib.sha256(b).hexdigest())
`)
		cmd.Stdin = bytes.NewReader(canonicalBytes)
		out, err := cmd.Output()
		if err != nil {
			t.Logf("python validation warning: %v", err)
		} else {
			pyHash := strings.TrimSpace(string(out))
			if pyHash != resp.Manifest.Sha256 {
				t.Errorf("Python canonical hash (%s) does not match Go manifest.sha256 (%s)", pyHash, resp.Manifest.Sha256)
			}
		}
	}
}

// 5. Test empty database watermark fallback
func TestContractWatermarkFallbackWhenEmpty(t *testing.T) {
	store, router, tmpDir := setupTestDBAndServer(t, "")
	defer store.Close()
	defer os.RemoveAll(tmpDir)

	toEpoch := int64(1791000000)
	sinceEpoch := int64(1790900000)
	req := httptest.NewRequest(http.MethodGet, "/api/wechat/export?since=1790900000&to=1791000000", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ExportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	expectedToWatermark := FormatRFC3339Epoch(toEpoch)
	if resp.Watermark != expectedToWatermark {
		t.Errorf("expected watermark fallback to 'to' (%s), got '%s'", expectedToWatermark, resp.Watermark)
	}
	if resp.Range.From != FormatRFC3339Epoch(sinceEpoch) {
		t.Errorf("expected range.from '%s', got '%s'", FormatRFC3339Epoch(sinceEpoch), resp.Range.From)
	}
	if resp.Manifest.MessageCount != 0 {
		t.Errorf("expected message_count=0, got %d", resp.Manifest.MessageCount)
	}
}
