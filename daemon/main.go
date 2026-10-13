package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ResolveDefaultDbPath determines the default DB path adhering to XDG conventions.
func ResolveDefaultDbPath() string {
	if envPath := os.Getenv("WX_DB_PATH"); envPath != "" {
		return envPath
	}
	// Android: store decrypted chat plaintext in a root-only directory, NOT the
	// world-traversable /data/local/tmp (mode 0771 with shell group rwx, and the
	// daemon previously created wx_vault.db as 0644 = world-readable).
	// /data/adb is only accessible by root; deploy/service.sh creates
	// /data/adb/wx_stream with mode 0700 at boot.
	if _, err := os.Stat("/data/local/tmp"); err == nil {
		return "/data/adb/wx_stream/vault.db"
	}
	// Standard XDG Data Home
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		_ = os.MkdirAll(filepath.Join(xdgData, "wx-pad-sniffer"), 0755)
		return filepath.Join(xdgData, "wx-pad-sniffer", "wx_vault.db")
	}
	if home, err := os.UserHomeDir(); err == nil {
		xdgDir := filepath.Join(home, ".local", "share", "wx-pad-sniffer")
		_ = os.MkdirAll(xdgDir, 0755)
		return filepath.Join(xdgDir, "wx_vault.db")
	}
	return "wx_vault.db"
}

func main() {
	var (
		configPath  = flag.String("config", "", "Path to JSON config file (e.g. /data/adb/wx_stream/config.json)")
		httpAddr    = flag.String("http", "", "HTTP listen address (e.g. 127.0.0.1:8765 or 0.0.0.0:8765)")
		dbPath      = flag.String("db", "", "Path to SQLite database")
		udsAddr     = flag.String("uds", "", "Unix Domain Socket address (use @ for Linux abstract socket)")
		token          = flag.String("token", "", "Bearer token for API authorization")
		noAuth         = flag.Bool("no-auth", false, "Explicitly disable Bearer token authorization")
		insecureNoAuth = flag.Bool("insecure-no-auth", false, "Explicitly allow unauthenticated access when token is empty")
		retainDays     = flag.Int("retain-days", -1, "Days of messages to retain (0 disables pruning)")
		logMaxBytes    = flag.Int64("log-max-bytes", -1, "Max log file bytes before rotation")
		logKeep     = flag.Int("log-keep", -1, "Number of rotated log backups to retain")
		logFile     = flag.String("log-file", "", "Log file path for rotation (empty = write to stderr only)")
	)
	flag.Parse()

	// 1. Load config file if specified
	cfg := DefaultConfig()
	if *configPath != "" {
		loaded, err := LoadConfigFile(*configPath)
		if err != nil {
			log.Fatalf("[WX-Daemon] Config load error: %v", err)
		}
		cfg = loaded
	}

	// 2. Flags override config
	if *httpAddr != "" {
		// Support either "host:port" or "port" or ":port"
		addr := *httpAddr
		if strings.Contains(addr, ":") {
			parts := strings.Split(addr, ":")
			if parts[0] != "" {
				cfg.Bind = parts[0]
			}
			if p, err := strconv.Atoi(parts[1]); err == nil {
				cfg.Port = p
			}
		} else {
			cfg.Bind = addr
		}
	}
	if *dbPath != "" {
		cfg.DbPath = *dbPath
	}
	if *udsAddr != "" {
		cfg.UdsPath = *udsAddr
	}
	if *token != "" {
		cfg.Token = *token
	}
	if *noAuth || *insecureNoAuth {
		val := false
		cfg.Auth = &val
	}
	if *retainDays >= 0 {
		cfg.RetainDays = *retainDays
	}
	if *logMaxBytes > 0 {
		cfg.LogMaxBytes = *logMaxBytes
	}
	if *logKeep > 0 {
		cfg.LogKeep = *logKeep
	}
	if *logFile != "" {
		cfg.LogFile = *logFile
	}

	// 3. Resolve log rotation writer
	if cfg.LogFile != "" {
		rotWriter, err := NewRotatingLogWriter(cfg.LogFile, cfg.LogMaxBytes, cfg.LogKeep)
		if err != nil {
			log.Printf("[WX-Daemon] WARN: failed to initialize rotating log at %s: %v", cfg.LogFile, err)
		} else {
			defer rotWriter.Close()
			log.SetOutput(MultiWriterWithStderr(rotWriter))
		}
	}

	// Compute effective AuthEnabled (Fail closed)
	authEnabled := true
	if cfg.Auth != nil && !*cfg.Auth {
		authEnabled = false
		log.Printf("[WX-Daemon] WARNING: Authentication is EXPLICITLY DISABLED via config or -no-auth/-insecure-no-auth.")
	} else if cfg.Token == "" {
		log.Fatalf("[WX-Daemon] FATAL: Authentication token is required but empty. To run unauthenticated, explicitly provide -no-auth or -insecure-no-auth flag (or \"auth\": false in config).")
	}
	finalHTTPAddr := net.JoinHostPort(cfg.Bind, strconv.Itoa(cfg.Port))

	log.Printf("[WX-Daemon] Starting wx-pad-sniffer daemon (OS=%s ARCH=%s)...", runtime.GOOS, runtime.GOARCH)
	log.Printf("[WX-Daemon] SQLite Vault: %s", cfg.DbPath)
	log.Printf("[WX-Daemon] HTTP Bind: %s, AuthEnabled: %v, TokenConfigured: %v", finalHTTPAddr, authEnabled, cfg.Token != "")
	log.Printf("[WX-Daemon] Chat Filter: allow=%d, block=%d", len(cfg.AllowChats), len(cfg.BlockChats))
	log.Printf("[WX-Daemon] Message Retention: %d days", cfg.RetainDays)

	store, err := InitStore(cfg.DbPath)
	if err != nil {
		log.Fatalf("[WX-Daemon] Failed to init store: %v", err)
	}
	// Restrict the SQLite file (and its WAL/shm siblings created by the driver)
	// to the owning user only.
	if err := os.Chmod(cfg.DbPath, 0600); err != nil {
		log.Printf("[WX-Daemon] WARN: failed to chmod %s to 0600: %v", cfg.DbPath, err)
	}
	defer store.Close()

	stopPruneChan := make(chan struct{})
	defer close(stopPruneChan)
	if cfg.RetainDays > 0 {
		// Run prune worker every 6 hours
		store.StartPruneWorker(cfg.RetainDays, 6*time.Hour, stopPruneChan)
	}

	state := &ServerState{
		StartTime:   time.Now(),
		Store:       store,
		Token:       cfg.Token,
		AuthEnabled: authEnabled,
		AllowChats:  cfg.AllowChats,
		BlockChats:  cfg.BlockChats,
	}

	// 4. Start HTTP Server
	router := SetupRouter(state)
	httpServer := &http.Server{
		Addr:         finalHTTPAddr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		log.Printf("[WX-Daemon] HTTP Server listening on %s (Endpoints: /health, /api/wechat/export)", finalHTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[WX-Daemon] HTTP Server error: %v", err)
		}
	}()

	// 5. Start UDS Listener
	udsListener, err := listenUDS(cfg.UdsPath)
	if err != nil {
		log.Fatalf("[WX-Daemon] Failed to listen on UDS (%s): %v", cfg.UdsPath, err)
	}
	defer udsListener.Close()
	log.Printf("[WX-Daemon] IPC UDS Server listening on %s", udsListener.Addr().String())

	// Channel for incoming message pipeline
	msgChan := make(chan *RawHookMessage, 10000)

	// Worker goroutine for database persistence
	go func() {
		for raw := range msgChan {
			state.TotalReceived.Add(1)
			norm := NormalizeRawMessage(raw)
			if err := store.SaveMessage(norm); err != nil {
				log.Printf("[WX-Daemon] Failed to persist message (%s): %v", norm.MsgId, err)
			}
		}
	}()

	// Accept connections loop
	go func() {
		for {
			conn, err := udsListener.Accept()
			if err != nil {
				if strings.Contains(err.Error(), "use of closed network connection") {
					break
				}
				log.Printf("[WX-Daemon] UDS Accept error: %v", err)
				continue
			}

			state.ActiveClients.Add(1)
			go handleUdsConnection(conn, state, msgChan)
		}
	}()

	// 6. Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	log.Printf("[WX-Daemon] Received signal %v, shutting down...", sig)

	_ = udsListener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)

	close(msgChan)
	log.Printf("[WX-Daemon] Daemon shutdown complete.")
}

