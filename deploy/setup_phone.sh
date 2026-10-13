#!/system/bin/sh
# ==============================================================================
# wx-pad-exporter One-Click Phone Optimization & Environment Setup
# Configures app area workspace, Magisk/KernelSU autostart, and optional system tuning.
# Run on phone via Root Shell (adb shell su -c /path/to/setup_phone.sh [--enable-global-tuning])
# ==============================================================================

echo "========================================================"
echo "  wx-pad-exporter Phone System Environment Setup"
echo "========================================================"

if [ "$(id -u)" -ne 0 ]; then
    echo "[!] Error: This script must be executed with root privileges (su)."
    exit 1
fi

ENABLE_GLOBAL_TUNING=0
for arg in "$@"; do
    case "$arg" in
        --enable-global-tuning|-g)
            ENABLE_GLOBAL_TUNING=1
            ;;
    esac
done

if [ "$ENABLE_GLOBAL_TUNING" -eq 1 ]; then
    echo "[*] 1. Configuring Doze & Battery Optimization Whitelist..."
    dumpsys deviceidle whitelist +com.tencent.mm 2>/dev/null
    dumpsys deviceidle whitelist +com.wx.sniffer 2>/dev/null
    echo "    -> Whitelisted com.tencent.mm and com.wx.sniffer"

    echo "[*] 2. Disabling Android Phantom Process Killer (Android 12/13/14+)..."
    device_config set_sync_disabled_for_tests persistent 2>/dev/null
    device_config put activity_manager max_phantom_processes 2147483647 2>/dev/null
    echo "    -> Set max_phantom_processes to unlimited"

    echo "[*] 3. Configuring Wi-Fi Sleep Policy (Always keep connected)..."
    settings put global wifi_sleep_policy 2 2>/dev/null
    echo "    -> Wi-Fi sleep policy set to ALWAYS_ON"

    echo "[*] 4. Granting Background Execution & AppOps permissions..."
    appops set com.tencent.mm RUN_IN_BACKGROUND allow 2>/dev/null
    appops set com.tencent.mm RUN_ANY_IN_BACKGROUND allow 2>/dev/null
    appops set com.wx.sniffer RUN_IN_BACKGROUND allow 2>/dev/null
    appops set com.wx.sniffer RUN_ANY_IN_BACKGROUND allow 2>/dev/null
    echo "    -> AppOps background execution granted"
else
    echo "[*] Global system tweaks (deviceidle / phantom-process / wifi_sleep_policy) are gated behind --enable-global-tuning."
    echo "    To opt-in, run: setup_phone.sh --enable-global-tuning"
    echo "    To reverse global tweaks, run: teardown_phone.sh --revert-global-tuning"
fi

echo "[*] 5. Setting up dedicated application workspace permissions..."
# Maintain Android default 0771 permissions on /data/local/tmp (NEVER chmod 777)
mkdir -p /data/local/tmp
chmod 771 /data/local/tmp

# Dedicated app area for wx-pad-exporter binaries and runtime with strict 0700 permissions
mkdir -p /data/local/tmp/wx_pad_exporter
chmod 700 /data/local/tmp/wx_pad_exporter
echo "    -> Maintained /data/local/tmp default 0771; created dedicated 0700 dir: /data/local/tmp/wx_pad_exporter"

# Root-only vault directory for encrypted/decrypted chat SQLite DB
mkdir -p /data/adb/wx_stream
chmod 700 /data/adb/wx_stream
echo "    -> Vault dir /data/adb/wx_stream prepared (mode 0700)"

# Setup Magisk / KernelSU service.d autostart if directory exists
if [ -d "/data/adb/service.d" ]; then
    echo "[*] Magisk/KernelSU detected at /data/adb/service.d"
    if [ -f "/data/local/tmp/wx_autostart.sh" ]; then
        cp -f /data/local/tmp/wx_autostart.sh /data/adb/service.d/99_wx_sniffer.sh 2>/dev/null
    elif [ -f "/data/local/tmp/service.sh" ]; then
        cp -f /data/local/tmp/service.sh /data/adb/service.d/99_wx_sniffer.sh 2>/dev/null
    fi
    chmod 755 /data/adb/service.d/99_wx_sniffer.sh 2>/dev/null
    echo "    -> Installed boot autostart script: /data/adb/service.d/99_wx_sniffer.sh"
fi

echo "========================================================"
echo "  Environment setup complete."
echo "========================================================"
