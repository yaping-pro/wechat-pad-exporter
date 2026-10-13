package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestUIDAllowDenyDecision tests that IsUIDAllowed and ParseUIDList behave correctly.
func TestUIDAllowDenyDecision(t *testing.T) {
	// Empty list defaults to root (0) only
	if !IsUIDAllowed(0, nil) {
		t.Errorf("expected UID 0 to be allowed with nil allowlist")
	}
	if IsUIDAllowed(1000, nil) {
		t.Errorf("expected UID 1000 to be denied with nil allowlist")
	}

	// Specific allowlist: root (0) and wechat app (10234)
	allowlist := []uint32{0, 10234}
	if !IsUIDAllowed(0, allowlist) {
		t.Errorf("expected UID 0 to be allowed")
	}
	if !IsUIDAllowed(10234, allowlist) {
		t.Errorf("expected UID 10234 to be allowed")
	}
	if IsUIDAllowed(1001, allowlist) {
		t.Errorf("expected UID 1001 to be denied")
	}

	// Parsing UID list
	parsed, err := ParseUIDList("0, 10234,  20000 ")
	if err != nil {
		t.Fatalf("unexpected error parsing UID list: %v", err)
	}
	if len(parsed) != 3 || parsed[0] != 0 || parsed[1] != 10234 || parsed[2] != 20000 {
		t.Fatalf("unexpected parsed result: %v", parsed)
	}

	// Bad input
	if _, err := ParseUIDList("0, foo, 10"); err == nil {
		t.Errorf("expected error parsing invalid UID list")
	}
}

// TestPeerUIDConnectionEnforcement tests actual unix domain socket peercred enforcement.
func TestPeerUIDConnectionEnforcement(t *testing.T) {
	sockPath := fmt.Sprintf("%s/test_peercred_%d.sock", os.TempDir(), time.Now().UnixNano())
	defer os.Remove(sockPath)

	l, err := ListenUDSProtected(sockPath)
	if err != nil {
		t.Fatalf("failed to listen on protected UDS: %v", err)
	}
	defer l.Close()

	// 1. Allowed connection (current process UID)
	currentUID := uint32(os.Getuid())
	allowlist := []uint32{currentUID}

	clientDone := make(chan error, 1)
	go func() {
		c, err := net.Dial("unix", sockPath)
		if err != nil {
			clientDone <- err
			return
		}
		defer c.Close()
		clientDone <- nil
	}()

	conn, err := l.Accept()
	if err != nil {
		t.Fatalf("accept error: %v", err)
	}
	defer conn.Close()

	peerUID, allowed, err := CheckPeerUID(conn, allowlist)
	if err != nil {
		t.Fatalf("check peer UID failed: %v", err)
	}
	if !allowed || peerUID != currentUID {
		t.Errorf("expected peer UID %d to be allowed, got UID %d allowed=%v", currentUID, peerUID, allowed)
	}

	if err := <-clientDone; err != nil {
		t.Fatalf("client error: %v", err)
	}

	// 2. Denied connection (different allowed UID)
	disallowlist := []uint32{currentUID + 9999}
	go func() {
		c, err := net.Dial("unix", sockPath)
		if err != nil {
			clientDone <- err
			return
		}
		defer c.Close()
		clientDone <- nil
	}()

	conn2, err := l.Accept()
	if err != nil {
		t.Fatalf("accept error 2: %v", err)
	}
	defer conn2.Close()

	peerUID2, allowed2, err := CheckPeerUID(conn2, disallowlist)
	if err != nil {
		t.Fatalf("check peer UID 2 failed: %v", err)
	}
	if allowed2 {
		t.Errorf("expected peer UID %d to be DENIED by disallowlist %v", peerUID2, disallowlist)
	}
	if err := <-clientDone; err != nil {
		t.Fatalf("client error 2: %v", err)
	}
}

// TestSafeLineSummaryPrivacy verifies that unparseable log payload does NOT leak message text.
func TestSafeLineSummaryPrivacy(t *testing.T) {
	sensitiveMsg := []byte(`{"talker":"wxid_secret","content":"Top secret confidential banking password 12345"}`)
	length, hashPrefix := SafeLineSummary(sensitiveMsg)
	if length != len(sensitiveMsg) {
		t.Errorf("expected length %d, got %d", len(sensitiveMsg), length)
	}
	if len(hashPrefix) != 16 {
		t.Errorf("expected 16-char sha256 prefix, got %d (%s)", len(hashPrefix), hashPrefix)
	}
	// Verify raw message is not present in hashPrefix
	if hashPrefix == string(sensitiveMsg) {
		t.Errorf("hash leaked raw message")
	}
}

// TestSQLLimitSafetyCapAndTruncation tests that a huge window with many rows does NOT load unbounded rows
// into memory, and verifies truncated: true when limit is hit.
func TestSQLLimitSafetyCapAndTruncation(t *testing.T) {
	dbFile := fmt.Sprintf("%s/test_limit_%d.db", os.TempDir(), time.Now().UnixNano())
	defer os.Remove(dbFile)

	store, err := InitStore(dbFile)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// Insert 2500 messages
	for i := 1; i <= 2500; i++ {
		msg := &NormalizedMessage{
			MsgId:       fmt.Sprintf("msg_%04d", i),
			ChatId:      "chat_room_1",
			DisplayName: "Room 1",
			IsGroup:     true,
			Sender:      "user_1",
			Kind:        "text",
			Text:        fmt.Sprintf("Text %d", i),
			Ts:          now - 2500 + int64(i),
		}
		if err := store.SaveMessage(msg); err != nil {
			t.Fatalf("failed to save msg %d: %v", i, err)
		}
	}

	// 1. Direct Store query with limit safety cap
	cappedRows, err := store.QueryMessagesByRangeWithLimit(0, now, 50)
	if err != nil {
		t.Fatalf("query with limit failed: %v", err)
	}
	if len(cappedRows) != 50 {
		t.Errorf("expected exactly 50 rows from SQL limit, got %d", len(cappedRows))
	}

	// 2. Export API call with limit=20
	state := &ServerState{
		StartTime:   time.Now(),
		Store:       store,
		Token:       "",
		AuthEnabled: false,
	}
	router := SetupRouter(state)

	req := httptest.NewRequest(http.MethodGet, "/api/wechat/export?since=0&limit=20", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export request failed with code %d: %s", rec.Code, rec.Body.String())
	}

	var resp ExportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal ExportResponse: %v", err)
	}

	if resp.Manifest.MessageCount != 20 {
		t.Errorf("expected 20 messages, got %d", resp.Manifest.MessageCount)
	}
	if !resp.Manifest.Truncated {
		t.Errorf("expected truncated: true when limit was reached on larger dataset")
	}

	// 3. Export API call with default limit (500)
	req2 := httptest.NewRequest(http.MethodGet, "/api/wechat/export?since=0", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	var resp2 ExportResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to unmarshal ExportResponse: %v", err)
	}
	if resp2.Manifest.MessageCount != 500 {
		t.Errorf("expected default 500 messages, got %d", resp2.Manifest.MessageCount)
	}
	if !resp2.Manifest.Truncated {
		t.Errorf("expected truncated: true when 500 limit reached on 2500 messages")
	}
}
