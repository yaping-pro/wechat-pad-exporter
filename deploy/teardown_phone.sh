#!/system/bin/sh
# ==============================================================================
# wx-pad-exporter Phone Environment Teardown & Rollback Script
# Reverses system changes made by setup_phone.sh and cleans up autostart scripts.
# Run on phone via Root Shell (adb shell su -c /path/to/teardown_phone.sh [--revert-global-tuning])
# ==============================================================================

echo "========================================================"
echo "  wx-pad-exporter Phone System Teardown"
echo "========================================================"

if [ "$(id -u)" -ne 0 ]; then
    echo "[!] Error: This script must be executed with root privileges (su)."
    exit 1
fi

REVERT_GLOBAL=0
for arg in "$@"; do
    case "$arg" in
        --revert-global-tuning|-g|--all)
            REVERT_GLOBAL=1
            ;;
    esac
done

echo "[*] 1. Stopping daemon and watchdog processes..."
pkill -f "/data/local/tmp/wx_daemon" 2>/dev/null
pkill -f "wx_watchdog.sh" 2>/dev/null
pkill -f "wx_autostart.sh" 2>/dev/null
rm -f /data/local/tmp/wx_daemon.pid /data/local/tmp/wx_watchdog.pid

echo "[*] 2. Removing service.d autostart scripts..."
rm -f /data/adb/service.d/99_wx_sniffer.sh
rm -f /data/adb/service.d/wx_autostart.sh
rm -f /data/adb/service.d/wx_watchdog.sh
echo "    -> Autostart scripts removed from /data/adb/service.d"

echo "[*] 3. Cleaning dedicated application workspace..."
rm -rf /data/local/tmp/wx_pad_exporter
rm -f /data/local/tmp/wx_daemon /data/local/tmp/service.sh /data/local/tmp/setup_phone.sh /data/local/tmp/teardown_phone.sh /data/local/tmp/wx_autostart.sh /data/local/tmp/wx_watchdog.sh

# Re-assert Android default 0771 permissions on /data/local/tmp
chmod 771 /data/local/tmp
echo "    -> Restored /data/local/tmp default 0771 permissions"

if [ "$REVERT_GLOBAL" -eq 1 ]; then
    echo "[*] 4. Reverting Doze & Battery Optimization Whitelist..."
    dumpsys deviceidle whitelist -com.tencent.mm 2>/dev/null
    dumpsys deviceidle whitelist -com.wx.sniffer 2>/dev/null
    echo "    -> Removed com.tencent.mm and com.wx.sniffer from deviceidle whitelist"

    echo "[*] 5. Resetting Android Phantom Process Killer setting..."
    device_config delete activity_manager max_phantom_processes 2>/dev/null
    device_config set_sync_disabled_for_tests none 2>/dev/null
    echo "    -> Reset max_phantom_processes to Android default"

    echo "[*] 6. Resetting Wi-Fi Sleep Policy..."
    settings delete global wifi_sleep_policy 2>/dev/null
    echo "    -> Reset wifi_sleep_policy"

    echo "[*] 7. Resetting AppOps background permissions..."
    appops reset com.tencent.mm 2>/dev/null
    appops reset com.wx.sniffer 2>/dev/null
    echo "    -> Reset AppOps permissions"
else
    echo "[*] Skipped global tuning rollback (pass --revert-global-tuning to revert deviceidle/phantom-process tweaks)."
fi

echo "========================================================"
echo "  Teardown complete. (Vault database at /data/adb/wx_stream preserved)."
echo "========================================================"
