# AGENT.md — Autonomous Agent Deployment Runbook

> **Target Audience**: AI Agents (Claude Code, Pi/omp, Codex, Cursor, AutoGPT, etc.) executing with a connected USB/TCP ADB target.  
> **Human Prerequisite**: The human user only needs to **root the device, enable USB debugging, and plug it in**. The agent autonomously executes and verifies the entire remaining pipeline.

---

## 1. Safety Rails & Operating Invariants

Before issuing any command, the agent **MUST** enforce the following safety invariants:

1. **100% Strictly Read-Only**: NEVER execute any write, inject synthetic keystrokes/taps to send messages, or call any active WeChat API.
2. **Never Force-Stop WeChat**: NEVER execute `am force-stop com.tencent.mm` on a logged-in WeChat session. Force-stopping risks session invalidation, push desynchronization, and account-level risk scoring.
3. **Loopback Only (`127.0.0.1`)**: NEVER bind `wx_daemon` to `0.0.0.0` or expose unencrypted HTTP to external network interfaces. All workstation access MUST route through `adb forward tcp:8765 tcp:8765` or encrypted SSH/WireGuard tunnels.
4. **No Dangling Scripts in `service.d`**: Magisk/KernelSU executes **every** file in `/data/adb/service.d/` on boot. NEVER store `.bak`, `.old`, or temporary files in `/data/adb/service.d/`. All backups must reside in `/data/local/tmp/wx_deploy_bak/`.
5. **Secret Hygiene**: NEVER print the Bearer token into commit messages, shared execution logs, or unencrypted world-readable storage. `/data/adb/wx_stream` MUST be mode `0700` and `config.json` MUST be mode `0600`.
6. **Ground Truth Flag Invariant**: The Go daemon binary accepts only the following real flags:
   - `-config <path>` (e.g. `/data/adb/wx_stream/config.json`)
   - `-http <addr>` (default `127.0.0.1:8765`)
   - `-db <path>` (default `/data/adb/wx_stream/vault.db`)
   - `-uds <addr>` (default `@wechat_stream_ipc`)
   - `-token <token>`
   - `-no-auth`
   - `-retain-days <n>`
   - `-log-file <path>`
   - `-log-max-bytes <n>`
   - `-log-keep <n>`
   - **FORBIDDEN PARAMETERS**: Do NOT invent nonexistent parameters (e.g. listening flags or env-based auth). The daemon strictly uses the flags above.

---

## 2. Preflight Probes & Hard Gates

The agent must run these probes sequentially. If any hard gate fails, stop immediately and output the exact remediation.

### Gate 1: Resolve ADB Target (Never Hardcode)
```bash
# List all connected targets
adb devices -l
```
- **Autonomous Resolution**:
  ```bash
  # Dynamically pick the target device
  DEVICE_COUNT=$(adb devices | grep -v "List of devices" | grep -c "device$")
  if [ "$DEVICE_COUNT" -eq 0 ]; then
      echo "[ABORT] No authorized adb device found. Please enable USB debugging and accept the authorization prompt on the screen."
      exit 1
  elif [ "$DEVICE_COUNT" -eq 1 ]; then
      TARGET_SERIAL=$(adb devices | grep -v "List of devices" | grep "device$" | awk '{print $1}')
      export ADB="adb -s $TARGET_SERIAL"
  else
      # If multiple devices exist, probe device properties or require human to specify serial
      echo "[ABORT] Multiple devices detected. Specify target serial via export ANDROID_SERIAL=<serial>."
      exit 1
  fi
  ```

### Gate 2: Assert Root Privilege (`su -c id`)
```bash
$ADB shell "su -c id"
```
- **Expected Output**: Must contain `uid=0(root) gid=0(root)`
- **Abort Condition**: Returns `127` (command not found), permission denied, or non-zero UID.
- **Remediation**: Prompt user: *"Root access is required. Ensure Magisk or KernelSU is installed and grant Root permission to Shell/ADB."*

