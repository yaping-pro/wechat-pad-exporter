# 智能体技能扩展槽 (Agent Skills Extension Point)

本目录为面向 AI 智能体（如 `omp`, `Claude Code`, `Codex` 等）的**前向兼容扩展插槽 (Forward-compatible Extension Point)**。

---

## 1. 目录设计目标 (Design Intent)

本项目专注于微信聊天数据的高性能、零读撕裂抽取与出口服务。为了避免核心仓库过于臃肿，数据下游的可视化分析、词云提炼、社群图谱生成等衍生能力均设计为解耦的**外挂式 Agent Skill**。

未来官方或社区开发的技能将以独立子目录形式在此组织：
```text
skills/
├── README.md                          # 扩展契约说明（本文件）
└── <skill-name>/                      # 规划中的扩展技能
    ├── SKILL.md                       # 智能体遵循的规范与 Prompt 指令
    ├── scripts/                       # 技能执行辅助脚本
    └── examples/                      # 样例产出
```

---

## 2. 预留技能规划 (Planned Skills)

- [`docs/AGENT_SKILLS.md`](../docs/AGENT_SKILLS.md)：已定义了完整的**可视化技能输入契约**与不可变保证（Invariants）。
- 规划中的首个首发技能为 `wechat-visualizer`：输入标准快照，输出单文件交互式 HTML 仪表盘与终端字符统计图表。

---

## 3. 当前实现状态说明 (Implementation Status)

> ⚠️ **注意**：本插槽当前为**规划中的预留扩展点 (Planned Extension Point)**。核心守护进程与数据抓取管道已全面遵循 [`schema/export.schema.json`](../schema/export.schema.json) 标准，具体可视化技能逻辑将在后续版本中分步落地。
