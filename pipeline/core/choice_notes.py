#!/usr/bin/env python3
"""
Melete 数据导入管道 — 逐项点评：enriched.json → choice_notes/*.json → choice_notes.json

富化产出的 reasoning 是一整段，把各选项的分析首尾相连地写在一起，
客户端无法可靠地把「讲 B 的那句」放到 B 下面。本阶段为每个选项单独产出一两句点评，
外加一段只在分歧时出现的「分歧说明」，供导入时写进 choice_explanation 与
answer_claim(ai_verdict).rationale。

  · 输入是已富化的题：题面 + 已有裁决 + 已有 reasoning / explanation
  · ⛔ 本阶段不产出裁决。对错由 enriched.json 里的 verdict 决定，点评只解释原因
  · 分片、可中断、写入前全量校验，与富化阶段同一套做法（见 shards.py）

子命令：
  status <bank>                 进度
  next   <bank> [--count N] [--at 题号] [--no-explanation]
                                打印待写的分片（喂给写点评的会话）
  write  <bank> <file>          从 Python 字面量文件（ITEMS = [...]）写入分片
  check  <bank>                 校验全部分片
  merge  <bank> [--partial]     合并成 choice_notes.json；--partial 允许未写完时先合并
"""
import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

from enrich import claims_of, fingerprint
from paths import REPO, bank_dir, rel
from shards import ShardStore, load_items, write_json

STAGE = "choice_notes"
NOTE_MIN, NOTE_MAX = 8, 150
DISPUTE_MIN, DISPUTE_MAX = 20, 300
# 纯文本：两个客户端都按纯文本显示点评，出现这些记号会原样露出来
FORBIDDEN_MARKS = ("**", "`", "|", "\n", "\r")
RIVAL_SOURCES = ("bank_label", "community_vote")


# ---------- 题目 ----------

def load_bank(bank: str) -> tuple[dict, dict]:
    src = bank_dir(bank) / "enriched.json"
    if not src.exists():
        sys.exit(f"✗ 找不到 {rel(src)}，先跑 enrich.py merge {bank}")
    spec_path = REPO / "pipeline" / "banks" / bank / "enrich_spec.json"
    spec = json.loads(spec_path.read_text(encoding="utf-8")) if spec_path.exists() else {}
    return json.loads(src.read_text(encoding="utf-8")), spec


def notes_locale(doc: dict, spec: dict) -> str:
    """点评的语言 = 解析的语言（SAP-C02 题面是英文，解析与点评是中文）。只取主语言。"""
    return (spec.get("explanation_locale") or doc["bank"].get("locale") or "zh").split("-")[0]


def skip_reason(q: dict) -> str | None:
    """这道题不需要点评的原因；需要时返回 None。导入时这几类题都不会展示 AI 裁决。"""
    e = q.get("enrichment") or {}
    warnings = q.get("warnings", [])
    if not q.get("choices"):
        return "整题无选项"
    if any(w.startswith("duplicate_choice_label_") for w in warnings):
        return "选项标签重复"
    if any(w.startswith("duplicate_of_") for w in warnings):
        return "重复题（导入时并入正本）"
    if not e:
        return "未富化"
    if e.get("data_issue") or not e.get("verdict"):
        return "题目结构性不可判（data_issue）"
    return None


def rivals(q: dict) -> list[tuple[str, str]]:
    """
    与 AI 裁决不一致、且本身格式完好的题库标注 / 社区投票：[(来源, 答案)]。
    格式不完好的主张（字母不在选项里、字母数不等于应选数）导入时会被丢弃，不算分歧。
    """
    verdict = q["enrichment"]["verdict"]
    labels = {c["label"] for c in q["choices"]}
    out = []
    for src in RIVAL_SOURCES:
        c = claims_of(q).get(src)
        if not c:
            continue
        ans = "".join(sorted(c["answer"]))
        if set(ans) - labels or len(ans) != q["pick"]:
            continue
        if ans != verdict:
            out.append((src, ans))
    return out


