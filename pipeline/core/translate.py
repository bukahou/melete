#!/usr/bin/env python3
"""
Melete 数据导入管道 — 翻译段：enriched.json → translated/<locale>/*.json → translated.<locale>.json

## 为什么是独立的一段，不是塞进 enrich.py

富化（裁决 / 解析 / 标签）已经跑完了。为了加一门语言把 1019 题重新富化一遍，
是在为不需要重算的东西付钱。翻译只读 enriched.json，⛔ 不碰任何富化结论。

## 为什么产物不写回 enriched.json

`enrich.py merge` 是**从 questions.json 重建** enriched.json 的 ——
译文若寄居在那个文件里，任何一次 re-merge 都会把它整个抹掉，
而且不报错。⇒ 译文必须有自己的文件。

## 断点续跑：照抄 enrich.py 那套，因为它扛过 1019 题

  · 按题号切固定分片，一片一个文件
  · **分片文件存在即完成** —— 不维护进度清单（清单与实际不同步是这类流程最经典的坑）
  · 每片独立校验，坏片删掉重跑

## 解析是【可选】的

题干 + 选项 ≈ 每题 400 字，解析 ≈ 2000 字（占全库 83%）。
不翻题面日本人根本没法做题；解析缺了只是回退到中文并标注。
⇒ 允许先跑一趟只翻题面，解析随后补。status 分两个进度报。

子命令（与 enrich.py 一一对应，刻意同名同形）：
  status <bank> --locale ja
  next   <bank> --locale ja [-n N]
  write  <bank> <file> --locale ja
  check  <bank> --locale ja
  merge  <bank> --locale ja
"""
import argparse
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

from paths import REPO, bank_dir, rel
# ⭐ 复用 enrich 的分片与指纹：两段必须切在同一个边界上、算出同一个指纹，
#    否则「第 3 片」在两段里指的不是同一批题，人工对照立刻失效。
from enrich import SHARD_SIZE, fingerprint, shard_name, shard_ranges

# 平假名 + 片假名。⭐ 这是本模块最重要的哨兵，理由见 check_item。
KANA = re.compile(r"[぀-ゟ゠-ヿ]")
# 译文短于原文这个比例即判为截断。取值宽松是有意的 —— 这条只抓「只译了第一句」，
# ⛔ 不试图评价译文质量，那不是机器能做的事。
MIN_LENGTH_RATIO = 0.25


def load_enriched(bank: str) -> dict:
    p = bank_dir(bank) / "enriched.json"
    if not p.exists():
        sys.exit(f"✗ 找不到 {rel(p)} —— 先把富化跑完（enrich.py merge {bank}）")
    return json.loads(p.read_text(encoding="utf-8"))


def derive_keep(doc: dict) -> list[str]:
    """
    从富化结果里的知识对象轴（AWS 是服务名）自动推出「必须原样保留」的名字。

    ⭐ 不写进 glossary 文件是有意的：
      · 它会跟着题库自动同步 —— 手抄一份必然某天对不上
      · 这些名字派生自**私有仓的题库数据**，⛔ 不该固化进公开的代码仓
        （本仓是公开仓，见根目录 CLAUDE.md「边界」）

    glossary 里的 keep 是【补充】，放这里推不出来的（如「アベイラビリティーゾーン」
    这种不属于服务名、但同样不许乱译的固定表述）。
    """
    names = set()
    for q in doc.get("questions", []):
        for s in (q.get("enrichment") or {}).get("services", []):
            if isinstance(s, str) and s.strip():
                names.add(s.strip())
    # ⚠️ 长的排前面：报错信息里先看到「Route 53」而不是先看到「S3」，更好读
    return sorted(names, key=lambda x: (-len(x), x))


