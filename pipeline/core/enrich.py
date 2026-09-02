#!/usr/bin/env python3
"""
Melete 数据导入管道 — 第二段：questions.json → enriched/*.json → enriched.json

不走 Batches API，改由 Claude Code 订阅额度逐片生成（额度闲置，Batches 那笔钱没必要花）。
代价是跨多次限额周期、可能换会话，所以本模块的核心职责是**让中断可恢复**：

  · 按题号切成固定分片，一片一个文件
  · 分片文件存在即完成 —— 不维护单独的进度清单，扫目录即状态
    （manifest 与实际文件不同步是这类流程最经典的坑，直接不给它机会）
  · 每片独立校验，坏片删掉重跑，不影响其他片

子命令：
  status <bank>   进度总览
  next   <bank>   打印下一个待做分片的题目（喂给解答会话）
  check  <bank>   校验分片，报告问题
  merge  <bank>   合并全部分片 → enriched.json

题库特有的约束（考纲域、concept 种子、服务命名）从
pipeline/banks/<bank>/enrich_spec.json 读取 —— 本模块零题库特有逻辑。
"""
import argparse
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SHARD_SIZE = 25
KEBAB = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")
CONFIDENCE = {"high", "medium", "low"}
ITEM_KEYS = {"no", "verdict", "confidence", "reasoning", "explanation",
             "domain", "services", "concepts", "data_issue"}
# notes 是 advisory 级：题目有瑕疵但答案仍可判定时的登记处（如译文撞词）。
# 与 data_issue 的分工：data_issue = 结构性不可判（verdict 必须留空），
# notes = 有毛病但不阻断。可选字段，旧分片没有它也合法。
OPTIONAL_KEYS = {"notes"}


# ---------- 载入 ----------

def load_bank(bank: str):
    qs_path = REPO / "data" / bank / "questions.json"
    spec_path = REPO / "pipeline" / "banks" / bank / "enrich_spec.json"
    if not qs_path.exists():
        sys.exit(f"✗ 找不到 {qs_path.relative_to(REPO)}，先跑 ingest.py")
    if not spec_path.exists():
        sys.exit(f"✗ 找不到 {spec_path.relative_to(REPO)}，新题库需要先写富化规格")
    doc = json.loads(qs_path.read_text(encoding="utf-8"))
    spec = json.loads(spec_path.read_text(encoding="utf-8"))
    return doc, spec


def shard_dir(bank: str) -> Path:
    return REPO / "data" / bank / "enriched"


def shard_ranges(total_no: list[int]) -> list[tuple[int, int]]:
    """按题号切片。题号不假设连续，但分片边界用题号表达，便于人工对照。"""
    lo, hi = min(total_no), max(total_no)
    return [(s, min(s + SHARD_SIZE - 1, hi)) for s in range(lo, hi + 1, SHARD_SIZE)]


def shard_name(rng: tuple[int, int]) -> str:
    return f"{rng[0]:04d}-{rng[1]:04d}.json"


def claims_of(q: dict) -> dict:
    return {c["source"]: c for c in q["claims"]}


def is_contested(q: dict) -> tuple[bool, list[str]]:
    """通用判据：这道题的答案是否存在不确定性。不含任何题库特有逻辑。"""
    c = claims_of(q)
    bl = (c.get("bank_label") or {}).get("answer")
    cv = c.get("community_vote")
    why = []
    if q.get("warnings"):
        why.append("解析告警")
    if cv is None:
        why.append("无社区投票（仅单一来源）")
    else:
        if bl and "".join(sorted(cv["answer"])) != "".join(sorted(bl)):
            why.append("标注与社区投票不一致")
        if (cv.get("confidence") or 0) < 60:
            why.append(f"社区共识低（{cv.get('confidence')}%）")
    return bool(why), why


# ---------- 校验 ----------

