# SAA-C03 富化任务规格

> 这份文档是**给解答会话读的任务说明**。另开一个 Claude Code 会话，
> 让它读本文件，即可接手继续跑，不需要任何额外交接。

## 你的任务

给 `data/aws-saa-c03/questions.json` 里的 1011 道 AWS SAA-C03 题目补上四样东西：

1. **裁决（verdict）** —— 这道题到底选什么
2. **推理（reasoning）** —— 为什么是它，以及**对立方错在哪**
3. **解析（explanation）** —— 面向学习者的完整讲解
4. **三维标签** —— `domain`（考纲域）/ `services`（AWS 服务）/ `concepts`（底层概念）

### 为什么裁决是核心

这个题库**884 道有对照数据的题里，38% 的题库标注答案与社区投票不一致**，
存在 98% 社区一致反对标注答案的情况。**照搬题库标注会背错三分之一。**

所以你的裁决不是走过场。遇到分歧题，你要独立判断谁对，并在 `reasoning` 里
**明确写出错的那一方错在哪**——这段话本身就是最有价值的学习材料。

## 工作循环

```bash
python3 pipeline/core/enrich.py status aws-saa-c03   # 看进度
python3 pipeline/core/enrich.py next   aws-saa-c03   # 取下一片的题目（25 道）
# ↑ 输出里有「写入目标」路径，把结果写进那个文件
python3 pipeline/core/enrich.py check  aws-saa-c03   # 校验，必须全绿
# 重复，直到 status 显示 41 片全部完成
python3 pipeline/core/enrich.py merge  aws-saa-c03   # 最后合并成 enriched.json
```

**一次跑一片（25 道）。** 分片文件存在即视为完成，所以：

- 额度用完中断了 → 下次直接 `next`，它会跳过已完成的片，不会重复
- 某片质量不满意 → 删掉那个文件重跑，不影响其他片
- 换会话接手 → 读本文件 + 跑 `status`，立刻知道进度

⚠️ **写完一片必须跑 `check`。** 校验不过的片在 `merge` 时会被拒绝。

## 输出格式

每片一个 JSON 文件，`data/aws-saa-c03/enriched/NNNN-NNNN.json`：

```json
{
  "bank": "aws-saa-c03",
  "range": [26, 50],
  "model": "claude-opus-5 (Claude Code 订阅额度)",
  "generated_at": "YYYY-MM-DD",
  "items": [
    {
      "no": 26,
      "verdict": "B",
      "confidence": "high",
      "reasoning": "为什么是 B，以及题库标注的 A 错在哪",
      "explanation": "面向学习者的完整解析",
      "domain": 3,
      "services": ["S3", "CloudFront"],
      "concepts": ["cdn", "caching"],
      "data_issue": null
    }
  ]
}
```

字段约束（`enrich.py check` 会逐条验）：

| 字段 | 约束 |
|---|---|
| `verdict` | 字母必须在该题选项里；**升序、不重复**；长度 == 该题 `pick` |
| `confidence` | `high` / `medium` / `low` |
| `reasoning` | 非空；**分歧题要求 ≥80 字**，必须说明对立方错在哪 |
| `explanation` | 非空 |
| `domain` | `1`-`4`，见 `enrich_spec.json` |
| `services` | 非空数组，官方短名（`S3` 不是 `Amazon S3`），见 `enrich_spec.json` |
| `concepts` | 非空数组，**kebab-case**，优先用 `enrich_spec.json` 的种子词 |
| `data_issue` | 正常为 `null`；题目**结构性不可判**时写明原因，**此时 `verdict` 留空字符串** |
| `notes`（可选） | **advisory 级**：题目有瑕疵但答案仍可判定时的登记处（如译文撞词），verdict 照给 |

### 关于 concepts

`enrich_spec.json` 里有 51 个种子词，**优先从里面选**。种子词覆盖不到时可以新增，
但必须是 kebab-case 的通用架构概念（不是 AWS 服务名）。

判据：`services` 回答「考哪个 AWS 服务」，`concepts` 回答「考哪条底层原理」。
`concepts` 是跨题库共享的，所以它应该是「换成 Azure 也成立」的那种概念。

### 关于 data_issue

题库有 21 道解析告警题，部分题目本身有缺陷（缺选项、答案字母不在选项里）。
遇到这种题**不要硬编一个答案**，把 `verdict` 留空、在 `data_issue` 里说明情况。