def load_glossary(bank: str, locale: str) -> dict:
    """
    术语表。**没有它就不许开工** —— 理由见模块外的设计说明：

    41 片跨多次会话生成，同一个「最终一致性」会在第 3 片译成「結果整合性」、
    第 40 片译成「最終的な整合性」。备考的人在考场上认的是官方那一个词。
    ⇒ 与 IPA 的封闭词表同一个手法：让缺陷【写不出来】，而不是事后靠人眼捞。
    """
    p = REPO / "pipeline" / "banks" / bank / f"glossary.{locale}.json"
    if not p.exists():
        sys.exit(
            f"✗ 找不到术语表 {rel(p)}\n"
            f"  ⛔ 刻意不允许「先翻着，术语回头统一」——\n"
            f"     41 片跨多次会话，术语一旦漂了就要返工几十片。"
        )
    g = json.loads(p.read_text(encoding="utf-8"))
    return {"terms": g.get("terms", {}), "keep": list(g.get("keep", []))}


def shard_dir(bank: str, locale: str) -> Path:
    return bank_dir(bank) / "translated" / locale


def source_of(q: dict) -> str:
    """
    题干原文。

    ⚠️ enriched.json 的 stem 【永远是原文】，与库里存的不是一回事：
    SAP-C02 的 stem 是英文（中文在 enrichment.translation 里），SAA-C03 的 stem 是中文。
    ⇒ 从这里译永远是「从原文译一次」，⛔ 不会出现 en→zh→ja 的二次失真。
    """
    return q["stem"]


def explanation_of(q: dict) -> str:
    return (q.get("enrichment") or {}).get("explanation") or ""


def check_item(it: dict, q: dict, glo: dict) -> list[str]:
    errs = []
    labels = {c["label"] for c in q["choices"]}

    stem = it.get("stem")
    if not isinstance(stem, str) or not stem.strip():
        errs.append("stem 为空")
    choices = it.get("choices")
    if not isinstance(choices, dict):
        errs.append("choices 须为 {label: 译文} 对象")
        choices = {}
    else:
        if set(choices) != labels:
            errs.append(f"choices 的选项 {sorted(choices)} ≠ 题目选项 {sorted(labels)}")
        for k, v in sorted(choices.items()):
            if not isinstance(v, str) or not v.strip():
                errs.append(f"choices[{k}] 为空")

    expl = it.get("explanation")
    if expl is not None and (not isinstance(expl, str) or not expl.strip()):
        errs.append("explanation 给了但为空 —— 不译就整个不给这个键，⛔ 别给空串")

    extra = set(it) - {"no", "fp", "stem", "choices", "explanation"}
    if extra:
        errs.append(f"多余字段 {sorted(extra)}")

    # ⭐⭐ 假名哨兵 —— 本模块最重要的一条校验。
    #
    # 中文里【没有假名】。所以「忘了翻译、把中文原样贴过来」这个失败模式
    # 必然被抓住，而它恰恰是长文本翻译里最常见、也最难用肉眼在 41 片里发现的。
    #
    # ⚠️ 为什么不用「译文与原文不相等」做判据：那与被测者同源 ——
    # 改几个字、删句标点就能骗过去，而假名骗不过去（要造假名就得真的在写日语）。
    # 这条呼应仓库里那句教训：「验证工具与被验证者共享同一盲区时，验证必然通过」。
    for name, text in [("stem", stem), *((f"choices[{k}]", v) for k, v in sorted(choices.items()))]:
        if isinstance(text, str) and text.strip() and not KANA.search(text):
            errs.append(f"{name} 里一个假名都没有 —— 多半是没翻译，原文被直接抄了过来")
    if isinstance(expl, str) and expl.strip() and not KANA.search(expl):
        errs.append("explanation 里一个假名都没有 —— 多半是没翻译")

    # 截断哨兵
    src_stem = source_of(q)
    if isinstance(stem, str) and len(stem) < len(src_stem) * MIN_LENGTH_RATIO:
        errs.append(f"stem 只有 {len(stem)} 字，原文 {len(src_stem)} 字 —— 疑似只译了开头")
    src_expl = explanation_of(q)
    if isinstance(expl, str) and src_expl and len(expl) < len(src_expl) * MIN_LENGTH_RATIO:
        errs.append(f"explanation 只有 {len(expl)} 字，原文 {len(src_expl)} 字 —— 疑似截断")

    # 术语表：原文出现某个词 ⇒ 译文必须用指定译词。
    #
    # ⚠️⚠️ 比对范围必须【只覆盖这一趟真的翻了的部分】。
    # 解析是可以后补的（见模块 docstring），只翻题面那一趟若拿「题干+选项+解析」
    # 的原文去比「题干+选项」的译文，解析里出现过的每一个术语都会报一次假警。
    # ⇒ 2026-09-08 实测：一条正确的题面译文被报出 4 个不存在的问题。
    # ⭐ 误报比漏报更伤：它训练人忽略这个检查，那这套闸就等于不存在。
    parts_src = [src_stem, *(c["body"] for c in q["choices"])]
    parts_dst = [str(stem or ""), *(str(v) for v in choices.values())]
    if isinstance(expl, str) and expl.strip():
        parts_src.append(src_expl)
        parts_dst.append(expl)
    joined_src, joined_dst = " ".join(parts_src), " ".join(parts_dst)
    for src_term, dst_term in sorted(glo["terms"].items()):
        if src_term in joined_src and dst_term not in joined_dst:
            errs.append(f"原文有「{src_term}」，译文里找不到约定译词「{dst_term}」")
    # keep：服务名等必须原样保留，⛔ 不许意译也不许改写成假名
    for term in glo["keep"]:
        if term in joined_src and term not in joined_dst:
            errs.append(f"「{term}」是必须原样保留的名字，译文里没有")
    return errs


