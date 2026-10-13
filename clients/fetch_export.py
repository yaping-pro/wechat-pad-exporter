#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
fetch_export.py - WeChat Pad Stream Exporter Consumer Client.
Pulls real-time message stream from phone daemon endpoint, validates schemas,
tracks incremental watermarks, and optionally notifies external monitoring systems.

Zero third-party dependencies (Python 3 stdlib only).
"""

import datetime
import ipaddress
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_ENDPOINT = (
    os.environ.get("WX_EXPORT_ENDPOINT")
    or os.environ.get("WX_EXPORT_URL")
    or os.environ.get("ENDPOINT")
    or "http://127.0.0.1:8765/api/wechat/export"
)
FALLBACK_ENDPOINT = os.environ.get("WX_FALLBACK_URL", "")


def _load_token_from_paths(paths):
    for p in paths:
        try:
            with open(os.path.expanduser(p), "r", encoding="utf-8") as f:
                content = f.read().strip()
                if content.startswith("{"):
                    tok = json.loads(content).get("token")
                    if tok:
                        return str(tok).strip()
                elif content:
                    return content
        except Exception:
            continue
    return ""


def _load_device_token():
    env_token = (
        os.environ.get("WX_DEVICE_TOKEN", "").strip()
        or os.environ.get("WX_EXPORT_TOKEN", "").strip()
        or os.environ.get("WXPAD_TOKEN", "").strip()
    )
    if env_token:
        return env_token
    token_file = os.environ.get("WX_TOKEN_FILE", "")
    candidate_paths = [token_file] if token_file else []
    candidate_paths.extend([
        "~/.config/wechat-exporter/token",
        "/etc/wechat-exporter/token",
        "/wechat-vault/config/device_token",
    ])
    return _load_token_from_paths(candidate_paths)


def _load_fallback_token():
    env_token = os.environ.get("WX_FALLBACK_TOKEN", "").strip()
    if env_token:
        return env_token
    return _load_token_from_paths([
        "~/.config/wechat-exporter/fallback_token",
        "/wechat-vault/config/export_service.json",
    ])


DEVICE_TOKEN = _load_device_token()
FALLBACK_TOKEN = _load_fallback_token()
FRESHNESS_MIN = int(os.environ.get("WX_FRESHNESS_MINUTES", "180"))
MAX_IDLE_MIN = int(os.environ.get("WX_MAX_IDLE_MINUTES", "1440"))
STATE_FILE = os.environ.get("WX_STATE_FILE", os.path.expanduser("~/.config/wechat-exporter/state.json"))

FETCH_TIMEOUT = int(os.environ.get("WX_FETCH_TIMEOUT", "15"))
FETCH_RETRIES = int(os.environ.get("WX_FETCH_RETRIES", "3"))
FETCH_RETRY_DELAYS = (2, 5, 10)  # seconds
FETCH_TOTAL_BUDGET = int(os.environ.get("WX_FETCH_TOTAL_BUDGET", "60"))

TAILNET_NET = ipaddress.ip_network("100.64.0.0/10")


def _host_of(url):
    u = url.split("://", 1)[-1]
    host = u.split("/", 1)[0]
    return host.split(":")[0]


def _bypass_proxy(url):
    host = _host_of(url)
    try:
        ip = ipaddress.ip_address(host)
        return ip in TAILNET_NET or ip.is_loopback
    except ValueError:
        return host in ("localhost", "127.0.0.1")


def _get(url, token=None, timeout=FETCH_TIMEOUT):
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}) if _bypass_proxy(url) else urllib.request.ProxyHandler()
    )
    req = urllib.request.Request(url, headers={"User-Agent": "wechat-pad-exporter-consumer/1.0"})
    if token:
        req.add_header("Authorization", "Bearer " + token)
    return opener.open(req, timeout=timeout)


def _is_transient(code, err):
    if code is not None:
        if 500 <= code < 600 or code in (408, 429):
            return True
        if 400 <= code < 500:
            return False
    return True


def _fetch_once(url, token=None, timeout=FETCH_TIMEOUT):
    try:
        resp = _get(url, token, timeout=timeout)
        code = resp.getcode()
        body = resp.read().decode("utf-8", "replace")
        return code, body, None
    except urllib.error.HTTPError as e:
        return e.code, None, f"HTTPError {e.code}"
    except Exception as e:
        return None, None, f"{type(e).__name__}: {e}"


def fetch(url, token=None, retries=FETCH_RETRIES, delays=FETCH_RETRY_DELAYS, budget=FETCH_TOTAL_BUDGET):
    start_time = time.monotonic()
    last_code = None
    last_body = None
    last_err = None

    for attempt in range(1, retries + 1):
        elapsed = time.monotonic() - start_time
        remaining = budget - elapsed
        if remaining <= 0:
            print(f"[fetch] budget exhausted ({elapsed:.1f}s >= {budget}s) before attempt {attempt}/{retries}")
            break

        timeout = min(float(FETCH_TIMEOUT), max(1.0, float(remaining)))
        code, body, err = _fetch_once(url, token, timeout=timeout)
        last_code, last_body, last_err = code, body, err

        if code == 200 and not err:
            if attempt > 1:
                print(f"fetch attempt {attempt}/{retries} ok")
            return code, body, None

        transient = _is_transient(code, err)
        if attempt < retries and transient:
            delay = delays[attempt - 1] if (attempt - 1) < len(delays) else delays[-1]
            elapsed = time.monotonic() - start_time
            if (elapsed + delay) > budget:
                print(f"fetch attempt {attempt}/{retries} failed: {err or f'code={code}'}; budget limit ({elapsed + delay:.1f}s/{budget}s)")
                break
            print(f"fetch attempt {attempt}/{retries} failed: {err or f'code={code}'}; retrying in {delay}s")
            time.sleep(delay)
        else:
            if not transient:
                print(f"fetch attempt {attempt}/{retries} failed with non-transient error: {err or f'code={code}'}")
            else:
                print(f"fetch attempt {attempt}/{retries} failed: {err or f'code={code}'}")
            break

    return last_code, last_body, last_err


def parse_iso(s):
    try:
        return datetime.datetime.fromisoformat(s)
    except Exception:
        return None


def validate(data):
    errs = []
    for k in ("exported_at", "range", "watermark", "chats", "manifest"):
        if k not in data:
            errs.append(f"missing top key: {k}")
    chats = data.get("chats")
    if not isinstance(chats, list):
        errs.append("chats is not a list")
    manifest = data.get("manifest")
    if not isinstance(manifest, dict):
        errs.append("manifest is not a dict")
    else:
        mc = manifest.get("message_count")
        if not isinstance(mc, int) or mc < 0:
            errs.append("manifest.message_count invalid")
        sha = manifest.get("sha256")
        if not (isinstance(sha, str) and len(sha) == 64 and all(c in "0123456789abcdef" for c in sha.lower())):
            errs.append("manifest.sha256 not 64-hex")
    return errs


def load_state():
    try:
        with open(STATE_FILE, "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception:
        return {}


def save_state(d):
    try:
        os.makedirs(os.path.dirname(STATE_FILE), exist_ok=True)
        tmp = STATE_FILE + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(d, f, indent=2)
        os.replace(tmp, STATE_FILE)
    except Exception as e:
        print(f"[warn] cannot write state: {e}")


def notify_fail(title, content):
    try:
        import notify
        notify.push_config["HITOKOTO"] = False
        notify.send(title, content)
        print("[notify] alert dispatched via notify.py")
    except Exception as e:
        print(f"[notify] notification handler not configured ({e}); logged to stdout")


def main():
    now = datetime.datetime.now(datetime.timezone.utc)
    used_token = DEVICE_TOKEN if DEVICE_TOKEN else None
    code, body, err = fetch(DEFAULT_ENDPOINT, used_token)
    used_url = DEFAULT_ENDPOINT

    if (code != 200 or err) and FALLBACK_ENDPOINT and FALLBACK_TOKEN:
        print(f"[info] primary endpoint failed ({code}/{err}), attempting fallback {FALLBACK_ENDPOINT}")
        used_token = FALLBACK_TOKEN
        code, body, err = fetch(FALLBACK_ENDPOINT, used_token)
        used_url = FALLBACK_ENDPOINT

    if err or code != 200:
        msg = f"fetch failed: code={code} err={err}"
        print("RESULT: FAIL")
        print(msg)
        notify_fail("WeChat Export Fetch Failed", f"{used_url}\n{msg}")
        return 2

    try:
        data = json.loads(body)
    except Exception as e:
        print("RESULT: FAIL (bad json)")
        notify_fail("WeChat Export Fetch Failed", f"{used_url}\nJSON parse error: {e}")
        return 2

    errs = validate(data)
    if errs:
        print("RESULT: FAIL (schema validation error)")
        for e in errs:
            print("  - " + e)
        notify_fail("WeChat Export Validation Error", f"{used_url}\n" + "\n".join(errs))
        return 2

    watermark = parse_iso(data.get("watermark", ""))
    chats = data.get("chats", [])
    manifest = data.get("manifest", {})
    msg_count = manifest.get("message_count", -1)
    sha = manifest.get("sha256", "")

    # Probe /health for daemon uptime, UDS connection state, and total messages
    health_url = urllib.parse.urljoin(used_url, "/health")
    h_code, h_body, h_err = fetch(health_url, used_token, retries=1, budget=10)
    h_data = {}
    if h_code == 200 and h_body:
        try:
            h_data = json.loads(h_body)
        except Exception as e:
            print(f"[warn] cannot parse health json: {e}")
    elif h_err or h_code != 200:
        print(f"[warn] health probe failed (code={h_code}, err={h_err})")

    h_status = h_data.get("status")
    h_uptime = h_data.get("uptime_seconds", 0)
    h_uds = h_data.get("uds_connected", False)
    h_total = h_data.get("total_messages")

    state = load_state()
    last_wm = state.get("last_watermark")
    last_total = state.get("last_total_messages")
    last_uptime = state.get("last_uptime")
    reset_count = state.get("uptime_reset_count", 0)

    if last_uptime is not None and h_uptime < last_uptime:
        reset_count += 1
    elif h_uptime > 1800:
        reset_count = 0

    stale = False
    stale_reason = ""
    age_min = None
    if watermark is None:
        stale = True
        stale_reason = "watermark is missing or invalid"
    else:
        age = (now - watermark).total_seconds() / 60.0
        age_min = round(age, 1)
        if age > MAX_IDLE_MIN:
            stale = True
            stale_reason = f"watermark age ({age_min:.1f} min) exceeds max idle limit ({MAX_IDLE_MIN} min)"
        elif age > FRESHNESS_MIN:
            # Over freshness threshold: verify if this is a legitimate quiet period vs upstream breakage
            if h_code != 200 or not h_data or h_status != "ok":
                stale = True
                stale_reason = f"stale watermark ({age_min:.1f} min > {FRESHNESS_MIN} min) and health probe failed (code={h_code}, err={h_err})"
            elif reset_count >= 3:
                stale = True
                stale_reason = f"daemon in crash loop (uptime reset {reset_count} consecutive times)"
            elif last_total is not None and h_total is not None and h_total > last_total:
                stale = True
                stale_reason = f"vault received new messages ({last_total} -> {h_total}) but export watermark did not advance"
            else:
                chat_msg_timestamps = [
                    parse_iso(m["ts"])
                    for c in chats
                    for m in c.get("messages", [])
                    if "ts" in m and parse_iso(m["ts"]) is not None
                ]
                max_chat_ts = max(chat_msg_timestamps) if chat_msg_timestamps else None
                if max_chat_ts is not None and max_chat_ts > watermark:
                    stale = True
                    stale_reason = f"export payload contains messages newer than watermark ({max_chat_ts.isoformat()} > {watermark.isoformat()})"
                elif h_uptime <= 0:
                    stale = True
                    stale_reason = f"daemon uptime is invalid ({h_uptime})"

    if stale:
        verdict = "STALE"
    elif watermark is not None and last_wm is not None and watermark.isoformat() == last_wm:
        if age_min is not None and age_min > FRESHNESS_MIN:
            verdict = "ok(quiet)"
        else:
            verdict = "ok(no new)"
    elif watermark is not None and last_wm is not None:
        verdict = "advanced"
    else:
        verdict = "first-run"

    print("=== wechat_pad_export_consumer ===")
    print(f"endpoint : {used_url}")
    if h_data:
        print(f"health   : status={h_status}, uptime={h_uptime}s, uds={'connected' if h_uds else 'disconnected'}, total_msgs={h_total}")
    print(f"chats    : {len(chats)}")
    print(f"messages : {msg_count}")
    print(f"sha256   : {sha}")
    print(f"watermark: {data.get('watermark')} (age_min={age_min}, freshness_limit={FRESHNESS_MIN})")
    print(f"last_wm  : {last_wm}")
    print(f"RESULT   : {verdict}")

    if stale:
        msg = f"{used_url}\nreason: {stale_reason or 'stale'}\nwatermark age={age_min}min > {FRESHNESS_MIN}min\nmessages={msg_count}"
        print(f"REASON   : {stale_reason or 'stale'}")
        notify_fail("WeChat Export Data Stale", msg)
        return 1

    save_state({
        "last_watermark": watermark.isoformat() if watermark else None,
        "last_sha256": sha,
        "last_success": now.isoformat(),
        "last_endpoint": used_url,
        "last_messages": msg_count,
        "last_total_messages": h_total,
        "last_uptime": h_uptime,
        "last_uds_connected": h_uds,
        "uptime_reset_count": reset_count,
    })
    return 0


if __name__ == "__main__":
    sys.exit(main())
