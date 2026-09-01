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

## ① 解析（已完成）

```bash
pdftotext -layout "$MELETE_SAA_PDF" /tmp/saa-c03.txt
python3 pipeline/banks/aws-saa-c03/parse.py /tmp/saa-c03.txt data/aws-saa-c03/questions.json
```

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

### 已修的两个 bug（换题库时容易重犯）

1. `ANS_LINE` 正则忘了 `re.M`，导致 `(.*)$` 只在「正确答案」恰为最后一行时匹配
   → `bank_label` 只抽出 104/1011 条
2. 西里尔字母混入拉丁选项字母，需 `str.maketrans` 归一

### 质量门禁

```
零告警率 ≥ 95%          实测 97.9% (990/1011)
bank_label 覆盖 = 100%  实测 1011/1011
多选题数与源文档一致     实测 121（源文档 108 两项 + 13 三项 = 121）
```

`warnings` 字段随每道题带进 JSON，**不静默丢弃**。当前 21 道有告警，全部是题库自身脏点：

| 告警 | 数量 | 性质 |
|---|---|---|
| `vote_header_without_pairs` | 15 | 有投票表头但无数据 |
| `*_answer_X_not_in_choices` | 7 | 答案字母不在选项里 |
| `no_options` / `only_0_choices` | 2 | 整题缺选项 |
| `choice_labels_disordered` | 1 | 选项字母乱序 |
| `*_len1_expect2` | 2 | 多选题只给了一个答案 |

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

```json
{
  "verdict": "B",
  "confidence": "high",
  "reasoning": "为什么是 B；以及题库标注的 A 错在哪",
  "explanation": "完整解析，面向学习者",
  "domain": 3,
  "services": ["S3", "CloudFront"],
  "concepts": ["cache/cdn"]
}
```

- `verdict` 必须是选项中存在的字母，且长度 == `pick`
- `concepts` 取值需落在 atlantis 现有路径集合内，取不到就留空 —— **不要编造路径**
- `domain` 为 SAA-C03 官方 4 域之一

### SAA-C03 官方考纲域

| # | 域 | 占比 |
|---|---|---|
| 1 | Design Secure Architectures | 30% |
| 2 | Design Resilient Architectures | 26% |
| 3 | Design High-Performing Architectures | 24% |
| 4 | Design Cost-Optimized Architectures | 20% |

### 执行方式

**Claude Batches API**（异步批处理，5 折），不走 Max 订阅配额。

理由：这是一次性大批量作业，跟「用闲置订阅额度做常态化小任务」是两码事。
1011 道题 × (输入 ~800 tok + 输出 ~600 tok)，预估 **$5–15**。

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
