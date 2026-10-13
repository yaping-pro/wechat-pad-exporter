# 智能体技能扩展契约规范 (Agent Skills Extension Contract)

> **版本**：`v1.0.0` (2026-10-13)  
> **状态**：**预留扩展插槽 (Planned Extension Hook)**  
> **数据 Schema**：[`schema/export.schema.json`](../schema/export.schema.json)

---

## 1. 概念定位与扩展愿景 (Vision)

`wx-pad-exporter` 项目聚焦于**可靠、低侵入、零读撕裂的微信数据抽取与标准化出口**。为了避免核心仓库过度膨胀，所有高级展现、可视化仪表盘、词云分析及图谱构建能力均设计为**外部智能体技能 (Agent Skills)**。

本契约作为 forward-compatible（前向兼容）的坚固接口，明确规定了一个下游“可视化/图谱/报告技能”（例如将来的 `skill-wechat-visualizer`）所接收的数据输入、允许产生的产物形式以及核心不可变契约。

---

## 2. 技能输入契约 (Skill Invariants & Input Interface)

任何接入本项目数据出口的智能体技能，均可 100% 依赖以下**核心不变性 (Invariants)**：

### 2.1 结构不变性 (Structural Invariants)
1. **Schema 严格对齐**：输入 Payload 必定完全符合 [`schema/export.schema.json`](../schema/export.schema.json) 的定义，包含 `exported_at`, `range`, `watermark`, `chats`, `manifest` 顶层节点。
2. **规范化哈希存在**：`manifest.sha256` 必定为 64 位小写十六进制字符串，技能可直接用于缓存键（Cache Key）或增量分片指纹。
3. **时区确定性**：所有时间戳均为明确标注时区的 ISO-8601/RFC3339 字符串（统一为 `+08:00`）。
4. **会话聚合保证**：`chats` 数组中每个会话的 `messages` 列表必定按消息时间戳（`ts`）严格**升序排列**。

### 2.2 基础输入模式
技能通常通过以下两种方式之一获取输入：
- **静态快照输入 (File-based)**：
  读取由 `clients/fetch_export.py` 输出的本地 JSON 快照文件（如 `/path/to/snapshot.json`）。
- **实时端点拉取 (API-based)**：
  通过 HTTP GET 请求直接拉取目标端点（`GET /api/wechat/export?since=...`）。

---

## 3. 规划中的可视化技能规约 (Planned Visualization Skill Spec)

### 3.1 预留技能标识
- **技能名称**：`wechat-visualizer` (或 `skill://wechat-visualizer`)
- **建议目录布局**：`skills/wechat-visualizer/SKILL.md`

### 3.2 允许的标准产出 (Permitted Outputs)
可视化技能应当生成轻量、无外部后端依赖的确定性产物：
1. **交互式单文件 HTML 仪表盘 (Self-contained HTML)**：
   - 依赖内联 CSS / 可选 CDN 静态脚本（如 Chart.js / ECharts）。
   - 包含：会话发言活跃度时间热力图、群聊成员互动关系连线图、消息类型分布饼图。
2. **终端 ASCII / ANSI 字符可视化 (Terminal Text Visuals)**：
   - 针对 CLI 场景的高频发言人柱状图与时间分布分布条（Bar chart）。
3. **Markdown 格式分析纪要 (Structured Markdown Report)**：
   - 包含高频关键词、重点议题聚类与互动网络总结。

### 3.3 技能安全与隐私约束 (Security Guardrails)
- **零外部上传**：技能生成的任何图表或 HTML 页面**严禁**将用户的聊天记录或发言人信息回传至任何未经用户确认的第三方追踪服务。
- **脱敏选项**：技能必须支持可选的 `--anonymize` 开关，在生成公开展示的图表时自动将发言人 `wxid` 与真实昵称替换为虚拟代号（如 `User-A`, `User-B`）。
