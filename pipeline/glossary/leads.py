#!/usr/bin/env python3
"""
用语集：标出每个分类的「主条目」（分类本身那一条）—— assemble.py 之后、load_glossary.py 之前跑。

    python3 pipeline/glossary/leads.py <bank-slug>

为什么要有（2026-10-08 用户反馈「应该先解释 EC2 是什么」）：
  EC2 分类里本来就有「Amazon EC2」这一条，但它和「部分预付」「付款选项」平排在一起，看不出谁是主干。
  分类页把主条目置顶成带释义的卡片；load_glossary.py 再按题目的服务标签给它补出题关联。

做三件事，结果写回 $MELETE_DATA_ROOT/<bank>/glossary.json（每条术语带 "lead": true/false）：
  1. 并入 glossary_extra.json —— 第 1 遍逐题抽词时没抽出服务本身的分类，手工补的主条目
  2. 自动匹配：去掉 Amazon / AWS 前缀与括号注释后，与分类同名的那条（S3 ↔ Amazon S3）
  3. 自动匹配不到的，查 lead_overrides.json（ALB ↔ Application Load Balancer 这类缩写对全称）

⚠️ 幂等：每次从头重算 lead；extra 里的术语已在文件里就跳过（重跑 assemble.py 会冲掉它们，再跑一遍本脚本即可）。
IPA 的分类是中分類，不是一个术语 ⇒ 不会有匹配，全部 lead=false，这是预期。
"""
import json
import os
import re
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "core"))
from paths import data_root  # noqa: E402

HERE = os.path.dirname(__file__)
GENERAL = "General"


def bare(name: str) -> str:
    """S3 / Amazon S3 / Amazon S3（简单存储服务）→ s3"""
    name = re.sub(r"\(.*?\)|（.*?）", "", name).strip()
    return re.sub(r"^(Amazon|AWS)\s+", "", name).strip().lower()


def main() -> None:
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    bank = sys.argv[1]
    path = os.path.join(data_root(), bank, "glossary.json")
    doc = json.load(open(path, encoding="utf-8"))
    terms = doc["terms"]

    extra_path = os.path.join(data_root(), bank, "glossary_extra.json")
    if os.path.exists(extra_path):
        have = {t["slug"] for t in terms}
        added = [t for t in json.load(open(extra_path, encoding="utf-8"))["terms"] if t["slug"] not in have]
        terms.extend(added)
        print(f"✓ 并入补充条目 {len(added)}")

    overrides = json.load(open(os.path.join(HERE, "lead_overrides.json"), encoding="utf-8"))["leads"]
    by_cat: dict[str, list[dict]] = {}
    for t in terms:
        t["lead"] = False
        by_cat.setdefault(t["category"], []).append(t)

    missing = []
    for cat, members in sorted(by_cat.items()):
        if cat == GENERAL:
            continue
        lead = next((t for t in members if bare(t["slug"]) == bare(cat)), None)
        if lead is None and cat in overrides:
            lead = next((t for t in members if t["slug"] == overrides[cat]), None)
        if lead is None:
            missing.append(cat)
            continue
        lead["lead"] = True

    json.dump(doc, open(path, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    n = sum(1 for t in terms if t["lead"])
    print(f"✓ 主条目 {n} / {len(by_cat) - (GENERAL in by_cat)} 个分类")
    if missing:
        print(f"⚠ 没有主条目的分类 {len(missing)}：{missing[:20]}")


if __name__ == "__main__":
    main()