def check_shard(path: Path, qmap: dict, glo: dict) -> list[str]:
    errs = []
    try:
        doc = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as e:
        return [f"JSON 解析失败: {e}"]
    items = doc.get("items")
    if not isinstance(items, list):
        return ["items 缺失或不是数组"]
    rng = doc.get("range")
    want = sorted(n for n in qmap if rng and rng[0] <= n <= rng[1])
    got = sorted(it.get("no") for it in items)
    if got != want:
        missing, extra = set(want) - set(got), set(got) - set(want)
        if missing:
            errs.append(f"缺题号 {sorted(missing)}")
        if extra:
            errs.append(f"多出题号 {sorted(extra)}")
    for it in items:
        no = it.get("no")
        if no not in qmap:
            continue
        errs += [f"#{no}: {e}" for e in check_item(it, qmap[no], glo)]
    return errs


def _scan(bank: str, locale: str):
    doc = load_enriched(bank)
    qmap = {q["no"]: q for q in doc["questions"]}
    glo = load_glossary(bank, locale)
    # 服务名从数据推，与题库自动同步；glossary 的 keep 是补充
    glo["keep"] = sorted(set(glo["keep"]) | set(derive_keep(doc)), key=lambda x: (-len(x), x))
    d = shard_dir(bank, locale)
    return doc, qmap, glo, d


def cmd_status(bank: str, locale: str) -> None:
    doc, qmap, glo, d = _scan(bank, locale)
    ranges = shard_ranges(list(qmap))
    done, bad, todo = [], [], []
    n_stem = n_expl = 0
    for r in ranges:
        p = d / shard_name(r)
        if not p.exists():
            todo.append(r)
            continue
        if check_shard(p, qmap, glo):
            bad.append(r)
            continue
        done.append(r)
        for it in json.loads(p.read_text(encoding="utf-8"))["items"]:
            n_stem += 1
            if it.get("explanation"):
                n_expl += 1
    total = len(qmap)
    n_has_expl = sum(1 for q in qmap.values() if explanation_of(q))
    print(f"题库      {bank}   目标语言 {locale}")
    print(f"术语表    {len(glo['terms'])} 条约定译词 · {len(glo['keep'])} 个保留原样的名字")
    print(f"分片      {len(ranges)} 片 × {SHARD_SIZE} 题")
    print(f"题面进度  {n_stem}/{total} 题  ({n_stem/total*100:.1f}%)   ← 不翻就没法做题")
    print(f"解析进度  {n_expl}/{n_has_expl} 题  ({n_expl/max(n_has_expl,1)*100:.1f}%)   ← 缺了回退中文并标注")
    print(f"校验失败  {len(bad)} 片" + (f"  → {[shard_name(r) for r in bad]}" if bad else ""))
    print(f"待处理    {len(todo)} 片")
    if todo:
        print(f"\n下一片    {shard_name(todo[0])}   （跑 translate.py next {bank} --locale {locale}）")


