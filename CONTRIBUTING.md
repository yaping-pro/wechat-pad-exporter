# Contributing to `wx-pad-exporter`

Thanks for your interest in contributing! `wx-pad-exporter` is an open-source data export and local AI assistant pipeline designed for personal data sovereignty.

Please read these guidelines and architectural boundaries before opening an Issue or Pull Request.

---

## 1. Repository Layout & Scope

This repository is self-contained and houses the complete end-to-end data pipeline:

| Directory | Component | Tech Stack / Role |
| :--- | :--- | :--- |
| `android-module/` | Android Hook Probe (`com.wx.sniffer`) | Kotlin + DexKit + LSPosed / Vector. Intercepts WCDB SQLite inserts in real time. |
| `daemon/` | Export Daemon (`wx_daemon`) | Go 1.23. Lightweight loopback service receiving UDS stream and managing SQLite WAL storage. |
| `clients/` | Consumer Clients & Bridges | Python. Includes cursor fetcher (`fetch_export.py`) and AI webhook gateway (`assistant_webhook.py`). |
| `deploy/` | Watchdog & Service Scripts | Shell. Magisk / KernelSU `service.d` auto-start, OOM protection, process guardian. |
| `docs/` | Architecture & API Specs | Markdown technical documentation and integration recipes. |
| `schema/` | JSON Data Contracts | JSON Schema definitions for exported payloads and snapshot envelopes. |
| `skills/` | Autonomous Agent Skills | Agent definitions and skill contracts for autonomous deployment and maintenance. |

---

## 2. Hard Invariants & Contribution Boundaries

Every PR must strictly adhere to the following architectural rules:

1. **100% Strictly Read-Only**:
   - We **never** accept code that sends messages, makes friend requests, clicks UI elements, or mutates WeChat internal databases.
   - The probe exists solely to read and export the user's own data to their own storage.
2. **Flag & Configuration Fidelity**:
   - The Go daemon binary accepts strictly defined flags: `-config`, `-http`, `-db`, `-uds`, `-token`, `-no-auth`, `-retain-days`, `-log-file`, `-log-max-bytes`, `-log-keep`.
   - Never invent nonexistent flags or alternate environment variable schemes.
3. **Loopback & Network Safety**:
   - Daemon defaults to `127.0.0.1:8765`. Never introduce code that defaults to `0.0.0.0` or disables authentication without explicit user intent (`-no-auth`).
4. **Zero Secrets & Zero PII**:
   - Keep `.env`, `*.pem`, `*.key`, `*.token`, `auth_key*`, `tskey-*`, `vault.db*`, and `*_dump.json` out of git commits (enforced by `.gitignore`).
   - Use placeholders (`<TOKEN>`, `127.0.0.1`) in documentation and test configs.

---

## 3. Commit Message Style

We follow Conventional Commits:

- `feat(daemon)`: New daemon capabilities or storage improvements
- `feat(hook)`: DexKit matcher or probe enhancements
- `feat(client)`: Consumer client or assistant webhook bridge features
- `fix(deploy)`: Watchdog or service script fixes
- `fix`: General bug fixes
- `docs`: Documentation and architecture updates
- `ci`: GitHub Actions workflow changes
- `chore`: Dependency updates and repository maintenance

Example:
```text
feat(daemon): add log rotation compression support
fix(hook): handle null cursor in attachment table interceptor
docs: update dify webhook integration example
```

---

## 4. Verification Before Submitting

Ensure all components pass local verification before submitting:

```bash
# Verify Go daemon tests and builds
cd daemon
go test -v ./...
go build -o /dev/null .

# Verify Android hook module builds (if modifying android-module/)
cd ../android-module
./gradlew check assembleRelease

# Verify Python client syntax
cd ../clients
python3 -m py_compile *.py
```

---

## 5. Code of Conduct

Be respectful and constructive. Discussions should focus strictly on the engineering of personal data sovereignty, local AI assistants, and open-source systems integration.
