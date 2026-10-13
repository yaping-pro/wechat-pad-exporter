#!/usr/bin/env bash
# scripts/selfcheck.sh - 链路自检四连（不写死 /tmp）
set -uo pipefail

# ===== 可覆盖的变量（放顶部，便于环境迁移）=====
DEV_ADB="${DEV_ADB:-<LAN_IP>:5555}"
HOMELAB_SSH="${HOMELAB_SSH:-linux}"
QL_HOST="${QL_HOST:-<TAILNET_IP>:5700}"
TAILNET_EP="${TAILNET_EP:-<TAILNET_IP>:8765}"
QL_DATA="${QL_DATA:-/home/<USER>/Projects/qinglong/data}"

PASS=0
FAIL=0

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

echo "=== 1. 设备侧 Daemon 健康 (adb -> 127.0.0.1:8765/health) ==="
out1=$(timeout 12 adb -s "$DEV_ADB" shell "su -c 'curl -s http://127.0.0.1:8765/health'" 2>&1)
if echo "$out1" | grep -q '"status":"ok"'; then check "f1 设备健康" 0 "$out1"; else check "f1 设备健康" 1 "$out1"; fi

echo "=== 2. 家服直连 Tailnet 端口 (ssh -> tailnet EP /health) ==="
out2=$(timeout 12 ssh -o BatchMode=yes -o ConnectTimeout=8 "$HOMELAB_SSH" "curl -sS http://$TAILNET_EP/health" 2>&1)
if echo "$out2" | grep -q '"status":"ok"'; then check "f2 家服直连" 0 "$out2"; else check "f2 家服直连" 1 "$out2"; fi

echo "=== 3. 青龙最近 5 次消费日志 ==="
out3=$(timeout 12 ssh -o BatchMode=yes -o ConnectTimeout=8 "$HOMELAB_SSH" "ls -la $QL_DATA/log/wx_export_check/ | tail -5" 2>&1)
if echo "$out3" | grep -q '\.log'; then check "f3 青龙日志" 0 "$(echo "$out3" | tr '\n' ' ' | head -c 300)"; else check "f3 青龙日志" 1 "$out3"; fi

echo "=== 4. 当前消费游标与哈希 ==="
out4=$(timeout 12 ssh -o BatchMode=yes -o ConnectTimeout=8 "$HOMELAB_SSH" "cat $QL_DATA/scripts/.wx_export_state.json" 2>&1)
if echo "$out4" | grep -q 'last_sha256'; then check "f4 状态文件" 0 "$(echo "$out4" | head -c 300)"; else check "f4 状态文件" 1 "$out4"; fi

echo ""
echo "==================== 自检总结 ===================="
echo "PASS=$PASS  FAIL=$FAIL"
[ "$FAIL" -eq 0 ] && echo "ALL GREEN ✅" || echo "有失败项 ❌（见上）"
exit $FAIL
