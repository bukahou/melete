#!/usr/bin/env python3
"""
Melete 数据导入管道 — SAP-C02 解析器（英文原版 + 社区讨论）

输入：pdftotext -layout 产出的纯文本
输出：questions.json（与 aws-saa-c03 完全相同的契约）

与 SAA-C03 解析器的关系
---------------------
这是**第二次实现同一份契约**，刻意从零写而非复制粘贴 ——
用它检验 questions.json 的格式究竟是通用的，还是长满了 SAA 的特征。

结论：契约本身通用，两者只在**素材形态**上不同：

| | SAA-C03 中文版 | SAP-C02 英文版 |
|---|---|---|
| 题头 | `问题 #N` | `Question #N` |
| 答案行 | `正确答案：X` | `Correct Answer: X` |
| 投票区 | `社区投票分配` `A（80%）` | `Community vote distribution` `A (86%)` |
| 社区讨论 | 无 | **每题几十条，必须剥离** |
| 译文缺陷 | 四类系统性缺陷 | 无（原文即英文） |

**唯一的新难点是剥离讨论区**：讨论里也有 `A.` 开头的行
（如「A. Correct answer. Source: ...」），若不切干净会被当成选项。
"""
import json
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))  # 仓库根
from pipeline.core.textrepair import restore_ligatures  # noqa: E402
import sys
from datetime import datetime, timezone

# 题头：Question #N，右侧常跟 "Topic 1" 页眉
Q_SPLIT = re.compile(r"^\s*\x0c?\s*Question\s*#(\d+)\b", re.M)

# 页眉/分页噪声
NOISE = re.compile(r"(Topic\s*\d+|\x0c)")

# 选项行：字母 + 点，允许缩进
OPT_START = re.compile(r"^\s{0,8}([A-F])\.\s*(.*)$")

ANS_LINE = re.compile(r"Correct Answer\s*:\s*([A-F]+)", re.M)
VOTE_PAIR = re.compile(r"([A-F]+)\s*\(\s*(\d+)\s*%\s*\)")
PICK_N = re.compile(r"\(Choose\s+(two|three|four|2|3|4)[.)]?", re.I)
# ★ 源站 抓取侧的缺陷：正文里形如「<字母>. 」的片段被当成选项标签剥掉，
# 于是末字母落在 A–F 的缩写 + 句号 + 空格被吞掉三个字符：
#     "VPC. Create" → "VPCreate"    "ALB. Turn" → "ALTurn"    "MFA. Configure" → "MFConfigure"
# 全库 32 题（6.0%），parse.py 原本零告警 —— 属静默缺陷。
# 修复是确定性的：缩写表 × 后接大写动词，无歧义。IPSet 是 WAF 真实资源名，不在表里。
ABBREV_EATEN = {"VP": "VPC", "AL": "ALB", "NL": "NLB", "MF": "MFA"}
EATEN_VERBS = ("Create|Update|Attach|Associate|Configure|Con gure|Add|Delete|Enable|Ensure|Use|"
               "Provision|Point|Set|Modify|Move|Run|Select|Store|Specify|Deploy|Launch|Migrate|"
               "Assign|Register|Apply|Change|Grant|Turn|Install|Connect|Replace|Remove|Verify|"
               "Activate|Route|Place|Put|Send|Allow|Restrict|Block|Choose|Increase|Reduce|Scale|"
               "Encrypt|Host|Write|Read|Import|Export|Publish|Subscribe|Invoke|Call|Include|"
               "Require|Define|De ne|Build|Make|Open|Give|Keep|Test|Start|Stop|Disable|Provide|Order|Accept")
EATEN_LABEL = re.compile(r"\b(" + "|".join(ABBREV_EATEN) + r")(" + EATEN_VERBS + r")\b")


def repair_text(text: str) -> tuple[str, list[str]]:
    """两类确定性修复；返回 (文本, 登记)。登记进 repairs 字段而非 warnings —— 已修好的不需要人看。"""
    repairs: list[str] = []

    def fix_eaten(m: re.Match) -> str:
        fixed = f"{ABBREV_EATEN[m.group(1)]}. {m.group(2)}"
        repairs.append(f"{m.group(0)}→{fixed}")
        return fixed

    text = EATEN_LABEL.sub(fix_eaten, text)
    text, lig = restore_ligatures(text)
    return text, repairs + lig


