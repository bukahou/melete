#!/usr/bin/env python3
"""
Melete 数据导入管道 — 第三段：enriched.json → 数据库

  python3 pipeline/core/load.py aws-saa-c03 [--dsn "$MELETE_DB_DSN"]

设计要点：
  · **幂等** —— 以 (bank.slug, question.external_no) 为业务键 upsert，可反复跑
  · **分批提交** —— 每批 ≤200 道，避开 TiDB 单事务 100 MB 上限
  · **不用外键** —— 关系完整性在本导入器里校验，不依赖数据库约束
  · **不假设 id 连续** —— 一律先 upsert 再回查 id 建映射

DSN 复用后端的 Go 风格格式（一处配置两处用）：
  user:password@tcp(host:3306)/melete?charset=utf8mb4&parseTime=true
"""
import argparse
import json
import os
import re
import sys
from pathlib import Path

try:
    import mysql.connector as mysql
except ImportError:
    sys.exit("✗ 需要 mysql-connector-python：pip install mysql-connector-python")

REPO = Path(__file__).resolve().parents[2]
BATCH = 200
DSN_RE = re.compile(r"^(?P<user>[^:]+):(?P<password>.*)@tcp\((?P<host>[^:)]+):(?P<port>\d+)\)/(?P<database>[^?]+)")


def parse_go_dsn(dsn: str) -> dict:
    """把后端用的 Go DSN 解析成连接参数，让前后端共用同一个环境变量。"""
    m = DSN_RE.match(dsn)
    if not m:
        sys.exit("✗ DSN 格式应为 user:password@tcp(host:3306)/dbname?...")
    d = m.groupdict()
    d["port"] = int(d["port"])
    return d


