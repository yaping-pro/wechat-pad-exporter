package main

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// RawHookMessage represents the incoming payload from the Xposed hook via IPC.
type RawHookMessage struct {
	MsgId       int64  `json:"msg_id"`
	MsgSvrId    int64  `json:"msg_svr_id"`
	Talker      string `json:"talker"`       // chat_id (e.g. 123456@chatroom or <WXID>)
	DisplayName string `json:"display_name"` // chat display name or nickname
	Type        int    `json:"type"`         // WeChat raw msg type (1, 3, 34, 43, 47, 49, 10000)
	SubType     int    `json:"sub_type"`     // AppMsg sub-type if Type == 49
	IsSend      int    `json:"is_send"`      // 1 = sent by user, 0 = received
	Content     string `json:"content"`      // raw text or xml content
	CreateTime  int64  `json:"create_time"`  // timestamp in milliseconds or seconds
	SelfWxid    string `json:"self_wxid"`    // current account wxid
}

// NormalizedMessage represents a cleaned, standardized message.
type NormalizedMessage struct {
	MsgId       string `json:"msg_id"`
	ChatId      string `json:"chat_id"`
	DisplayName string `json:"display_name"`
	IsGroup     bool   `json:"is_group"`
	Sender      string `json:"sender"`
	Kind        string `json:"kind"` // "text", "image", "voice", "video", "link", "other"
	Text        string `json:"text"`
	Note        string `json:"note"`
	Ts          int64  `json:"ts"` // epoch seconds
}

// MapWeChatType converts WeChat numeric message types to kind strings (backward compatibility).
func MapWeChatType(msgType int) string {
	kind, _, _ := MapWeChatTypeFull(msgType, 0, "")
	return kind
}

// MapWeChatTypeFull maps WeChat raw types to (kind, text, note) per the home service Muse spec.
// Kinds: "text" | "image" | "voice" | "video" | "link" | "file" | "other"
func MapWeChatTypeFull(msgType int, subType int, rawText string) (string, string, string) {
	baseType := msgType
	switch baseType {
	case 1:
		return "text", rawText, ""
	case 3:
		return "image", "", "图片"
	case 34:
		return "voice", "", "语音"
	case 43:
		return "video", "", "视频"
	case 49:
		kind := "link"
		if subType == 6 || subType == 74 || strings.Contains(rawText, "<type>6</type>") || strings.Contains(rawText, "<type>74</type>") {
			kind = "file"
		}
		return kind, rawText, "卡片/链接/文件"
	case 47:
		return "other", "", "动画表情"
	case 48:
		return "other", rawText, "位置"
	case 10000, 10002:
		return "other", rawText, "系统消息"
	default:
		return "other", rawText, "其他"
	}
}

// NormalizeRawMessage transforms raw hook data into a clean, normalized format.
func NormalizeRawMessage(raw *RawHookMessage) *NormalizedMessage {
	isGroup := strings.HasSuffix(raw.Talker, "@chatroom")

	sender := raw.Talker
	content := raw.Content
	note := ""

	// Group message sender extraction:
	// In WeChat group messages, incoming content has format "[sender_wxid]:\n[actual_body]"
	if isGroup {
		if raw.IsSend == 1 {
			if raw.SelfWxid != "" {
				sender = raw.SelfWxid
			} else {
				sender = "self"
			}
		} else {
			if idx := strings.Index(content, ":\n"); idx > 0 {
				sender = content[:idx]
				content = content[idx+2:]
			} else if idx := strings.Index(content, ":\r\n"); idx > 0 {
				sender = content[:idx]
				content = content[idx+3:]
			}
		}
	} else {
		if raw.IsSend == 1 {
			if raw.SelfWxid != "" {
				sender = raw.SelfWxid
			} else {
				sender = "self"
			}
		} else {
			sender = raw.Talker
		}
	}

	kind, content, note := MapWeChatTypeFull(raw.Type, raw.SubType, content)
	// Calculate timestamp in epoch seconds
	ts := raw.CreateTime
	if ts > 1000000000000 { // milliseconds
		ts = ts / 1000
	}
	if ts <= 0 {
		ts = time.Now().Unix()
	}

	// Unique message identifier
	msgId := ""
	if raw.MsgSvrId != 0 {
		msgId = fmt.Sprintf("%d", raw.MsgSvrId)
	} else if raw.MsgId != 0 {
		msgId = fmt.Sprintf("local_%d_%d", raw.MsgId, ts)
	} else {
		msgId = fmt.Sprintf("gen_%s_%d_%d", raw.Talker, ts, time.Now().UnixNano()%100000)
	}

	displayName := raw.DisplayName
	if displayName == "" {
		displayName = raw.Talker
	}

	return &NormalizedMessage{
		MsgId:       msgId,
		ChatId:      raw.Talker,
		DisplayName: displayName,
		IsGroup:     isGroup,
		Sender:      sender,
		Kind:        kind,
		Text:        content,
		Note:        note,
		Ts:          ts,
	}
}

