#!/usr/bin/env python3
"""
Melete 数据导入管道 — 逐题校对题面：整理后的题干与选项 → proofread/*.json → proofread.json

素材是第三方机翻的中文，题面里有机翻的产品名、错字、抽取残留和串位的选项。
本阶段让写校对的会话逐题读「用户实际看到的文字」（facetext.FacePreparer 整理之后），
产出可以机械套用的修正候选：哪个字段、原文哪一段、改成什么、为什么。

  · 候选不直接生效：审过之后，产品名进 term_normalize.json（全库统一），其余进 errata.json（逐题）
  · 分片、可中断、写入前全量校验，与富化 / 逐项点评同一套做法（见 shards.py）

子命令：
  status <bank>                    进度
  next   <bank> [--count N] [--at 题号]
                                   打印待校对的分片（喂给写校对的会话）
  write  <bank> <file>             从 Python 字面量文件（ITEMS = [...]）写入分片
  check  <bank>                    校验全部分片的结构
  merge  <bank> [--partial]        合并成 proofread.json
  lint   <bank> [--list]           只读：点评 / 解析里出现、题面里找不到的英文专名（补规则的候选，只提名）
"""
import argparse
import copy
import json
import re
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

from enrich import fingerprint
from facetext import ERRATA_KINDS, ErrataError, FacePreparer, field_names, get_field, locate
from paths import REPO, bank_dir, rel
from shards import ShardStore, load_items, write_json

STAGE = "proofread"
FIND_MAX, REPLACE_MAX = 120, 200
WHY_MIN, WHY_MAX = 4, 120
ISSUE_MIN, ISSUE_MAX = 4, 200
MAX_FIXES = 12
FIX_KEYS = {"field", "find", "replace", "kind", "why"}
ISSUE_KEYS = {"field", "issue"}
LINE_BREAKS = ("\n", "\r")


def imported(q: dict) -> bool:
    """会进库、用户看得到的题：有选项，且不是被并入正本的重复题。"""
    return bool(q.get("choices")) and not any(w.startswith("duplicate_of_") for w in q.get("warnings", []))


class Bank:
    def __init__(self, bank: str):
        self.slug = bank
        src = bank_dir(bank) / "enriched.json"
        if not src.exists():
            sys.exit(f"✗ 找不到 {rel(src)}")
        spec_dir = REPO / "pipeline" / "banks" / bank
        spec_path = spec_dir / "enrich_spec.json"
        spec = json.loads(spec_path.read_text(encoding="utf-8")) if spec_path.exists() else {}
        doc = json.loads(src.read_text(encoding="utf-8"))
        self.raw = {q["no"]: q for q in doc["questions"]}
        shown = copy.deepcopy(doc["questions"])
        try:
            FacePreparer(spec, spec_dir, bank_dir(bank)).prepare(shown)
        except ErrataError as e:
            sys.exit("✗ 勘误表不成立，先修好再校对：\n    " + "\n    ".join(e.problems[:20]))
        self.shown = {q["no"]: q for q in shown}
        self.wanted = {n for n, q in self.raw.items() if imported(q)}
        self.store = ShardStore(bank_dir(bank) / STAGE, list(self.raw))

    def face(self, no: int) -> dict[str, str]:
        q = self.shown[no]
        return {f: get_field(q, f) for f in field_names(q)}

    def wanted_in(self, rng) -> set[int]:
        return {n for n in self.store.members(rng) if n in self.wanted}

    def check_shard(self, rng) -> list[str]:
        wanted = self.wanted_in(rng)
        if not wanted:
            return []
        return self.store.check(rng, wanted, lambda it: check_item(it, self.face(it["no"]), strict=False))

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


# ---------- 校验 ----------

def _line_errors(name: str, text, lo: int, hi: int) -> list[str]:
    if not isinstance(text, str):
        return [f"{name} 应为字符串"]
    errs = []
    if not lo <= len(text) <= hi:
        errs.append(f"{name} {len(text)} 字，应在 {lo}–{hi} 字之间")
    if any(b in text for b in LINE_BREAKS):
        errs.append(f"{name} 含换行，须为一行")
    return errs


