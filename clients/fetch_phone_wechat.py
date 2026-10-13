#!/usr/bin/env python3
"""
wx-pad-sniffer Consumer Client
Fetch WeChat chat exports from phone endpoint (or Homelab export API) over Tailscale.

Compatible with both:
1. Phone wx_daemon endpoint: http://<TAILNET_IP>:8765/api/wechat/export
2. Homelab Linux1 export API: http://<TAILNET_IP>:8765/api/wechat/export
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

DEFAULT_BASE_URL = os.environ.get("WXPAD_BASE_URL", "http://<TAILNET_IP>:8765")
DEFAULT_TOKEN = os.environ.get("WXPAD_TOKEN", "")
TAILSCALE_CGNAT_NET = ipaddress.ip_network("100.64.0.0/10")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fetch WeChat export payload from phone daemon or homelab API."
    )
    parser.add_argument(
        "--base-url",
        default=DEFAULT_BASE_URL,
        help=f"Base URL of export service (default: {DEFAULT_BASE_URL}, or env WXPAD_BASE_URL)",
    )
    parser.add_argument(
        "--token",
        default=DEFAULT_TOKEN,
        help="Bearer token if authentication is enabled (or env WXPAD_TOKEN)",
    )
    parser.add_argument(
        "--since",
        default=None,
        help="Start timestamp: Unix epoch (seconds) or RFC3339 string (e.g. 2026-10-01T00:00:00+08:00)",
    )
    parser.add_argument(
        "--to",
        default=None,
        help="End timestamp: Unix epoch (seconds) or RFC3339 string",
    )
    parser.add_argument(
        "--watermark-file",
        default=None,
        help="File storing the last export watermark. Automatically sets --since from file and updates it on success.",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=30.0,
        help="HTTP request timeout in seconds (default: 30.0)",
    )
    parser.add_argument(
        "--no-proxy",
        action="store_true",
        help="Explicitly bypass system/env HTTP proxies (auto-enabled for Tailscale & private IPs)",
    )
    parser.add_argument(
        "--health",
        action="store_true",
        help="Probe /health endpoint instead of /api/wechat/export",
    )
    parser.add_argument(
        "--summary",
        action="store_true",
        help="Output human-readable summary instead of full JSON dump",
    )
    parser.add_argument(
        "--raw",
        action="store_true",
        help="Output raw compact JSON to stdout without pretty-printing",
    )
    parser.add_argument(
        "--out",
        default=None,
        help="Save output JSON payload to specified file path",
    )
    return parser.parse_args()


def should_bypass_proxy(url: str, force_no_proxy: bool = False) -> bool:
    if force_no_proxy:
        return True
    hostname = urllib.parse.urlparse(url).hostname
    if not hostname:
        return False
    try:
        ip = ipaddress.ip_address(hostname)
        return ip.is_private or ip.is_loopback or (ip in TAILSCALE_CGNAT_NET)
    except ValueError:
        # Hostname (not raw IP)
        return hostname in ("localhost", "127.0.0.1")


def fetch_endpoint(
    url: str,
    token: str | None = None,
    timeout: float = 30.0,
    force_no_proxy: bool = False,
) -> tuple[int, dict[str, Any]]:
    req = urllib.request.Request(url)
    req.add_header("Accept", "application/json")
    req.add_header("User-Agent", "wx-pad-consumer/1.0")

    if token:
        req.add_header("Authorization", f"Bearer {token}")

    if should_bypass_proxy(url, force_no_proxy):
        # Prevent Python urllib from mistakenly forwarding Tailscale/internal IPs to HTTP proxy
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    else:
        opener = urllib.request.build_opener()

    try:
        with opener.open(req, timeout=timeout) as resp:
            status_code = resp.status
            data = json.loads(resp.read().decode("utf-8"))
            return status_code, data
    except urllib.error.HTTPError as e:
        err_body = ""
        try:
            err_body = e.read().decode("utf-8")
            err_json = json.loads(err_body)
            err_msg = err_json.get("error", err_body)
        except Exception:
            err_msg = err_body or str(e)
        sys.stderr.write(f"[ERROR] HTTP {e.code} {e.reason}: {err_msg}\n")
        sys.exit(1)
    except urllib.error.URLError as e:
        sys.stderr.write(f"[ERROR] Network/Connection Error: {e.reason}\n")
        sys.exit(2)
    except json.JSONDecodeError as e:
        sys.stderr.write(f"[ERROR] Invalid JSON response: {e}\n")
        sys.exit(3)
    except Exception as e:
        sys.stderr.write(f"[ERROR] Unexpected failure: {e}\n")
        sys.exit(4)


def read_watermark(path: str) -> str | None:
    p = Path(path).expanduser()
    if p.exists() and p.is_file():
        try:
            val = p.read_text(encoding="utf-8").strip()
            return val if val else None
        except Exception as e:
            sys.stderr.write(f"[WARN] Failed to read watermark from {path}: {e}\n")
    return None


def write_watermark(path: str, watermark: str) -> None:
    p = Path(path).expanduser()
    try:
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(watermark + "\n", encoding="utf-8")
    except Exception as e:
        sys.stderr.write(f"[WARN] Failed to write watermark to {path}: {e}\n")


def print_summary(data: dict[str, Any]) -> None:
    time_range = data.get("range", {})
    from_ts = time_range.get("from", "N/A")
    to_ts = time_range.get("to", "N/A")
    watermark = data.get("watermark", "N/A")
    exported_at = data.get("exported_at", "N/A")

    chats = data.get("chats", [])
    manifest = data.get("manifest", {})
    total_messages = manifest.get("message_count", sum(len(c.get("messages", [])) for c in chats))
    sha256 = manifest.get("sha256", "N/A")

    kinds_count: dict[str, int] = {}
    direct_chats = 0
    group_chats = 0

    for chat in chats:
        k = chat.get("kind", "direct")
        if k == "group":
            group_chats += 1
        else:
            direct_chats += 1

        for msg in chat.get("messages", []):
            mkind = msg.get("kind", "other")
            kinds_count[mkind] = kinds_count.get(mkind, 0) + 1

    print("=" * 60)
    print(" WeChat Export Summary")
    print("=" * 60)
    print(f" Exported At     : {exported_at}")
    print(f" Time Range      : {from_ts} -> {to_ts}")
    print(f" Watermark       : {watermark}")
    print(f" Total Chats     : {len(chats)} (Direct: {direct_chats}, Group: {group_chats})")
    print(f" Total Messages  : {total_messages}")
    print(f" Checksum SHA256 : {sha256}")
    if kinds_count:
        print(" Kinds Breakdown :")
        for kind_name in sorted(kinds_count.keys()):
            print(f"   - {kind_name:<10}: {kinds_count[kind_name]}")
    else:
        print(" Kinds Breakdown : (no messages in range)")
    print("=" * 60)


def main() -> None:
    args = parse_args()
    base_url = args.base_url.rstrip("/")

    if args.health:
        url = f"{base_url}/health"
        _, data = fetch_endpoint(
            url, token=args.token, timeout=args.timeout, force_no_proxy=args.no_proxy
        )
        print(json.dumps(data, indent=2, ensure_ascii=False))
        return

    # Determine since
    since_val = args.since
    if not since_val and args.watermark_file:
        saved_wm = read_watermark(args.watermark_file)
        if saved_wm:
            since_val = saved_wm

    # Build query params
    params: dict[str, str] = {}
    if since_val:
        params["since"] = str(since_val)
    if args.to:
        params["to"] = str(args.to)

    query_str = urllib.parse.urlencode(params)
    url = f"{base_url}/api/wechat/export"
    if query_str:
        url = f"{url}?{query_str}"

    _, data = fetch_endpoint(
        url, token=args.token, timeout=args.timeout, force_no_proxy=args.no_proxy
    )

    # If watermark file requested, persist new watermark
    new_watermark = data.get("watermark") or data.get("exported_at")
    if args.watermark_file and new_watermark:
        write_watermark(args.watermark_file, new_watermark)

    # Save to file if requested
    if args.out:
        out_path = Path(args.out).expanduser()
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")

    # Display results
    if args.summary:
        print_summary(data)
    elif args.raw:
        print(json.dumps(data, ensure_ascii=False))
    else:
        print(json.dumps(data, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
