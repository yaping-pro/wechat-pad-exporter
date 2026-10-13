#!/usr/bin/env bash
# deploy/deploy_to_device.sh - Deploy daemon & scripts to Android target device
#
# Commands:
#   deploy-scripts  Push runtime scripts & service.d autostart scripts to device
#   status          Compare remote md5 against local files to detect drift
#
# Key design points:
#   1. Binder DAC trap: all am/pm/settings/svc/dumpsys calls must discard output
#      (>/dev/null 2>&1) to avoid silent failure (Failed transaction 2147483646).
#   2. Backups must NEVER be placed in /data/adb/service.d/ (Magisk executes all *.sh).
#   3. Supports TCP adb or USB adb serial.

set -euo pipefail

# ---- Configurable Environment Variables ----
DEV_ADB="${DEV_ADB:-<LAN_IP>:5555}"                # TCP adb or localhost:5555
DEV_SERIAL="${DEV_SERIAL:-<DEVICE_SERIAL>}"        # USB adb serial if available
TARGET_USER="${TARGET_USER:-0}"                    # Android user ID (0 for tablet, 999 for dual-app)
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="$REPO_ROOT/deploy"

# Target device paths
RT_DIR="/data/local/tmp"          # Runtime scripts
SVC_DIR="/data/adb/service.d"     # Magisk/KernelSU boot scripts
BAK_DIR="/data/local/tmp/wx_deploy_bak"

SVC_SCRIPTS=(wx_autostart.sh wx_watchdog.sh)
RT_SCRIPTS=(service.sh setup_phone.sh)

log(){ printf '[deploy_to_device] %s\n' "$*"; }
die(){ printf '[deploy_to_device][ERR] %s\n' "$*" >&2; exit 1; }

resolve_adb_target() {
  local cands=("$DEV_ADB" "$DEV_SERIAL")
  local c in_devices
  for c in "${cands[@]}"; do
    [ -n "$c" ] || continue
    if [[ "$c" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+$ ]]; then
      in_devices="$(adb devices 2>/dev/null | awk '{print $1}' | grep -xF "$c")"
      [ -z "$in_devices" ] && adb connect "$c" >/dev/null 2>&1 || true
    fi
    if adb -s "$c" shell "echo ok" >/dev/null 2>&1; then
      printf '%s' "$c"; return 0
    fi
  done
  return 1
}

ADB_TARGET="$(resolve_adb_target)" \
  || die "Device unreachable: candidates DEV_ADB=$DEV_ADB / DEV_SERIAL=$DEV_SERIAL failed to connect. Check adb and retry."
ADB=(adb -s "$ADB_TARGET")
adb_run() { "${ADB[@]}" "$@"; }

local_md5() {
  if command -v md5sum >/dev/null 2>&1; then
    md5sum "$1" | awk '{print $1}'
  else
    md5 -q "$1"
  fi
}

remote_md5() {
  adb_run shell "su -c 'md5sum \"$1\" 2>/dev/null'" 2>/dev/null \
    | awk '{print $1}'
}

device_online() {
  adb_run shell "echo ok" >/dev/null 2>&1
}

