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

from paths import REPO, bank_dir, rel  # 路径解析的唯一归属地, 见该模块 docstring
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
    # locale 显式传入而不是从 bank 字典里取：那里存的是【素材的语言】，
    # 而库里这一列是【展示的语言】。SAP-C02 素材是英文、展示是中文译文，
    # 两者不同 —— 分工是 parse.py 记录素材事实，load.py 决定展示形态。
    def upsert_bank(self, bank: dict, meta: dict, locale: str) -> int:
        self.cur.execute(
            """INSERT INTO bank (slug, name, locale, kind, meta) VALUES (%s,%s,%s,%s,%s)
               ON DUPLICATE KEY UPDATE name=VALUES(name), locale=VALUES(locale),
                                       kind=VALUES(kind), meta=VALUES(meta)""",
            (bank["slug"], bank["name"], locale,
             bank.get("kind", "cert"), json.dumps(meta, ensure_ascii=False)))
        self.conn.commit()
        self.cur.execute("SELECT id FROM bank WHERE slug=%s", (bank["slug"],))
        return self.cur.fetchone()[0]

    # ---- question ----
    def upsert_questions(self, bank_id: int, qs: list) -> dict:
        rows = [(bank_id, q["no"], q.get("session", ""), display_text(q)[0], q["kind"], q["pick"],
                 (q.get("enrichment") or {}).get("data_issue"),
                 json.dumps({"warnings": q.get("warnings", []),
                             **({"duplicates": q["duplicates"]} if q.get("duplicates") else {})},
                            ensure_ascii=False))
                for q in qs]
        self._exec(
            """INSERT INTO question (bank_id, external_no, session, stem, kind, pick_count, data_issue, raw)
               VALUES (%s,%s,%s,%s,%s,%s,%s,%s)
               ON DUPLICATE KEY UPDATE stem=VALUES(stem), kind=VALUES(kind),
                   pick_count=VALUES(pick_count), data_issue=VALUES(data_issue), raw=VALUES(raw)""",
            rows)
        # 不假设 id 连续，回查建映射。
        # ⚠️ 键是 (session, external_no) 而不是 external_no —— 多套卷子的题库里
        # 光凭题号不唯一（IPA 15 套各有一个 問1）。单套题库 session 是空串，行为不变。
        self.cur.execute("SELECT session, external_no, id FROM question WHERE bank_id=%s", (bank_id,))
        return {(sess, no): qid for sess, no, qid in self.cur.fetchall()}

    # ---- choice / claim / explanation ----
    def upsert_choices(self, qmap: dict, qs: list) -> int:
        rows = [(qid(qmap, q), ch["label"], display_text(q)[1][ch["label"]])
                for q in qs for ch in q["choices"]]
        self._exec("""INSERT INTO choice (question_id, label, body) VALUES (%s,%s,%s)
                      ON DUPLICATE KEY UPDATE body=VALUES(body)""", rows)
        return len(rows)

    # ---- 译文（question_i18n / choice_i18n / explanation）----
    def choice_ids(self, qmap: dict) -> dict:
        """(question_id, label) → choice.id。choice_i18n 按 choice_id 存，必须先拿到它。"""
        ids = sorted(set(qmap.values()))
        out = {}
        for i in range(0, len(ids), 500):  # ⚠️ TiDB 单事务/单语句都别喂太大
            chunk = ids[i:i + 500]
            self.cur.execute(
                f"SELECT question_id, label, id FROM choice WHERE question_id IN ({','.join(['%s'] * len(chunk))})",
                chunk)
            out.update({(q, l): c for q, l, c in self.cur.fetchall()})
        return out

    def upsert_question_i18n(self, qmap: dict, tr_items: list, locale: str) -> int:
        """
        题干译文 → question_i18n。

        ⭐ 与旧的 display_text() 路线的根本区别：那条路把译文【写进 question.stem】，
        原文就此从库里消失（SAP-C02 的英文原文正是这么丢的，只剩 enriched.json 里有）。
        这里译文另存一张表，question.stem 永远是原文 ⇒ 同一道题可以同时供多种语言。
        """
        rows = [(qmap[(it.get("session", ""), it["no"])], locale, it["stem"])
                for it in tr_items if (it.get("session", ""), it["no"]) in qmap and it.get("stem")]
        self._exec("""INSERT INTO question_i18n (question_id, locale, stem, source)
                      VALUES (%s,%s,%s,'ai')
                      ON DUPLICATE KEY UPDATE stem=VALUES(stem)""", rows)
        return len(rows)

    def upsert_choice_i18n(self, qmap: dict, cmap: dict, tr_items: list, locale: str) -> int:
        rows = []
        for it in tr_items:
            key = (it.get("session", ""), it["no"])
            if key not in qmap:
                continue
            for label, body in (it.get("choices") or {}).items():
                cid = cmap.get((qmap[key], label))
                # ⚠️ 选项对不上就跳过，⛔ 不猜 —— 译文的 label 与库里不一致
                # 说明素材变过，那是要人看的事，不该在这里静默补一行
                if cid and body:
                    rows.append((cid, locale, body))
        self._exec("""INSERT INTO choice_i18n (choice_id, locale, body, source)
                      VALUES (%s,%s,%s,'ai')
                      ON DUPLICATE KEY UPDATE body=VALUES(body)""", rows)
        return len(rows)

    def upsert_translated_explanations(self, qmap: dict, tr_items: list, locale: str) -> int:
        """译文里的解析 → explanation(locale)。explanation 表本来就是多语言的，直接加一行。"""
        rows = [(qmap[(it.get("session", ""), it["no"])], "ai", locale, it["explanation"])
                for it in tr_items
                if (it.get("session", ""), it["no"]) in qmap and it.get("explanation")]
        self._exec("""INSERT INTO explanation (question_id, source, locale, body)
                      VALUES (%s,%s,%s,%s)
                      ON DUPLICATE KEY UPDATE body=VALUES(body)""", rows)
        return len(rows)

    # 管道拥有的主张来源。user_note 之类由用户在应用里写入的来源不在此列 —— 重导不得动它们。
    PIPELINE_SOURCES = ("bank_label", "community_vote", "ai_verdict")

    def upsert_claims(self, qmap: dict, qs: list) -> int:
        # 先清掉本次不再产出的管道主张：纯 upsert 会让上一次导入留下的行成为幽灵
        # （#125 实证：解析器判定字母主张不可信后，旧的 community_vote 行仍在库里）。
        # 只删管道来源；按 (question_id, source) 精确删，不碰用户写入的来源。
        wanted = {(qid(qmap, q), c["source"]) for q in qs for c in q["claims"]}
        qids = [qid(qmap, q) for q in qs]
        stale = []
        for i in range(0, len(qids), BATCH):
            chunk = qids[i:i + BATCH]
            self.cur.execute(
                f"SELECT question_id, source FROM answer_claim WHERE question_id IN ({','.join(['%s'] * len(chunk))})"
                f" AND source IN ({','.join(['%s'] * len(self.PIPELINE_SOURCES))})",
                chunk + list(self.PIPELINE_SOURCES))
            stale += [(qid, src) for qid, src in self.cur.fetchall() if (qid, src) not in wanted]
        if stale:
            self._exec("DELETE FROM answer_claim WHERE question_id=%s AND source=%s", stale)
            print(f"  · 清理过期主张 {len(stale)} 条")

        rows = []
        for q in qs:
            for c in q["claims"]:
                meta = {k: v for k, v in c.items()
                        if k not in ("source", "answer", "confidence", "rationale")}
                rows.append((qid(qmap, q), c["source"], c["answer"], c.get("confidence"),
                             c.get("rationale"),
                             json.dumps(meta, ensure_ascii=False) if meta else None))
        self._exec("""INSERT INTO answer_claim (question_id, source, answer, confidence, rationale, meta)
                      VALUES (%s,%s,%s,%s,%s,%s)
                      ON DUPLICATE KEY UPDATE answer=VALUES(answer), confidence=VALUES(confidence),
                          rationale=VALUES(rationale), meta=VALUES(meta)""", rows)
        return len(rows)

    def upsert_explanations(self, qmap: dict, qs: list, locale: str) -> int:
        rows = [(qid(qmap, q), "ai", locale, q["enrichment"]["explanation"])
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
            # ⚠️ concepts 是可选的：题库可以声明本轮不产（IPA 2026-09-05）。
            # 判据与 enrich.py 的 wants_concepts 一致 —— 用 .get 而不是 e["concepts"]，
            # 否则「不产 concept」这个决定在导入端会崩。
            # ⭐ 这是「通用化只做了一半」的第三个现场（前两处：enrich.py 的
            # ITEM_KEYS 与校验器）—— 同一个决定要在三处分别兑现，
            # 而前两处改了、这处没改，直到真的导入才炸。
            wanted.update((0, "concept", c) for c in e.get("concepts") or [])

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
        qids = [qid(qmap, q) for q in qs if q.get("enrichment")]
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
            question_id = qid(qmap, q)
            # weight 2 = 主标签（考纲域），1 = 次要
            rows.append((question_id, tmap[(bank_id, "domain", f"domain-{e['domain']}")], 2))
            rows += [(question_id, tmap[(bank_id, "topic", s)], 1) for s in e[topic_field]]
            rows += [(question_id, tmap[(0, "concept", c)], 1) for c in e.get("concepts") or []]
        self._exec("""INSERT INTO question_tag (question_id, tag_id, weight) VALUES (%s,%s,%s)
                      ON DUPLICATE KEY UPDATE weight=VALUES(weight)""", rows)
        return len(rows)


def qid(qmap: dict, q: dict) -> int:
    """取一道题在库里的 id。

    ⚠️ 键是 (session, external_no) 而不是光凭题号 —— 多套卷子的题库里
    题号不唯一（IPA 15 套各有一个 問1）。这里是键的【唯一组成处】：
    七个调用点都走它，不各写各的元组，免得哪天加维度时漏改一处。
    """
    return qmap[(q.get("session", ""), q["no"])]


def display_text(q: dict) -> tuple[str, dict]:
    """题面的展示文本：有译文用译文，没有就用原文。

    ⚠️ 这是取题面文本的【唯一入口】—— 题干与选项必须走同一处，
    否则会出现「题干中文、选项英文」这种半吊子状态，而它不会报任何错。

    ⚠️ 这里的 `or 原文` 回退【只对没有声明 translation 的题库有效】。
    声明了的题库由 check_translations() 在导入前硬卡，缺一条就不导 ——
    静默回退会让「这个题库是中文的」悄悄变成「大部分是中文的」。
    """
    tr = (q.get("enrichment") or {}).get("translation") or {}
    tr_choices = tr.get("choices") or {}
    stem = tr.get("stem") or q["stem"]
    bodies = {ch["label"]: tr_choices.get(ch["label"]) or ch["body"] for ch in q["choices"]}
    return stem, bodies


def check_translations(qs: list) -> list[str]:
    """spec 声明了 translation 时，逐题核对译文齐备 —— 缺任何一条都拒绝导入。

    enrich.py 在【产出】那一端已有同名门禁；这里补在【落库】这一端。
    两端各卡一次的理由：产物可能是手工补过的，也可能来自旧版本 spec。
    """
    errs = []
    for q in qs:
        tr = (q.get("enrichment") or {}).get("translation") or {}
        if not (tr.get("stem") or "").strip():
            errs.append(f"#{q['no']}: 缺 translation.stem")
        tr_choices = tr.get("choices") or {}
        missing = [ch["label"] for ch in q["choices"]
                   if not (tr_choices.get(ch["label"]) or "").strip()]
        if missing:
            errs.append(f"#{q['no']}: 缺选项译文 {','.join(missing)}")
    return errs


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
        # 同一题出现重复选项标签（#200 两个 B、#337/#515 两个 C）：字母不再唯一指向一个选项，
        # 题库标注与社区投票的字母都失去意义 —— 两者「不一致」很可能只是各自沿用了不同的标签口径
        # （#200 实证：BDF 与 BCE 指向同一组内容）。整题主张丢弃，避免制造假分歧。
        if any(w.startswith("duplicate_choice_label_") for w in q.get("warnings", [])):
            if q["claims"]:
                soft.append(f"#{q['no']}: 选项标签重复，{len(q['claims'])} 条字母主张全部不可信，跳过")
            q["claims"] = []
            continue
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

    src = bank_dir(args.bank) / "enriched.json"
    if not src.exists():
        sys.exit(f"✗ 找不到 {rel(src)}\n"
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

    # 译文门禁 —— 放在这里而不是更早：无选项题与被折叠的重复题都不进库，
    # 不该因为它们缺译文而拦下整批。
    #
    # 展示语言：spec 声明了 translation 就用译文的语言，否则用素材语言。
    # locale 归一到主语言（zh-CN → zh），与库里既有的 explanation.locale 对齐 ——
    # 同一个库里混用 zh 和 zh-CN 会让按语言取内容的查询悄悄漏掉一半。
    tr_spec = spec.get("translation") or {}
    if tr_spec:
        tr_errs = check_translations(qs)
        if tr_errs:
            print(f"✗ 本题库声明了 translation（{tr_spec.get('locale')}），但有 {len(tr_errs)} 处缺失：")
            for e in tr_errs[:20]:
                print(f"    {e}")
            if len(tr_errs) > 20:
                print(f"    …… 另有 {len(tr_errs) - 20} 处")
            sys.exit("  ⛔ 拒绝导入 —— 缺译文不得静默回退到原文")
        display_locale = (tr_spec.get("locale") or "zh").split("-")[0]
        print(f"✓ 译文      {len(qs)} 题齐备，题面按 {display_locale} 导入"
              f"（素材语言 {doc['bank'].get('locale')}）\n")
    else:
        display_locale = doc["bank"].get("locale", "zh")

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
    }, display_locale)
    qmap = ld.upsert_questions(bank_id, qs)
    n_ch = ld.upsert_choices(qmap, qs)
    n_cl = ld.upsert_claims(qmap, qs)
    # 解析的语言 ≠ 题库的语言：SAP-C02 题面是英文，但富化会话按学习者语言写中文解析。
    # 由 spec 声明 explanation_locale，缺省才退回题库 locale（SAA 中文题库两者相同）。
    n_ex = ld.upsert_explanations(qmap, qs, spec.get("explanation_locale") or doc["bank"].get("locale", "zh"))
    # ---- 译文：translated.<locale>.json（translate.py merge 的产物）----
    #
    # ⚠️ 与上面 display_text() 那条【烧死译文】的老路是两回事，两者暂时并存：
    #   老路：译文覆盖 question.stem，原文从库里消失（SAP-C02 的英文就是这么丢的）
    #   新路：译文另存 question_i18n，stem 永远是原文 ⇒ 同一道题能供多种语言
    # 老路要拆，但那会把 SAP-C02 的展示语言从 zh 改成 en，是对既有题库的行为变更，
    # 单独一步做（已登记 tracker P8）。⛔ 别在这个改动里顺手改掉。
    n_i18n = {}
    for tr_path in sorted(bank_dir(args.bank).glob("translated.*.json")):
        tr_doc = json.loads(tr_path.read_text(encoding="utf-8"))
        tr_locale, tr_items = tr_doc["locale"], tr_doc.get("items", [])
        if tr_locale == display_locale:
            print(f"⚠ 跳过 {tr_path.name}：它的语言与题面展示语言相同（{tr_locale}），"
                  f"存进 i18n 表没有意义")
            continue
        cmap = ld.choice_ids(qmap)
        a = ld.upsert_question_i18n(qmap, tr_items, tr_locale)
        b = ld.upsert_choice_i18n(qmap, cmap, tr_items, tr_locale)
        c = ld.upsert_translated_explanations(qmap, tr_items, tr_locale)
        n_i18n[tr_locale] = (a, b, c, tr_doc.get("complete", True))

    tmap = ld.upsert_tags(bank_id, qs, spec)
    topic_field = spec.get("tag_types", {}).get("topic", {}).get("enrichment_field", "topics")
    n_qt = ld.link_question_tags(bank_id, qmap, qs, tmap, topic_field)
    conn.close()

    n_enriched = sum(1 for q in qs if q.get("enrichment"))
    print(f"✓ 题库      {doc['bank']['slug']}  (bank_id={bank_id})")
    print(f"✓ 题目      {len(qs)}   其中已富化 {n_enriched}")
    for loc, (a, b, c, complete) in sorted(n_i18n.items()):
        flag = "" if complete else "   ⚠ 部分译文（complete=false）"
        print(f"✓ 译文 {loc}    题干 {a} · 选项 {b} · 解析 {c}{flag}")
    print(f"✓ 选项      {n_ch}")
    print(f"✓ 答案主张  {n_cl}")
    print(f"✓ 解析      {n_ex}")
    # ⚠️ 只报【本题库的】标签数。tmap 里还有全部全局 concept（bank_id=0，
    # 目前 1075 个），把它算进来会打印出「标签 1100」这种数字 ——
    # 而 IPA 只有 25 个。⛔ 一个会让人以为出了事的数字本身就是缺陷：
    # 2026-09-05 我就是被它绊了一下，回头查库才确认导入是对的。
    own = sum(1 for (bid, _, _) in tmap if bid == bank_id)
    print(f"✓ 标签      {own}   关联 {n_qt}"
          + (f"   （另引用全局 concept {len(tmap) - own} 个）" if len(tmap) > own else ""))


if __name__ == "__main__":
    main()
