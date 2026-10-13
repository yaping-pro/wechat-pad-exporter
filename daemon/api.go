package main

import (
	"bytes"
	"crypto/subtle"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var shanghaiZone = time.FixedZone("CST", 8*3600)

// FormatRFC3339 formats a time.Time to RFC3339 string in Asia/Shanghai (+08:00).
func FormatRFC3339(t time.Time) string {
	return t.In(shanghaiZone).Format("2006-01-02T15:04:05-07:00")
}

// FormatRFC3339Epoch formats epoch seconds to RFC3339 string in Asia/Shanghai (+08:00).
func FormatRFC3339Epoch(epochSec int64) string {
	return time.Unix(epochSec, 0).In(shanghaiZone).Format("2006-01-02T15:04:05-07:00")
}

// ParseTimeParam parses a time string (epoch seconds or RFC3339/ISO8601) to epoch seconds.
func ParseTimeParam(val string, defaultEpoch int64) (int64, error) {
	if val == "" {
		return defaultEpoch, nil
	}
	val = strings.TrimSpace(val)

	// If pure float or integer epoch
	if !strings.Contains(val, "-") && !strings.Contains(val, "T") && !strings.Contains(val, ":") {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return int64(f), nil
		}
	}

	// Fix URL query decode converting '+' in timezone offset to ' ' (e.g. "2006-01-02T15:04:05 08:00")
	if idx := strings.LastIndex(val, " "); idx > 0 && idx == len(val)-6 && strings.Contains(val[idx:], ":") {
		val = val[:idx] + "+" + val[idx+1:]
	}

	valFixed := strings.ReplaceAll(val, "Z", "+00:00")
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, valFixed, shanghaiZone); err == nil {
			return t.Unix(), nil
		}
	}

	return 0, fmt.Errorf("Invalid time format: %q", val)
}

// CanonicalJSONBytes serializes an object into canonical UTF-8 JSON bytes (sorted keys, compact separators, no HTML escaping).
func CanonicalJSONBytes(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, err
	}
	res := buf.Bytes()
	if len(res) > 0 && res[len(res)-1] == '\n' {
		res = res[:len(res)-1]
	}
	return res, nil
}

// ServerState tracks runtime statistics for the daemon.
type ServerState struct {
	StartTime     time.Time
	Store         *Store
	ActiveClients atomic.Int32
	TotalReceived atomic.Int64
	Token         string
	AuthEnabled   bool
	AllowChats    []string
	BlockChats    []string
}

// CheckAuth validates the Authorization: Bearer <token> header against state.Token.
func (state *ServerState) CheckAuth(r *http.Request) bool {
	if !state.AuthEnabled {
		return true
	}
	if state.Token == "" {
		return false
	}
	authHeader := r.Header.Get("Authorization")
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return false
	}
	givenToken := parts[1]
	return subtle.ConstantTimeCompare([]byte(givenToken), []byte(state.Token)) == 1
}

