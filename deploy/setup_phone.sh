#!/system/bin/sh
# ==============================================================================
# wx-pad-sniffer One-Click Phone Optimization & Environment Setup
# Configures Doze whitelist, background execution limits, Wi-Fi policy, and persistence.
# Run on phone via Root Shell (adb shell su -c /path/to/setup_phone.sh)
# ==============================================================================

echo "========================================================"
echo "  wx-pad-sniffer Phone System Environment Setup"
echo "========================================================"

if [ "$(id -u)" -ne 0 ]; then
    echo "[!] Error: This script must be executed with root privileges (su)."
    exit 1
fi

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

echo "[*] 5. Setting up /data/local/tmp workspace permissions..."
mkdir -p /data/local/tmp
chmod 777 /data/local/tmp
# Create the root-only vault directory for the decrypted chat SQLite DB.
# /data/local/tmp is world-traversable (0771, shell group rwx) so the vault
# must NOT live there; the daemon creates it as 0600 inside this 0700 dir.
mkdir -p /data/adb/wx_stream
chmod 700 /data/adb/wx_stream
echo "    -> Vault dir /data/adb/wx_stream prepared (mode 0700)"


# Setup Magisk / KernelSU service.d autostart if directory exists
if [ -d "/data/adb/service.d" ]; then
    echo "[*] Magisk/KernelSU detected at /data/adb/service.d"
    cp -f /data/local/tmp/service.sh /data/adb/service.d/99_wx_sniffer.sh 2>/dev/null
    chmod 755 /data/adb/service.d/99_wx_sniffer.sh 2>/dev/null
    echo "    -> Installed boot autostart script: /data/adb/service.d/99_wx_sniffer.sh"
fi

echo "========================================================"
echo "  System tuning complete. Phone is ready for sniffing!"
echo "========================================================"
