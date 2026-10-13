# 安全策略与漏洞披露 (Security Policy)

## 支持的版本 (Supported Versions)

| 版本 | 是否支持安全更新 |
| :--- | :--- |
| `1.x` (当前主要版本) | :white_check_mark: 支持 |
| `< 1.0.0` | :x: 不再维护 |

## 报告安全漏洞 (Reporting a Vulnerability)

如果您在本项目中发现了潜在的安全漏洞或凭据泄露风险，请**切勿在公开的 GitHub Issues 中提交**。

请遵循负责任的漏洞披露流程（Responsible Disclosure）：
1. 通过电子邮件向维护团队报告：`security@yaping.pro`
2. 报告应包含漏洞复现步骤、PoC 样例以及预估的影响范围。
3. 我们会在 48 小时内确认收到报告，并在修复方案验证通过后发布安全补丁公告。

## 核心安全加固规范 (Hardening Guidelines)

1. **守护进程监听地址**：生产部署时务必保持 `bind: "127.0.0.1"`，严禁监听 `0.0.0.0`。
2. **认证令牌**：生成至少 32 字节高熵随机令牌（例如 `openssl rand -hex 32`）并配置在 `config.json` 与客户端环境变量中。
3. **数据跨主机传输**：如需跨设备拉取，强烈推荐使用 `adb forward tcp:8765 tcp:8765`、SSH 本地端口转发或加密点对点隧道（如 Tailscale），切勿使用纯明文 HTTP 穿越公共互联网。