class Loader:
    def __init__(self, conn):
        self.conn = conn
        self.cur = conn.cursor()

    def _exec(self, sql: str, rows: list) -> None:
        """分批执行，避开 TiDB 单事务上限。空列表直接跳过。"""
        for i in range(0, len(rows), BATCH):
            self.cur.executemany(sql, rows[i:i + BATCH])
            self.conn.commit()

    # ---- bank ----
    def upsert_bank(self, bank: dict, meta: dict) -> int:
        self.cur.execute(
            """INSERT INTO bank (slug, name, locale, kind, meta) VALUES (%s,%s,%s,%s,%s)
               ON DUPLICATE KEY UPDATE name=VALUES(name), locale=VALUES(locale),
                                       kind=VALUES(kind), meta=VALUES(meta)""",
            (bank["slug"], bank["name"], bank.get("locale", "zh"),
             bank.get("kind", "cert"), json.dumps(meta, ensure_ascii=False)))
        self.conn.commit()
        self.cur.execute("SELECT id FROM bank WHERE slug=%s", (bank["slug"],))
        return self.cur.fetchone()[0]

    # ---- question ----
    def upsert_questions(self, bank_id: int, qs: list) -> dict:
        rows = [(bank_id, q["no"], q["stem"], q["kind"], q["pick"],
                 (q.get("enrichment") or {}).get("data_issue"),
                 json.dumps({"warnings": q.get("warnings", []),
                             **({"duplicates": q["duplicates"]} if q.get("duplicates") else {})},
                            ensure_ascii=False))
                for q in qs]
        self._exec(
            """INSERT INTO question (bank_id, external_no, stem, kind, pick_count, data_issue, raw)
               VALUES (%s,%s,%s,%s,%s,%s,%s)
               ON DUPLICATE KEY UPDATE stem=VALUES(stem), kind=VALUES(kind),
                   pick_count=VALUES(pick_count), data_issue=VALUES(data_issue), raw=VALUES(raw)""",
            rows)
        # 不假设 id 连续，回查建映射
        self.cur.execute("SELECT external_no, id FROM question WHERE bank_id=%s", (bank_id,))
        return dict(self.cur.fetchall())

    # ---- choice / claim / explanation ----
    def upsert_choices(self, qmap: dict, qs: list) -> int:
        rows = [(qmap[q["no"]], ch["label"], ch["body"]) for q in qs for ch in q["choices"]]
        self._exec("""INSERT INTO choice (question_id, label, body) VALUES (%s,%s,%s)
                      ON DUPLICATE KEY UPDATE body=VALUES(body)""", rows)
        return len(rows)

    def upsert_claims(self, qmap: dict, qs: list) -> int:
        rows = []
        for q in qs:
            for c in q["claims"]:
                meta = {k: v for k, v in c.items()
                        if k not in ("source", "answer", "confidence", "rationale")}
                rows.append((qmap[q["no"]], c["source"], c["answer"], c.get("confidence"),
                             c.get("rationale"),
                             json.dumps(meta, ensure_ascii=False) if meta else None))
        self._exec("""INSERT INTO answer_claim (question_id, source, answer, confidence, rationale, meta)
                      VALUES (%s,%s,%s,%s,%s,%s)
                      ON DUPLICATE KEY UPDATE answer=VALUES(answer), confidence=VALUES(confidence),
                          rationale=VALUES(rationale), meta=VALUES(meta)""", rows)
        return len(rows)

    def upsert_explanations(self, qmap: dict, qs: list, locale: str) -> int:
        rows = [(qmap[q["no"]], "ai", locale, q["enrichment"]["explanation"])
                for q in qs if q.get("enrichment", {}).get("explanation")]
        self._exec("""INSERT INTO explanation (question_id, source, locale, body)
                      VALUES (%s,%s,%s,%s)
                      ON DUPLICATE KEY UPDATE body=VALUES(body)""", rows)
        return len(rows)

    # ---- tag ----
    def upsert_tags(self, bank_id: int, qs: list, spec: dict) -> dict:
        """domain / service 是题库私有（bank_id=本题库），concept 全局共享（bank_id=0）。"""
        topic_field = spec.get("tag_types", {}).get("topic", {}).get("enrichment_field", "topics")
        wanted: set[tuple[int, str, str]] = set()
        for q in qs:
            e = q.get("enrichment")
            if not e:
                continue
            wanted.add((bank_id, "domain", f"domain-{e['domain']}"))
            # 「知识对象」轴统一叫 topic —— AWS 里是服务、LPIC 里是命令、Java 里是 API。
            # 富化产物里的字段名由题库 spec 自己声明（SAA/SAP 沿用 services），
            # 这样 core 不认识任何题库词汇，题库也不必为了 core 改产物。
            wanted.update((bank_id, "topic", s) for s in e[topic_field])
            wanted.update((0, "concept", c) for c in e["concepts"])

        domains = spec.get("domains", {})
        domains_zh = spec.get("domains_zh", {})
        rows = []
        for bid, typ, val in sorted(wanted):
            i18n = None
            if typ == "domain":
                key = val.split("-", 1)[1]
                names = {k: v for k, v in (("en", domains.get(key)), ("zh", domains_zh.get(key))) if v}
                if names:
                    i18n = json.dumps(names, ensure_ascii=False)
            rows.append((bid, typ, val, i18n))
        self._exec("""INSERT INTO tag (bank_id, type, value, i18n) VALUES (%s,%s,%s,%s)
                      ON DUPLICATE KEY UPDATE i18n=COALESCE(VALUES(i18n), i18n)""", rows)

        self.cur.execute("SELECT bank_id, type, value, id FROM tag WHERE bank_id IN (0,%s)", (bank_id,))
        return {(b, t, v): i for b, t, v, i in self.cur.fetchall()}

    def link_question_tags(self, bank_id: int, qmap: dict, qs: list, tmap: dict, topic_field: str) -> int:
        """标签关联先删后插 —— 重跑富化时标签可能变少，纯 upsert 无法清理旧关联。"""
        qids = [qmap[q["no"]] for q in qs if q.get("enrichment")]
        for i in range(0, len(qids), BATCH):
            chunk = qids[i:i + BATCH]
            self.cur.execute(
                f"DELETE FROM question_tag WHERE question_id IN ({','.join(['%s'] * len(chunk))})",
                chunk)
            self.conn.commit()

        rows = []
        for q in qs:
            e = q.get("enrichment")
            if not e:
                continue
            qid = qmap[q["no"]]
            # weight 2 = 主标签（考纲域），1 = 次要
            rows.append((qid, tmap[(bank_id, "domain", f"domain-{e['domain']}")], 2))
            rows += [(qid, tmap[(bank_id, "topic", s)], 1) for s in e[topic_field]]
            rows += [(qid, tmap[(0, "concept", c)], 1) for c in e["concepts"]]
        self._exec("""INSERT INTO question_tag (question_id, tag_id, weight) VALUES (%s,%s,%s)
                      ON DUPLICATE KEY UPDATE weight=VALUES(weight)""", rows)
        return len(rows)


