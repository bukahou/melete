#!/usr/bin/env python3
"""
Melete 数据导入管道 — 逐项点评的翻译：choice_notes.json → choice_notes_i18n/<locale>/*.json → choice_notes.<locale>.json

逐项点评（每个选项一两句）和分歧说明（AI 裁决与题库标注不一致的原因）是用户揭晓答案后最先读的文字。
本阶段把它们译成另一种界面语言；导入时点评进 choice_explanation（按 locale），分歧说明进 answer_claim_i18n。

  · 只翻点评与分歧说明本身，⛔ 不改任何裁决、不重写点评内容
  · 术语表与题面翻译是同一张（glossary.<locale>.json），校验也是同一个函数（translate.glossary_errors）
  · 分片、可中断、写入前全量校验，与其他阶段同一套做法（shards.py）
  · 指纹覆盖题面与原文点评：原文点评日后改了，对应译文在 check / merge 时判为过期，⛔ 不会静默沿用
  · 题面有意不翻的题（untranslated.<locale>.json）点评也不翻：同一题界面上一律显示原文，缺口看得见

子命令：
  status <bank> --locale ja
  next   <bank> --locale ja [--count N] [--at 题号]
  write  <bank> <file> --locale ja
  check  <bank> --locale ja
  merge  <bank> --locale ja [--partial]
"""
import argparse
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

from choice_notes import FORBIDDEN_MARKS
from paths import bank_dir, rel
from shards import ShardStore, load_items, write_json
from translate import _scan, glossary_errors, looks_untranslated

# 中文点评 20–132 字、分歧说明 84–227 字；日文通常长 1.2–1.8 倍。上限只拦失控，下限另有比例哨兵
NOTE_MIN, NOTE_MAX = 10, 320
DISPUTE_MIN, DISPUTE_MAX = 40, 600
# 译文短于原文这个比例即判为截断或缩写（中译日几乎只会变长）
MIN_LENGTH_RATIO = 0.5


def stage_dir(bank: str, locale: str) -> Path:
    return bank_dir(bank) / "choice_notes_i18n" / locale


def load_source_notes(bank: str) -> dict:
    p = bank_dir(bank) / "choice_notes.json"
    if not p.exists():
        sys.exit(f"✗ 找不到 {rel(p)}，先跑 choice_notes.py merge {bank}")
    return json.loads(p.read_text(encoding="utf-8"))


def note_fp(q_fp: str, src: dict) -> str:
    """译文依赖的全部输入：题面指纹 + 原文点评 + 原文分歧说明。"""
    body = json.dumps({"q": q_fp, "choices": src["choices"], "dispute": src["dispute"]},
                      ensure_ascii=False, sort_keys=True)
    return hashlib.sha256(body.encode("utf-8")).hexdigest()[:8]


def load_translations(data_dir: Path, source_items: dict, raw_fp: dict) -> tuple[list[dict], list[str]]:
    """
    读 merge 的产物 choice_notes.<语言>.json → ([{"locale", "complete", "items": {题号: 条目}}], 过期条目)。

    source_items 是当前的原文点评 {题号: 条目}，raw_fp 是素材原文的题面指纹 {题号: fp}。
    指纹对不上的条目不放进 items、记进过期清单，由调用方决定怎么处理（导入时整批停下）。
    """
    out, stale = [], []
    for path in sorted(data_dir.glob("choice_notes.*.json")):
        doc = json.loads(path.read_text(encoding="utf-8"))
        items = {}
        for it in doc["items"]:
            src = source_items.get(it["no"])
            if src is None or it.get("fp") != note_fp(raw_fp.get(it["no"], ""), src):
                stale.append(f"{path.name} #{it['no']}")
            else:
                items[it["no"]] = it
        out.append({"locale": doc["locale"], "complete": doc.get("complete", False), "items": items})
    return out, stale


class Bank:
    def __init__(self, bank: str, locale: str):
        self.slug, self.locale = bank, locale
        src = load_source_notes(bank)
        if src["locale"] == locale:
            sys.exit(f"✗ {bank} 的点评本来就是 {locale}，不用翻")
        self.source_locale = src["locale"]
        # 题面（整理后）与术语表：与题面翻译同一个入口，保证看到的、校验的都一致
        _, self.qmap, self.glo, face_dir = _scan(bank, locale)
        self.notes = {it["no"]: it for it in src["items"] if it["no"] not in self.glo["_skip"]}
        self.fps = {no: note_fp(self.qmap[no]["_fp"], it) for no, it in self.notes.items()}
        self.face = {}  # 已有的题面译文：给译者对照用
        for p in sorted(face_dir.glob("*.json")) if face_dir.exists() else []:
            for it in json.loads(p.read_text(encoding="utf-8")).get("items", []):
                self.face[it["no"]] = it
        self.store = ShardStore(stage_dir(bank, locale), sorted(self.qmap))

    def wanted_in(self, rng) -> set[int]:
        return {n for n in self.store.members(rng) if n in self.notes}

    def check_shard(self, rng) -> list[str]:
        wanted = self.wanted_in(rng)
        if not wanted:
            return []
        return self.store.check(rng, wanted, lambda it: check_item(it, self.notes[it["no"]], self.fps[it["no"]], self.glo))

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