def check_item(item: dict, q: dict, spec: dict) -> list[str]:
    errs = []
    extra = set(item) - ITEM_KEYS - OPTIONAL_KEYS
    missing = ITEM_KEYS - set(item)
    if extra:
        errs.append(f"多余字段 {sorted(extra)}")
    if missing:
        errs.append(f"缺字段 {sorted(missing)}")
    if missing:
        return errs

    labels = {ch["label"] for ch in q["choices"]}
    verdict = item["verdict"] or ""
    issue = item["data_issue"]

    if issue:
        if verdict:
            errs.append("data_issue 非空时 verdict 应为空字符串")
    else:
        if not verdict:
            errs.append("verdict 为空但未说明 data_issue")
        elif set(verdict) - labels:
            errs.append(f"verdict {verdict!r} 含选项中不存在的字母（可选 {sorted(labels)}）")
        elif list(verdict) != sorted(set(verdict)):
            errs.append(f"verdict {verdict!r} 须为升序且不重复")
        elif len(verdict) != q["pick"]:
            errs.append(f"verdict {verdict!r} 长度 {len(verdict)} ≠ 应选 {q['pick']} 项")

    if item["confidence"] not in CONFIDENCE:
        errs.append(f"confidence {item['confidence']!r} 不在 {sorted(CONFIDENCE)}")
    for k in ("reasoning", "explanation"):
        if not isinstance(item[k], str) or not item[k].strip():
            errs.append(f"{k} 为空")
    if str(item["domain"]) not in spec["domains"]:
        errs.append(f"domain {item['domain']!r} 不在 {sorted(spec['domains'])}")
    for k in ("services", "concepts"):
        v = item[k]
        if not isinstance(v, list) or not v or not all(isinstance(x, str) and x.strip() for x in v):
            errs.append(f"{k} 须为非空字符串列表")
    if "notes" in item and (not isinstance(item["notes"], str) or not item["notes"].strip()):
        errs.append("notes 存在时须为非空字符串")
    if isinstance(item["concepts"], list):
        bad = [c for c in item["concepts"] if isinstance(c, str) and not KEBAB.match(c)]
        if bad:
            errs.append(f"concepts 须为 kebab-case，违规 {bad}")

    contested, _ = is_contested(q)
    if contested and not issue and len(item["reasoning"]) < 80:
        errs.append(f"存在答案分歧的题，reasoning 仅 {len(item['reasoning'])} 字（要求 ≥80，须说明分歧方错在哪）")
    return errs


def check_shard(path: Path, qmap: dict, spec: dict) -> list[str]:
    errs = []
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception as e:
        return [f"JSON 解析失败: {e}"]
    lo, hi = (int(x) for x in path.stem.split("-"))
    want = {n for n in qmap if lo <= n <= hi}
    got = {it.get("no") for it in data.get("items", [])}
    if want - got:
        errs.append(f"缺题号 {sorted(want - got)}")
    if got - want:
        errs.append(f"多出不属于本片的题号 {sorted(got - want)}")
    for it in data.get("items", []):
        if it.get("no") not in qmap:
            continue
        for e in check_item(it, qmap[it["no"]], spec):
            errs.append(f"#{it['no']}: {e}")
    return errs


# ---------- 子命令 ----------

def cmd_status(bank: str) -> None:
    doc, spec = load_bank(bank)
    qs = doc["questions"]
    qmap = {q["no"]: q for q in qs}
    d = shard_dir(bank)
    ranges = shard_ranges(list(qmap))
    done, bad, todo = [], [], []
    for r in ranges:
        p = d / shard_name(r)
        if not p.exists():
            todo.append(r)
        elif check_shard(p, qmap, spec):
            bad.append(r)
        else:
            done.append(r)
    n_done = sum(len([n for n in qmap if r[0] <= n <= r[1]]) for r in done)
    print(f"题库      {bank}")
    print(f"题目总数  {len(qs)}")
    print(f"分片      {len(ranges)} 片 × {SHARD_SIZE} 题")
    print(f"已完成    {len(done)} 片 / {n_done} 题  ({n_done/len(qs)*100:.1f}%)")
    print(f"校验失败  {len(bad)} 片" + (f"  → {[shard_name(r) for r in bad]}" if bad else ""))
    print(f"待处理    {len(todo)} 片")
    if todo:
        print(f"\n下一片    {shard_name(todo[0])}   （跑 enrich.py next {bank} 取题目）")