def validate(qs: list) -> tuple[list[str], list[str]]:
    """
    关系完整性在应用层校验 —— 数据库里没有外键兜底。

    区分两类问题：
      硬错误 → 拒绝导入（数据结构本身坏了，继续下去会写脏数据）
      软错误 → 跳过该条记录并告警（题库自身的脏点，不该阻塞其余 1000 多道好题）

    「答案字母不在选项里」属于软错误：P0 解析阶段就已在 warnings 里标记过，
    是素材固有缺陷而非解析 bug。丢掉这一条 claim，题目其余部分照常可用。
    """
    hard, soft = [], []
    seen = set()
    for q in qs:
        if q["no"] in seen:
            hard.append(f"题号重复: #{q['no']}")
        seen.add(q["no"])
        if not q["choices"]:
            soft.append(f"#{q['no']}: 整题无选项，跳过")
            continue
        labels = {ch["label"] for ch in q["choices"]}
        kept = []
        for c in q["claims"]:
            if set(c["answer"]) - labels:
                soft.append(f"#{q['no']}: claim[{c['source']}] 答案 {c['answer']!r} 含选项外字母，跳过该条")
            elif len(c["answer"]) != q["pick"]:
                # 残缺主张（如 #302：multi 题两个来源都只解析出单字母）。
                # 失真的主张比没有主张更糟 —— 它会把「标注 vs 社区一致性」统计
                # 污染成假一致，也会误导界面上的并列展示。
                soft.append(f"#{q['no']}: claim[{c['source']}] 长度 {len(c['answer'])} ≠ 应选 {q['pick']} 项，跳过该条")
            else:
                kept.append(c)
        q["claims"] = kept
    return hard, soft


def fold_duplicates(qs: list) -> tuple[list, list[str]]:
    """
    重复题只导入正本（用户拍板 2026-09-02，方案 b）：
    解析器给后出现的题挂了 duplicate_of_N，这里把它的答案主张并入 N，然后不导入它。

    为什么并入而不是丢弃：#10≡#438 两题的题库标注互相矛盾（CE vs A），
    「题库在两处给了不同答案」本身就是一条有价值的主张 —— 与「题库说 A、社区说 B」是同一种呈现。
    并入规则（answer_claim 以 (question_id, source) 唯一）：
      正本没有这个来源      → 追加为新主张，meta.from_no 记出处
      同来源、同答案        → 什么都不做（#84≡#85 就是这种）
      同来源、答案不同      → 记进正本该主张的 meta.variants
    FSRS 上这样只剩一张卡；schema 一行不改。
    """
    by_no = {q["no"]: q for q in qs}
    dropped: set[int] = set()
    notes: list[str] = []
    for q in qs:
        target = next((int(w.rsplit("_", 1)[1]) for w in q.get("warnings", []) if w.startswith("duplicate_of_")), None)
        if target is None:
            continue
        canon = by_no.get(target)
        if canon is None or target in dropped:
            notes.append(f"#{q['no']}: 标记为 #{target} 的重复，但正本不可用，照常独立导入")
            continue
        canon.setdefault("duplicates", []).append(q["no"])
        for c in q["claims"]:
            same = next((k for k in canon["claims"] if k["source"] == c["source"]), None)
            if same is None:
                canon["claims"].append({**c, "from_no": q["no"]})
                notes.append(f"#{q['no']} → #{target}: 补入主张 {c['source']}={c['answer']}")
            elif same["answer"] != c["answer"]:
                same.setdefault("variants", []).append(
                    {"no": q["no"], "answer": c["answer"], **({"confidence": c["confidence"]} if c.get("confidence") is not None else {})})
                notes.append(f"#{q['no']} → #{target}: {c['source']} 答案不同（{c['answer']} vs {same['answer']}），记入 variants")
        dropped.add(q["no"])
        notes.append(f"#{q['no']}: 与 #{target} 完全重复，不导入")
    return [q for q in qs if q["no"] not in dropped], notes


