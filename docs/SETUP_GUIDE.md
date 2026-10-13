# Setup Guide (install / configure from scratch)

> This guide is a **templated install guide** distilled from `README.md`, `HANDOFF.md`, `device/README.md`, and the `ops/*.sh` / `scripts/*.sh` scripts. It replaces the personal ops runbook with environment-neutral placeholders so a new user can stand the pipeline up. **Do not commit real values** — every `<...>` below is a placeholder you fill from your own environment. Secrets come from your own credential store (e.g. 1Password `op` injection); never paste them into the repo.

## 0. Prerequisites

- A **rooted Android device** (the reference build targets a Redmi 10X 5G, Android 12 / MIUI 13) with **Magisk (27+) + Zygisk** and **LSPosed** active.
- The **device-source repository `<A_REPO>`** (`wx-pad-sniffer`) — this is where the export daemon and Xposed module are built. This pipeline repo (`wx-export-pipeline`) does **not** contain that source; clone it separately.
- **Go** (for cross-compiling the daemon) and the Android SDK/NDK or `gradlew` (for building the Xposed module inside `<A_REPO>/android-module`).
- **Python 3.8+** on the consumer/home server (the consumer script uses only the standard library — no `pip install` needed).
- A **private network / Tailnet** (e.g. Tailscale) so the phone's local daemon can be reached from the home server without exposing it publicly.
- A **home server** (the docs reference a Qinglong / 青龙 panel in Docker, but any host that can run a cron + Python script works). An SSH alias to it is convenient.
- `adb` on your workstation, reachable to the phone over TCP (USB-free) using `<PHONE_LAN_ADB>`.

### Placeholders used in this guide

| Placeholder | Meaning | Example (do NOT commit the real value) |
| :--- | :--- | :--- |
| `<A_REPO>` | Path/URL to the device-source repo | `wx-pad-sniffer` |
| `<TAILNET_PHONE_IP>` | Phone's Tailnet node IP:port | `<TAILNET_IP>:8765` |
| `<PHONE_LAN_ADB>` | Phone's TCP adb address | `<LAN_IP>:5555` |
| `<DEVICE_SERIAL>` | `adb` device serial | `<DEVICE_SERIAL>` |
| `<HOME_SSH>` | SSH alias/host for the home server | `linux` |
| `<HOMELAB_IP>` | Home server Tailnet/IP:port | `<TAILNET_IP>:5700` |
| `<WECHAT_CLONE_UID>` | WeChat clone (work-profile) UID | `<CLONE_UID>` |
| `<QL_DATA_PATH>` | Qinglong data root on the home server | `/home/<USER>/Projects/qinglong/data` |
| `<TAILNET_USER>` | Your Tailnet login handle | `<TAILNET_USER>@` |

## 1. Build the device-side binaries (in `<A_REPO>`)

The daemon and Xposed module are built in the device-source repo, then pushed from here.

### 1a. Export daemon (Go, cross-compiled to android/arm64)

```bash
cd <A_REPO>
GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
  go build -ldflags="-s -w" -o /data/local/tmp/wx_daemon ./daemon
```

### 1b. Xposed / LSPosed modules (Kotlin, via gradle)

```bash
cd <A_REPO>/android-module
./gradlew assembleRelease
# Produces wx-sniffer.apk and tablethook.apk under app/build/outputs/apk/release/
```

Install the APKs on the device and activate both modules (`com.wx.sniffer`, `com.roro.tablethook`) in the LSPosed manager, then reboot.

## 2. Push runtime assets to the device

From **this** repo, use `ops/push_device.sh`. It pushes the deploy scripts and registers Magisk `service.d` self-start entries.

```bash
# Override defaults with your environment:
export DEV_ADB="<PHONE_LAN_ADB>"        # e.g. <LAN_IP>:5555
export DEV_SERIAL="<DEVICE_SERIAL>"     # e.g. <DEVICE_SERIAL>
export PKG_UID="<WECHAT_CLONE_UID>"     # e.g. <CLONE_UID>
export SNIFFER_PKG="com.wx.sniffer"      # your sniffer module package name

bash ops/push_device.sh deploy-scripts   # push + register service.d
bash ops/push_device.sh status           # verify md5 drift between repo and device
```

