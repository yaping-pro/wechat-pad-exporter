package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DaemonConfig holds configuration fields loaded from JSON config file and flags.
type DaemonConfig struct {
	Bind         string   `json:"bind"`
	Port         int      `json:"port"`
	UdsPath      string   `json:"uds_path"`
	DbPath       string   `json:"db_path"`
	Token        string   `json:"token"`
	Auth         *bool    `json:"auth,omitempty"` // nil = default true unless NoAuth flag or token empty
	AllowChats   []string `json:"allow_chats"`
	BlockChats   []string `json:"block_chats"`
	LogFile      string   `json:"log_file,omitempty"`
	LogMaxBytes  int64    `json:"log_max_bytes,omitempty"`
	LogKeep      int      `json:"log_keep,omitempty"`
	RetainDays   int      `json:"retain_days,omitempty"`
}

// DefaultConfig returns baseline configuration values.
func DefaultConfig() DaemonConfig {
	authDefault := true
	return DaemonConfig{
		Bind:        "127.0.0.1",
		Port:        8765,
		UdsPath:     "@wechat_stream_ipc",
		DbPath:      ResolveDefaultDbPath(),
		Token:       "",
		Auth:        &authDefault,
		AllowChats:  []string{},
		BlockChats:  []string{},
		LogFile:     "",
		LogMaxBytes: 10 * 1024 * 1024, // 10MB
		LogKeep:     3,
		RetainDays:  90,
	}
}

// LoadConfigFile reads and parses a JSON config file into DaemonConfig.
func LoadConfigFile(path string) (DaemonConfig, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}

	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return cfg, fmt.Errorf("failed to read config file %s: %w", cleanPath, err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse JSON config file %s: %w", cleanPath, err)
	}

	return cfg, nil
}
