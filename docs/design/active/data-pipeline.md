# 数据导入管道

> 状态：active · 最后更新 2026-09-01
> P0 已完成，P1 待办。

## 定位

**项目代码，但不是服务代码。** 它不常驻、不响应请求，但必须长期存在于仓库里：

- 题库会更新（勘误、补题）→ 要重跑
- AI 结果要能重新生成（换模型、改提示词）→ 要重跑
- **第二个题库进来时整套管道复用** → 这是它最重要的价值

语言用 **Python**：单向 ETL，不需要与后端共享类型；PDF/文本处理生态压倒性优势。

## 三段解耦

```
PDF ──[① 解析]──> questions.json ──[② AI 富化]──> enriched.json ──[③ 导入]──> DB
                       ↑                              ↑
                可 review / 可进 git / 可 diff       同上
```

**产出稳定的中间格式，不直接写数据库。** 好处：

- 解析出错能直接看中间产物，不用查数据库
- AI 结果进 git，**版本可追溯**（改提示词重跑，diff 一眼看出哪些题的裁决变了）
- 换数据库不用改解析器
- **上线不需要迁移数据库**，拿 `enriched.json` 对生产库重跑 ③ 即可

这是「接口 > 实现」在数据管道上的应用。

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

## ① 解析（已完成）

```bash
python3 pipeline/core/ingest.py aws-saa-c03    # 编排：抽取 → 解析 → 登记
```

`ingest.py` 是通用编排器（`pipeline/core/`），题库特有的只有 `parse.py`。

### 源文件形态（踩过的坑，换题库时对照）

```
'问题 #12                                    主题 1'   ← 页眉混在题号行，需剥离
'  A. 选项正文...'
'  Global Accelerator 标准加速器。'                    ← 续行只缩进，无字母前缀
'正确答案：C      B'                                   ← 前=题库标注，后=社区徽章
'  社区投票分配'
'                A（80%）        C（20%）'             ← 完整投票分布
'\x0c问题 #13 ...'                                     ← 分页符粘在下一题行首
```

答案行形态统计（1019 行）：

```
：L        880   单字母（社区徽章在下一行或没有）
：LL       106   多选标注 "AB"
：L__L      16   标注 + 大空白 + 社区徽章（仅这 17 例同行）
：LLL       14   三选
：С          1   ⚠️ 西里尔字母 U+0421，不是拉丁 C
```

**社区徽章已弃用** —— 与投票分布的第一名冗余，只保留 `bank_label` 与 `community_vote` 两个来源。

### 已修的 bug（换题库时容易重犯）

0. **题头只认中文「问题 #N」**，8 道英文题头「Question #N」的题被整题吞进前一题
   （表征：#252 投票分布 200%）。更危险的是当时用**中文 grep 去「验证」这些题不存在**
   —— 验证工具与解析器共享同一盲区，得出了「题库自身缺号」的错误结论。
   教训：验证一个工具的盲区，必须用**不同构造**的手段（这次靠的是投票和异常这个
   独立信号回溯）。已加哨兵：`vote_distribution_sum_gt_100`

1. `ANS_LINE` 正则忘了 `re.M`，导致 `(.*)$` 只在「正确答案」恰为最后一行时匹配
   → `bank_label` 只抽出 104/1011 条
2. 西里尔字母混入拉丁选项字母，需 `str.maketrans` 归一

### 两条方法论（2026-09-02，与富化侧协作中沉淀）

- **校验解析器的手段不能与解析器共享同一套模式。** 中文 grep 验证中文正则 =
  盲区里互相印证。打破盲区靠的是**独立信号**（这次是投票和 200% 这个数值异常）。
  富化侧的对应纪律：凡「数据自洽但数值不合常理」一律上报，不自行消化
- **社区共识度取首选项的原始百分比，不做归一化。** 源站 的 distribution
  是长尾截断的（合计常在 79-89%），A(45%)/B(20%) 归一化会虚标成 69% 的高共识，
  把真正的低共识题漏出筛网

### 系统性译文缺陷：三例撞词（本素材最主要的语义缺陷模式）

