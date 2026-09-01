#!/usr/bin/env python3
"""
Melete 数据导入管道 — 第一段：PDF 文本 → 结构化 JSON

输入：pdftotext -layout 产出的纯文本
输出：questions.json（稳定中间格式，可 review / 可进 git / 可 diff）

设计要点：
  · 答案不是单一字段，而是一组带来源的「主张」(claims)
  · 解析异常不静默丢弃，全部记入 warnings 并在报告中统计
"""
import json
import re
import sys
from datetime import datetime, timezone

# 页眉/页脚噪声：出现在题号行尾或独立成行
NOISE = re.compile(r"(主题\s*\d+|Topic\s*\d+|考试\s*[A-Z]|\x0c)")
Q_SPLIT = re.compile(r"^\s*\x0c?\s*问题\s*#(\d+)\b", re.M)
OPT_START = re.compile(r"^\s{0,6}([A-F])\.\s*(.*)$")
ANS_LINE = re.compile(r"正确答案\s*[：:]\s*([^\n]*)$", re.M)
# 西里尔字母混入拉丁选项字母（题库脏点）
CYRILLIC = str.maketrans({"А":"A","В":"B","С":"C","Е":"E","Ѕ":"S","Р":"P","Д":"D","Ғ":"F"})
VOTE_PAIR = re.compile(r"([A-F]+)\s*[（(]\s*(\d+)\s*%\s*[）)]")
PICK_N = re.compile(r"[（(]\s*选择(两|三|四|2|3|4|２|３)\s*[个项]?[。．.]?\s*[）)]")
CN_NUM = {"两": 2, "三": 3, "四": 4, "2": 2, "3": 3, "4": 4, "２": 2, "３": 3}


def clean(line: str) -> str:
    return NOISE.sub("", line).rstrip()


def parse_block(no: int, body: str) -> dict:
    warnings = []
    lines = [clean(l) for l in body.split("\n")]

    # ---- 切三段：题干 / 选项 / 答案 ----
    opt_at = ans_at = None
    for i, l in enumerate(lines):
        if opt_at is None and OPT_START.match(l) and OPT_START.match(l).group(1) == "A":
            opt_at = i
        if ans_at is None and "正确答案" in l:
            ans_at = i
    if opt_at is None:
        warnings.append("no_options")
        opt_at = len(lines)
    if ans_at is None:
        warnings.append("no_answer_line")
        ans_at = len(lines)
    if ans_at < opt_at:
        warnings.append("answer_before_options")

    stem = " ".join(x.strip() for x in lines[:opt_at] if x.strip())

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

    # ---- 答案主张 ----
    claims = []
    tail = "\n".join(lines[ans_at:])
    m = ANS_LINE.search(tail)
    if m:
        # 只取首个连续块：遇到 2 个以上空格即认为后面是社区徽章（冗余，丢弃）
        head = re.split(r"\s{2,}", m.group(1).translate(CYRILLIC).strip())[0]
        label = "".join(sorted(set(re.findall(r"[A-F]", head))))
        if label:
            claims.append({"source": "bank_label", "answer": label})
        else:
            warnings.append("empty_bank_label")

    if "社区投票分配" in tail:
        seg = tail.split("社区投票分配", 1)[1].translate(CYRILLIC)
        pairs = VOTE_PAIR.findall(seg[:400])
        if pairs:
            dist = {}
            for letters, pct in pairs:
                dist["".join(sorted(set(letters)))] = int(pct)
            top = max(dist.items(), key=lambda kv: kv[1])
            claims.append({
                "source": "community_vote",
                "answer": top[0],
                "confidence": top[1],
                "distribution": dist,
            })
        else:
            warnings.append("vote_header_without_pairs")

    # ---- 单选/多选 ----
    pm = PICK_N.search(stem)
    pick = CN_NUM.get(pm.group(1), 2) if pm else 1
    kind = "multi" if pick > 1 else "single"

    # ---- 一致性校验 ----
    labels = [c["label"] for c in choices]
    if labels != sorted(labels) or len(set(labels)) != len(labels):
        warnings.append("choice_labels_disordered")
    if len(choices) < 4:
        warnings.append(f"only_{len(choices)}_choices")
    for c in claims:
        if c["source"] in ("bank_label", "community_vote") and len(c["answer"]) != pick:
            warnings.append(f"{c['source']}_len{len(c['answer'])}_expect{pick}")
        for ch in c["answer"]:
            if ch not in labels:
                warnings.append(f"{c['source']}_answer_{ch}_not_in_choices")
    if not stem:
        warnings.append("empty_stem")

    return {
        "no": no,
        "kind": kind,
        "pick": pick,
        "stem": stem,
        "choices": choices,
        "claims": claims,
        "warnings": warnings,
    }


def main(src: str, dst: str) -> None:
    raw = open(src, encoding="utf-8").read()
    parts = Q_SPLIT.split(raw)
    questions = []
    for i in range(1, len(parts) - 1, 2):
        questions.append(parse_block(int(parts[i]), parts[i + 1]))

    doc = {
        "bank": {
            "slug": "aws-saa-c03",
            "name": "AWS Certified Solutions Architect – Associate (SAA-C03)",
            "locale": "zh",
            "kind": "cert",
            "source": "SAA-C03 中文 题目+答案 新.pdf",
            "extracted_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        },
        "questions": questions,
    }
    json.dump(doc, open(dst, "w", encoding="utf-8"), ensure_ascii=False, indent=1)

    # ---- 质量报告 ----
    from collections import Counter
    warn = Counter(w for q in questions for w in q["warnings"])
    clean_n = sum(1 for q in questions if not q["warnings"])
    print(f"题目总数        {len(questions)}")
    print(f"零告警          {clean_n}  ({clean_n/len(questions)*100:.1f}%)")
    print(f"有告警          {len(questions)-clean_n}")
    print(f"多选题          {sum(1 for q in questions if q['kind']=='multi')}")
    src_cnt = Counter(c["source"] for q in questions for c in q["claims"])
    print("\n答案主张来源分布:")
    for k, v in src_cnt.most_common():
        print(f"  {k:<18} {v}")
    print("\n告警明细:")
    for k, v in warn.most_common(15):
        print(f"  {k:<40} {v}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
