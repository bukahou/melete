# Melete

> Μελέτη — 三缪斯中司「练习、修习」的那一位。
> 发音 **ME-le-te** /ˈmɛlɪtiː/（美勒忒）。

**题库驱动的学习平台。** AWS SAA-C03 是第一个题库，不是唯一一个。

---

## ⚠️ 边界（最优先，动手前必读）

### 本仓库是公开仓 —— 题库数据不在这里

```
melete (public)          代码：backend / web / pipeline
config (private)         数据：banks/melete/ ← 题库在这里
```

`pipeline/core/*.py` 通过 **`MELETE_DATA_ROOT`** 找到数据（见 `pipeline/core/paths.py`）。
**必填、无默认值** —— 一个"合理的默认值"会让脚本把题库写回公开仓，
正是把数据挪出去要避免的那件事本身。

设置：`source ~/work/github/config/local/ubuntu/env/melete.env`

### 为什么数据不能进来

题库素材来自第三方汇编，含完整题目与答案，相关认证协议禁止披露考试内容。
公开一份考试转储在作品集里是减分项，不是加分项。

⚠️ **删文件不够。** 公开仓的 git 历史全部可读 —— `git clone` 就拿到全部对象。
2026-09-07 公开时用 `git filter-repo` 把 `data/` 从全部历史重写掉，
并推到一个**从未接收过那些 blob** 的新仓库（force push 后 GitHub 仍保留
不可达对象，知道 SHA 就能取回）。旧仓 `bukahou/meleteold` 保持 private 封存。

### 三道闸，缺一不可

| 闸 | 位置 | 防的是 |
|---|---|---|
| `MELETE_DATA_ROOT` 无默认值 | `pipeline/core/paths.py` | 脚本猜错路径往仓里写 |
| `.gitignore` 的 `/data/` | 仓根 | 写进来了也 commit 不了 |
| CI 断言 `data/` 为空 | `.github/workflows/test.yml` | `git add -f` 绕过上一条 |

⛔ 三道都是必要的：前两道各自都能被绕开，第三道是兜底。

### 凭证纪律（公开仓的严格版）

- **凭证类一律无默认值 + required** —— 生产忘配就启动失败，优于默默连错库
- 本地开发凭证走 config 私有仓 `local/ubuntu/env/melete.env`，变量前缀 `MELETE_`
- 生产凭证走 K8s Secret
- `.env.example` 只放明显是占位的值
- 代码里零硬编码
- ⛔ **本仓公开后，任何分支上的任何提交都是公开的** ——
  不能再有"先提交上去，反正是私有的，回头再清理"。GitHub 有秒级爬虫

### 不写进本仓库的东西

| | 原因 |
|---|---|
| 题库数据（`data/`） | 见上。三道闸拦着 |
| 原始素材（PDF 等） | 体积大 + 版权。走**收件区**约定，见下方「素材收件区」一节 |
| 内网 IP / 节点名 / 集群拓扑 / Tunnel UUID / 具体主机名 | ⛔ **任何文件都不行，含设计文档与注释**。部署清单在 config 私有仓 `clusters/集群甲/apps/melete/`<br>⚠️ 这条原先写的是「**部署清单**放 config 私有仓」，于是 `docs/design/active/deployment.md` 钻了空子 —— 它是设计文档不是清单，就把当时执行的 `flarectl dns create --content <tunnel UUID>` 原样抄了进来（`9e2c385`，2026-09-02）。<br>⭐ 教训：规则按**文件类型**划范围就会漏，按**信息类型**划才不会。而且仓库当时是 private，这条规则不产生任何可感代价 —— **规则只在被违反且有后果时才显出漏洞，私有仓不给它这个机会**。 |
| 任何真实凭证 | 见上。**已 commit 的凭证视为已泄漏**，删文件和补 .gitignore 都无效 |
| 各题库的素材脏点清单 | 它描述的是素材不是代码，归属地在 `config/banks/melete/README.md` |
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
| **P6** | 第二个题库，验证扩展性 | ✅ SAP-C02 已入库（2026-09-03）；`pipeline/core/*` 零改动即接入，见 tracker |

---

## 素材收件区（inbox）

**原始素材（PDF 等）不进仓库** —— 体积大 + 版权。约定如下：

```
$MELETE_INBOX/<bank-slug>/<任意文件名>              默认 ~/melete-inbox/
          ↓  python3 pipeline/core/ingest.py <bank-slug>
$MELETE_DATA_ROOT/<bank-slug>/questions.json       结构化产物  ⚠️ 落在 config 私有仓
$MELETE_DATA_ROOT/<bank-slug>/source.yaml          素材登记单
```

⚠️ **产物不落在本仓** —— `MELETE_DATA_ROOT` 指向 config 私有仓的 `banks/melete/`。
本仓的 `.gitignore` 把 `/data/` 挡死，就是为了万一配置出错时兜住。

流程：

1. 素材长期存放在**你自己的磁盘**上，仓库不管
2. 要导入时，复制到收件区里对应的题库子目录
3. 跑 `ingest.py`（未设 `MELETE_DATA_ROOT` 会直接报错退出，不会猜路径）
4. 处理完，**由你自己**把素材拿走 —— **脚本不删除、不移动任何用户文件**

### 登记单（source.yaml）解决什么问题

素材拿走之后，仍然知道：产物来自哪个文件（`filename` + `sha256` + `bytes`）、
什么时候导入的、用哪个 git 版本的解析器生成的、产出了多少题多少告警。

⭐ 注意登记单里记的是**解析器的 git 版本**，查的是**本仓**（解析器是代码，在这里），
不是数据仓 —— 见 `pipeline/core/ingest.py` 的 `git_rev_of()`。

将来拿到新版素材，**比对 sha256 即可判断是不是同一份**，不用凭记忆。

---

## 当前状态

### P0 已完成

```
pipeline/banks/aws-saa-c03/parse.py                解析器          ← 本仓
$MELETE_DATA_ROOT/aws-saa-c03/questions.json       1019 道题       ← config 私有仓
```

重跑方式：

```bash
source ~/work/github/config/local/ubuntu/env/melete.env   # 设 MELETE_DATA_ROOT
# 把 PDF 放进 ~/melete-inbox/aws-saa-c03/ 后：
python3 pipeline/core/ingest.py aws-saa-c03
```

### 解析质量

```
题目总数    1019  （题号连续无缺）
零告警      989  (97.1%)
有告警       30
多选题      123
答案主张    bank_label 1019 · community_vote 898
```

告警题的 `warnings` 字段带进 JSON，留给富化环节或人工处理。

### 素材的已知脏点 → 见 config 私有仓

各题库素材的具体脏点（哪几道题引用图片、连字丢失、字母被西里尔字母污染、
哪些题头是英文导致被漏抓）**不在本仓** —— 它们描述的是素材，不是代码，
归属地是 `config/banks/melete/README.md`。

⭐ 但其中一条教训是通用的，留在这里：

> **验证工具与被验证者共享同一盲区时，验证必然通过。**

曾有 8 道题的题头是英文而解析器只认中文，于是被整题吞进前一题。
当时"验证这 8 题是否存在"用的是中文 grep —— 与解析器同一个盲区，
所以验证顺利通过，问题直到很久以后才暴露。
哨兵不能与被测者同源：这里的哨兵是「投票百分比之和 > 100 即报警」。
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
