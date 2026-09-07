# Melete

> Μελέτη — 三缪斯中司「练习、修习」的那一位。**ME-le-te** /ˈmɛlɪtiː/

题库驱动的学习平台。被动阅读不产生学习，主动回忆才产生 —— 所以题目是第一等公民，
知识条目是解释「为什么」的支撑材料。

---

## 🔒 题库数据不在本仓

本仓是代码。题库数据（题目 / 答案 / 富化产物）在一个**私有仓**里，
由 `MELETE_DATA_ROOT` 指出位置 —— 见 `pipeline/core/paths.py`。

理由是版权：各题库素材的授权状况不同，**统一按不可分发处理**。
把数据放在代码之外，边界就不依赖任何人记得住规矩。

```bash
source ~/work/github/config/local/ubuntu/env/melete.env   # 设 MELETE_DATA_ROOT
python3 pipeline/core/ingest.py <bank-slug>
```

⛔ **`MELETE_DATA_ROOT` 无默认值，未设即报错退出。** 不是洁癖：
一个"合理的默认值"会让脚本把题库写回本仓，正是这个边界要防的事。

三道闸：无默认值的环境变量 · `.gitignore` 的 `/data/` · CI 断言 `data/` 为空。
详见 `CLAUDE.md` 的「边界」一节。

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
mkdir -p ~/melete-inbox/aws-saa-c03 && cp <素材.pdf> ~/melete-inbox/aws-saa-c03/
python3 pipeline/core/ingest.py aws-saa-c03
# 处理完自行把素材拿走 —— 原始素材不进仓库（体积 + 版权）
```

下一步 P1：用 Claude Batches API 对 1011 道题一次性产出「答案裁决 + 解析 + 三维标签」。

**为什么需要裁决**：884 道有对照数据的题中，**340 道（38%）题库标注答案与社区投票不一致**，
且存在 98% 社区一致反对标注答案的情况。照搬标注答案会背错三分之一。