// listenUDS handles both Linux Abstract Unix Domain Socket (@name) and filesystem-based socket paths.
func listenUDS(addr string) (net.Listener, error) {
	if runtime.GOOS == "linux" || runtime.GOOS == "android" {
		// On Linux and Android, @prefix indicates abstract namespace socket
		return net.Listen("unix", addr)
	}

	// On non-Linux (macOS/Darwin fallback for local testing):
	// Abstract sockets are not supported; convert to a temporary file socket
	fileSock := addr
	if strings.HasPrefix(addr, "@") {
		fileSock = filepath.Join(os.TempDir(), strings.TrimPrefix(addr, "@")+".sock")
	}
	_ = os.Remove(fileSock)
	return net.Listen("unix", fileSock)
}

// handleUdsConnection processes streaming JSON lines from the Android hook client.
func handleUdsConnection(conn net.Conn, state *ServerState, out chan<- *RawHookMessage) {
	defer func() {
		_ = conn.Close()
		state.ActiveClients.Add(-1)
	}()

	scanner := bufio.NewScanner(conn)
	// Max line buffer: 1MB to accommodate large XML or link contents
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var rawMsg RawHookMessage
		if err := json.Unmarshal(line, &rawMsg); err != nil {
			log.Printf("[WX-Daemon] JSON Unmarshal error: %v (raw: %s)", err, string(line))
			continue
		}

		select {
		case out <- &rawMsg:
		default:
			log.Printf("[WX-Daemon] Warning: msgChan buffer full, dropping message %d", rawMsg.MsgId)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[WX-Daemon] Connection read error: %v", err)
	}
}
