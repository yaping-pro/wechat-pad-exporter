#!/system/bin/sh
# WeChat Watchdog & Tinker Patch Quarantine Loop

TARGET_USER="${TARGET_USER:-0}"
PID_FILE="/data/local/tmp/wx_watchdog.pid"
LOG_FILE="/data/adb/wx_stream/autostart.log"
echo "$$" > "$PID_FILE"

COOLDOWN_SECONDS=90
UDS_WARN_THROTTLE=0

log_wd() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [watchdog] $1" >> "$LOG_FILE"
}

log_wd "Watchdog loop started (PID: $$, target_user: $TARGET_USER)"

while true; do
    sleep 30

    # A. 守护 wx_daemon 存活 (daemon 是服务端，挂了立即拉起)
    if ! pidof wx_daemon >/dev/null 2>&1; then
        log_wd "wx_daemon dead, restarting..."
        nohup /data/local/tmp/wx_daemon \
            -uds @wechat_stream_ipc \
            -http 127.0.0.1:8765 \
            -db /data/adb/wx_stream/vault.db \
            -config /data/adb/wx_stream/config.json \
            >> /data/adb/wx_stream/daemon.log 2>&1 &
    fi

    # B. 检查目标用户下模块完整性
    if ! pm list packages --user "$TARGET_USER" 2>/dev/null | grep -q "com.wx.sniffer"; then
        pm install-existing --user "$TARGET_USER" com.wx.sniffer >/dev/null 2>&1
    fi

    # C. 前置检查：目标用户是否解锁 (FBE 锁屏保护)
    USER_STATE=$(am get-started-user-state "$TARGET_USER" 2>/dev/null)
    if [ -n "$USER_STATE" ] && [ "$USER_STATE" != "RUNNING_UNLOCKED" ]; then
        # 屏幕未解锁时，不拉起应用、不测 UDS
        continue
    fi

    # 动态检测目标用户的微信实际分配 UID
    DETECTED_UID=$(dumpsys package --user "$TARGET_USER" com.tencent.mm 2>/dev/null | grep "userId=" | head -1 | awk -F= '{print $2}' | tr -d ' \r\n')

    # D. 获取微信主进程 PID (精准匹配，排除 :push 进程)
    MM_PID=$(ps -A -o PID,USER,ARGS 2>/dev/null | grep -E "[^ ]+ +com\.tencent\.mm$" | awk '{print $1}' | head -1)

    # E. 守护 WeChat 主进程存活 (只拉起，绝不杀死)
    if [ -z "$MM_PID" ] || [ ! -d "/proc/$MM_PID" ]; then
        log_wd "WeChat main process dead (user $TARGET_USER), starting LauncherUI..."
        am start --user "$TARGET_USER" -n com.tencent.mm/.ui.LauncherUI >/dev/null 2>&1
    else
        # 计算微信主进程存活秒数
        UPTIME_S=$(cut -d. -f1 /proc/uptime 2>/dev/null)
        JIFFIES=$(cut -d" " -f22 "/proc/$MM_PID/stat" 2>/dev/null)
        PROC_AGE_S=0
        if [ -n "$JIFFIES" ] && [ -n "$UPTIME_S" ]; then
            PROC_START_S=$((JIFFIES / 100))
            PROC_AGE_S=$((UPTIME_S - PROC_START_S))
        fi

        # F. 动态 OOM Protection (针对 WeChat 主进程、push 进程及本地 daemon)
        echo -800 > "/proc/$MM_PID/oom_score_adj" 2>/dev/null
        for ppid in $(ps -A -o PID,USER,ARGS 2>/dev/null | grep -E "[^ ]+ +com\.tencent\.mm:push$" | awk '{print $1}'); do
            echo -500 > "/proc/$ppid/oom_score_adj" 2>/dev/null
        done
        for dpid in $(pidof wx_daemon 2>/dev/null); do
            echo -1000 > "/proc/$dpid/oom_score_adj" 2>/dev/null
        done

        # G. UDS 数据流连通性健康探测 (纯只读探测与限流日志，绝不 force-stop 微信)
        if command -v curl >/dev/null 2>&1; then
            if [ "$PROC_AGE_S" -ge "$COOLDOWN_SECONDS" ]; then
                AUTH_HEADER=""
                if [ -f /data/adb/wx_stream/config.json ]; then
                    TOK=$(grep -o '"token"[^,}]*' /data/adb/wx_stream/config.json 2>/dev/null | head -1 | awk -F: '{print $2}' | tr -d ' "\r\n')
                    if [ -n "$TOK" ]; then
                        AUTH_HEADER="-H Authorization: Bearer $TOK"
                    fi
                fi
                HEALTH=$(curl -s $AUTH_HEADER --max-time 3 http://127.0.0.1:8765/health 2>/dev/null)
                if echo "$HEALTH" | grep -q '"uds_connected":false'; then
                    # 仅在初次发现或每 10 次检测 (5分钟) 记一条告警，绝不杀微信
                    if [ "$UDS_WARN_THROTTLE" -eq 1 ] || [ $((UDS_WARN_THROTTLE % 10)) -eq 0 ]; then
                        log_wd "UDS health WARNING: uds_connected=false (age=${PROC_AGE_S}s). WeChat left running undisturbed."
                    fi
                elif echo "$HEALTH" | grep -q '"uds_connected":true'; then
                    UDS_WARN_THROTTLE=0
                fi
            fi
        fi

        # H. Tinker 热更补丁持久化检肃与隔离 (防 beta 补丁回潮，绝不杀微信)
        if [ "$TARGET_USER" = "0" ]; then
            TINKER_DIR="/data/data/com.tencent.mm/tinker"
            TINKER_PREF="/data/data/com.tencent.mm/shared_prefs/tinker_patch_share_config.xml"
        else
            TINKER_DIR="/data/user/$TARGET_USER/com.tencent.mm/tinker"
            TINKER_PREF="/data/user/$TARGET_USER/com.tencent.mm/shared_prefs/tinker_patch_share_config.xml"
        fi

        if [ -d "$TINKER_DIR" ]; then
            FOUND_PATCH=0
            for pdir in "$TINKER_DIR"/patch-*; do
                if [ -d "$pdir" ]; then
                    FOUND_PATCH=1
                    TS=$(date '+%Y%m%d_%H%M%S')
                    mkdir -p "/data/adb/wx_stream/tinker_quarantine/${TS}_patches"
                    mv "$pdir" "/data/adb/wx_stream/tinker_quarantine/${TS}_patches/" 2>/dev/null
                    log_wd "Tinker patch quarantined: $pdir -> ${TS}_patches (WeChat undisturbed)"
                fi
            done
            if [ -f "$TINKER_DIR/patch_meta.info" ]; then
                TS=$(date '+%Y%m%d_%H%M%S')
                mkdir -p "/data/adb/wx_stream/tinker_quarantine/${TS}_patches"
                mv "$TINKER_DIR/patch_meta.info" "/data/adb/wx_stream/tinker_quarantine/${TS}_patches/" 2>/dev/null
                for gdir in "$TINKER_DIR"/guard/patch-*; do
                    if [ -e "$gdir" ]; then
                        mv "$gdir" "/data/adb/wx_stream/tinker_quarantine/${TS}_patches/" 2>/dev/null
                    fi
                done
                log_wd "Tinker patch_meta.info & guard quarantined (WeChat undisturbed)"
                FOUND_PATCH=1
            fi
            if [ "$FOUND_PATCH" -eq 1 ] && [ -f "$TINKER_PREF" ]; then
                sed -i 's/0x28004e38/0x28004e32/g' "$TINKER_PREF" 2>/dev/null
                if [ -n "$DETECTED_UID" ]; then
                    chown "$DETECTED_UID:$DETECTED_UID" "$TINKER_PREF" 2>/dev/null
                fi
                chmod 660 "$TINKER_PREF" 2>/dev/null
            fi
        fi
    fi
done
