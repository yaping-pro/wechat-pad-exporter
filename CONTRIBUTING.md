# Contributing

Thanks for your interest. This repo is the **pipeline / consumer / ops** side of a two-part system. Please read the boundary rules below before opening a PR.

## Source-of-truth boundary (important)

The **device-side source is NOT in this repository.**

> Per `device/README.md`:
> "设备端**源码**不在本仓库。" / "**设备端源码在 A 仓库** `wx-pad-sniffer`（有 remote + CNB CI + APK 产物）"

And `docs/upstream/README.md`:

> "本目录存放从设备端源码仓库 **A = `wx-pad-sniffer`** 复制的文档"

So:

- **Do not** open pull requests that add or modify the WeChat Xposed module, the `wx_daemon` Go source, or the `deploy/` scripts that belong to device builds. Those live in the separate repo **`wx-pad-sniffer`** and must be changed there.
- This repo is for: the consumer script (`consumer/`), ops/deploy scripts (`ops/`), the sing-box config, self-check tooling (`scripts/`), and documentation about the pipeline.
- If you fix something that originates in the device source, open the change against `wx-pad-sniffer` and, if needed, mirror the resulting docs into `docs/upstream/` here (keeping their md5 + timestamp, as that directory documents).

## Commit message style

Follow the existing convention — read `git log` for live examples. In short:

- Conventional-Commit prefix scoped to the area: `feat(consumer):`, `fix(device):`, `docs(oss):`, `fix(deploy):`, etc.
- Imperative subject, lowercase first word, no trailing period.
- Body explains *why*, not just *what*, when the change is non-obvious.

## Before you open a PR

- Keep `.env`, `*.pem`, `*.key`, `config.local.json`, `*.log`, and `.wx_export_state.json` out of the diff (see `.gitignore`). Use placeholders, never real values.
- Do not commit chat content or credentials — the `.gitignore` already blocks `*_dump.json`, `WX_CONTENT_*`, `*.xlog`.
- Run `bash scripts/selfcheck.sh` if your change affects the link it exercises.

## Code of conduct

Be respectful. Keep discussion focused on the engineering of *exporting your own data from your own account*.
