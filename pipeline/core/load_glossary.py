#!/usr/bin/env python3
"""
用语集导入（P9 第 6 步）：$MELETE_DATA_ROOT/<bank>/glossary.json → term / term_question。

    python3 pipeline/core/load_glossary.py <bank-slug> [--dsn <go-dsn>]

glossary.json 的形状（由生成管道产出，在 config 私有仓）：
    {
      "terms": [{"slug", "names": {"zh","ja"}, "reading"?, "definition": {"zh","ja"}, "category"}],
      "links": [{"session", "no", "terms": ["<slug>", ...]}]     # 每道题考到哪些术语
    }

⭐ 管道拥有整张术语表：文件里没有的术语会被删掉（连同它的关联）——
   重跑生成时术语可能被归并 / 剔除，纯 upsert 会留下幽灵条目（与 load.py 清理过期主张同理）。
⚠️ 题目按 (session, 原题号) 定位 —— 与 load.py 同一个键；题库里不存在的题跳过并报告。
"""
import argparse
import json
import os
import sys

import mysql.connector as mysql

sys.path.insert(0, os.path.dirname(__file__))
from load import BATCH, parse_go_dsn  # noqa: E402
from paths import data_root  # noqa: E402


def search_text(t: dict) -> str:
    """检索用小写拼接：正式名称 + 各语言名 + 读音。⛔ 不在 SQL 里拆 JSON（TiDB 纪律之四）。"""
    parts = [t["slug"], *t.get("names", {}).values(), t.get("reading") or ""]
    return " ".join(p for p in parts if p).lower()[:1024]


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("bank")
    ap.add_argument("--dsn", default=os.environ.get("MELETE_DB_DSN"), help="Go 风格 DSN，缺省取 MELETE_DB_DSN")
    args = ap.parse_args()
    if not args.dsn:
        sys.exit("✗ 未给 DSN（--dsn 或 MELETE_DB_DSN）")

    path = os.path.join(data_root(), args.bank, "glossary.json")
    doc = json.load(open(path, encoding="utf-8"))
    terms, links = doc["terms"], doc["links"]
    slugs = {t["slug"] for t in terms}
    if len(slugs) != len(terms):
        sys.exit("✗ glossary.json 里有重复的 slug —— 归并没做干净，⛔ 不导入")
    dangling = sorted({s for l in links for s in l["terms"]} - slugs)
    if dangling:
        sys.exit(f"✗ 关联里引用了不存在的术语 {len(dangling)} 个，例：{dangling[:5]}")

    conn = mysql.connect(**parse_go_dsn(args.dsn), charset="utf8mb4", autocommit=False)
    cur = conn.cursor()
    cur.execute("SELECT id FROM bank WHERE slug = %s", (args.bank,))
    row = cur.fetchone()
    if not row:
        sys.exit(f"✗ 题库不存在：{args.bank}（先跑 load.py）")
    bank_id = row[0]

    # 1. 术语 upsert
    rows = [(bank_id, t["slug"], json.dumps(t["names"], ensure_ascii=False), t.get("reading") or None,
             json.dumps(t["definition"], ensure_ascii=False), t.get("category") or "", search_text(t))
            for t in terms]
    for i in range(0, len(rows), BATCH):
        cur.executemany("""
            INSERT INTO term (bank_id, slug, names, reading, definition, category, search_text)
            VALUES (%s,%s,%s,%s,%s,%s,%s)
            ON DUPLICATE KEY UPDATE names=VALUES(names), reading=VALUES(reading), definition=VALUES(definition),
                                    category=VALUES(category), search_text=VALUES(search_text)""", rows[i:i + BATCH])
        conn.commit()

    cur.execute("SELECT slug, id FROM term WHERE bank_id = %s", (bank_id,))
    term_id = dict(cur.fetchall())

    # 2. 文件里已没有的术语：连同关联一起删（管道拥有整张表）
    stale = [i for s, i in term_id.items() if s not in slugs]
    for i in range(0, len(stale), BATCH):
        chunk = stale[i:i + BATCH]
        marks = ",".join(["%s"] * len(chunk))
        cur.execute(f"DELETE FROM term_question WHERE term_id IN ({marks})", chunk)
        cur.execute(f"DELETE FROM term WHERE id IN ({marks})", chunk)
        conn.commit()

    # 3. 关联：本题库的先全删再插（术语可能被归并，旧关联必须清掉）
    cur.execute("SELECT session, external_no, id FROM question WHERE bank_id = %s", (bank_id,))
    qid = {(s, n): i for s, n, i in cur.fetchall()}
    cur.execute("DELETE tq FROM term_question tq JOIN term t ON t.id = tq.term_id WHERE t.bank_id = %s", (bank_id,))
    conn.commit()
    pairs, missing = set(), []
    for l in links:
        q = qid.get((l.get("session", ""), l["no"]))
        if q is None:
            missing.append(l["no"])
            continue
        pairs.update((term_id[s], q) for s in l["terms"])
    pairs = sorted(pairs)
    for i in range(0, len(pairs), BATCH):
        cur.executemany("INSERT INTO term_question (term_id, question_id) VALUES (%s,%s)", pairs[i:i + BATCH])
        conn.commit()

    print(f"✓ 术语      {len(terms)}（删除过期 {len(stale)}）")
    print(f"✓ 出题关联  {len(pairs)}（{len(links) - len(missing)} 题）")
    if missing:
        print(f"⚠ 题库里不存在的题 {len(missing)} 道，已跳过：{missing[:10]}")


if __name__ == "__main__":
    main()
