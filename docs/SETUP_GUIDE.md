# Setup Guide (install / configure from scratch)

> This guide is an environment-neutral installation and deployment guide for `wx-pad-exporter`.
> The repository is completely self-contained and includes:
> - `android-module/`: LSPosed hook module source (`com.wx.sniffer`)
> - `daemon/`: Go-based export daemon source (`wx_daemon`)
> - `deploy/`: Device runtime scripts and Magisk/KernelSU `service.d` autostart scripts
> - `clients/`: Python client utilities (`fetch_export.py`, `assistant_webhook.py`, `fetch_phone_wechat.py`)
> - `scripts/`: Verification scripts (`scripts/selfcheck.sh`)
> - `docs/`: Architecture, contract specifications, and integration guides
>
> **Do not commit real values** — every `<...>` below is a placeholder you fill from your own environment. Secrets must come from your credential manager or environment variables; never commit plaintext secrets into the repository.

## 0. Prerequisites

- A **rooted Android device** (the reference build targets a Redmi 10X 5G, Android 12 / MIUI 13) with **Magisk (27+) or KernelSU** and **LSPosed** active.
- **Go 1.22+** (for building the phone daemon binary `wx_daemon`).
- **Android SDK & JDK 17+** with `gradlew` (for building the Xposed hook module inside `android-module/`).
- **Python 3.8+** on the client or consumer host (scripts use only standard library — no external `pip` dependencies).
- A **private network / Tailnet** (e.g. Tailscale) so the phone's daemon can be reached securely over private IP without public exposure.
- `adb` on your workstation, reachable to the phone via USB or TCP (`<PHONE_LAN_ADB>`).

### Placeholders used in this guide

| Placeholder | Meaning | Example (do NOT commit real values) |
| :--- | :--- | :--- |
| `<TAILNET_PHONE_IP>` | Phone's Tailnet IP:port | `<TAILNET_IP>:8765` |
| `<PHONE_LAN_ADB>` | Phone's TCP adb address | `<LAN_IP>:5555` |
| `<DEVICE_SERIAL>` | `adb` device serial | `<DEVICE_SERIAL>` |
| `<HOME_SSH>` | SSH alias/host for your client/server | `linux` |
| `<HOMELAB_IP>` | Client server endpoint IP:port | `<TAILNET_IP>:5700` |
| `<WECHAT_CLONE_UID>` | WeChat clone (work-profile) UID | `<CLONE_UID>` |
| `<QL_DATA_PATH>` | Consumer data root on the host | `/home/<USER>/Projects/qinglong/data` |

---

## 1. Build the device-side binaries

All components are built directly from this repository root.

### 1a. Export daemon (Go, cross-compiled to android/arm64)

```bash
cd daemon
GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
  go build -ldflags="-s -w" -o /tmp/wx_daemon .
```

### 1b. Xposed / LSPosed module (Kotlin, via gradle)

```bash
cd android-module
./gradlew assembleRelease
# Produces app-release.apk under android-module/app/build/outputs/apk/release/
```

Install the APK on the device:
```bash
adb install android-module/app/build/outputs/apk/release/app-release.apk
```
Activate `com.wx.sniffer` in LSPosed manager for the target WeChat package (`com.tencent.mm`), then reboot or force-restart WeChat.

---

## 2. Push runtime assets to the device

Use `deploy/deploy_to_device.sh` to push runtime scripts and register Magisk `service.d` self-start scripts:

```bash
# Set device targets:
export DEV_ADB="<PHONE_LAN_ADB>"        # e.g. <LAN_IP>:5555
export DEV_SERIAL="<DEVICE_SERIAL>"     # USB serial if applicable

# Push binaries and scripts:
adb push /tmp/wx_daemon /data/local/tmp/wx_daemon
adb shell chmod 755 /data/local/tmp/wx_daemon

bash deploy/deploy_to_device.sh deploy-scripts   # push scripts + register service.d
bash deploy/deploy_to_device.sh status           # verify md5 checksum consistency
```

- Runtime scripts land in `/data/local/tmp` (`service.sh`, `setup_phone.sh`).
- Self-start scripts (`wx_autostart.sh`, `wx_watchdog.sh`) land in `/data/adb/service.d`.
- Backups are stored safely in `/data/local/tmp/wx_deploy_bak` (never inside `service.d`, preventing accidental Magisk execution).

---

## 3. Configure and start daemon

Configure `daemon/config.example.json` on the device or provide CLI flags.

```bash
# Push configuration
adb shell mkdir -p /data/adb/wx_stream
adb push daemon/config.example.json /data/adb/wx_stream/config.json
# Ensure a strong token is set in /data/adb/wx_stream/config.json
```

Start the service via `service.sh`:
```bash
adb shell "su -c 'sh /data/local/tmp/service.sh start'"
```

Verify the local health probe:
```bash
adb shell "su -c 'curl -s http://127.0.0.1:8765/health'"
```

---

## 4. Run consumer clients

The `clients/` directory provides Python standard-library clients:

### 4a. Real-time assistant webhook bridge (`clients/assistant_webhook.py`)
Forward real-time message stream to Dify, Feishu, or custom webhooks:
```bash
python3 clients/assistant_webhook.py \
  --endpoint "http://127.0.0.1:8765/api/wechat/export" \
  --token "<YOUR_TOKEN>" \
  --webhook-url "https://open.feishu.cn/open-apis/bot/v2/hook/<UUID>" \
  --webhook-type feishu
```

### 4b. Stream fetch & verification (`clients/fetch_export.py` / `clients/fetch_phone_wechat.py`)
```bash
python3 clients/fetch_export.py
```

---

## 5. Verify the entire link

Run `scripts/selfcheck.sh` on your workstation or home server:

```bash
export DEV_ADB="<PHONE_LAN_ADB>"
export HOMELAB_SSH="<HOME_SSH>"
export QL_HOST="<HOMELAB_IP>"
export TAILNET_EP="<TAILNET_PHONE_IP>"
export QL_DATA="<QL_DATA_PATH>"
bash scripts/selfcheck.sh
```

Expected output:
```
=== 1. 设备侧 Daemon 健康 ===
[PASS] f1 设备健康
...
ALL GREEN ✅
```

---

## 6. Notes & non-negotiable paths

- Daemon binary on device: `/data/local/tmp/wx_daemon` (listens on `127.0.0.1:8765`).
- Daemon SQLite message cache: `/data/adb/wx_stream/vault.db` (permission `0600`).
- Runtime scripts directory: `/data/local/tmp`.
- Boot autostart directory: `/data/adb/service.d`.
- Hooked WeChat package: `com.tencent.mm` (main profile UID 0 or dual-app UID 999).
- For data contracts and payload schema, see `docs/DATA_CONTRACT.md` and `schema/export.schema.json`.