结构层脏点（缺选项、错切、图片丢失）靠解析器检测即可；**语义层的译文撞词只有精读才能发现**，
且危害递增。三例都由富化侧在做题时撞见、再回头做全库检索定性：

| # | 英文原词 | 误译成 | 危害 | 检测 |
|---|---|---|---|---|
| 1 | Spot Instances | 按需实例（撞 On-Demand） | 同题选项失去区分度，#128 四个选项两两相同 | `duplicate_choice_bodies_*` |
| 2 | Aurora Serverless | 按需 Aurora（再撞 On-Demand） | #511 社区 59/41 分裂 —— 读中译版看不出 C 是 Serverless | `suspect_serverless_mistranslation_*` |
| 3 | provisioned / reserved concurrency | 译法四选一、且互相串用 | **跨题矛盾**：#516 教「预留并发不消冷启动」，#597 却用它消冷启动 | `lambda_concurrency_term_ambiguous_*` |

第三例危害最大：前两例是单点误译，它是**互相矛盾的判据** —— 会摧毁学习者刚建立的正确认知。
完整映射表与逐题处理约定见 `pipeline/banks/aws-saa-c03/enrich_spec.md`。

**换题库时的启示**：机翻题库的术语撞词集中在「同一英文概念有多个中文习惯译法」
与「不同英文概念共享一个中文词」两类，值得在 P0 阶段就为该领域的核心术语建一张
对照表并全库检索，而不是等做题时逐个撞见。

### 质量门禁

```
零告警率 ≥ 95%          实测 97.9% (990/1011)
bank_label 覆盖 = 100%  实测 1011/1011
多选题数与源文档一致     实测 121（源文档 108 两项 + 13 三项 = 121）
```

`warnings` 字段随每道题带进 JSON，**不静默丢弃**。当前 46 道有告警，全部是题库自身脏点：

| 告警 | 数量 | 性质 |
|---|---|---|
| `vote_header_without_pairs` | 15 | 有投票表头但无数据 |
| `*_answer_X_not_in_choices` | 7 | 答案字母不在选项里 |
| `no_options` / `only_0_choices` | 1 | 整题缺选项（原 2 道，#868 已由 `choice_a_missing` 的修复救回） |
| `choice_labels_disordered` | 1 | 选项字母乱序 |
| `*_len1_expect2` | 2 | 多选题只给了一个答案 |
| `stem_ends_with_colon_missing_block` | 1 | #96 题干引用的策略块是图片，文本层缺失（2026-09-01 补的检测） |
| `duplicate_choice_bodies_*` | 2 | #84/#128 —— Spot 误译「按需实例」与 On-Demand 撞词致选项重复（2026-09-01 补） |
| `choice_body_suspiciously_short_*` | 1 | #182 —— 选项边界错切，B 只剩 9 字残句（2026-09-02 补；判据：方案描述题里长度 < 中位数 25%） |
| `ligature_restored_file` | 4 | #260/#283/#332/#800 —— ﬁ 连字丢失致「file→le」，已自动恢复（2026-09-02 补） |
| `vote_distribution_sum_gt_105` | 0 | 吞题哨兵：合计远超 100 即说明题头切分漏切（#252 曾达 200%）。阈值留 5% 舍入余量 —— #483 的 53+33+15=101 是各项独立四舍五入的正常题，曾被 >100 误报 |
| `stem_contains_gap_noise` | 1 | #321 —— stem 截断混入噪声（连续大段空白；双问句判据误报 125 道已弃用） |
| `lambda_concurrency_term_ambiguous_*` | 8 | #175/#379/#516/#573/#597/#600/#720/#807 —— provisioned 与 reserved 的中译不统一且串用，见上方撞词第三例 |
| `suspect_serverless_mistranslation_*` | 2 | #511 实锤（Serverless→「按需」）、#93 副词用法误报 |
| `choice_a_missing` | 1 | #868 —— 源 PDF 缺选项 A（B/C/D 齐全）。**曾是解析 bug**：起点只认 A，缺 A 就把 B/C/D 全丢进题干，整题变成「0 选项不可用」；改为退而认首个选项字母后救回 3 个可用选项 |
| `stem_references_missing_block` | 5 | #96/#423/#429/#477/#494 —— 题干说「以下 JSON/策略」但正文是图片没抓到。比冒号启发式更一般：引用在题干**中部**时冒号判据失效 |
| `stem_missing_question` | 6 | #96/#232/#460/#467/#473/#868 —— SAA 题干必以设问句收尾，末尾 30 字无问号即被截断。**零散分布，非批量排版问题**（曾假设是批量丢失，全库验证否定） |
| `choice_labels_not_contiguous*` | 3 | #125/#423/#756 —— 标签非从 A 起连续，说明某选项被吞并。**零误报信号**：看结构而非长度，与被弃用的「超长合并选项」判据（误报 33 处）形成对照 |

