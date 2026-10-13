# 安全策略与漏洞披露 (Security Policy)

## 支持的版本 (Supported Versions)

| 版本 | 是否支持安全更新 |
| :--- | :--- |
| `1.x` (当前主要版本) | :white_check_mark: 支持 |
| `< 1.0.0` | :x: 不再维护 |

## 报告安全漏洞 (Reporting a Vulnerability)

如果您在本项目中发现了潜在的安全漏洞或凭据泄露风险，请**切勿在公开的 GitHub Issues 中提交**。

请通过 GitHub 官方支持的 **Private Vulnerability Reporting** 私密通道报告：
1. 访问本仓库的 Security 页面：`https://github.com/yaping-pro/wx-pad-exporter/security/advisories/new`（或在仓库页面点击 **Security** 标签页中的 **Report a vulnerability**）。
2. 报告应包含漏洞复现步骤、PoC 样例以及预估的影响范围。
3. 维护者会在确认收到报告后及时评估风险，并在修复方案验证通过后发布安全补丁与安全通报。

## 核心安全加固规范 (Hardening Guidelines)

1. **守护进程监听地址**：生产部署时务必保持 `bind: "127.0.0.1"`，严禁监听 `0.0.0.0`。
2. **认证令牌**：生成至少 32 字节高熵随机令牌（例如 `openssl rand -hex 32`）并配置在 `config.json` 与客户端环境变量中。
3. **数据跨主机传输**：如需跨设备拉取，强烈推荐使用 `adb forward tcp:8765 tcp:8765`、SSH 本地端口转发或加密点对点隧道（如 Tailscale），切勿使用纯明文 HTTP 穿越公共互联网。

## 官方发布证书公钥指纹 (Release Signing Certificate Fingerprint)

为防止供应链投毒、仿冒篡改与非官方自签名 APK，`wx-pad-exporter` 官方 GitHub Release 分发的 APK 产物均由专用离线 Keystore 进行 v1 + v2 签名。

用户与审计人员可在安装前通过 `apksigner` 自行验证 APK 证书公钥指纹：

```bash
apksigner verify --print-certs wechat-pad-exporter-module-v*.apk
```

**官方发布证书 SHA-256 指纹**：
```text
76:AD:A6:29:EA:2A:12:C6:77:67:22:52:15:63:2B:19:F8:31:78:F3:4F:92:C2:79:E6:44:6E:BC:25:84:30:C1
```

CI 构建流水线内置强制性真值门禁（Truth Gate），任何与上述指纹不匹配的构建产物均会被 CI 严格阻断，绝对禁止发布。