def check_item(it: dict, face: dict[str, str], strict: bool) -> list[str]:
    """
    校验一条校对结果（不含 fp）。strict=True 时还要求每个 find 在整理后的字段里恰好出现一次、
    同一字段内互不重叠 —— 写入时用；已落盘的分片只查结构（规则表或勘误更新后，旧候选可能已经不再需要）。
    """
    if set(it) != {"no", "fixes", "unfixable"}:
        return [f"字段应为 no / fixes / unfixable，实际 {sorted(it)}"]
    errs = []
    fixes, issues = it["fixes"], it["unfixable"]
    if not isinstance(fixes, list) or not isinstance(issues, list):
        return ["fixes 与 unfixable 须为列表（没有就写 []）"]
    if len(fixes) > MAX_FIXES:
        errs.append(f"fixes 有 {len(fixes)} 条，上限 {MAX_FIXES}（问题太多的题写进 unfixable）")
    spans: dict[str, list[tuple[int, int]]] = {}
    for i, f in enumerate(fixes):
        where = f"fixes[{i}]"
        if not isinstance(f, dict) or set(f) != FIX_KEYS:
            errs.append(f"{where}: 字段应为 {sorted(FIX_KEYS)}")
            continue
        if f["field"] not in face:
            errs.append(f"{where}: 没有字段 {f['field']!r}（可用：{', '.join(face)}）")
            continue
        if f["kind"] not in ERRATA_KINDS:
            errs.append(f"{where}: kind 应为 {ERRATA_KINDS} 之一")
        errs += _line_errors(f"{where}.find", f["find"], 1, FIND_MAX)
        errs += _line_errors(f"{where}.replace", f["replace"], 0, REPLACE_MAX)
        errs += _line_errors(f"{where}.why", f["why"], WHY_MIN, WHY_MAX)
        if isinstance(f["find"], str) and f["find"] == f["replace"]:
            errs.append(f"{where}: find 与 replace 相同")
        if strict and isinstance(f["find"], str) and f["find"]:
            hits = locate(face[f["field"]], f["find"])
            if len(hits) != 1:
                errs.append(f"{where}: {f['find']!r} 在 {f['field']} 里出现 {len(hits)} 次，应恰好 1 次"
                            f"（从显示的文字原样复制；不唯一就多带几个字）")
            else:
                spans.setdefault(f["field"], []).append((hits[0], hits[0] + len(f["find"])))
    for field, ss in spans.items():
        ss.sort()
        if any(a_end > b_start for (_, a_end), (b_start, _) in zip(ss, ss[1:])):
            errs.append(f"{field}: 有两条 fix 的 find 互相重叠，合成一条")
    for i, x in enumerate(issues):
        where = f"unfixable[{i}]"
        if not isinstance(x, dict) or set(x) != ISSUE_KEYS:
            errs.append(f"{where}: 字段应为 {sorted(ISSUE_KEYS)}")
            continue
        if x["field"] not in face and x["field"] != "question":
            errs.append(f"{where}: field 应为 question 或 {', '.join(face)}")
        errs += _line_errors(f"{where}.issue", x["issue"], ISSUE_MIN, ISSUE_MAX)
    return errs


# ---------- 子命令 ----------

def cmd_status(b: Bank) -> None:
    done, bad, todo = b.scan()
    n_done = sum(len(b.wanted_in(r)) for r in done)
    print(f"题库      {b.slug}")
    print(f"需要校对  {len(b.wanted)} 题（另有 {len(b.raw) - len(b.wanted)} 题不进库：无选项或重复）")
    print(f"已完成    {n_done} 题  ({n_done / max(len(b.wanted), 1) * 100:.1f}%)")
    print(f"分片      {len(b.store.ranges)} 片：完成 {len(done)} · 校验失败 {len(bad)} · 待写 {len(todo)}")
    if bad:
        print(f"校验失败  {[b.store.name(r) for r in bad]}")
    if todo:
        print(f"\n下一片    {b.store.name(todo[0])}   （python3 pipeline/core/proofread.py next {b.slug}）")


