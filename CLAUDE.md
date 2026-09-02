# Melete

> Μελέτη — 三缪斯中司「练习、修习」的那一位。
> 发音 **ME-le-te** /ˈmɛlɪtiː/（美勒忒）。

**题库驱动的学习平台。** AWS SAA-C03 是第一个题库，不是唯一一个。

---

## ⚠️ 安全边界（最优先，动手前必读）

### 本仓库必须永久保持 private

最硬的理由是**版权**，不是内网信息：

- 题库源自第三方汇编（源站 类），含 1011 道完整题目 + 答案
- **AWS Certification Agreement 明确禁止披露考试内容**
- 公开 = 分发考试转储；这在作品集里是减分项，不是加分项

**这不是「暂时私有、以后开源」。** 仓库整体永久私有。

### 但代码有开源价值，所以边界现在就划好

```
melete/
├── pipeline/
│   ├── core/                 通用管道，零题库特有逻辑   ┐
│   └── banks/<slug>/         题库特有解析器（永久私有） │
├── data/<slug>/              ⚠️ 题库数据，绝不出私有仓  │ 将来可拆出去开源
├── backend/                  Go 服务（通用）            ┘
└── web/                      Next.js（通用）
```

将来若拆分开源，拆的是 `backend/` + `web/` + `pipeline/core/`。
**前提：backend 不得硬编码任何 AWS / SAA-C03 特有的东西。**
这也是「标签用通用 `(type, value)`」设计的第二个收益。

### 凭证纪律

- **凭证类一律无默认值 + required** —— 生产忘配就启动失败，优于默默连错库
- 本地开发凭证走 config 私有仓 `local/ubuntu/env/melete.env`，变量前缀 `MELETE_`
- 生产凭证走 K8s Secret
- `.env.example` 只放明显是占位的值
- 代码里零硬编码

### 不写进本仓库的东西

| | 原因 |
|---|---|
| 原始素材（PDF 等） | 体积大 + 版权。走**收件区**约定，见下方「素材收件区」一节 |
| 内网 IP / 节点名 / 集群拓扑 | 部署清单放 config 私有仓 `clusters/集群甲/apps/melete/` |
| 任何真实凭证 | 见上。**已 commit 的凭证视为已泄漏**，删文件和补 .gitignore 都无效 |

---

## 定位

被动阅读不产生学习，主动回忆才产生。所以：

```
题目       = 第一等公民（学习单元、FSRS 调度对象、进度统计基础）
知识条目   = 支撑材料（挂在概念标签上，做错题时才被拉出来）
AI         = 贯穿三处（离线裁决/解析/打标 · 在线问答 · 内容生产）
```

atlantis（`~/work/github/atlantis`，114 篇中日双语知识条目）将来**复制**迁入，
降级为支撑材料，不阻塞主线。**原仓库保持现状，不动。**

---

## 四个核心设计判断（改动前先理解为什么）

### 1. 答案不是一个字段，是一组「主张」

素材里 **884 道有对照数据的题中，340 道（38%）题库标注答案与社区投票不一致**，
且分歧不是边缘情况（有 98% 社区一致反对标注答案的）。

```
❌ question.correct_answer = "A"
✅ answer_claim 表：
     source            answer  confidence  rationale
     bank_label        A       —           题库标注
     community_vote    B       98%         社区投票
     ai_verdict        B       high        "A 错在…，B 正确因为…"
     user_note         B       —           你自己的判断
```

界面上呈现「题库说 A，98% 的人说 B，AI 判 B 并给出理由」—— **这个呈现本身就是最好的学习材料**。
新增答案来源不用改 schema。

### 2. 标签是 `(type, value)` 通用结构，不硬编码 AWS

| type | 内容 | 回答什么 | 作用域 |
|---|---|---|---|
| `domain` | SAA-C03 官方 4 个考纲域 | 「我离及格还差多少」 | 题库私有 |
| `topic` | 知识对象轴：AWS 是服务（S3 / EC2 …），LPIC 是命令，Java 是 API | 「我哪一块不熟」 | 题库私有；**显示名由 `bank.meta.tagTypes` 给** |
| `concept` | 最终一致性 / 故障转移 / CDN … | 「缺的是 AWS 知识还是底层原理」 | **全局共享，链到知识条目** |

第二个题库进来时 schema 一行不用改，且能通过 `concept` 与 AWS 题目互相关联。

**三个 type 是角色不是名字**：名字（「考纲域」「服务」）、考纲权重、及格线都在 `bank.meta`
（来自 `pipeline/banks/<slug>/enrich_spec.json` 的 `tag_types` / `pass_score`），
前端经 `tagTypeLabel(bank.meta, type)` 读取 —— **前端与后端代码里不得出现任何题库词**。