def cmd_next(bank: str, locale: str, count: int, with_explanation: bool) -> None:
    doc, qmap, glo, d = _scan(bank, locale)
    todo = [r for r in shard_ranges(list(qmap))
            if not (d / shard_name(r)).exists() or check_shard(d / shard_name(r), qmap, glo)]
    if not todo:
        print(f"✓ 全部分片已完成且校验通过，可以跑 merge 了")
        return
    for r in todo[:count]:
        print(f"{'='*70}\n写入目标: $MELETE_DATA_ROOT/{bank}/translated/{locale}/{shard_name(r)}")
        print(f"{'='*70}\n")
        print(f"⛔ 术语表是【封闭】的：下面列出的词，译文必须用指定译词，不得另译。")
        print(f"⛔ keep 里的名字必须【原样保留】—— 不译、不改写成假名。\n")
        for k, v in sorted(glo["terms"].items()):
            print(f"    {k} → {v}")
        print(f"\n  保留原样: {'、'.join(glo['keep'][:40])}"
              + (f" …… 共 {len(glo['keep'])} 个" if len(glo["keep"]) > 40 else "") + "\n")
        for no in sorted(n for n in qmap if r[0] <= n <= r[1]):
            q = qmap[no]
            print(f"### 问题 #{no}  fp={fingerprint(q)}")
            print(f"\n[题干]\n{source_of(q)}\n")
            for ch in q["choices"]:
                print(f"  {ch['label']}. {ch['body']}")
            if with_explanation and explanation_of(q):
                print(f"\n[解析]\n{explanation_of(q)}")
            print()


def cmd_write(bank: str, locale: str, src: Path) -> None:
    """从 Python 字面量文件写入分片 —— 与 enrich.py write 同一理由：
    JSON 的转义规则对含日文标点与换行的长文本太脆弱，三引号字符串里全是字面值。"""
    doc, qmap, glo, d = _scan(bank, locale)
    if not src.exists():
        sys.exit(f"✗ 找不到输入文件: {src}")
    ns: dict = {}
    try:
        exec(compile(src.read_text(encoding="utf-8"), str(src), "exec"), ns)
    except Exception as e:
        sys.exit(f"✗ 输入文件执行失败: {type(e).__name__}: {e}")
    items = ns.get("ITEMS")
    if not isinstance(items, list) or not items:
        sys.exit("✗ 输入文件必须定义一个非空的 ITEMS 列表")

    errs = []
    for it in items:
        no = it.get("no")
        if no not in qmap:
            errs.append(f"#{no}: 题号不在题库中")
            continue
        # 指纹门禁：与 enrich 同一条纪律 —— 翻译比富化更容易「凭印象写」，
        # 因为一段看着合理的日文根本无从判断它对不对应原题。
        want, got = fingerprint(qmap[no]), it.get("fp")
        if got != want:
            errs.append(f"#{no}: fp 不符（期望 {want}，收到 {got or '缺失'}）"
                        f" —— 请先跑 `translate.py next` 取真实题面")
            continue
        errs += [f"#{no}: {e}" for e in check_item(it, qmap[no], glo)]
    if errs:
        print(f"✗ 校验未通过（{len(errs)} 个问题），未写入任何文件：")
        for e in errs[:25]:
            print(f"    {e}")
        if len(errs) > 25:
            print(f"    …… 另有 {len(errs)-25} 个")
        sys.exit(1)

    ranges = shard_ranges(list(qmap))
    buckets: dict[tuple[int, int], list] = {}
    for it in items:
        rng = next((r for r in ranges if r[0] <= it["no"] <= r[1]), None)
        if rng is None:
            sys.exit(f"✗ #{it['no']} 不属于任何分片区间")
        buckets.setdefault(rng, []).append(it)

    d.mkdir(parents=True, exist_ok=True)
    for rng, batch in sorted(buckets.items()):
        path = d / shard_name(rng)
        try:
            existing = json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
        except json.JSONDecodeError:
            existing = {}
        merged = {i["no"]: i for i in existing.get("items", [])}
        for i in batch:
            keep = {k: v for k, v in i.items() if k != "fp"}
            # 允许「先题面、后解析」分两趟：这一趟没给 explanation 就保留上一趟的
            if "explanation" not in keep and merged.get(i["no"], {}).get("explanation"):
                keep["explanation"] = merged[i["no"]]["explanation"]
            merged[i["no"]] = keep
        existing.update({
            "bank": bank, "locale": locale, "range": list(rng),
            "model": existing.get("model", "claude (Claude Code 订阅额度)"),
            "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
            "items": [merged[k] for k in sorted(merged)],
        })
        path.write_text(json.dumps(existing, ensure_ascii=False, indent=1), encoding="utf-8")
        want = sum(1 for n in qmap if rng[0] <= n <= rng[1])
        n_expl = sum(1 for i in existing["items"] if i.get("explanation"))
        print(f"✓ {shard_name(rng)}  写入 {len(batch)} 题，"
              f"该片现有题面 {len(existing['items'])}/{want} · 解析 {n_expl}/{want}")
        for it in sorted(batch, key=lambda x: x["no"]):
            print(f"     #{it['no']}  {it['stem'][:40]}…")


