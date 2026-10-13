#!/system/bin/sh
# WeChat Autostart & Watchdog Supervisor (Magisk service.d / manual)
# Architecture: Industrial Robust Watchdog
# Core Invariants:
# 1. NEVER force-stop WeChat. WeChat self-heals UDS connections via sniffer reconnect loops.
# 2. Strict singleton watchdog instance via PID file & process termination of stale workers.
# 3. Fail-safe: Skip WeChat launching if target user is in FBE locked state (before first unlock).
# 4. Accurate main-process detection (anchored regex `com\.tencent\.mm$`).
# 5. Non-interfering OOM protection: WeChat main=-800, push=-500, daemon=-1000.
# 6. All Android framework calls MUST discard stdout/stderr (>/dev/null 2>&1) to prevent Binder DAC (2147483646).
# 7. Persistent Tinker Hotfix Quarantine: Automatically inspects and quarantines beta patches without killing WeChat.

TARGET_USER="${TARGET_USER:-0}"
LOG_FILE="/data/adb/wx_stream/autostart.log"
PID_FILE="/data/local/tmp/wx_watchdog.pid"
MAX_LOG_LINES=200
mkdir -p /data/adb/wx_stream
mkdir -p /data/adb/wx_stream/tinker_quarantine
chmod 700 /data/adb/wx_stream

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [wx_autostart] $1" >> "$LOG_FILE"
    # 轮转: 超过上限则保留尾部
    if [ -f "$LOG_FILE" ]; then
        LINES=$(wc -l < "$LOG_FILE" 2>/dev/null || echo 0)
        if [ "$LINES" -gt "$MAX_LOG_LINES" ]; then
            tail -n "$MAX_LOG_LINES" "$LOG_FILE" > "${LOG_FILE}.tmp" 2>/dev/null \
                && mv "${LOG_FILE}.tmp" "$LOG_FILE"
        fi
    fi
}

log "Service.d wx_autostart script started (target_user=$TARGET_USER)"

# 1. 单例治理：清理任何已存在的 watchdog sh 循环，杜绝多实例踩踏
if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE" 2>/dev/null)
    if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
        log "Terminating existing watchdog instance (PID: $OLD_PID)..."
        kill -9 "$OLD_PID" 2>/dev/null
    fi
    rm -f "$PID_FILE" 2>/dev/null
fi

# 杀死所有遗留的 watchdog 循环子进程
for stale_pid in $(ps -A -o PID,ARGS 2>/dev/null | grep -E "wx_watchdog|autostart\.log|sleep 30" | awk '{print $1}'); do
    kill -9 "$stale_pid" 2>/dev/null
done

# 2. 等待系统启动完成
until [ "$(getprop sys.boot_completed)" = "1" ]; do
    sleep 2
done
log "sys.boot_completed is 1"

# 3. 等待基础系统服务就绪
sleep 5

# 4. 屏幕常亮与网络保活设置
settings put global wifi_sleep_policy 2 >/dev/null 2>&1
svc power stayon true >/dev/null 2>&1
log "Configured wifi_sleep_policy=2 and stayon=true"

# 5. 电量白名单保护 (Doze 豁免)
dumpsys deviceidle whitelist +com.tencent.mm >/dev/null 2>&1
dumpsys deviceidle whitelist +com.wx.sniffer >/dev/null 2>&1
log "Added WeChat and sniffer to deviceidle whitelist"

# 6. 确保目标用户环境下 Xposed 模块安装就绪
if ! pm list packages --user "$TARGET_USER" 2>/dev/null | grep -q "com.wx.sniffer"; then
    log "Installing com.wx.sniffer for user $TARGET_USER..."
    pm install-existing --user "$TARGET_USER" com.wx.sniffer >/dev/null 2>&1
fi

# Vector / LSPosed CLI 作用域配置
VECTOR_CLI=""
if [ -x /data/adb/modules/zygisk_vector/cli ]; then
    VECTOR_CLI="/data/adb/modules/zygisk_vector/cli"
elif [ -x /data/adb/lspd/cli ]; then
    VECTOR_CLI="/data/adb/lspd/cli"
fi

if [ -n "$VECTOR_CLI" ]; then
    $VECTOR_CLI modules enable com.wx.sniffer >/dev/null 2>&1
    $VECTOR_CLI scope add com.wx.sniffer "com.tencent.mm/$TARGET_USER" >/dev/null 2>&1
    log "Vector CLI configured modules and scopes"
fi

# 7. 确保 wx_daemon 运行并就绪
if ! pidof wx_daemon >/dev/null 2>&1; then
    log "Starting wx_daemon..."
    nohup /data/local/tmp/wx_daemon \
        -uds @wechat_stream_ipc \
        -http 127.0.0.1:8765 \
        -db /data/adb/wx_stream/vault.db \
        -config /data/adb/wx_stream/config.json \
        >> /data/adb/wx_stream/daemon.log 2>&1 &
fi

# 等待 daemon HTTP 端口可用 (最多 10s)
for i in $(seq 1 10); do
    if command -v curl >/dev/null 2>&1; then
        HEALTH=$(curl -s --max-time 1 http://127.0.0.1:8765/health 2>/dev/null)
        if echo "$HEALTH" | grep -q '"status":"ok"'; then
            log "wx_daemon HTTP endpoint is ready"
            break
        fi
    elif pidof wx_daemon >/dev/null 2>&1; then
        log "wx_daemon process is running"
        break
    fi
    sleep 1
done

# 8. 尝试拉起微信 (仅当目标用户已完全解锁)
TARGET_STATE=$(am get-started-user-state "$TARGET_USER" 2>/dev/null)
if [ "$TARGET_STATE" = "RUNNING_UNLOCKED" ] || [ -z "$TARGET_STATE" ]; then
    if ! ps -A -o USER,ARGS 2>/dev/null | grep -E "[^ ]+ +com\.tencent\.mm$" >/dev/null 2>&1; then
        log "User $TARGET_USER unlocked, starting WeChat LauncherUI..."
        am start --user "$TARGET_USER" -n com.tencent.mm/.ui.LauncherUI >/dev/null 2>&1
    fi
else
    log "User $TARGET_USER is not unlocked ($TARGET_STATE), skipping initial WeChat launch"
fi

# 9. 启动 Watchdog 后台守护进程 (工业级鲁棒设计)
if [ -f /data/adb/service.d/wx_watchdog.sh ]; then
    chmod 755 /data/adb/service.d/wx_watchdog.sh
    TARGET_USER="$TARGET_USER" nohup /system/bin/sh /data/adb/service.d/wx_watchdog.sh >> /data/adb/wx_stream/autostart.log 2>&1 &
    log "Watchdog background process launched successfully (via wx_watchdog.sh)"
fi