### 3. FSRS 以「题目」为调度单元

不是知识点、不是标签。题目是可判定对错的最小单元，FSRS 需要明确的 rating 信号。
知识条目是查阅材料，不适合背诵式调度。

### 4. 认证接 Akasha

复用自研 OIDC IdP。既省一套认证，又让 Akasha 从 demo 变成真的在支撑多应用的身份中枢。

---

## 数据模型

```sql
-- 内容侧
bank          id, slug, name, description, locale, kind(cert|custom), meta json
question      id, bank_id, external_no, stem, kind(single|multi), pick_count, raw json
choice        id, question_id, label(A-F), body
answer_claim  id, question_id, source, answer, confidence, rationale, meta json   ★
tag           id, bank_id NULL, type(domain|topic|concept), value, i18n json
question_tag  question_id, tag_id, weight
article       id, path, locale, title, description, sections json      -- atlantis 迁入
article_tag   article_id, tag_id                                       -- 经 concept 关联题目

-- 学习侧
account       id, akasha_sub, ...
card          account_id, question_id, state, due, stability, difficulty, reps, lapses
attempt       id, account_id, question_id, chosen, correct, duration_ms, rating(1-4), context json  -- 出处：{mode, tagId}
```

「我哪里不会」不需要额外设计：`attempt ⋈ question_tag ⋈ tag` 按标签聚合正确率即是。

**学习侧只存事实，不存状态**：错题 / 不确定 / 没做过 / 顺序断点 / 专项进度全部是对 `attempt` 的查询
（每题只取最近一次）。`attempt.context` 是唯一「推不出来」的信息 —— 这道题是从哪个入口做的；
「上次专项」= 最近一条 `context.mode ∉ {unseen, all}` 的作答。将来唯一合法的状态表是 FSRS 的 `card`（P3）。

---

## 技术栈

| 层 | 选型 | 理由 |
|---|---|---|
| 前端 | Next.js 16 + React 19 + Tailwind 4 | 沿用 atlantis 的栈，组件/i18n/块渲染器可直接复用 |
| 后端 | Go + Echo/chi + GORM | 与 geass-v3 一致 |
| API 契约 | Spec-first OpenAPI | 与 geass-v3 一致 |
| 数据库 | **开发 = 开发机 MySQL 8.0.46 / 生产 = TiDB Cloud** | 见下方迁移策略 |
| 认证 | Akasha OIDC | 见判断 4 |
| 部署 | 集群 SSR + Cilium Gateway API HTTPRoute | 复制 geass-v3-web 模式 |
| AI 离线 | Claude Batches API（5 折） | 1011 道一次跑完，约 $5–15 |
| AI 在线 | 后端代理 Claude API，key 存 K8s Secret | 前端不碰 key |
| 移动 | PWA | 刷题发生在碎片时间；Next.js 原生支持，可离线 |

### 数据库迁移策略：内容重跑，记录不迁

**不做 dump + import。** 三段式管道的中间产物才是 source of truth：

```
PDF → questions.json → enriched.json → [导入] → DB
                            ↑ 真正的 source of truth
```

上线时拿同一份 `enriched.json` 对 TiDB 再跑一遍导入即可。
开发库随便造随便删。真正不可重建的只有学习记录（`attempt`/`card`），
而开发期那些记录恰恰不想带到生产。

### MySQL ↔ TiDB 四条纪律（从第一天守，成本极低）

1. **不用外键**，关系约束放应用层（TiDB 的 FK 晚且实验性，会静默不级联）
2. **不假设 id 连续**（TiDB 各节点各持一段 auto_increment。geass 的 tags 表已踩过）
3. **批量写入分批提交**（TiDB 单事务默认 100 MB 上限）
4. **复杂 JSON 查询放应用层**，别写进 SQL

⚠️ **P2 之前必须拿 TiDB 建一次表跑一次导入**，确认 schema 两边都吃得下。

---

## 分期

| 阶段 | 内容 | 状态 |
|---|---|---|
| **P0** | PDF → 结构化 JSON | ✅ 已完成 |
| **P1** | Batches 跑一遍：裁决 + 解析 + 三维标签 | 待办 |
| **P2** | 最小可用：刷题 / 看解析 / 基础统计 | 待办（**到这里就已经能用**） |
| **P3** | FSRS 调度 | 待办 |
| **P4** | AI 实时问答 | 待办 |
| **P5** | atlantis 迁入 + concept 关联 | 待办 |
| **P6** | 第二个题库，验证扩展性 | 待办 |

---

## 素材收件区（inbox）