def _plain_text_errors(field: str, text, lo: int, hi: int) -> list[str]:
    if not isinstance(text, str) or not text.strip():
        return [f"{field} 为空"]
    errs = []
    if text != text.strip():
        errs.append(f"{field} 首尾有空白")
    if not lo <= len(text) <= hi:
        errs.append(f"{field} {len(text)} 字，应在 {lo}–{hi} 字之间")
    if marks := [m for m in FORBIDDEN_MARKS if m in text]:
        errs.append(f"{field} 含 {[m.encode('unicode_escape').decode() for m in marks]}，须为一行纯文本")
    return errs


def check_item(it: dict, q: dict) -> list[str]:
    """校验已落盘的一条（不含 fp）。"""
    errs = []
    if set(it) != {"no", "choices", "dispute"}:
        return [f"字段应为 no / choices / dispute，实际 {sorted(it)}"]
    notes = it["choices"]
    labels = {c["label"] for c in q["choices"]}
    if not isinstance(notes, dict):
        return ["choices 须为 {选项字母: 点评} 对象"]
    if set(notes) != labels:
        errs.append(f"choices 的选项 {sorted(notes)} ≠ 题目选项 {sorted(labels)}")
    for label in sorted(set(notes) & labels):
        errs += _plain_text_errors(f"choices[{label}]", notes[label], NOTE_MIN, NOTE_MAX)

    disputed = rivals(q)
    if disputed:
        errs += _plain_text_errors("dispute", it["dispute"], DISPUTE_MIN, DISPUTE_MAX)
    elif it["dispute"] is not None:
        errs.append("AI 裁决与题库标注、社区投票都一致，dispute 应为 None")
    return errs


# ---------- 分片 ----------

class Bank:
    def __init__(self, bank: str):
        self.slug = bank
        self.doc, self.spec = load_bank(bank)
        self.locale = notes_locale(self.doc, self.spec)
        self.qmap = {q["no"]: q for q in self.doc["questions"]}
        if len(self.qmap) != len(self.doc["questions"]):
            sys.exit("✗ enriched.json 里题号重复，分片无法按题号路由")
        self.wanted = {n for n, q in self.qmap.items() if skip_reason(q) is None}
        self.store = ShardStore(bank_dir(bank) / STAGE, list(self.qmap))

    def wanted_in(self, rng) -> set[int]:
        return {n for n in self.store.members(rng) if n in self.wanted}

    def check_shard(self, rng) -> list[str]:
        wanted = self.wanted_in(rng)
        if not wanted:
            return []
        return self.store.check(rng, wanted, lambda it: check_item(it, self.qmap[it["no"]]))

    def scan(self):
        done, bad, todo = [], [], []
        for rng in self.store.ranges:
            if not self.wanted_in(rng):
                done.append(rng)
            elif not self.store.path(rng).exists():
                todo.append(rng)
            elif self.check_shard(rng):
                bad.append(rng)
            else:
                done.append(rng)
        return done, bad, todo


# ---------- 子命令 ----------

def cmd_status(b: Bank) -> None:
    done, bad, todo = b.scan()
    n_done = sum(len(b.wanted_in(r)) for r in done)
    skipped = len(b.qmap) - len(b.wanted)
    print(f"题库      {b.slug}（点评语言 {b.locale}）")
    print(f"需要点评  {len(b.wanted)} 题（另有 {skipped} 题不需要：无选项、重复、不可判）")
    print(f"已完成    {n_done} 题  ({n_done / max(len(b.wanted), 1) * 100:.1f}%)")
    print(f"分片      {len(b.store.ranges)} 片：完成 {len(done)} · 校验失败 {len(bad)} · 待写 {len(todo)}")
    if bad:
        print(f"校验失败  {[b.store.name(r) for r in bad]}")
    if todo:
        print(f"\n下一片    {b.store.name(todo[0])}   （python3 pipeline/core/choice_notes.py next {b.slug}）")