def _text_errors(name: str, text, src: str, lo: int, hi: int) -> list[str]:
    if not isinstance(text, str) or not text.strip():
        return [f"{name} 为空"]
    errs = []
    if text != text.strip():
        errs.append(f"{name} 首尾有空白")
    if not lo <= len(text) <= hi:
        errs.append(f"{name} {len(text)} 字，应在 {lo}–{hi} 字之间")
    elif len(text) < len(src) * MIN_LENGTH_RATIO:
        errs.append(f"{name} 只有 {len(text)} 字，原文 {len(src)} 字 —— 疑似漏译或缩写")
    if marks := [m for m in FORBIDDEN_MARKS if m in text]:
        errs.append(f"{name} 含 {[m.encode('unicode_escape').decode() for m in marks]}，须为一行纯文本")
    if why := looks_untranslated(text):
        errs.append(f"{name} {why}")
    return errs


def check_item(it: dict, src: dict, want_fp: str, glo: dict) -> list[str]:
    """校验一条译文：指纹仍对得上、字母与原文一致、每条一行纯文本、确实译了、术语表与英文名原样保留。"""
    if set(it) != {"no", "fp", "choices", "dispute"}:
        return [f"字段应为 no / fp / choices / dispute，实际 {sorted(it)}"]
    if it["fp"] != want_fp:
        return [f"fp 不符（期望 {want_fp}，收到 {it['fp'] or '缺失'}）—— 原文点评或题面已变，先跑 next 取当前内容重译"]
    if not isinstance(it["choices"], dict):
        return ["choices 须为 {选项字母: 译文} 对象"]
    errs = []
    if set(it["choices"]) != set(src["choices"]):
        errs.append(f"choices 的选项 {sorted(it['choices'])} ≠ 原文点评的选项 {sorted(src['choices'])}")
    parts_src, parts_dst = [], []
    for label in sorted(set(it["choices"]) & set(src["choices"])):
        errs += _text_errors(f"choices[{label}]", it["choices"][label], src["choices"][label], NOTE_MIN, NOTE_MAX)
        parts_src.append(src["choices"][label])
        parts_dst.append(str(it["choices"][label]))
    if src["dispute"] is None:
        if it["dispute"] is not None:
            errs.append("原文没有分歧说明，dispute 应为 None")
    else:
        errs += _text_errors("dispute", it["dispute"], src["dispute"], DISPUTE_MIN, DISPUTE_MAX)
        parts_src.append(src["dispute"])
        parts_dst.append(str(it["dispute"] or ""))
    return errs + glossary_errors(parts_src, parts_dst, glo, glo.get("_senses", {}).get(src["no"]))


GUIDE = """\
逐项点评与分歧说明的翻译（{src} → {dst}）。每题一个条目：

  {{"no": 题号, "fp": "标题行里的 fp", "choices": {{"A": "…", …}}, "dispute": None 或 "…"}}

· 忠实翻译，⛔ 不改观点、不增删内容；每条一行纯文本（⛔ 不用 **、反引号、竖线、换行）
· 产品名 / 功能名保留英文原样（Systems Manager、S3 File Gateway …），⛔ 不改写成片假名
· 下面的术语表是封闭的：表里的词，译文必须用指定译词
· 原文没有分歧说明的题，dispute 写 None
· 对照着看【题干与选项】（有日文的显示日文），点评要和选项说的是同一件事、用同一个叫法
· 写入：把 ITEMS = [...] 存成一个 .py 文件，然后
    python3 pipeline/core/notes_i18n.py write {bank} <文件> --locale {dst}
"""


def cmd_status(b: Bank) -> None:
    done, bad, todo = b.scan()
    n_done = sum(len(b.wanted_in(r)) for r in done)
    print(f"题库      {b.slug}（{b.source_locale} → {b.locale}）")
    print(f"有点评    {len(b.notes)} 题")
    print(f"已翻      {n_done} 题  ({n_done / max(len(b.notes), 1) * 100:.1f}%)")
    print(f"分片      {len(b.store.ranges)} 片：完成 {len(done)} · 校验失败 {len(bad)} · 待写 {len(todo)}")
    if bad:
        print(f"校验失败  {[b.store.name(r) for r in bad]}")