**原始素材（PDF 等）不进仓库** —— 体积大 + 版权。约定如下：

```
$MELETE_INBOX/<bank-slug>/<任意文件名>        默认 ~/melete-inbox/
          ↓  python3 pipeline/core/ingest.py <bank-slug>
data/<bank-slug>/questions.json               结构化产物（进 git）
data/<bank-slug>/source.yaml                  素材登记单（进 git，仅元数据）
```

流程：

1. 素材长期存放在**你自己的磁盘**上，仓库不管
2. 要导入时，复制到收件区里对应的题库子目录
3. 跑 `ingest.py`
4. 处理完，**由你自己**把素材拿走 —— **脚本不删除、不移动任何用户文件**

### 登记单（source.yaml）解决什么问题

素材拿走之后，仓库仍然知道：产物来自哪个文件（`filename` + `sha256` + `bytes`）、
什么时候导入的、用哪个 git 版本的解析器生成的、产出了多少题多少告警。

将来拿到新版素材，**比对 sha256 即可判断是不是同一份**，不用凭记忆。

---

## 当前状态

### P0 已完成

```
pipeline/banks/aws-saa-c03/parse.py     解析器
data/aws-saa-c03/questions.json         1011 道题（1.4 MB）
```

重跑方式：

```bash
# 把 PDF 放进 ~/melete-inbox/aws-saa-c03/ 后：
python3 pipeline/core/ingest.py aws-saa-c03
```

### 解析质量

```
题目总数    1019  （题号连续无缺；8 道英文题头曾漏抓，2026-09-02 找回）
零告警      989  (97.1%)
有告警       30   ← 含后补检测：撞词重复/错切/连字恢复/图片策略块
多选题      123
答案主张    bank_label 1019 · community_vote 898
```

**21 道告警题的 warnings 字段已带进 JSON**，留给 P1 的 AI 环节或人工处理。
典型脏点：投票表头无数据（15）、答案字母不在选项里（7）、整题缺选项（2）。

### 素材已知特征

- 题库标注答案与社区投票 **340 道不一致**（884 道有对照数据的题中占 38%）
- **47 道低共识题**（社区首选得票 < 60%）—— 真正有争议的难题
- 共 **1019 道、题号连续无缺**。其中 8 道（108/129/253/326/429/718/939/960）
  的题头是英文「Question #N」（正文仍中文），曾被只认中文题头的解析器漏抓、
  整题内容吞进前一题（#252 投票分布出现 200% 即此表征）。
  ⚠️ 教训：当时用中文 grep「验证」出这 8 题不存在 ——
  **验证工具与被验证者共享同一盲区时，验证必然通过**。哨兵：投票和 >100 即报警
- 「ﬁ」连字在文本层丢失致「file 系统/服务器」变「le 系统/服务器」
  （#260/#283/#332/#800），解析器已按已验证模式自动恢复并登记
- 题号 905 以后共 104 道**无社区投票数据**（最新加入的）；另有 15 道投票表头无数据
- **#96 与 #253 题干引用的 IAM 策略是图片**，文本层不存在，无法独立裁决
  （#96 有冒号结尾启发式兜底；#253 是「中部引用」形态，现有启发式抓不到，
  靠富化侧人工发现 —— 两道均以 data_issue 处理）
- ~~0 道题引用图表~~ —— **被 #96 推翻**：架构图确实一张都没有，但存在以图片形式
  嵌入的策略/代码块（目前已知仅 #96 一例，解析器已有冒号结尾启发式兜底）
- 原 PDF 每页一张 1756×2484@150dpi 整页图 = 水印背景，**不是内容，可忽略**
- 存在西里尔字母混入拉丁选项字母的脏点（如 `С` U+0421），解析器已归一

### P1 的设计要点

三种标签 + 答案裁决 + 解析，**必须在同一次 AI 调用里产出**，不要分三遍跑 1011 道题：

```json
{
  "verdict": "B",
  "confidence": "high",
  "reasoning": "为什么是 B，以及 A 错在哪",
  "explanation": "完整解析",
  "domain": 3,
  "services": ["S3", "CloudFront"],
  "concepts": ["cache/cdn"]
}
```

---

## 待定事项

- P1 的模型选择与预算上限
- 应用的 hostname（需查 config 仓 CLAUDE.md 的「在用 hostname 清单」避免撞名）
- atlantis 内容迁入的时机（P5，不阻塞主线）

---

## 协作约定

遵循用户全局 CLAUDE.md 的**决策教练模式**：浮现盲点 → 列候选与利弊 → 给推荐和理由 → **用户拍板**。
不要直接给最终方案让用户只能 yes/no。