### Gate 3: Detect WeChat Package & Active Profile UID
```bash
# Check if WeChat is installed
$ADB shell "pm list packages | grep com.tencent.mm"
```
- **Abort Condition**: Empty output.
- **Remediation**: WeChat is not installed on the device. Prompt user to install WeChat.
- **UID / Profile Resolution**:
  ```bash
  TARGET_USER=0
  DETECTED_UID=$($ADB shell "dumpsys package --user $TARGET_USER com.tencent.mm 2>/dev/null | grep 'userId=' | head -1 | awk -F= '{print \$2}' | tr -d ' \r\n'")
  if [ -z "$DETECTED_UID" ]; then
      # Probe secondary/dual-app user (e.g. 999)
      TARGET_USER=999
      DETECTED_UID=$($ADB shell "dumpsys package --user $TARGET_USER com.tencent.mm 2>/dev/null | grep 'userId=' | head -1 | awk -F= '{print \$2}' | tr -d ' \r\n'")
  fi
  echo "Target user: $TARGET_USER, Detected UID: $DETECTED_UID"
  ```

### Gate 4: Assert WeChat Login State & Tablet Mode
```bash
# Check if WeChat account database (MicroMsg directory) exists
$ADB shell "su -c 'ls -d /data/data/com.tencent.mm/MicroMsg/* 2>/dev/null | grep -E \"[0-9a-f]{32}\" | head -1'"
```
- **Login Verification**: An MD5 directory under `MicroMsg` indicates WeChat has completed login and generated an account profile vault.
- **Check Tablet / Dual-Login State**:
  Check whether WeChat is running concurrently in Pad/Tablet mode:
  ```bash
  $ADB shell "su -c 'dumpsys package com.tencent.mm | grep -E \"versionName|firstInstallTime\" | head -2'"
  ```
- **HARD STOP CONDITION**: If WeChat is **not** logged in, or if the user is on a phone and needs dual-login with their primary mobile device, the agent **MUST STOP** and report:
  > *"WeChat must be logged into Pad/Tablet mode first so it can coexist with your primary phone. Please follow the instructions in companion repo [yaping-pro/wx-tablet-mode](https://github.com/yaping-pro/wx-tablet-mode) to complete tablet login, then re-run this runbook."*

### Gate 5: Assert LSPosed / Vector Xposed Environment
```bash
$ADB shell "su -c '[ -d /data/adb/lspd ] || [ -d /data/adb/modules/zygisk_vector ] || [ -d /data/adb/modules/zygisk_lsposed ] && echo OK || echo MISSING'"
```
- **Abort Condition**: Returns `MISSING`.
- **Remediation**: Install LSPosed (Zygisk) or Vector module in Magisk/KernelSU and reboot the device.

---

## 3. Ordered Autonomous Execution Steps

Execute these steps in strict sequence:

### Step 1: Secure Directory Setup & High-Entropy Token Generation
```bash
# 1. Generate 32-byte cryptographically secure random token locally on workstation
TOKEN=$(openssl rand -hex 32)
echo "Generated token for session: ${TOKEN:0:8}..."

# 2. Prepare isolated root directory on device (0700)
$ADB shell "su -c 'mkdir -p /data/adb/wx_stream && chmod 700 /data/adb/wx_stream'"

# 3. Write config.json atomically (mode 0600)
$ADB shell "su -c 'cat << \"EOF\" > /data/adb/wx_stream/config.json
{
  \"bind\": \"127.0.0.1\",
  \"port\": 8765,
  \"uds_path\": \"@wechat_stream_ipc\",
  \"db_path\": \"/data/adb/wx_stream/vault.db\",
  \"token\": \"$TOKEN\",
  \"auth\": true,
  \"allow_chats\": [],
  \"block_chats\": [],
  \"log_file\": \"/data/adb/wx_stream/daemon.log\",
  \"log_max_bytes\": 10485760,
  \"log_keep\": 3,
  \"retain_days\": 90
}
EOF
chmod 600 /data/adb/wx_stream/config.json'"
```
- **Expected Verification**: `$ADB shell "su -c 'ls -ld /data/adb/wx_stream /data/adb/wx_stream/config.json'"` shows `drwx------` and `-rw-------`.