GUIDE = """\
逐题校对题面（题干 + 选项）：只找会误导或妨碍阅读的错，⛔ 不润色、不改文风。

每题一个条目（没有问题的题也要写，fixes 与 unfixable 都是 []）：

  {{"no": 题号, "fp": "标题行里的 fp", "fixes": [...], "unfixable": [...]}}

fixes 每条：{{"field": "stem" 或 "choice:A", "find": "原文片段", "replace": "改成", "kind": "…", "why": "依据"}}
  · find 从下面显示的文字里原样复制，必须在该字段里恰好出现一次（不唯一就多带几个字）
  · kind：
      term      机翻的 AWS 产品名 / 功能名 → 官方英文名
                例：「系统管理器状态管理器」→「Systems Manager State Manager」、「中转网关」→「Transit Gateway」
                ⛔ AWS 中文文档本身就用中文的概念名词不动：存储桶、区域、可用区、安全组、子网、只读副本、生命周期策略……
                ⛔ 已经是英文的不动；拿不准官方英文写法时不改，写进 unfixable
      typo      错字、英文拼写错误（例：Balanacer → Balancer、uswest-2 → us-west-2）
      garble    抽取残留：被拆开的词、多余的空格或字符（例：「us-east- 1」→「us-east-1」）
      misalign  选项文字串位：一个选项的内容跑进了另一个选项（两边各写一条 fix）
      wording   译法错到会让人读错意思：只改错的那几个字，不重写整句
  · ⛔ 不改变题意和答案；不补原文没有的信息；不动选项字母
  · 一行纯文本；find ≤ {find_max} 字，replace ≤ {replace_max} 字（可为空串，表示删掉），why {why_min}–{why_max} 字
unfixable 每条：{{"field": "stem" / "choice:X" / "question", "issue": "问题描述"}}
  · 看出有问题、但没有把握改或改不了：文字被截断、缺选项、要看原题才能确定、不确定官方英文名
写入：把 ITEMS = [...] 存成一个 .py 文件，然后
    python3 pipeline/core/proofread.py write {bank} <文件>
"""


def cmd_next(b: Bank, count: int, at: int | None) -> None:
    _, bad, todo = b.scan()
    pending = sorted(bad + todo)
    if at is not None:
        pending = [r for r in pending if r[0] <= at <= r[1]]
        if not pending:
            sys.exit(f"✗ #{at} 所在的分片已完成，或不需要校对")
    if not pending:
        print("✓ 全部分片已完成且校验通过，可以跑 merge 了")
        return
    print(GUIDE.format(find_max=FIND_MAX, replace_max=REPLACE_MAX, why_min=WHY_MIN, why_max=WHY_MAX, bank=b.slug))
    for rng in pending[:count]:
        print(f"{'=' * 70}\n分片 {b.store.name(rng)}（{len(b.wanted_in(rng))} 题）\n{'=' * 70}\n")
        for no in sorted(b.wanted_in(rng)):
            q = b.shown[no]
            print(f"### #{no}  [{q['kind']} · 应选 {q['pick']} 项]  fp={fingerprint(b.raw[no])}")
            print(f"\n[stem] {q['stem']}\n")
            for ch in q["choices"]:
                print(f"  [choice:{ch['label']}] {ch['body']}")
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
        if no not in b.wanted:
            errs.append(f"#{no}: 不是要校对的题（题号不在题库中，或不进库）")
            continue
        if it.get("fp") != fingerprint(b.raw[no]):
            errs.append(f"#{no}: fp 不符（期望 {fingerprint(b.raw[no])}，收到 {it.get('fp') or '缺失'}）"
                        f" —— 先跑 next 取真实题面，不要凭记忆写")
            continue
        errs += [f"#{no}: {e}" for e in check_item({k: v for k, v in it.items() if k != "fp"}, b.face(no), strict=True)]
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
    meta = {"bank": b.slug, "stage": STAGE, "model": "claude（Claude Code 订阅额度）"}
    for rng, n, total in b.store.write(stored, meta):
        print(f"✓ {b.store.name(rng)}  写入 {n} 题，该片现有 {total}/{len(b.wanted_in(rng))}")
    n_fix = sum(len(it["fixes"]) for it in stored)
    n_issue = sum(len(it["unfixable"]) for it in stored)
    print(f"     本批 fixes {n_fix} 条 · unfixable {n_issue} 条 · 没有问题的题 "
          f"{sum(1 for it in stored if not it['fixes'] and not it['unfixable'])} 道")