def main() -> None:
    ap = argparse.ArgumentParser(description="把富化产物导入数据库")
    ap.add_argument("bank")
    ap.add_argument("--dsn", default=os.environ.get("MELETE_DB_DSN"),
                    help="默认取环境变量 MELETE_DB_DSN")
    args = ap.parse_args()

    if not args.dsn:
        sys.exit("✗ 未提供 DSN。设置 MELETE_DB_DSN 或用 --dsn\n"
                 "  凭证类无默认值是有意为之 —— 忘配就失败，优于默默连错库")

    src = REPO / "data" / args.bank / "enriched.json"
    if not src.exists():
        sys.exit(f"✗ 找不到 {src.relative_to(REPO)}\n"
                 f"  先跑：python3 pipeline/core/enrich.py merge {args.bank}")
    spec_path = REPO / "pipeline" / "banks" / args.bank / "enrich_spec.json"
    spec = json.loads(spec_path.read_text(encoding="utf-8")) if spec_path.exists() else {}

    doc = json.loads(src.read_text(encoding="utf-8"))
    qs = doc["questions"]
    hard, soft = validate(qs)
    if hard:
        print("✗ 数据校验未通过（硬错误）：")
        for e in hard[:20]:
            print(f"    {e}")
        sys.exit(1)
    if soft:
        print(f"⚠ 题库脏点 {len(soft)} 处，已跳过相应记录，其余照常导入：")
        for e in soft:
            print(f"    {e}")
        print()

    # 整题无选项的跳过导入（无法作为学习单元），但保留在 questions.json 里可追溯
    qs = [q for q in qs if q["choices"]]

    qs, dup_notes = fold_duplicates(qs)
    if dup_notes:
        print(f"⚠ 重复题 {len(dup_notes)} 条处置（只导正本，主张并入）：")
        for e in dup_notes:
            print(f"    {e}")
        print()

    conn = mysql.connect(**parse_go_dsn(args.dsn), charset="utf8mb4", autocommit=False)
    ld = Loader(conn)
    # bank.meta = 题库的自描述展示元数据：标签轴叫什么、考纲权重、及格线。
    # 前端不得写死这些词 —— 换成 LPIC 时只有这里不同。
    # 键名用 camelCase：这个 JSON 就是 API 契约里的 BankMeta 原样透传，
    # 与 OpenAPI 的 tagTypes / passScore / maxScore 对齐，后端不做二次映射。
    # enrichment_field 是导入侧的私事，不进 meta。
    tag_types = {t: {k: v for k, v in m.items() if k != "enrichment_field"}
                 for t, m in spec.get("tag_types", {}).items()}
    bank_id = ld.upsert_bank(doc["bank"], {
        "tagTypes": tag_types,
        "passScore": spec.get("pass_score"),
        "maxScore": spec.get("max_score"),
        "domains": spec.get("domains", {}),
    })
    qmap = ld.upsert_questions(bank_id, qs)
    n_ch = ld.upsert_choices(qmap, qs)
    n_cl = ld.upsert_claims(qmap, qs)
    n_ex = ld.upsert_explanations(qmap, qs, doc["bank"].get("locale", "zh"))
    tmap = ld.upsert_tags(bank_id, qs, spec)
    topic_field = spec.get("tag_types", {}).get("topic", {}).get("enrichment_field", "topics")
    n_qt = ld.link_question_tags(bank_id, qmap, qs, tmap, topic_field)
    conn.close()

    n_enriched = sum(1 for q in qs if q.get("enrichment"))
    print(f"✓ 题库      {doc['bank']['slug']}  (bank_id={bank_id})")
    print(f"✓ 题目      {len(qs)}   其中已富化 {n_enriched}")
    print(f"✓ 选项      {n_ch}")
    print(f"✓ 答案主张  {n_cl}")
    print(f"✓ 解析      {n_ex}")
    print(f"✓ 标签      {len(tmap)}   关联 {n_qt}")


if __name__ == "__main__":
    main()