### Step 2: Install Xposed Sniffer Module APK & Scope Configuration
```bash
# Push and install module APK
$ADB install -r wechat-pad-exporter-module-v1.0.2.apk

# If target user is a secondary user (e.g. 999), install into secondary user profile:
if [ "$TARGET_USER" -ne 0 ]; then
    $ADB shell "pm install-existing --user $TARGET_USER com.wx.sniffer"
fi

# Enable module in Vector / LSPosed CLI if CLI tool is available
$ADB shell "su -c '
if [ -x /data/adb/modules/zygisk_vector/cli ]; then
    /data/adb/modules/zygisk_vector/cli modules enable com.wx.sniffer >/dev/null 2>&1
    /data/adb/modules/zygisk_vector/cli scope add com.wx.sniffer \"com.tencent.mm/$TARGET_USER\" >/dev/null 2>&1
elif [ -x /data/adb/lspd/cli ]; then
    /data/adb/lspd/cli modules enable com.wx.sniffer >/dev/null 2>&1
    /data/adb/lspd/cli scope add com.wx.sniffer \"com.tencent.mm/$TARGET_USER\" >/dev/null 2>&1
fi
'"
```
- **Manual LSPosed Fallback**: If CLI is not present, instruct user or use UI automation to:
  1. Open LSPosed / Vector manager.
  2. Toggle on `com.wx.sniffer` (WeChat Pad Exporter).
  3. Ensure `com.tencent.mm` is checked in the scope list.

### Step 3: Push Daemon Binary & Launch with `-config`
```bash
# Push daemon binary to runtime directory
$ADB push wx_daemon-android-arm64 /data/local/tmp/wx_daemon
$ADB shell "chmod 755 /data/local/tmp/wx_daemon"

# Kill any stale instance cleanly
$ADB shell "su -c 'pkill -f /data/local/tmp/wx_daemon || true'"

# Start daemon in background using -config flag
$ADB shell "su -c 'nohup /data/local/tmp/wx_daemon -config /data/adb/wx_stream/config.json >> /data/adb/wx_stream/daemon.log 2>&1 &'"
```
- **Verification**: Check process table:
  ```bash
  $ADB shell "pidof wx_daemon"
  ```
  Expected output: a valid non-empty numeric PID.

### Step 4: Install Boot Autostart & Watchdog Supervisors
```bash
# Push deploy scripts to temporary staging
$ADB push deploy/wx_autostart.sh /data/local/tmp/wx_autostart.sh
$ADB push deploy/wx_watchdog.sh /data/local/tmp/wx_watchdog.sh
$ADB push deploy/service.sh /data/local/tmp/service.sh
$ADB push deploy/setup_phone.sh /data/local/tmp/setup_phone.sh

# Run phone optimization (Doze whitelist, phantom process limit, wifi sleep policy)
$ADB shell "su -c 'sh /data/local/tmp/setup_phone.sh'"

# Install into Magisk / KernelSU service.d
$ADB shell "su -c '
if [ -d /data/adb/service.d ]; then
    cp -f /data/local/tmp/wx_autostart.sh /data/adb/service.d/wx_autostart.sh
    cp -f /data/local/tmp/wx_watchdog.sh /data/adb/service.d/wx_watchdog.sh
    chmod 755 /data/adb/service.d/wx_autostart.sh /data/adb/service.d/wx_watchdog.sh
    # Clean up any unsafe backup or draft files in service.d
    rm -f /data/adb/service.d/*.bak /data/adb/service.d/*.old /data/adb/service.d/*~
fi
'"
```

### Step 5: Port Forwarding & Health Probe Verification
```bash
# Establish loopback ADB tunnel from workstation to device
adb forward tcp:8765 tcp:8765

# 1. Negative test: Request health WITHOUT token -> MUST succeed (health endpoint is public)
# Or request protected export endpoint WITHOUT token -> MUST return HTTP 401 Unauthorized
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8765/api/wechat/export
# Expected output: 401

# 2. Positive test: Call /health endpoint with token
HEALTH_RESP=$(curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8765/health)
echo "Health Response: $HEALTH_RESP"
```
- **Expected Output**:
  ```json
  {"status":"ok","uptime_seconds":5,"uds_connected":false,"active_uds_connections":0,"total_messages":0,"version":"1.0.0-pad"}
  ```

---

## 4. Understanding `uds_connected: false` (Do NOT False-Fail)

