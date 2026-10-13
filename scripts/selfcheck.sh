#!/usr/bin/env bash
# scripts/selfcheck.sh - End-to-end integration self-check
# Environment-agnostic diagnostic script. Skips gracefully if target hosts/devices are unconfigured.
set -uo pipefail

DEV_ADB="${DEV_ADB:-}"
DEV_TOKEN="${DEV_TOKEN:-}"
HOMELAB_SSH="${HOMELAB_SSH:-}"
TAILNET_EP="${TAILNET_EP:-}"
QL_DATA="${QL_DATA:-}"

PASS=0
FAIL=0
SKIP=0

check() {
  local name="$1"; local rc="$2"; local out="$3"
  if [ "$rc" -eq 0 ]; then
    echo "[PASS] $name"
    echo "       $out"
    PASS=$((PASS+1))
  else
    echo "[FAIL] $name (rc=$rc)"
    [ -n "$out" ] && echo "       $out"
    FAIL=$((FAIL+1))
  fi
}

skip() {
  local name="$1"; local reason="$2"
  echo "[SKIP] $name: $reason"
  SKIP=$((SKIP+1))
}

echo "=== 1. Device Daemon Health (adb -> 127.0.0.1:8765/health) ==="
if [ -z "$DEV_ADB" ] || [ "$DEV_ADB" = "<LAN_IP>:5555" ]; then
  skip "f1 Device Health" "DEV_ADB not configured (set DEV_ADB=<host:port> or serial to enable)"
else
  AUTH_HEADER=""
  if [ -n "$DEV_TOKEN" ]; then
    AUTH_HEADER="-H \"Authorization: Bearer $DEV_TOKEN\""
  fi
  out1=$(timeout 12 adb -s "$DEV_ADB" shell "su -c 'curl -s $AUTH_HEADER http://127.0.0.1:8765/health'" 2>&1 || true)
  if echo "$out1" | grep -q '"status":"ok"'; then
    check "f1 Device Health" 0 "$out1"
  else
    check "f1 Device Health" 1 "$out1"
  fi
fi

echo "=== 2. Remote Homelab Tailnet Port (ssh -> tailnet EP /health) ==="
if [ -z "$HOMELAB_SSH" ] || [ "$HOMELAB_SSH" = "linux" ] && [ -z "${SSH_AUTH_SOCK:-}" ]; then
  # Check if HOMELAB_SSH host is reachable
  if [ -z "$HOMELAB_SSH" ] || [ -z "$TAILNET_EP" ] || [ "$TAILNET_EP" = "<TAILNET_IP>:8765" ]; then
    skip "f2 Homelab Tailnet Port" "HOMELAB_SSH or TAILNET_EP not configured"
  else
    out2=$(timeout 12 ssh -o BatchMode=yes -o ConnectTimeout=5 "$HOMELAB_SSH" "curl -sS http://$TAILNET_EP/health" 2>&1 || true)
    if echo "$out2" | grep -q '"status":"ok"'; then
      check "f2 Homelab Tailnet Port" 0 "$out2"
    else
      check "f2 Homelab Tailnet Port" 1 "$out2"
    fi
  fi
else
  skip "f2 Homelab Tailnet Port" "HOMELAB_SSH or TAILNET_EP unconfigured"
fi

echo "=== 3. Qinglong Consumer Logs ==="
if [ -z "$HOMELAB_SSH" ] || [ -z "$QL_DATA" ] || [ "$QL_DATA" = "/home/<USER>/Projects/qinglong/data" ]; then
  skip "f3 Qinglong Logs" "QL_DATA not configured"
else
  out3=$(timeout 12 ssh -o BatchMode=yes -o ConnectTimeout=5 "$HOMELAB_SSH" "ls -la $QL_DATA/log/wx_export_check/ | tail -5" 2>&1 || true)
  if echo "$out3" | grep -q '\.log'; then
    check "f3 Qinglong Logs" 0 "$(echo "$out3" | tr '\n' ' ' | head -c 300)"
  else
    check "f3 Qinglong Logs" 1 "$out3"
  fi
fi

echo "=== 4. Consumer State Cursor & Hash ==="
if [ -z "$HOMELAB_SSH" ] || [ -z "$QL_DATA" ] || [ "$QL_DATA" = "/home/<USER>/Projects/qinglong/data" ]; then
  skip "f4 State File" "QL_DATA not configured"
else
  out4=$(timeout 12 ssh -o BatchMode=yes -o ConnectTimeout=5 "$HOMELAB_SSH" "cat $QL_DATA/scripts/.wx_export_state.json" 2>&1 || true)
  if echo "$out4" | grep -q 'last_sha256'; then
    check "f4 State File" 0 "$(echo "$out4" | head -c 300)"
  else
    check "f4 State File" 1 "$out4"
  fi
fi

echo ""
echo "==================== Selfcheck Summary ===================="
echo "PASS=$PASS  FAIL=$FAIL  SKIP=$SKIP"
if [ "$FAIL" -eq 0 ]; then
  echo "ALL CHECKS PASSED OR GRACEFULLY SKIPPED ✅"
  exit 0
else
  echo "Checks failed ❌ (see details above)"
  exit "$FAIL"
fi
