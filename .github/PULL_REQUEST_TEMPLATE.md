## 变更概述 / Pull Request Overview

<!-- 请简要说明本 PR 包含的修改内容与意图 / Briefly explain the changes and motivation -->

### 关联 Issue / Related Issues
<!-- 如有，请使用 Closes #123 自动关联 / Link issues -->
Closes #

---

## 变更涉及组件 / Affected Components

- [ ] `android-module/`: Android Hook 探针 (`com.wx.sniffer`)
- [ ] `daemon/`: Go 守护进程 (`wx_daemon`)
- [ ] `clients/`: Python 消费端 / Webhook 网关
- [ ] `deploy/`: 守护脚本与自启配置
- [ ] `schema/`: JSON Schema 数据契约
- [ ] `docs/`: 架构与说明文档

---

## 严格只读与架构底线审查 / Read-Only & Architecture Check

- [ ] **严格只读承诺 (100% Strictly Read-Only)**: 本 PR **绝对不包含**任何消息发送、自动加好友、自动化 UI 点击、模拟注入或群控逻辑。
- [ ] **网络安全默认值 (Security Defaults)**: Daemon 默认监听 `127.0.0.1`，鉴权逻辑保持开启 (`auth: true` / fail-closed)，无破坏安全默认行为的改动。
- [ ] **配置契约保真**: 未擅自改动现有 flag 定义或打破与既有客户端的向后兼容性。

---

## 测试与验证 / How Has This Been Tested?

请描述本地测试验证命令与结果：
```bash
# 请勾选已完成并通过的本地测试:
```
- [ ] Go 单元测试通过 (`cd daemon && go test -v ./...`)
- [ ] Go 构建成功 (`cd daemon && go build -o /dev/null .`)
- [ ] Android 模块编译通过 (`cd android-module && ./gradlew assembleRelease`) *(若触碰安卓端)*
- [ ] Python 脚本语法验证通过 (`python3 -m py_compile clients/*.py`) *(若触碰客户端)*

---

## 贡献合规自查清单 / Contributor Checklist

- [ ] **零凭据与隐私保护 (Zero Secrets / Zero PII)**: 代码、文档、提交记录中绝无任何真实 Token、私网 IP、密钥、真实微信号、UIN、手机号或聊天记录 dump。
- [ ] **遵循提交规范**: Commit message 遵循 Conventional Commits (例如 `feat(daemon): ...`, `fix(client): ...`)。
- [ ] **开源许可授权 (MIT)**: 我确认此贡献属于原创或合法引入，并同意依照本仓库的 MIT 许可证开源。