def cmd_check(b: Bank) -> None:
    total = 0
    for rng in b.store.ranges:
        if not b.wanted_in(rng) or not b.store.path(rng).exists():
            continue
        errs = b.check_shard(rng)
        total += len(errs)
        print(("✓ " if not errs else "✗ ") + b.store.name(rng) + (f"  ({len(errs)} 个问题)" if errs else ""))
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
    out = bank_dir(b.slug) / "proofread.json"
    write_json(out, {
        "bank": b.slug,
        "complete": not todo,
        "merged_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "items": sorted(items, key=lambda it: it["no"]),
    })
    kinds = Counter(f["kind"] for it in items for f in it["fixes"])
    print(f"✓ {rel(out)}  {len(items)}/{len(b.wanted)} 题 · fixes {sum(kinds.values())} 条 {dict(kinds)}"
          f" · unfixable {sum(len(it['unfixable']) for it in items)} 条")


# 两个以上大写开头的词连写：State Manager、Transit Gateway、Kinesis Data Streams
_PROPER = re.compile(r"\b(?:[A-Z][A-Za-z0-9]*\s){1,3}[A-Z][A-Za-z0-9]*\b")


def cmd_lint(b: Bank, show_list: bool) -> None:
    """
    只读：某个选项的点评里出现、而该选项文字与题干里都找不到的英文专名 ——
    用户在点评里读到 State Manager，在选项里却看到「状态管理器」，就认不出说的是同一个东西。
    只提名（点评也可能顺带提到选项里本来没有的服务），⛔ 不据此改任何东西。
    """
    notes_path = bank_dir(b.slug) / "choice_notes.json"
    if not notes_path.exists():
        sys.exit(f"✗ 找不到 {rel(notes_path)}：没有逐项点评就没有可比的对象")
    notes = {it["no"]: it["choices"] for it in json.loads(notes_path.read_text(encoding="utf-8"))["items"]}
    squash = lambda s: re.sub(r"\s+", "", s).lower()
    names, per_q = Counter(), {}
    for no in sorted(b.wanted & set(notes)):
        q = b.shown[no]
        stem = squash(q["stem"])
        missing = set()
        for c in q["choices"]:
            seen = stem + squash(c["body"])
            missing |= {f"{c['label']}:{p}" for p in _PROPER.findall(notes[no].get(c["label"], ""))
                        if squash(p) not in seen}
        if missing:
            per_q[no] = sorted(missing)
            names.update(m.split(":", 1)[1] for m in missing)
    print(f"选项点评里有、该选项与题干里都找不到的英文专名：{len(per_q)} 题（上限，只提名）")
    print(f"最多：{names.most_common(25)}")
    if show_list:
        for no, ms in per_q.items():
            print(f"  #{no}: {', '.join(ms)}")


def main() -> None:
    ap = argparse.ArgumentParser(description="Melete 逐题校对题面")
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("status", "check"):
        sub.add_parser(name).add_argument("bank")
    p = sub.add_parser("next")
    p.add_argument("bank")
    p.add_argument("--count", type=int, default=1, help="一次输出几片")
    p.add_argument("--at", type=int, help="只取包含这个题号的那一片")
    w = sub.add_parser("write")
    w.add_argument("bank")
    w.add_argument("file", type=Path)
    m = sub.add_parser("merge")
    m.add_argument("bank")
    m.add_argument("--partial", action="store_true")
    li = sub.add_parser("lint")
    li.add_argument("bank")
    li.add_argument("--list", action="store_true")
    a = ap.parse_args()
    b = Bank(a.bank)
    {"status": lambda: cmd_status(b),
     "check": lambda: cmd_check(b),
     "next": lambda: cmd_next(b, a.count, a.at),
     "write": lambda: cmd_write(b, a.file),
     "merge": lambda: cmd_merge(b, a.partial),
     "lint": lambda: cmd_lint(b, a.list)}[a.cmd]()


if __name__ == "__main__":
    main()