EN_NUM = {"two": 2, "three": 3, "four": 4, "2": 2, "3": 3, "4": 4}

# ★ 讨论区起点：源站 的评论都以「用户名 + 徽章/相对时间」开头，例如
#     robertohyena Highly Voted  1 year, 7 months ago
#     TonytheTiger  3 weeks, 3 days ago
#     someone Most Recent  2 days ago
# 题目区到此为止。这是本解析器唯一比 SAA 版多出来的逻辑。
DISCUSSION_START = re.compile(
    r"^\s{2,}\S+.*?(Highly Voted|Most Recent|\d+\s+(?:year|month|week|day|hour|minute)s?\s*,?.*?ago)",
    re.M,
)


def clean(line: str) -> str:
    return NOISE.sub("", line).rstrip()


def split_core(body: str) -> tuple[str, bool]:
    """把一题切成「题目区」与「讨论区」，只返回题目区。"""
    m = DISCUSSION_START.search(body)
    return (body[: m.start()], True) if m else (body, False)


def parse_block(no: int, body: str) -> dict:
    warnings: list[str] = []
    core, had_discussion = split_core(body)
    if not had_discussion:
        # 每题都该有讨论区；没有说明切分可能出错，或该题确实无人评论
        warnings.append("no_discussion_section")

    lines = [clean(l) for l in core.split("\n")]

    # ---- 定位选项区与答案行 ----
    opt_at = ans_at = None
    first_opt = None
    for i, l in enumerate(lines):
        m = OPT_START.match(l)
        if m:
            if first_opt is None:
                first_opt = i
            if opt_at is None and m.group(1) == "A":
                opt_at = i
        if ans_at is None and "Correct Answer" in l:
            ans_at = i

    # 缺 A 时退而认第一个选项字母（SAA 版踩过：#868 因缺 A 导致整题作废）
    if opt_at is None and first_opt is not None:
        warnings.append("choice_a_missing")
        opt_at = first_opt
    if opt_at is None:
        warnings.append("no_options")
        opt_at = len(lines)
    if ans_at is None:
        warnings.append("no_answer_line")
        ans_at = len(lines)
    if ans_at < opt_at:
        warnings.append("answer_before_options")

    stem = " ".join(x.strip() for x in lines[:opt_at] if x.strip())
    if stem.rstrip().endswith((":", "：")):
        warnings.append("stem_ends_with_colon_missing_block")

    # ---- 选项：字母行开启，缩进续行合并 ----
    choices, cur = [], None
    for l in lines[opt_at:ans_at]:
        m = OPT_START.match(l)
        if m and (not choices or m.group(1) > choices[-1]["label"]):
            if cur:
                choices.append(cur)
            cur = {"label": m.group(1), "body": m.group(2).strip()}
        elif cur is not None and l.strip():
            cur["body"] += " " + l.strip()
    if cur:
        choices.append(cur)

    # ---- 文本层确定性修复（连字丢失 / 被吞的缩写句点）----
    repairs: list[str] = []
    stem, r = repair_text(stem)
    repairs += r
    for ch in choices:
        ch["body"], r = repair_text(ch["body"])
        repairs += r

    # 结构性缺陷检测（判据与 SAA 版一致，此处复用同一套告警名）
    if choices:
        expect = [chr(ord("A") + i) for i in range(len(choices))]
        if [c["label"] for c in choices] != expect:
            missing = [x for x in expect if x not in {c["label"] for c in choices}]
            warnings.append("choice_labels_not_contiguous"
                            + (f"_missing_{''.join(missing)}" if missing else ""))
        seen: dict[str, str] = {}
        for ch in choices:
            key = re.sub(r"\s+", "", ch["body"]).lower()
            if key in seen:
                warnings.append(f"duplicate_choice_bodies_{seen[key]}{ch['label']}")
            else:
                seen[key] = ch["label"]
    if 0 < len(choices) < 4:
        warnings.append(f"only_{len(choices)}_choices")

    # ---- 答案主张 ----
    tail = "\n".join(lines[ans_at:])
    claims = []
    labels = {c["label"] for c in choices}

    am = ANS_LINE.search(tail)
    if am:
        ans = "".join(sorted(set(am.group(1))))
        claims.append({"source": "bank_label", "answer": ans})
        for ch in ans:
            if choices and ch not in labels:
                warnings.append(f"bank_label_answer_{ch}_not_in_choices")

    if "Community vote distribution" in tail:
        seg = tail.split("Community vote distribution", 1)[1]
        pairs = VOTE_PAIR.findall(seg[:400])
        if pairs:
            dist = {}
            for letters, pct in pairs:
                dist["".join(sorted(set(letters)))] = int(pct)
            top = max(dist.items(), key=lambda kv: kv[1])
            # 合计远超 100 说明吞了下一题的投票行（SAA 版实测过：漏切题头导致 200%）
            if sum(dist.values()) > 105:
                warnings.append("vote_distribution_sum_gt_105")
            claims.append({
                "source": "community_vote",
                "answer": top[0],
                "confidence": top[1],
                "distribution": dist,
            })
            for ch in top[0]:
                if choices and ch not in labels:
                    warnings.append(f"community_vote_answer_{ch}_not_in_choices")
        else:
            warnings.append("vote_header_without_pairs")

    # ---- 单选/多选 ----
    pm = PICK_N.search(stem)
    pick = EN_NUM.get(pm.group(1).lower(), 2) if pm else 1
    kind = "multi" if pick > 1 else "single"
    for c in claims:
        if len(c["answer"]) != pick:
            warnings.append(f"{c['source']}_len{len(c['answer'])}_expect{pick}")

    return {
        "no": no,
        "kind": kind,
        "pick": pick,
        "stem": stem,
        "choices": choices,
        "claims": claims,
        "warnings": warnings,
        "repairs": repairs,
    }


