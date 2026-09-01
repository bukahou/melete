# Melete 任务进度

> 最后更新 2026-09-01

## 分期总览

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0 | PDF → 结构化 JSON | ✅ 完成 |
| P1 | AI 富化：裁决 + 解析 + 三维标签 | ⬜ 待办 |
| P2 | 最小可用：刷题 / 看解析 / 基础统计 | ⬜ 待办 |
| P3 | FSRS 调度 | ⬜ 待办 |
| P4 | AI 实时问答 | ⬜ 待办 |
| P5 | atlantis 内容迁入 + concept 关联 | ⬜ 待办 |
| P6 | 第二个题库，验证扩展性 | ⬜ 待办 |

**P2 就是可用产品**，P3 之后才是「聪明」。不要在 P2 之前追求完整。

---

## P0 · PDF → 结构化 JSON ✅

- [x] 确认 PDF 有文本层（509 页 A3，每页一张整页水印图，非内容）
- [x] 摸清源文件形态（页眉/续行/答案行/投票分布/分页符）
- [x] 写解析器 `pipeline/banks/aws-saa-c03/parse.py`
- [x] 修 `re.M` 缺失导致 bank_label 只抽到 104 条的 bug
- [x] 西里尔字母混入拉丁选项字母的归一
- [x] 质量报告 + warnings 随题带出
- [x] 产出 `data/aws-saa-c03/questions.json`（1011 道，97.9% 零告警）

## P1 · AI 富化 ⬜

- [ ] 确定模型（Sonnet 5 / Opus 5）与成本上限
- [ ] 写提示词：一次调用产出 verdict + reasoning + explanation + 三维标签
- [ ] 从 atlantis 抽出 concept 路径白名单（防止 AI 编造路径）
- [ ] 走 Claude Batches API 跑 1011 道
- [ ] 质量门禁：verdict 合法率 100% / concepts 路径存在率 100%
- [ ] 47 道低共识题人工抽查
- [ ] 21 道解析告警题单独处理
- [ ] 产出 `data/aws-saa-c03/enriched.json`

## P2 · 最小可用 ⬜

- [ ] 定 hostname（查 config 仓「在用 hostname 清单」避免撞名）
- [ ] `api/openapi/melete.yaml` Spec-first 契约
- [ ] Schema 建表（先 开发机 MySQL）
- [ ] `pipeline/core/load.py` 幂等导入器
- [ ] **拿 TiDB 跑一次完整导入验证**（不要等上线才发现差异）
- [ ] Go 后端骨架：bank / question 查询
- [ ] Next.js 前端：刷题页 + 单题详情（三方答案主张并列展示）
- [ ] Akasha OIDC 接入
- [ ] 基础统计：按 domain / service 的正确率

## P3 · FSRS ⬜

- [ ] 选 FSRS 实现（Go 侧库或自实现）
- [ ] `card` 表状态机 + 「今天该复习什么」查询
- [ ] 作答 → rating 映射规则（对/错 + 用时 → 1-4）
- [ ] 复习队列页

## P4 · AI 实时问答 ⬜

- [ ] 后端 `/api/ai/ask` 代理（key 不出后端）
- [ ] 单用户日调用上限（防误用烧钱）
- [ ] 前端追问入口

## P5 · atlantis 迁入 ⬜

- [ ] 复制 114 篇 × 2 语言内容（**原仓库不动**）
- [ ] `article` / `article_tag` 导入
- [ ] concept 标签双向关联
- [ ] 错题 → 知识条目跳转

## P6 · 第二个题库 ⬜

- [ ] 选题库
- [ ] 只写 `pipeline/banks/<slug>/parse.py`，验证 core 无需改动

---

## 阻塞 / 待定

- [ ] GitHub 仓库尚未创建（`gh repo create bukahou/melete --private`）
- [ ] hostname 未定
- [ ] P1 的模型与预算未定

## 已知风险

| 风险 | 说明 |
|---|---|
| 题库答案不可信 | 340 道标注与社区不一致。**若照搬标注答案会背错三分之一** —— 这是 P1 存在的根本理由 |
| 104 道无投票数据 | 题号 905–1011，只有单一答案来源，AI 裁决无对照 |
| 仓库可见性 | 必须永久 private，理由见根目录 CLAUDE.md |