GUIDE = """\
逐项点评的写法（每题一个条目）：

  {{"no": 题号, "fp": "标题行里的 fp", "choices": {{"A": "…", "B": "…", …}}, "dispute": None 或 "…"}}

· choices：每个选项一两句，说清【这个选项】为什么对、或为什么错。
    - 以已有的 AI 裁决为准，⛔ 不改判。对错由界面标出，句子不用以「正确」「错误」开头
    - 只讲这一个选项；不要写成「B 和 D 的对比」—— 这句话会单独显示在该选项下面
    - 讲出具体的原因（能力边界、题干里的哪个条件不满足），不要只说「不符合题意」
    - 一行纯文本，{note_min}–{note_max} 字；⛔ 不用 **、反引号、表格、换行
· dispute：只有标题行写着「要写分歧说明」的题才写，其余一律 None。
    - 说明题库标注或社区投票为什么错、AI 为什么判成现在这样；{dispute_min}–{dispute_max} 字，一行纯文本
    - 各选项的分析已经在 choices 里，这里不再逐项复述
· 语言：{locale}
· 写入：把 ITEMS = [...] 存成一个 .py 文件，然后
    python3 pipeline/core/choice_notes.py write {bank} <文件>
"""


def cmd_next(b: Bank, count: int, with_explanation: bool, at: int | None) -> None:
    _, bad, todo = b.scan()
    pending = sorted(bad + todo)
    if at is not None:
        pending = [r for r in pending if r[0] <= at <= r[1]]
        if not pending:
            sys.exit(f"✗ #{at} 所在的分片已完成，或不需要点评")
    if not pending:
        print("✓ 全部分片已完成且校验通过，可以跑 merge 了")
        return
    print(GUIDE.format(note_min=NOTE_MIN, note_max=NOTE_MAX, dispute_min=DISPUTE_MIN,
                       dispute_max=DISPUTE_MAX, locale=b.locale, bank=b.slug))
    for rng in pending[:count]:
        print(f"{'=' * 70}\n分片 {b.store.name(rng)}（{len(b.wanted_in(rng))} 题）\n{'=' * 70}\n")
        for no in sorted(b.wanted_in(rng)):
            q = b.qmap[no]
            e = q["enrichment"]
            disputed = rivals(q)
            head = f"### #{no}  [{q['kind']} · 应选 {q['pick']} 项]  fp={fingerprint(q)}"
            if disputed:
                head += "   ⚠ 要写分歧说明：" + "，".join(f"{s} {a}" for s, a in disputed) + f" ≠ AI {e['verdict']}"
            print(head)
            print(f"\n{q['stem']}\n")
            for ch in q["choices"]:
                print(f"  {ch['label']}. {ch['body']}")
            c = claims_of(q)
            bl = (c.get("bank_label") or {}).get("answer") or "—"
            cv = c.get("community_vote")
            vote = (f"{cv['answer']} [" + " ".join(f"{k}{v}%" for k, v in cv.get("distribution", {}).items()) + "]"
                    if cv else "无数据")
            print(f"\n  AI 裁决: {e['verdict']}（{e['confidence']}）   题库标注: {bl}   社区投票: {vote}")
            print(f"\n  —— 已有的裁决理由 ——\n{e['reasoning']}")
            if with_explanation:
                print(f"\n  —— 已有的解析 ——\n{e['explanation']}")
            print()