// Store handles SQLite WAL persistence.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// InitStore initializes the SQLite database with WAL and pragmas.
func InitStore(dbPath string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Set connection pool parameters
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	schema := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		msg_id TEXT UNIQUE,
		chat_id TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		is_group INTEGER NOT NULL DEFAULT 0,
		sender TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT 'text',
		text TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		ts INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(ts);
	CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages(chat_id, ts);
	CREATE INDEX IF NOT EXISTS idx_messages_msg_id ON messages(msg_id);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// SaveMessage inserts a normalized message into SQLite.
func (s *Store) SaveMessage(msg *NormalizedMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	isGroupInt := 0
	if msg.IsGroup {
		isGroupInt = 1
	}

	query := `
	INSERT OR IGNORE INTO messages (
		msg_id, chat_id, display_name, is_group, sender, kind, text, note, ts, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query,
		msg.MsgId,
		msg.ChatId,
		msg.DisplayName,
		isGroupInt,
		msg.Sender,
		msg.Kind,
		msg.Text,
		msg.Note,
		msg.Ts,
		time.Now().Unix(),
	)
	return err
}

// QueryMessagesByRange fetches messages between since and to timestamps (inclusive).
func (s *Store) QueryMessagesByRange(since, to int64) ([]NormalizedMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT msg_id, chat_id, display_name, is_group, sender, kind, text, note, ts
	FROM messages
	WHERE ts >= ? AND ts <= ?
	ORDER BY ts ASC, id ASC
	`
	rows, err := s.db.Query(query, since, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []NormalizedMessage
	for rows.Next() {
		var msg NormalizedMessage
		var isGroupInt int
		if err := rows.Scan(
			&msg.MsgId,
			&msg.ChatId,
			&msg.DisplayName,
			&isGroupInt,
			&msg.Sender,
			&msg.Kind,
			&msg.Text,
			&msg.Note,
			&msg.Ts,
		); err != nil {
			return nil, err
		}
		msg.IsGroup = (isGroupInt == 1)
		result = append(result, msg)
	}

	return result, rows.Err()
}

// CountMessages returns the total count of stored messages.
func (s *Store) CountMessages() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count)
	return count, err
}

// PruneMessages deletes rows older than cutoffEpoch and executes PRAGMA incremental_vacuum.
// It acquires the store write lock so readers and writers are safely coordinated.
func (s *Store) PruneMessages(cutoffEpoch int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM messages WHERE ts < ?`, cutoffEpoch)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old messages: %w", err)
	}
	deleted, _ := res.RowsAffected()

	// Run incremental vacuum / wal checkpoint to maintain database health
	_, _ = s.db.Exec(`PRAGMA incremental_vacuum(100)`)
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`)

	return deleted, nil
}

// StartPruneWorker starts a background timer-based goroutine to periodically prune old messages.
func (s *Store) StartPruneWorker(retainDays int, interval time.Duration, stopChan <-chan struct{}) {
	if retainDays <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run initial prune shortly after startup (10s delay)
		select {
		case <-time.After(10 * time.Second):
			cutoff := time.Now().Unix() - int64(retainDays)*86400
			if n, err := s.PruneMessages(cutoff); err == nil && n > 0 {
				fmt.Printf("[WX-Daemon] Initial prune removed %d messages older than %d days\n", n, retainDays)
			}
		case <-stopChan:
			return
		}

		for {
			select {
			case <-ticker.C:
				cutoff := time.Now().Unix() - int64(retainDays)*86400
				if n, err := s.PruneMessages(cutoff); err == nil && n > 0 {
					fmt.Printf("[WX-Daemon] Periodic prune removed %d messages older than %d days\n", n, retainDays)
				}
			case <-stopChan:
				return
			}
		}
	}()
}