cmd_deploy_scripts() {
  device_online || die "Device offline: ADB_TARGET=$ADB_TARGET"

  log "Backing up existing scripts to $BAK_DIR (safe location outside service.d)..."
  adb_run shell "su -c 'mkdir -p \"$BAK_DIR\"'" >/dev/null 2>&1
  for s in "${RT_SCRIPTS[@]}" "${SVC_SCRIPTS[@]}"; do
    adb_run shell "su -c 'cp -f \"$RT_DIR/$s\" \"$BAK_DIR/$s.bak\" 2>/dev/null; cp -f \"$SVC_DIR/$s\" \"$BAK_DIR/$s.svc.bak\" 2>/dev/null'" >/dev/null 2>&1 || true
  done

  log "Pushing runtime scripts -> $RT_DIR..."
  for s in "${RT_SCRIPTS[@]}"; do
    src="$DEPLOY_DIR/$s"
    [ -f "$src" ] || die "Local file missing: $src"
    adb_run push "$src" "$RT_DIR/$s" >/dev/null 2>&1 || die "Push failed: $s"
    adb_run shell "su -c 'chmod 755 \"$RT_DIR/$s\"'" >/dev/null 2>&1
    log "  OK  $s -> $RT_DIR/$s"
  done

  log "Pushing autostart scripts -> $SVC_DIR (Magisk/KernelSU service.d)..."
  adb_run shell "su -c 'mkdir -p \"$SVC_DIR\"'" >/dev/null 2>&1
  for s in "${SVC_SCRIPTS[@]}"; do
    src="$DEPLOY_DIR/$s"
    [ -f "$src" ] || die "Local file missing: $src"
    adb_run push "$src" "$RT_DIR/$s" >/dev/null 2>&1 || die "Push to $RT_DIR failed: $s"
    adb_run shell "su -c 'cp -f \"$RT_DIR/$s\" \"$SVC_DIR/$s\" && chmod 755 \"$SVC_DIR/$s\"'" >/dev/null 2>&1 || die "Failed to install to $SVC_DIR: $s"
    adb_run shell "su -c 'ls -l \"$SVC_DIR/$s\"'" 2>/dev/null | awk '{print "  OK  "$0}'
  done

  log "Verifying deployments:"
  for s in "${RT_SCRIPTS[@]}"; do
    _chk "$RT_DIR/$s" "$DEPLOY_DIR/$s"
  done
  for s in "${SVC_SCRIPTS[@]}"; do
    _chk "$SVC_DIR/$s" "$DEPLOY_DIR/$s"
  done

  log "Deployment complete."
}

_chk() {
  local remote="$1" local="$2"
  local lr rr
  lr="$(local_md5 "$local")"
  rr="$(remote_md5 "$remote")"
  if [ "$lr" = "$rr" ]; then
    log "  [MATCH] $remote"
  else
    log "  [DRIFT!] $remote  local=$lr remote=$rr"
  fi
}

cmd_status() {
  if ! device_online; then
    log "Device unreachable ($ADB_TARGET). Local checksums:"
    for s in "${RT_SCRIPTS[@]}" "${SVC_SCRIPTS[@]}"; do
      printf '  %-18s local=%s\n' "$s" "$(local_md5 "$DEPLOY_DIR/$s")"
    done
    return 1
  fi
  log "=== Device scripts vs Local repository ==="
  log "Target: $ADB_TARGET"
  printf '%-18s %-34s %-34s %s\n' "SCRIPT" "LOCAL" "REMOTE" "STATE"
  for s in "${RT_SCRIPTS[@]}"; do
    _status_row "$s" "$RT_DIR/$s"
  done
  for s in "${SVC_SCRIPTS[@]}"; do
    _status_row "$s" "$SVC_DIR/$s"
  done
}

_status_row() {
  local s="$1" remote="$2"
  local lr rr st
  lr="$(local_md5 "$DEPLOY_DIR/$s")"
  rr="$(remote_md5 "$remote")"
  if [ -z "$rr" ]; then st="MISSING"; rr="(none)";
  elif [ "$lr" = "$rr" ]; then st="OK";
  else st="DRIFT"; fi
  printf '%-18s %-34s %-34s %s\n' "$s" "$lr" "$rr" "$st"
}

usage() {
  cat <<'USAGE'
deploy/deploy_to_device.sh <deploy-scripts|status>

  deploy-scripts  Push deploy/ scripts to device and make executable
  status          Compare device checksums against local files

Environment overrides:
  DEV_ADB        TCP adb address (default: <LAN_IP>:5555)
  DEV_SERIAL     USB adb serial  (default: <DEVICE_SERIAL>)
  TARGET_USER    Android user ID (default: 0)
USAGE
}

case "${1:-}" in
  deploy-scripts) cmd_deploy_scripts ;;
  status)         cmd_status ;;
  *) usage; exit 1 ;;
esac