def cmd_write(b: Bank, src: Path) -> None:
    if not src.exists():
        sys.exit(f"✗ 找不到输入文件: {src}")
    try:
        items = load_items(src)
    except (SyntaxError, ValueError) as e:
        sys.exit(f"✗ 输入文件读不出 ITEMS: {e}")
    if not items:
        sys.exit("✗ ITEMS 为空")

    errs = []
    for it in items:
        no = it.get("no") if isinstance(it, dict) else None
        if no not in b.qmap:
            errs.append(f"#{no}: 题号不在题库中")
            continue
        q = b.qmap[no]
        if reason := skip_reason(q):
            errs.append(f"#{no}: 这题不需要点评（{reason}）")
            continue
        if it.get("fp") != fingerprint(q):
            errs.append(f"#{no}: fp 不符（期望 {fingerprint(q)}，收到 {it.get('fp') or '缺失'}）"
                        f" —— 先跑 next 取真实题面，不要凭记忆写")
            continue
        errs += [f"#{no}: {e}" for e in check_item({k: v for k, v in it.items() if k != "fp"}, q)]
    nos = [it.get("no") for it in items if isinstance(it, dict)]
    if len(nos) != len(set(nos)):
        errs.append("同一批里题号重复")
    if errs:
        print(f"✗ 校验未通过（{len(errs)} 个问题），未写入任何文件：")
        for e in errs[:30]:
            print(f"    {e}")
        if len(errs) > 30:
            print(f"    …… 另有 {len(errs) - 30} 个")
        sys.exit(1)

    stored = [{k: v for k, v in it.items() if k != "fp"} for it in items]
    meta = {"bank": b.slug, "stage": STAGE, "locale": b.locale, "model": "claude（Claude Code 订阅额度）"}
    for rng, n, total in b.store.write(stored, meta):
        print(f"✓ {b.store.name(rng)}  写入 {n} 题，该片现有 {total}/{len(b.wanted_in(rng))}")
    for it in sorted(stored, key=lambda x: x["no"]):
        print(f"     #{it['no']}  {b.qmap[it['no']]['stem'][:40]}…")


def cmd_check(b: Bank) -> None:
    total = 0
    for rng in b.store.ranges:
        if not b.wanted_in(rng) or not b.store.path(rng).exists():
            continue
        errs = b.check_shard(rng)
        total += len(errs)
        print(("✓ " if not errs else f"✗ ") + b.store.name(rng) + (f"  ({len(errs)} 个问题)" if errs else ""))
        for e in errs[:20]:
            print(f"    {e}")
    print(f"\n{'✓ 已写的分片全部通过' if total == 0 else f'✗ 共 {total} 个问题'}")
    sys.exit(1 if total else 0)


def cmd_merge(b: Bank, partial: bool) -> None:
    done, bad, todo = b.scan()
    if bad:
        sys.exit(f"✗ 有 {len(bad)} 片未通过校验：{[b.store.name(r) for r in bad]}，先修好再 merge")
    if todo and not partial:
        sys.exit(f"✗ 还有 {len(todo)} 片没写。要先合并已完成的部分，加 --partial")
    items = []
    for rng in done:
        if b.wanted_in(rng):
            items += b.store.read(rng)["items"]
    out = bank_dir(b.slug) / "choice_notes.json"
    write_json(out, {
        "bank": b.slug,
        "locale": b.locale,
        "complete": not todo,
        "merged_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "items": sorted(items, key=lambda it: it["no"]),
    })
    flag = "" if not todo else f"（部分：还有 {len(todo)} 片未写）"
    print(f"✓ {rel(out)}  {len(items)}/{len(b.wanted)} 题{flag}")


def main() -> None:
    ap = argparse.ArgumentParser(description="Melete 逐项点评编排器")
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("status", "check"):
        sub.add_parser(name).add_argument("bank")
    p = sub.add_parser("next")
    p.add_argument("bank")
    p.add_argument("--count", type=int, default=1, help="一次输出几片")
    p.add_argument("--no-explanation", action="store_true", help="不打印已有解析（只看裁决理由）")
    p.add_argument("--at", type=int, help="只取包含这个题号的那一片")
    w = sub.add_parser("write")
    w.add_argument("bank")
    w.add_argument("file", type=Path)
    m = sub.add_parser("merge")
    m.add_argument("bank")
    m.add_argument("--partial", action="store_true")
    a = ap.parse_args()
    b = Bank(a.bank)
    {"status": lambda: cmd_status(b),
     "check": lambda: cmd_check(b),
     "next": lambda: cmd_next(b, a.count, not a.no_explanation, a.at),
     "write": lambda: cmd_write(b, a.file),
     "merge": lambda: cmd_merge(b, a.partial)}[a.cmd]()


if __name__ == "__main__":
    main()