def cmd_next(bank: str, count: int) -> None:
    doc, spec = load_bank(bank)
    qmap = {q["no"]: q for q in doc["questions"]}
    d = shard_dir(bank)
    todo = [r for r in shard_ranges(list(qmap))
            if not (d / shard_name(r)).exists() or check_shard(d / shard_name(r), qmap, spec)]
    if not todo:
        print("✓ 全部分片已完成且校验通过，可以跑 merge 了")
        return
    for r in todo[:count]:
        out = d / shard_name(r)
        print(f"{'='*70}\n写入目标: data/{bank}/enriched/{shard_name(r)}\n{'='*70}\n")
        for no in sorted(n for n in qmap if r[0] <= n <= r[1]):
            q = qmap[no]
            c = claims_of(q)
            contested, why = is_contested(q)
            print(f"### 问题 #{no}  [{q['kind']} · 应选 {q['pick']} 项]"
                  + (f"  ⚠ {'；'.join(why)}" if contested else ""))
            print(f"\n{q['stem']}\n")
            for ch in q["choices"]:
                print(f"  {ch['label']}. {ch['body']}")
            bl = (c.get("bank_label") or {}).get("answer")
            print(f"\n  题库标注: {bl or '—'}")
            cv = c.get("community_vote")
            if cv:
                dist = "  ".join(f"{k}({v}%)" for k, v in cv.get("distribution", {}).items())
                print(f"  社区投票: {cv['answer']}  [{dist}]")
            else:
                print(f"  社区投票: 无数据")
            if q.get("warnings"):
                print(f"  解析告警: {q['warnings']}")
            print()


def cmd_check(bank: str) -> None:
    doc, spec = load_bank(bank)
    qmap = {q["no"]: q for q in doc["questions"]}
    d = shard_dir(bank)
    files = sorted(d.glob("*.json")) if d.exists() else []
    if not files:
        sys.exit("✗ 还没有任何分片")
    total = 0
    for p in files:
        errs = check_shard(p, qmap, spec)
        total += len(errs)
        if errs:
            print(f"✗ {p.name}  ({len(errs)} 个问题)")
            for e in errs[:20]:
                print(f"    {e}")
            if len(errs) > 20:
                print(f"    …… 另有 {len(errs)-20} 个")
        else:
            print(f"✓ {p.name}")
    print(f"\n{'✓ 全部通过' if total == 0 else f'✗ 共 {total} 个问题'}")
    sys.exit(1 if total else 0)


def cmd_merge(bank: str) -> None:
    doc, spec = load_bank(bank)
    qmap = {q["no"]: q for q in doc["questions"]}
    d = shard_dir(bank)
    items, problems = {}, 0
    for p in sorted(d.glob("*.json")):
        errs = check_shard(p, qmap, spec)
        if errs:
            problems += 1
            print(f"✗ {p.name} 校验未过，已跳过（{len(errs)} 个问题）")
            continue
        for it in json.loads(p.read_text(encoding="utf-8"))["items"]:
            items[it["no"]] = it
    if problems:
        sys.exit(f"\n✗ 有 {problems} 片未通过校验，先修好再 merge")

    out = REPO / "data" / bank / "enriched.json"
    doc["enriched_at"] = datetime.now(timezone.utc).isoformat(timespec="seconds")
    for q in doc["questions"]:
        e = items.get(q["no"])
        if not e:
            continue
        q["enrichment"] = {k: v for k, v in e.items() if k != "no"}
        if e["verdict"]:
            q["claims"].append({
                "source": "ai_verdict",
                "answer": e["verdict"],
                "confidence": {"high": 90, "medium": 70, "low": 50}[e["confidence"]],
                "rationale": e["reasoning"],
            })
    json.dump(doc, out.open("w", encoding="utf-8"), ensure_ascii=False, indent=1)
    n = sum(1 for q in doc["questions"] if "enrichment" in q)
    print(f"✓ data/{bank}/enriched.json   已富化 {n}/{len(doc['questions'])} 题")


def main() -> None:
    ap = argparse.ArgumentParser(description="Melete AI 富化编排器")
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("status", "check", "merge"):
        sub.add_parser(name).add_argument("bank")
    p = sub.add_parser("next")
    p.add_argument("bank")
    p.add_argument("--count", type=int, default=1, help="一次输出几片")
    a = ap.parse_args()
    {"status": lambda: cmd_status(a.bank),
     "check": lambda: cmd_check(a.bank),
     "merge": lambda: cmd_merge(a.bank),
     "next": lambda: cmd_next(a.bank, a.count)}[a.cmd]()


if __name__ == "__main__":
    main()