留给 ② 的 AI 环节或人工处理。

### 输出格式

```json
{
  "bank": { "slug", "name", "locale", "kind", "source", "extracted_at" },
  "questions": [{
    "no": 12,
    "kind": "single",
    "pick": 1,
    "stem": "...",
    "choices": [{"label": "A", "body": "..."}],
    "claims": [
      {"source": "bank_label", "answer": "C"},
      {"source": "community_vote", "answer": "A", "confidence": 80,
       "distribution": {"A": 80, "C": 20}}
    ],
    "warnings": []
  }]
}
```

---

## ② AI 富化（P1，待办）

### 一次调用产出全部，不分三遍

三种标签 + 答案裁决 + 解析**必须在同一次调用里完成**。分三遍跑 1011 道题 = 三倍成本和三倍延迟，且三次结果可能互相矛盾。

### 输出契约

每片一个文件，`items` 数组的元素形如：

```json
{
  "no": 26,
  "verdict": "B",
  "confidence": "high",
  "reasoning": "为什么是 B；以及题库标注的 A 错在哪",
  "explanation": "完整解析，面向学习者",
  "domain": 3,
  "services": ["S3", "CloudFront"],
  "concepts": ["cache/cdn"]
}
```

- `verdict` 必须是选项中存在的字母，升序不重复，长度 == `pick`
- `concepts` 为 **kebab-case 通用架构概念**，优先取 `enrich_spec.json` 的 51 个种子词
- `domain` 为 SAA-C03 官方 4 域之一
- 题目本身有缺陷时（缺选项等）`verdict` 留空、`data_issue` 写明原因，**不要硬编答案**

> **concept 词表决策（2026-09-01）**：曾考虑限定在 atlantis 现有路径集合内，已否决 ——
> atlantis 那 114 篇主题是 Linux / K8s / 基建，AWS 架构概念覆盖很薄，
> 强制限定会导致大量题 `concepts` 为空，第三个标签维度形同虚设。
> concept 按设计是**全局共享**的，atlantis 只是它的消费方之一，不该反过来当约束源。
> 代价：P5 迁入 atlantis 时需要一轮人工归并。

### SAA-C03 官方考纲域

| # | 域 | 占比 |
|---|---|---|
| 1 | Design Secure Architectures | 30% |
| 2 | Design Resilient Architectures | 26% |
| 3 | Design High-Performing Architectures | 24% |
| 4 | Design Cost-Optimized Architectures | 20% |

### 执行方式：Claude Code 订阅额度 + 分片续跑

~~Claude Batches API~~ —— **已否决**。评估过：分层跑（Sonnet 低档 + Opus 高档）
约 $16，其中 thinking token 是成本主导项。但用户的 Max 订阅额度本就闲置，
这笔钱没有花的理由。

改为**由 Claude Code 会话逐片生成**。代价是跨多次限额周期、可能换会话，
所以 `pipeline/core/enrich.py` 的核心职责是**让中断可恢复**：

```
data/<bank>/enriched/NNNN-NNNN.json     一片 25 题，41 片
```

- **分片文件存在即完成** —— 不维护单独的进度清单。
  manifest 与实际文件不同步是这类流程最经典的坑，直接不给它机会