The agent **MUST NOT** consider `"uds_connected": false` a deployment failure.
- **Mechanism**: The Xposed probe (`com.wx.sniffer`) inside WeChat connects to the Unix Domain Socket (`@wechat_stream_ipc`) **lazily on demand**.
- Right after daemon boot or WeChat launch, the probe keeps the connection idle.
- As soon as WeChat receives or sends the next real message, the hook intercepts the SQLite event, establishes the socket handshake within milliseconds, and `uds_connected` flips to `true`.
- **Diagnostic Action**: Send a test message (e.g. "File Transfer" / 文件传输助手) on the primary phone or tablet to verify live ingestion.

---

## 5. Optional Step: Assistant Webhook Bridge Setup

To forward new chat messages in real time to an upstream AI Assistant (Dify, Feishu, or Custom webhook):

```bash
export WX_EXPORT_ENDPOINT="http://127.0.0.1:8765/api/wechat/export"
export WX_DEVICE_TOKEN="$TOKEN"
export ASSISTANT_WEBHOOK_URL="https://your-dify-or-agent-endpoint/webhook"
export ASSISTANT_WEBHOOK_TYPE="dify"  # Supported types: 'dify', 'feishu', 'custom'

# Run webhook listener in background
nohup python3 clients/assistant_webhook.py --poll-interval 3 >> assistant_bridge.log 2>&1 &
```

---

## 6. Verification Checklist & Agent Report Format

After completing all execution steps, the agent must verify:
- [x] `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8765/api/wechat/export` returns `401`.
- [x] `curl -s -H "Authorization: Bearer <TOKEN>" http://127.0.0.1:8765/health` returns `200` with `"status":"ok"`.
- [x] Exactly one `wx_daemon` process is running: `$ADB shell "pidof wx_daemon | wc -w"` equals `1`.
- [x] `/data/adb/service.d/` contains only `wx_autostart.sh` and `wx_watchdog.sh` (zero `.bak` or `.old` files).
- [x] `/data/adb/wx_stream` permission is `0700`, `config.json` permission is `0600`.

### Agent Report Format to Human User
```markdown
### 🚀 WeChat Pad Exporter Deployment Complete

- **Target Device**: <SERIAL> (<MODEL>)
- **Daemon Status**: Running (PID: <PID>)
- **HTTP Endpoint**: http://127.0.0.1:8765 (Local tunnel active via `adb forward tcp:8765 tcp:8765`)
- **Bearer Token**: `<GENERATED_TOKEN>` (Store this token securely in your password manager / env vars)
- **Watchdog & Autostart**: Active under `/data/adb/service.d/` (survives reboots)
- **Ingestion State**: Daemon is listening on `@wechat_stream_ipc`. (Note: `uds_connected: false` will automatically flip to `true` upon the next incoming/outgoing message)

**Next Steps**:
1. Run `python3 clients/fetch_export.py` to stream messages.
2. (Optional) Run `python3 clients/assistant_webhook.py` to bridge events into your personal AI assistant.
```

---

## 7. Idempotency & Rollback Reference

### Idempotency
- All commands in this runbook are safe to re-run.
- If `wx_daemon` is already running, re-running the script kills the stale PID and restarts cleanly with the current config.
- `deploy/wx_autostart.sh` and `deploy/wx_watchdog.sh` manage PID files and terminate duplicate worker instances automatically.

### Rollback / Clean Uninstall
To completely remove the exporter and restore original device state:
```bash
# 1. Stop daemon and watchdog
$ADB shell "su -c 'pkill -f wx_daemon; pkill -f wx_watchdog; rm -f /data/local/tmp/wx_*.pid'"

# 2. Remove boot service scripts
$ADB shell "su -c 'rm -f /data/adb/service.d/wx_autostart.sh /data/adb/service.d/wx_watchdog.sh /data/adb/service.d/99_wx_sniffer.sh'"

# 3. Uninstall Xposed probe module
$ADB uninstall com.wx.sniffer

# 4. Remove binaries and temporary runtime files
$ADB shell "su -c 'rm -f /data/local/tmp/wx_daemon /data/local/tmp/wx_autostart.sh /data/local/tmp/wx_watchdog.sh /data/local/tmp/service.sh /data/local/tmp/setup_phone.sh'"

# 5. Remove encrypted database and credentials (IRREVERSIBLE: purges exported history)
$ADB shell "su -c 'rm -rf /data/adb/wx_stream'"

# 6. Remove ADB port forwarding
adb forward --remove tcp:8765
```
