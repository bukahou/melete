# Melete

> Μελέτη — 三缪斯中司「练习、修习」的那一位。**ME-le-te** /ˈmɛlɪtiː/

题库驱动的学习平台。被动阅读不产生学习，主动回忆才产生 —— 所以题目是第一等公民，
知识条目是解释「为什么」的支撑材料。

---

## 🔒 本仓库必须永久保持 PRIVATE

**理由是版权，不是内网信息。**

- 含第三方汇编题库，1011 道完整题目 + 答案
- AWS Certification Agreement 明确禁止披露考试内容
- 公开 = 分发考试转储；这在作品集里是减分项

**这不是「暂时私有、以后开源」。** 若将来要开源，拆出去的是
`backend/` + `web/` + `pipeline/core/`，而 `data/` 与题库特有解析器永久留在私有仓。

变更仓库可见性前请重读根目录 `CLAUDE.md` 的「安全边界」一节。

---

## 目录

```
pipeline/
  core/                 通用管道（AI 富化 / 导入），零题库特有逻辑
  banks/<slug>/         题库特有解析器
data/<slug>/            题库中间产物（故意纳入 git，供 review 与 diff）
backend/                Go 服务
web/                    Next.js 前端
docs/
  design/active/        架构设计 · 数据管道规格
  tasks/active/         任务进度
```

## 文档

| 文档 | 内容 |
|---|---|
| `CLAUDE.md` | 安全边界 · 核心设计判断 · 当前状态 —— **动手前先读** |
| `docs/design/active/architecture.md` | 领域模型 · Schema DDL · 部署 · 被否决的方案 |
| `docs/design/active/data-pipeline.md` | 三段式管道规格 · 源文件形态 · 质量门禁 |
| `docs/tasks/active/tracker.md` | 分期任务与进度 |

## 当前状态

**P0 完成** —— 1011 道题已解析为结构化 JSON，97.9% 零告警。

```bash
pdftotext -layout "$MELETE_SAA_PDF" /tmp/saa-c03.txt
python3 pipeline/banks/aws-saa-c03/parse.py /tmp/saa-c03.txt data/aws-saa-c03/questions.json
```

下一步 P1：用 Claude Batches API 对 1011 道题一次性产出「答案裁决 + 解析 + 三维标签」。

**为什么需要裁决**：884 道有对照数据的题中，**340 道（38%）题库标注答案与社区投票不一致**，
且存在 98% 社区一致反对标注答案的情况。照搬标注答案会背错三分之一。