def cmd_check(bank: str, locale: str) -> None:
    doc, qmap, glo, d = _scan(bank, locale)
    files = sorted(d.glob("*.json")) if d.exists() else []
    if not files:
        sys.exit(f"✗ 还没有任何分片（{rel(d)}）")
    total = 0
    for p in files:
        errs = check_shard(p, qmap, glo)
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


def cmd_merge(bank: str, locale: str) -> None:
    doc, qmap, glo, d = _scan(bank, locale)
    items, problems = {}, 0
    for p in sorted(d.glob("*.json")):
        errs = check_shard(p, qmap, glo)
        if errs:
            problems += 1
            print(f"✗ {p.name} 校验未过，已跳过（{len(errs)} 个问题）")
            continue
        for it in json.loads(p.read_text(encoding="utf-8"))["items"]:
            items[it["no"]] = it
    if problems:
        sys.exit(f"\n✗ 有 {problems} 片未通过校验，先修好再 merge")

    out = bank_dir(bank) / f"translated.{locale}.json"
    payload = {
        "bank": bank,
        "locale": locale,
        "source_locale": doc["bank"].get("locale"),
        "merged_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "items": [items[k] for k in sorted(items)],
    }
    json.dump(payload, out.open("w", encoding="utf-8"), ensure_ascii=False, indent=1)
    n_expl = sum(1 for i in payload["items"] if i.get("explanation"))
    print(f"✓ {rel(out)}   题面 {len(items)}/{len(qmap)} · 解析 {n_expl}/{len(qmap)}")


def main() -> None:
    ap = argparse.ArgumentParser(description="Melete 翻译编排器（enriched.json → 译文分片）")
    # ⚠️ --locale 挂在【每个子命令】上而不是顶层：顶层选项必须写在子命令【之前】，
    # 而人手打出来的顺序总是 `translate.py status <bank> --locale ja`。
    # 让它在自然位置能用，比在文档里解释一遍便宜。
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--locale", default="ja", help="目标语言（默认 ja）")
    common.add_argument("bank")
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("status", "check", "merge"):
        sub.add_parser(name, parents=[common])
    p = sub.add_parser("next", parents=[common])
    p.add_argument("-n", type=int, default=1, help="一次取几片")
    p.add_argument("--no-explanation", action="store_true", help="只取题面，不带解析（先翻题面那一趟）")
    p = sub.add_parser("write", parents=[common])
    p.add_argument("file", type=Path)
    a = ap.parse_args()
    if a.cmd == "status":
        cmd_status(a.bank, a.locale)
    elif a.cmd == "next":
        cmd_next(a.bank, a.locale, a.n, not a.no_explanation)
    elif a.cmd == "write":
        cmd_write(a.bank, a.locale, a.file)
    elif a.cmd == "check":
        cmd_check(a.bank, a.locale)
    elif a.cmd == "merge":
        cmd_merge(a.bank, a.locale)


if __name__ == "__main__":
    main()