// IsChatAllowed checks if a chat is allowed by allow_chats and block_chats.
func (state *ServerState) IsChatAllowed(chatID, displayName string) bool {
	names := make(map[string]bool)
	if chatID != "" {
		names[chatID] = true
	}
	if displayName != "" {
		names[displayName] = true
	}

	// Blocklist takes precedence
	if len(state.BlockChats) > 0 {
		for _, b := range state.BlockChats {
			if names[b] {
				return false
			}
		}
	}

	// If allowlist is configured, at least one identifier must match
	if len(state.AllowChats) > 0 {
		matched := false
		for _, a := range state.AllowChats {
			if names[a] {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// ExportMessageItem matches the export message contract.
type ExportMessageItem struct {
	Ts     string `json:"ts"`
	Sender string `json:"sender"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Note   string `json:"note"`
}

// ExportChatItem represents a chat container with aggregated messages.
// Superset: provides both home service contract (name, kind) and mobile fields (chat_id, display_name, is_group).
type ExportChatItem struct {
	Name        string              `json:"name"`
	Kind        string              `json:"kind"`
	ChatId      string              `json:"chat_id,omitempty"`
	DisplayName string              `json:"display_name,omitempty"`
	IsGroup     bool                `json:"is_group"`
	Messages    []ExportMessageItem `json:"messages"`
}

// ExportRange represents the query window.
type ExportRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ExportManifest contains integrity and volume metadata.
type ExportManifest struct {
	Sha256       string `json:"sha256"`
	MessageCount int    `json:"message_count"`
}

// ExportResponse represents the complete export payload contract.
type ExportResponse struct {
	ExportedAt string           `json:"exported_at"`
	Range      ExportRange      `json:"range"`
	Watermark  string           `json:"watermark"`
	Chats      []ExportChatItem `json:"chats"`
	Manifest   ExportManifest   `json:"manifest"`
}

// HealthResponse represents the health check payload.
type HealthResponse struct {
	Status        string `json:"status"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	UdsConnected  bool   `json:"uds_connected"`
	ActiveUds     int32  `json:"active_uds_connections"`
	TotalMessages int64  `json:"total_messages"`
	TotalReceived int64  `json:"total_received_stream"`
	MemoryBytes   uint64 `json:"memory_bytes"`
	Version       string `json:"version"`
}

// SetupRouter sets up the HTTP router for the daemon.
func SetupRouter(state *ServerState) *http.ServeMux {
	mux := http.NewServeMux()

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Bearer Token Authorization check (if auth enabled)
		if !state.CheckAuth(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
			return
		}

		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		totalMsgs, _ := state.Store.CountMessages()
		activeUds := state.ActiveClients.Load()

		resp := HealthResponse{
			Status:        "ok",
			UptimeSeconds: int64(time.Since(state.StartTime).Seconds()),
			UdsConnected:  activeUds > 0,
			ActiveUds:     activeUds,
			TotalMessages: totalMsgs,
			TotalReceived: state.TotalReceived.Load(),
			MemoryBytes:   memStats.Alloc,
			Version:       "1.0.0-pad",
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(resp)
	}

	// GET /health and GET /api/wechat/health
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/wechat/health", healthHandler)

	// GET /api/wechat/export?since=<epoch|rfc3339>&to=<epoch|rfc3339>
	mux.HandleFunc("/api/wechat/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Bearer Token Authorization check
		if !state.CheckAuth(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
			return
		}

		query := r.URL.Query()
		now := time.Now().Unix()

		// Default since = now - 86400 (24 hours ago), default to = now
		since, err := ParseTimeParam(query.Get("since"), now-86400)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		to, err := ParseTimeParam(query.Get("to"), now)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Query SQLite messages within range
		msgs, err := state.Store.QueryMessagesByRange(since, to)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to query messages: " + err.Error()})
			return
		}

		// Group messages by chat_id
		chatOrder := make([]string, 0)
		chatMap := make(map[string]*ExportChatItem)
		var maxTs int64 = 0

		filteredTotalCount := 0
		for _, msg := range msgs {
			// Apply chat filtering
			if !state.IsChatAllowed(msg.ChatId, msg.DisplayName) {
				continue
			}

			filteredTotalCount++
			if msg.Ts > maxTs {
				maxTs = msg.Ts
			}

			item := ExportMessageItem{
				Ts:     FormatRFC3339Epoch(msg.Ts),
				Sender: msg.Sender,
				Kind:   msg.Kind,
				Text:   msg.Text,
				Note:   msg.Note,
			}

			chat, exists := chatMap[msg.ChatId]
			if !exists {
				chatKind := "single"
				if msg.IsGroup {
					chatKind = "group"
				}
				chatName := msg.DisplayName
				if chatName == "" {
					chatName = msg.ChatId
				}

				chat = &ExportChatItem{
					Name:        chatName,
					Kind:        chatKind,
					ChatId:      msg.ChatId,
					DisplayName: msg.DisplayName,
					IsGroup:     msg.IsGroup,
					Messages:    make([]ExportMessageItem, 0),
				}
				chatMap[msg.ChatId] = chat
				chatOrder = append(chatOrder, msg.ChatId)
			}
			chat.Messages = append(chat.Messages, item)
		}

		chats := make([]ExportChatItem, 0, len(chatOrder))
		for _, cid := range chatOrder {
			chats = append(chats, *chatMap[cid])
		}

		// Watermark: latest message time, or falls back to 'to' if empty
		watermarkEpoch := to
		if filteredTotalCount > 0 {
			watermarkEpoch = maxTs
		}
		watermarkStr := FormatRFC3339Epoch(watermarkEpoch)

		// Calculate SHA-256 canonical hash of chats
		canonicalBytes, err := CanonicalJSONBytes(chats)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to serialize canonical json: " + err.Error()})
			return
		}
		hasher := sha256.New()
		hasher.Write(canonicalBytes)
		manifestHash := hex.EncodeToString(hasher.Sum(nil))

		resp := ExportResponse{
			ExportedAt: FormatRFC3339(time.Now()),
			Range: ExportRange{
				From: FormatRFC3339Epoch(since),
				To:   FormatRFC3339Epoch(to),
			},
			Watermark: watermarkStr,
			Chats:     chats,
			Manifest: ExportManifest{
				Sha256:       manifestHash,
				MessageCount: filteredTotalCount,
			},
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(resp)
	})

	return mux
}