- Runtime scripts land in `/data/local/tmp`.
- Self-start scripts (`wx_autostart.sh`, `wx_watchdog.sh`, `sfa_watchdog.sh`) land in `/data/adb/service.d`.
- Backups go to `/data/local/tmp/wx_deploy_bak` (never under `service.d` — Magisk would execute them).

## 3. Deploy the tunnel config (optional, for remote reachability)

If you use sing-box / SFA to expose `127.0.0.1:8765` over your Tailnet:

```bash
export DEV_ADB="<PHONE_LAN_ADB>"
export DEV_SERIAL="<DEVICE_SERIAL>"
export EP_MAC="<TAILNET_PHONE_IP>"      # e.g. <TAILNET_IP>:8765
export EP_HOME="<TAILNET_PHONE_IP>"
export HOME_SSH="<HOME_SSH>"            # e.g. linux

# Fill real auth values into singbox/config.json via your secret manager (placeholders ONLY),
# then deploy (the script refuses to push if *_PLACEHOLDER remains):
bash ops/deploy_singbox.sh deploy
# rollback to last backup if something breaks:
bash ops/deploy_singbox.sh rollback
```

The config is written to `/data/adb/sing-box/config.json` on the device.

## 4. Configure the consumer / home server

The consumer script (`consumer/wx_export_check.py`) is pure Python stdlib. Copy it to your home server's data dir (e.g. `<QL_DATA_PATH>/scripts/`) and schedule it with cron or your Qinglong panel.

### 4a. Environment variables the consumer honors

| Variable | Default (template) | Purpose |
| :--- | :--- | :--- |
| `WX_EXPORT_ENDPOINT` (or `WX_EXPORT_URL`, `ENDPOINT`) | `http://<TAILNET_PHONE_IP>/api/wechat/export` | Primary phone export URL |
| `WX_HOME_URL` | `http://<HOMELAB_IP>/api/wechat/export` | Fallback home-server-proxied URL |
| `WX_HOME_TOKEN` | `""` | Bearer token for the export endpoint (inject from secret store) |
| `WX_FRESHNESS_MINUTES` | `180` | Max age of data before alerting |
| `WX_STATE_FILE` | `/ql/data/scripts/.wx_export_state.json` | Cursor/checksum state file |
| `WX_FETCH_TIMEOUT` | `15` | Per-request timeout (s) |
| `WX_FETCH_RETRIES` | `3` | Retry count on transient failure |
| `WX_FETCH_TOTAL_BUDGET` | `60` | Total retry budget (s) |

### 4b. Self-check environment variables

`scripts/selfcheck.sh` honors: `DEV_ADB`, `HOMELAB_SSH`, `QL_HOST`, `TAILNET_EP`, `QL_DATA`.

Example for the self-check:

```bash
export DEV_ADB="<PHONE_LAN_ADB>"
export HOMELAB_SSH="<HOME_SSH>"
export QL_HOST="<HOMELAB_IP>"
export TAILNET_EP="<TAILNET_PHONE_IP>"
export QL_DATA="<QL_DATA_PATH>"
bash scripts/selfcheck.sh
```

> Qinglong OpenAPI credentials (`QL_ID`, `QL_TOKEN`, `QL_SEC`) are used only if you integrate with the Qinglong panel's API (see `docs/QINGLONG_API.md`). They must come from your secret store, never from the repo.

## 5. Verify the whole link

```bash
# On the workstation / home server:
bash scripts/selfcheck.sh
```

Expected: four `[PASS]` lines (`f1 设备健康`, `f2 家服直连`, `f3 青龙日志`, `f4 状态文件`) and `ALL GREEN ✅`.

## 6. Notes / non-negotiable device paths

- Daemon binary path on device: `/data/local/tmp/wx_daemon` (listens on `127.0.0.1:8765`).
- Deploy scripts root: `/data/local/tmp`; Magisk self-start: `/data/adb/service.d`; sing-box config: `/data/adb/sing-box/config.json`.
- The WeChat instance hooked is `com.tencent.mm` in the clone profile (UID `<WECHAT_CLONE_UID>`). The module package names are `com.wx.sniffer` and `com.roro.tablethook` — substitute your own if you renamed them in `<A_REPO>`.
- For the full architecture, credential-injection rules, and known traps, read `HANDOFF.md` (§11 defines the self-contained boundary between this repo and `<A_REPO>`).