## 质量基准

**`data/aws-saa-c03/enriched/0001-0025.json` 是样板，动笔前先读它。**
那一片有 10 道分歧题，涵盖了大部分需要处理的情况。

要点：

- `reasoning` 直接对线：「题库标注 X 是错的，N% 的社区投票选 Y 才对」，然后说清楚为什么
- `explanation` 要有**可迁移的判据**，不是复述答案。好的解析给的是
  「看到 A 和 B 同时出现就选 C」这类能用到下一道题上的规则
- 善用对照表。服务边界辨析（SNS vs SQS、CloudFront vs Global Accelerator、
  Secrets Manager vs Parameter Store）用表格比散文清楚得多
- 遇到选项只差一句话的题，明确指出「答案藏在那句差异里」并逐字对齐
- 社区共识低于 60% 的题，`confidence` 给 `medium`，并在解析里说明争议在哪

## 已知的题库特征

- 题号 **905–1011 共 104 道无社区投票数据**，只有题库标注一个来源。
  这些题你的裁决是唯一的第二意见，格外认真对待
- 存在西里尔字母混入拉丁选项字母的脏点，解析器已归一
- 题目是中文的。`reasoning` / `explanation` 用中文，
  `services` / `concepts` 用英文（跨语言稳定，且要进数据库当标签）

## 已知系统性译文缺陷：Spot → 误译「按需实例」

「Spot Instances（竞价实例）」多处被机翻成「按需实例」，与真正的 On-Demand 撞词。
全库检索（2026-09-01）已定性：

- 实锤：#84（A≡C）、#128（A≡C、B≡D，结构性）、#140、#444（D 选项「使用按需实例
  而非按需实例」同句矛盾）
- 疑似（跑到时注意）：**#230 的 D、#770 的 A** —— 无法与原文比对确证，
  若答案不受影响，verdict 照给 + notes 登记
- 误报排除（真按需语境）：#383、#585、#940、#1013

处理约定：结构性不可判 → data_issue；答案不受影响 → verdict 照给 + notes。

### 第二例：Serverless → 误译「按需」

同一撞词模式的另一面：**Aurora Serverless 被译成「按需 Aurora」**，与 On-Demand 再次撞词。

- 实锤 **#511 选项 C**「按需 Amazon Aurora 兼容 PostgreSQL 的数据库」= Aurora Serverless。
  源 PDF 原文即如此，是中译版自身缺陷。这直接解释了该题社区 59/41 的分裂 ——
  读中译版的人看不出 C 指的是 Serverless
- 判据依据：全库 16 处 Aurora Serverless 均正常译作「Serverless/无服务器」，仅此一处反常
- #93「按需使用数据库克隆功能」是副词用法，非误译，属检测器的已知提示项
- 告警：`suspect_serverless_mistranslation_X`（提示性，需人工分辨副词用法）

### 第三例：Lambda 并发术语译法不统一（危害最大）

前两例是**单点误译**，这一例是**跨题矛盾** —— 危害更大，因为它会摧毁学习者刚建立的判据。

| 英文原词 | 作用 | 本题库的中译 |
|---|---|---|
| **provisioned** concurrency | 预热执行环境，**消除冷启动** | 预置并发 / 预配置并发 / 预配置并发性 / 预置并发数（四种） |
| **reserved** concurrency | 只划并发配额上限，**对冷启动无效** | 预留并发量 |

涉及的 8 道题（`lambda_concurrency_term_ambiguous_X` 告警）：

- **#516** —— 两个概念在同题内作为分水岭出现（B 预配置并发性 vs D 预留并发量），
  答案正是靠这个区分定的。**这道题的译法是对的**
- **#597** —— 正确选项写「增加 Lambda 预留并发量」，但题意是每天上班前预热环境消冷启动，
  **实指 provisioned**。与 #516 直接矛盾
- **#573** —— 选项 A「预留并发量」在问冷启动的题里出现；D「Lambda 快速启动」是 SnapStart
  的非标准译法。该题正确答案是 C（增加内存），A/D 均为干扰项，故歧义不影响裁决
- 其余 #175 / #379 / #600 / #720 / #807 逐题分辨

**处理约定**：遇到该告警时，**按题意判断实指哪一个**（问冷启动 → provisioned；
问配额/节流 → reserved），verdict 照给，并用 notes 写明「中译作 X，实指 Y」。
