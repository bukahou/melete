#!/usr/bin/env python3
"""
用语集导入（P9 第 6 步）：$MELETE_DATA_ROOT/<bank>/glossary.json → term / term_question。

    python3 pipeline/core/load_glossary.py <bank-slug> [--dsn <go-dsn>]

glossary.json 的形状（由生成管道产出，在 config 私有仓）：
    {
      "terms": [{"slug", "names": {"zh","ja"}, "reading"?, "definition": {"zh","ja"}, "category", "lead"?}],
      "links": [{"session", "no", "terms": ["<slug>", ...]}]     # 每道题考到哪些术语
    }

⭐ 管道拥有整张术语表：文件里没有的术语会被删掉（连同它的关联）——
   重跑生成时术语可能被归并 / 剔除，纯 upsert 会留下幽灵条目（与 load.py 清理过期主张同理）。
⭐ 主条目（lead，由 pipeline/glossary/leads.py 标出）另按题目的服务标签补关联：
   题目带 topic 标签 X ⇒ 关联分类 X 的主条目。第 1 遍逐题抽词挑的是细节词，题目只说「一台 EC2」时
   往往不抽 EC2 本身（实测 SAA 的「Amazon EC2」只关联了 7 题）—— 标签恰好就是「这题考到哪个服务」。
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
             json.dumps(t["definition"], ensure_ascii=False), t.get("category") or "", bool(t.get("lead")), search_text(t))
            for t in terms]
    for i in range(0, len(rows), BATCH):
        cur.executemany("""
            INSERT INTO term (bank_id, slug, names, reading, definition, category, is_lead, search_text)
            VALUES (%s,%s,%s,%s,%s,%s,%s,%s)
            ON DUPLICATE KEY UPDATE names=VALUES(names), reading=VALUES(reading), definition=VALUES(definition),
                                    category=VALUES(category), is_lead=VALUES(is_lead), search_text=VALUES(search_text)""", rows[i:i + BATCH])
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
    n_extracted = len(pairs)

    # 4. 主条目按服务标签补关联
    lead_of = {t["category"]: term_id[t["slug"]] for t in terms if t.get("lead")}
    cur.execute("""
        SELECT qt.question_id, t.value FROM question_tag qt
        JOIN tag t ON t.id = qt.tag_id JOIN question q ON q.id = qt.question_id
        WHERE q.bank_id = %s AND t.type = 'topic'""", (bank_id,))
    pairs.update((lead_of[v], q) for q, v in cur.fetchall() if v in lead_of)
    n_by_tag = len(pairs) - n_extracted
    pairs = sorted(pairs)
    for i in range(0, len(pairs), BATCH):
        cur.executemany("INSERT INTO term_question (term_id, question_id) VALUES (%s,%s)", pairs[i:i + BATCH])
        conn.commit()

    print(f"✓ 术语      {len(terms)}（删除过期 {len(stale)}）")
    print(f"✓ 出题关联  {len(pairs)}（{len(links) - len(missing)} 题；其中主条目按服务标签补的 {n_by_tag}）")
    print(f"✓ 主条目    {len(lead_of)}")
    if missing:
        print(f"⚠ 题库里不存在的题 {len(missing)} 道，已跳过：{missing[:10]}")


if __name__ == "__main__":
    main()