def cmd_next(b: Bank, count: int, at: int | None) -> None:
    _, bad, todo = b.scan()
    pending = sorted(bad + todo)
    if at is not None:
        pending = [r for r in pending if r[0] <= at <= r[1]]
        if not pending:
            sys.exit(f"✗ #{at} 所在的分片已完成，或没有点评")
    if not pending:
        print("✓ 全部分片已完成且校验通过，可以跑 merge 了")
        return
    print(GUIDE.format(src=b.source_locale, dst=b.locale, bank=b.slug))
    for k, v in sorted(b.glo["terms"].items()):
        print(f"    {k} → {v}")
    print()
    for rng in pending[:count]:
        print(f"{'=' * 70}\n分片 {b.store.name(rng)}（{len(b.wanted_in(rng))} 题）\n{'=' * 70}\n")
        for no in sorted(b.wanted_in(rng)):
            q, src, face = b.qmap[no], b.notes[no], b.face.get(no) or {}
            print(f"### #{no}  fp={b.fps[no]}")
            print(f"[题干] {face.get('stem') or q['stem']}")
            for ch in q["choices"]:
                shown = (face.get("choices") or {}).get(ch["label"]) or ch["body"]
                print(f"  [{ch['label']}] {shown}")
                print(f"      点评: {src['choices'].get(ch['label'], '（无）')}")
            print(f"  分歧说明: {src['dispute'] if src['dispute'] else 'None'}\n")


def cmd_write(b: Bank, path: Path) -> None:
    if not path.exists():
        sys.exit(f"✗ 找不到输入文件: {path}")
    try:
        items = load_items(path)
    except (SyntaxError, ValueError) as e:
        sys.exit(f"✗ 输入文件读不出 ITEMS: {e}")
    if not items:
        sys.exit("✗ ITEMS 为空")
    errs = []
    for it in items:
        no = it.get("no") if isinstance(it, dict) else None
        if no not in b.notes:
            errs.append(f"#{no}: 这题没有点评")
            continue
        errs += [f"#{no}: {e}" for e in check_item(it, b.notes[no], b.fps[no], b.glo)]
    nos = [it.get("no") for it in items if isinstance(it, dict)]
    if len(nos) != len(set(nos)):
        errs.append("同一批里题号重复")
    if errs:
        print(f"✗ 校验未通过（{len(errs)} 个问题），未写入任何文件：")
        for e in errs[:30]:
            print(f"    {e}")
        sys.exit(1)
    meta = {"bank": b.slug, "stage": "choice_notes_i18n", "locale": b.locale,
            "source_locale": b.source_locale, "model": "claude（Claude Code 订阅额度）"}
    for rng, n, total in b.store.write(items, meta):
        print(f"✓ {b.store.name(rng)}  写入 {n} 题，该片现有 {total}/{len(b.wanted_in(rng))}")


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
    """
    合并分片。默认要求全部分片写完且通过校验。
    --partial：中途先把已经译好的导进 dev 看效果 —— 逐条用 check_item 筛（⛔ 不是跳过校验），
    放行的只有「这一片还没写满」这一种状态；产物记 complete=false。
    """
    done, bad, todo = b.scan()
    if (bad or todo) and not partial:
        sys.exit(f"✗ 还有 {len(bad)} 片未通过校验、{len(todo)} 片没写。要先合并已完成的部分，加 --partial")
    items, dropped = [], 0
    for rng in b.store.ranges:
        if not b.wanted_in(rng) or not b.store.path(rng).exists():
            continue
        for it in b.store.read(rng)["items"]:
            no = it.get("no")
            if no in b.notes and not check_item(it, b.notes[no], b.fps[no], b.glo):
                items.append(it)   # 带着 fp：导入时据此判断译文是否过期
            else:
                dropped += 1
    if dropped:
        print(f"⚠ {dropped} 条未通过校验，已排除")
    out = bank_dir(b.slug) / f"choice_notes.{b.locale}.json"
    complete = len(items) == len(b.notes)
    write_json(out, {
        "bank": b.slug,
        "locale": b.locale,
        "source_locale": b.source_locale,
        "complete": complete,
        "merged_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "items": sorted(items, key=lambda it: it["no"]),
    })
    flag = "" if complete else "（部分）"
    print(f"✓ {rel(out)}  {len(items)}/{len(b.notes)} 题{flag}")


def main() -> None:
    ap = argparse.ArgumentParser(description="Melete 逐项点评翻译编排器")
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--locale", default="ja", help="目标语言（默认 ja）")
    common.add_argument("bank")
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("status", "check"):
        sub.add_parser(name, parents=[common])
    p = sub.add_parser("next", parents=[common])
    p.add_argument("--count", type=int, default=1)
    p.add_argument("--at", type=int, help="只取包含这个题号的那一片")
    w = sub.add_parser("write", parents=[common])
    w.add_argument("file", type=Path)
    m = sub.add_parser("merge", parents=[common])
    m.add_argument("--partial", action="store_true")
    a = ap.parse_args()
    b = Bank(a.bank, a.locale)
    {"status": lambda: cmd_status(b),
     "check": lambda: cmd_check(b),
     "next": lambda: cmd_next(b, a.count, a.at),
     "write": lambda: cmd_write(b, a.file),
     "merge": lambda: cmd_merge(b, a.partial)}[a.cmd]()


if __name__ == "__main__":
    main()
