#!/usr/bin/env python3
"""
用语集生成管道 · 组装（P9 第 6 步，裁决 #5「从题目里抽」）。

三遍产出 → $MELETE_DATA_ROOT/<bank>/glossary.json（config 私有仓），再由 pipeline/core/load_glossary.py 入库。

    python3 pipeline/glossary/assemble.py <work-dir>

<work-dir> 的布局（三遍都由子 agent 产出，规则见同目录 pass3_rules.md 与各遍的提示词）：
    pass1_out/<bank>-NN.json   第 1 遍：逐题抽出的术语（原始写法）  → 出题关联
    pass2_out/<bank>.json      第 2 遍：原始写法 → 正式名称（null = 剔除）+ 分组
    pass3_out/<bank>-NN.json   第 3 遍：每个正式名称写一次 名称 / 读音 / 释义

⛔ 任何一遍对不上（缺词、多词、语言字段不对）就停，⛔ 不带病写出 —— 用语集是给学习者看的成品。
"""
import glob
import json
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "core"))
from paths import data_root  # noqa: E402

BANKS = ["aws-saa-c03", "aws-sap-c02", "ipa-ip"]


def assemble(work: str, bank: str) -> dict:
    p2 = json.load(open(f"{work}/pass2_out/{bank}.json", encoding="utf-8"))
    canon, category = p2["map"], {t["slug"]: t["category"] for t in p2["terms"]}

    entries = {}
    for p in sorted(glob.glob(f"{work}/pass3_out/{bank}-*.json")):
        for e in json.load(open(p, encoding="utf-8"))["terms"]:
            entries[e["slug"]] = e
    missing = sorted(set(category) - set(entries))
    if missing:
        sys.exit(f"✗ {bank}: 第 3 遍缺 {len(missing)} 个术语的释义，例：{missing[:5]}")

    ipa = bank.startswith("ipa")
    terms, problems = [], []
    for slug in sorted(category):
        e = entries[slug]
        langs = {"ja"} if ipa else {"zh", "ja"}
        if set(e.get("names", {})) != langs or set(e.get("definition", {})) != langs:
            problems.append(f"{slug}: 语言字段应为 {sorted(langs)}")
        if ipa and not e.get("reading"):
            problems.append(f"{slug}: IPA 缺读音")
        terms.append({"slug": slug, "names": e["names"], "reading": e.get("reading"),
                      "definition": e["definition"], "category": category[slug]})
    if problems:
        sys.exit(f"✗ {bank}: {len(problems)} 处不合格，例：{problems[:5]}")

    links = []
    for p in sorted(glob.glob(f"{work}/pass1_out/{bank}-*.json")):
        for q in json.load(open(p, encoding="utf-8"))["questions"]:
            slugs = sorted({canon[r] for r in q["terms"] if canon.get(r)})
            if slugs:
                links.append({"session": q.get("session", ""), "no": q["no"], "terms": slugs})
    return {"bank": bank, "terms": terms, "links": links}


def main() -> None:
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    work = sys.argv[1]
    for bank in BANKS:
        doc = assemble(work, bank)
        out = os.path.join(data_root(), bank, "glossary.json")
        json.dump(doc, open(out, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
        n_links = sum(len(l["terms"]) for l in doc["links"])
        print(f"✓ {bank:12s} 术语 {len(doc['terms']):5d} · 关联题 {len(doc['links']):5d} · 关联 {n_links:6d} → {out}")


if __name__ == "__main__":
    main()