- 每片独立校验，坏片删掉重跑，不影响其他片
- 换会话接手：读 `pipeline/banks/<bank>/enrich_spec.md` + 跑 `status`，立刻知道进度

```bash
enrich.py status <bank>   # 进度
enrich.py next   <bank>   # 取下一片题目（喂给解答会话）
enrich.py check  <bank>   # 校验
enrich.py merge  <bank>   # 合并 → enriched.json（不进 git，可重建）
```

题库特有的约束（考纲域 / concept 种子 / 服务命名）在
`pipeline/banks/<bank>/enrich_spec.json`，**core 零题库特有逻辑**。

### 两条方法论（2026-09-02，与富化侧协作中沉淀）

- **校验解析器的手段不能与解析器共享同一套模式。** 中文 grep 验证中文正则 =
  盲区里互相印证。打破盲区靠的是**独立信号**（这次是投票和 200% 这个数值异常）。
  富化侧的对应纪律：凡「数据自洽但数值不合常理」一律上报，不自行消化
- **社区共识度取首选项的原始百分比，不做归一化。** 源站 的 distribution
  是长尾截断的（合计常在 79-89%），A(45%)/B(20%) 归一化会虚标成 69% 的高共识，
  把真正的低共识题漏出筛网

### 系统性译文缺陷：三例撞词（本素材最主要的语义缺陷模式）

结构层脏点（缺选项、错切、图片丢失）靠解析器检测即可；**语义层的译文撞词只有精读才能发现**，
且危害递增。三例都由富化侧在做题时撞见、再回头做全库检索定性：

| # | 英文原词 | 误译成 | 危害 | 检测 |
|---|---|---|---|---|
| 1 | Spot Instances | 按需实例（撞 On-Demand） | 同题选项失去区分度，#128 四个选项两两相同 | `duplicate_choice_bodies_*` |
| 2 | Aurora Serverless | 按需 Aurora（再撞 On-Demand） | #511 社区 59/41 分裂 —— 读中译版看不出 C 是 Serverless | `suspect_serverless_mistranslation_*` |
| 3 | provisioned / reserved concurrency | 译法四选一、且互相串用 | **跨题矛盾**：#516 教「预留并发不消冷启动」，#597 却用它消冷启动 | `lambda_concurrency_term_ambiguous_*` |

第三例危害最大：前两例是单点误译，它是**互相矛盾的判据** —— 会摧毁学习者刚建立的正确认知。
完整映射表与逐题处理约定见 `pipeline/banks/aws-saa-c03/enrich_spec.md`。

**换题库时的启示**：机翻题库的术语撞词集中在「同一英文概念有多个中文习惯译法」
与「不同英文概念共享一个中文词」两类，值得在 P0 阶段就为该领域的核心术语建一张
对照表并全库检索，而不是等做题时逐个撞见。

### 质量门禁

```
verdict 合法率 = 100%           不合法的回炉重跑
concepts 路径存在率 = 100%      编造的路径视为失败
340 道分歧题必须有 reasoning     这是本项目的核心价值
低共识题(社区<60%) 单独 review   47 道，人工抽查
```

---

## ③ 导入（P2，待办）

```bash
python3 pipeline/core/load.py data/aws-saa-c03/enriched.json --dsn "$MELETE_DB_DSN"
```

- **幂等**：以 `(bank.slug, question.external_no)` 为业务键 upsert，可反复跑
- **分批提交**：每批 ≤ 200 道，避开 TiDB 单事务 100 MB 上限
- **不用外键**，关系完整性在导入器里校验
- 标签先 upsert 到 `tag`，再建 `question_tag` 关联

⚠️ **P2 之前必须拿 TiDB 跑一次完整导入**，确认 schema 与批量行为两边一致。

---

## 换题库时要做什么

1. 新建 `pipeline/banks/<slug>/parse.py`，输出同一份 `questions.json` 契约
2. `pipeline/core/` 的 ② ③ 段**不需要改**
3. 题库特有的 `domain` 标签定义写进 `bank.meta`
4. `concept` 标签是全局的，新题库会自动与既有题目产生关联