def main(src: str, dst: str) -> None:
    raw = open(src, encoding="utf-8", errors="replace").read()
    parts = Q_SPLIT.split(raw)
    questions = [parse_block(int(parts[i]), parts[i + 1]) for i in range(1, len(parts) - 1, 2)]

    doc = {
        "bank": {
            "slug": "aws-sap-c02",
            "name": "AWS Certified Solutions Architect – Professional (SAP-C02)",
            "locale": "en",
            "kind": "cert",
            "source": "AWS Certified Solutions Architect - Professional SAP-C02 529题.pdf",
            "extracted_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        },
        "questions": questions,
    }
    json.dump(doc, open(dst, "w", encoding="utf-8"), ensure_ascii=False, indent=1)

    # ---- 质量报告 ----
    from collections import Counter
    nos = sorted(q["no"] for q in questions)
    gaps = sorted(set(range(nos[0], nos[-1] + 1)) - set(nos))
    warn = Counter(w.split("_missing")[0] for q in questions for w in q["warnings"])
    clean_n = sum(1 for q in questions if not q["warnings"])
    repaired = sum(1 for q in questions if q["repairs"])
    n_repairs = sum(len(q["repairs"]) for q in questions)
    print(f"文本修复        {repaired} 题 / {n_repairs} 处（连字还原 + 被吞缩写句点；登记在 repairs 字段）")
    print(f"题目总数        {len(questions)}  (题号 {nos[0]}–{nos[-1]})")
    if gaps:
        print(f"题号缺失        {gaps}  ← 需与源文档核对是否题库自身缺号")
    print(f"零告警          {clean_n}  ({clean_n/len(questions)*100:.1f}%)")
    print(f"有告警          {len(questions)-clean_n}")
    print(f"多选题          {sum(1 for q in questions if q['kind']=='multi')}")
    src_cnt = Counter(c["source"] for q in questions for c in q["claims"])
    print("\n答案主张来源分布:")
    for k, v in src_cnt.most_common():
        print(f"  {k:<18} {v}")
    if warn:
        print("\n告警明细:")
        for k, v in warn.most_common(15):
            print(f"  {k:<42} {v}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
