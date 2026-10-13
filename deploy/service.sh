#!/system/bin/sh
# ==============================================================================
# wx-pad-sniffer Daemon Service Launcher
# Compatible with Magisk / KernelSU boot script (/data/adb/service.d/) or manual launch
# ==============================================================================

DAEMON_BIN="/data/local/tmp/wx_daemon"
DB_DIR="/data/adb/wx_stream"
DB_PATH="$DB_DIR/vault.db"
CONFIG_FILE="$DB_DIR/config.json"
HTTP_ADDR="127.0.0.1:8765"
UDS_ADDR="@wechat_stream_ipc"
LOG_FILE="$DB_DIR/daemon.log"
DAEMON_LOG_MAX_BYTES=10485760
DAEMON_LOG_KEEP=3
PID_FILE="/data/local/tmp/wx_daemon.pid"

# Wait for system boot to settle if started by service.d
until [ "$(getprop sys.boot_completed)" = "1" ]; do
    sleep 3
done

start_daemon() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if kill -0 "$PID" 2>/dev/null; then
            echo "[service.sh] wx_daemon already running with PID $PID"
            return 0
        fi
    fi

    if [ ! -f "$DAEMON_BIN" ]; then
        echo "[service.sh] ERROR: Daemon binary not found at $DAEMON_BIN"
        return 1
    fi

    chmod +x "$DAEMON_BIN"

    # Ensure the vault directory exists and is root-only (0700). The decrypted
    # chat plaintext must not live in the world-traversable /data/local/tmp.
    mkdir -p "$DB_DIR"
    chmod 700 "$DB_DIR"

    if [ ! -f "$CONFIG_FILE" ]; then
        echo "[service.sh] ERROR: Configuration file not found at $CONFIG_FILE"
        echo "[service.sh] The daemon fails closed by default to prevent unauthorized access."
        echo "[service.sh] Please create $CONFIG_FILE with a valid 'token' (e.g. from daemon/config.example.json) before starting."
        return 1
    fi

    DAEMON_ARGS="-http=$HTTP_ADDR -db=$DB_PATH -uds=$UDS_ADDR -config=$CONFIG_FILE"

    echo "[service.sh] Starting wx_daemon on $HTTP_ADDR (UDS: $UDS_ADDR)..."
    nohup "$DAEMON_BIN" $DAEMON_ARGS >> "$LOG_FILE" 2>&1 &
    DAEMON_PID=$!
    echo "$DAEMON_PID" > "$PID_FILE"
    echo "[service.sh] Daemon started with PID $DAEMON_PID. Log: $LOG_FILE"
}

stop_daemon() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        echo "[service.sh] Stopping wx_daemon (PID $PID)..."
        kill "$PID" 2>/dev/null
        rm -f "$PID_FILE"
    else
        echo "[service.sh] No PID file found, killing by binary name..."
        pkill -f "$DAEMON_BIN" 2>/dev/null
    fi
}

case "$1" in
    start)
        start_daemon
        ;;
    stop)
        stop_daemon
        ;;
    restart)
        stop_daemon
        sleep 1
        start_daemon
        ;;
    status)
        if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
            echo "[service.sh] Running (PID $(cat "$PID_FILE"))"
        else
            echo "[service.sh] Stopped"
        fi
        ;;
    *)
        start_daemon
        ;;
esac
