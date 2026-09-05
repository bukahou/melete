#!/usr/bin/env python3
"""
Melete 数据导入管道 — 第一段的【视觉】变体：扫描版 PDF → questions.json

  为什么需要它：IPA（情報処理推進機構）公开的 IT パスポート 题目 PDF 是**纯扫描图** ——
  零嵌入字体，56 页 = 56 张 1432×2026 灰度 JPEG。pdftotext 抽出 56 字节（每页一个换页符）。
  现有的 ingest.py 走文本层，对它整条不适用。

  ⚠️ 而【答案】PDF 有完整文本层 —— 这是本题库最幸运的地方，见下方「对账」。

架构与 enrich.py 同源，理由也相同：不调 API，由 Claude Code 会话逐片转写
（会话本来就能读图）。所以核心职责同样是**让中断可恢复**：

  · 按页切成固定分片，一片一个文件
  · 分片文件存在且校验通过即完成 —— 扫目录即状态，不维护单独的进度清单
  · 每片独立校验，坏片删掉重跑

⛔ 转写与富化**必须分成两段**，不得合并成「一次读图直接产出解析」：
   合了就分不清「读错了」和「判错了」，而这两种错的修法完全不同。
   分开还有一个好处 —— 转写产物随时能对着原图复核。

子命令：
  pages    <bank>          把 PDF 渲染成页图（幂等，已存在则跳过）
  status   <bank>          进度总览
  next     <bank>          打印下一个待转写分片要读哪些页图
  write    <bank> <file>   从 Python 字面量写入分片（与 enrich.py 同一理由：避开 JSON 转义）
  check    <bank>          校验分片 + 与答案 PDF 对账
  merge    <bank>          合并全部分片 → questions.json（含 answer_claim）

题库特有的东西（PDF 文件名、每页题数、选项标签）从
pipeline/banks/<bank>/transcribe_spec.json 读取 —— 本模块零题库特有逻辑。
"""
import argparse
import ast
import hashlib
import json
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
INBOX = Path.home() / "melete-inbox"
SHARD_PAGES = 12  # 一片 12 页 ≈ 20 题，与 enrich.py 的 25 题一片量级相当
RENDER_DPI = 150  # 原扫描是 200dpi；150 已足够辨认且页图更小


# ─────────────────────────────────────────────────────────────────────
# 规格与路径
# ─────────────────────────────────────────────────────────────────────
def load_spec(bank: str) -> dict:
    p = REPO / "pipeline" / "banks" / bank / "transcribe_spec.json"
    if not p.exists():
        sys.exit(f"✗ 找不到 {p.relative_to(REPO)}")
    return json.loads(p.read_text(encoding="utf-8"))


def pages_dir(bank: str) -> Path:
    # 页图是**中间产物**，体积大且可从 PDF 重建 —— 放收件区，⛔ 不进仓库
    return INBOX / bank / "pages"


def shard_dir(bank: str) -> Path:
    d = REPO / "data" / bank / "transcribed"
    d.mkdir(parents=True, exist_ok=True)
    return d


def shard_name(lo: int, hi: int) -> str:
    return f"p{lo:03d}-{hi:03d}.json"


def shard_ranges(total_pages: int, first: int) -> list[tuple[int, int]]:
    """按页分片。first 是第一页正文页码（封面/说明页跳过）。"""
    out = []
    p = first
    while p <= total_pages:
        out.append((p, min(p + SHARD_PAGES - 1, total_pages)))
        p += SHARD_PAGES
    return out


# ─────────────────────────────────────────────────────────────────────
# 页图渲染
# ─────────────────────────────────────────────────────────────────────
def cmd_pages(bank: str) -> None:
    spec = load_spec(bank)
    pdf = INBOX / bank / spec["questions_pdf"]
    if not pdf.exists():
        sys.exit(f"✗ 找不到素材 {pdf}\n  按收件区约定，原始 PDF 放在 {INBOX / bank}/，不进仓库")
    d = pages_dir(bank)
    d.mkdir(parents=True, exist_ok=True)

    total = pdf_page_count(pdf)
    missing = [n for n in range(1, total + 1) if not (d / f"p{n:03d}.png").exists()]
    if not missing:
        print(f"✓ {total} 页已全部渲染过：{d}")
        return
    print(f"渲染 {len(missing)} 页 → {d}（{RENDER_DPI}dpi）")
    # 一次渲染全部：pdftoppm 的 -f/-l 是页范围，逐页调用会把 PDF 反复解析 N 遍
    subprocess.run(
        ["pdftoppm", "-r", str(RENDER_DPI), "-png", str(pdf), str(d / "p")],
        check=True)
    # pdftoppm 的补零位数随总页数变化，统一改名成三位，让分片名可预期
    for f in d.glob("p-*.png"):
        f.rename(d / f"p{int(f.stem.split('-')[-1]):03d}.png")
    print(f"✓ {len(list(d.glob('p*.png')))} 页")


def pdf_page_count(pdf: Path) -> int:
    out = subprocess.run(["pdfinfo", str(pdf)], capture_output=True, text=True, check=True).stdout
    m = re.search(r"^Pages:\s+(\d+)", out, re.M)
    if not m:
        sys.exit(f"✗ 读不出 {pdf.name} 的页数")
    return int(m.group(1))


# ─────────────────────────────────────────────────────────────────────
# ⭐ 答案 PDF —— 独立于转写过程的外部哨兵
# ─────────────────────────────────────────────────────────────────────
def load_answers(bank: str) -> dict[int, str]:
    """从答案 PDF 抽出 {題番号: 正解}。

    ⭐ 它是**独立来源**：不参与转写，因此可以拿来卡转写的错。
    转写出的题号集合必须与它完全一致 —— 它不知道转写会犯什么错，
    但只要漏页、串页、题号错位，集合就对不上。
    这类哨兵的价值在于它不编码「我猜会出什么错」。
    """
    spec = load_spec(bank)
    pdf = INBOX / bank / spec["answers_pdf"]
    if not pdf.exists():
        sys.exit(f"✗ 找不到答案 PDF {pdf}")
    txt = subprocess.run(["pdftotext", "-layout", str(pdf), "-"],
                         capture_output=True, text=True, check=True).stdout
    labels = "".join(spec["choice_labels"])
    # 版式是「問 N  正解」多列并排，逐个匹配即可，不必理解列结构
    pairs = re.findall(rf"問\s*(\d+)\s*([{labels}])", txt)
    if not pairs:
        sys.exit(f"✗ 答案 PDF 里一条 (問N, 正解) 都没抽到 —— 版式变了？")
    return {int(n): a for n, a in pairs}


# ─────────────────────────────────────────────────────────────────────
# 校验
# ─────────────────────────────────────────────────────────────────────
def check_shard(path: Path, spec: dict) -> list[str]:
    """校验一个分片。返回问题列表，空列表 = 通过。"""
    errs = []
    try:
        items = json.loads(path.read_text(encoding="utf-8"))["items"]
    except Exception as e:  # noqa: BLE001 —— 坏片的形态无法预知，一律报出来
        return [f"读不出来: {e}"]

    labels = spec["choice_labels"]
    seen = set()
    for it in items:
        no = it.get("no")
        if not isinstance(no, int):
            errs.append(f"題番号缺失或非整数: {it!r:.60}")
            continue
        if no in seen:
            errs.append(f"#{no}: 题号重复")
        seen.add(no)
        if not (it.get("stem") or "").strip():
            errs.append(f"#{no}: 题干为空")
        ch = it.get("choices")
        if not isinstance(ch, dict):
            errs.append(f"#{no}: choices 须为 {{标签: 正文}} 对象")
            continue
        # ⚠️ 选项标签必须【正好】是这一组：多、少、错都说明跨题粘连或漏抓
        if sorted(ch) != sorted(labels):
            errs.append(f"#{no}: 选项标签是 {sorted(ch)}，应为 {labels}")
        for k, v in ch.items():
            if not str(v).strip():
                errs.append(f"#{no}: 选项 {k} 正文为空")
    return errs


def cmd_check(bank: str) -> None:
    spec = load_spec(bank)
    d = shard_dir(bank)
    shards = sorted(d.glob("p*.json"))
    if not shards:
        sys.exit("✗ 一个分片都没有，先跑 next 开始转写")

    bad = 0
    got: dict[int, Path] = {}
    for s in shards:
        errs = check_shard(s, spec)
        if errs:
            bad += 1
            print(f"✗ {s.name}")
            for e in errs[:8]:
                print(f"    {e}")
        else:
            for it in json.loads(s.read_text(encoding="utf-8"))["items"]:
                if it["no"] in got:
                    print(f"✗ #{it['no']} 同时出现在 {got[it['no']].name} 与 {s.name}")
                    bad += 1
                got[it["no"]] = s

    # ⭐ 与答案 PDF 对账 —— 本模块最重要的一条检查
    answers = load_answers(bank)
    only_q = sorted(set(got) - set(answers))
    only_a = sorted(set(answers) - set(got))
    if only_q:
        print(f"✗ 转写出的题号在答案里没有: {only_q[:20]}")
    if only_a:
        print(f"⚠ 答案里有但还没转写: {len(only_a)} 题 {only_a[:20]}")

    # ⚠️ 分片各自合法 ≠ 合起来完整：一个只转了前几页的分片，其内部题号连续、
    # 每题都合法，check_shard 完全看不出它漏了后半截 —— 于是 next 会把它当已完成
    # 跳过去。2026-09-05 实测踩到（只转了 12 页里的 3 页）。
    # 题号在整套里是 1..100 连续的，所以「合起来有缺口」是可判定的。
    if got:
        gaps = [n for n in range(min(got), max(got) + 1) if n not in got]
        if gaps:
            print(f"✗ 题号有缺口 {len(gaps)} 处（分片各自合法，但合起来漏了）: {gaps[:20]}")
            bad += 1

    print(f"\n分片 {len(shards)}（坏 {bad}） · 已转写 {len(got)} 题 · 答案 {len(answers)} 题")
    if not bad and not only_q and not only_a:
        print("✓ 全部通过且与答案完全对账，可以 merge 了")


def cmd_status(bank: str) -> None:
    spec = load_spec(bank)
    d = pages_dir(bank)
    total = len(list(d.glob("p*.png")))
    if not total:
        sys.exit("✗ 还没渲染页图，先跑 pages")
    ranges = shard_ranges(total, spec.get("first_content_page", 1))
    done = sum(1 for r in ranges
               if (shard_dir(bank) / shard_name(*r)).exists()
               and not check_shard(shard_dir(bank) / shard_name(*r), spec))
    print(f"页图 {total} 页 · 分片 {done}/{len(ranges)} 完成")


def cmd_next(bank: str, count: int) -> None:
    spec = load_spec(bank)
    d = pages_dir(bank)
    total = len(list(d.glob("p*.png")))
    if not total:
        sys.exit("✗ 还没渲染页图，先跑 pages")

    todo = [r for r in shard_ranges(total, spec.get("first_content_page", 1))
            if not (shard_dir(bank) / shard_name(*r)).exists()
            or check_shard(shard_dir(bank) / shard_name(*r), spec)]
    if not todo:
        print("✓ 全部分片已完成且校验通过，可以跑 check → merge 了")
        return

    answers = load_answers(bank)
    done_nos = set()
    for s in shard_dir(bank).glob("p*.json"):
        try:
            done_nos |= {it["no"] for it in json.loads(s.read_text(encoding="utf-8"))["items"]}
        except Exception:  # noqa: BLE001 —— 坏片由 check 负责报，这里只统计
            pass
    print(f"进度：已转写 {len(done_nos)}/{len(answers)} 题\n")

    for lo, hi in todo[:count]:
        print(f"{'=' * 70}\n写入目标: data/{bank}/transcribed/{shard_name(lo, hi)}\n{'=' * 70}")
        print(f"\n逐页读下面这些图，把每道题转写成结构化数据：\n")
        for n in range(lo, hi + 1):
            print(f"  {d / f'p{n:03d}.png'}")
        print(f"""
格式（Python 字面量，用 transcribe.py write 写入）：

    {{"items": [
        {{"no": 3,
          "stem": "题干原文。换行照原样保留。\\n\\n表格转成 Markdown：\\n"
                  "|      | 列1 | 列2 |\\n|---|---|---|\\n| 行1 | .. | .. |",
          "choices": {{{", ".join(f'"{c}": "..."' for c in spec["choice_labels"])}}},
          "has_figure": false}},
    ]}}

⚠️ 规则：
  · 題番号、选项标签必须与原图一致 —— 校验会卡 {spec["choice_labels"]} 这一组标签
  · 表格【必须】转写成 Markdown：它是内容不是装饰，答案往往直接依赖表里的数字
  · 真正的图（流程图 / 网络拓扑 / 状态迁移）转不成文本 —— 置 has_figure=true
    并在 stem 末尾用一行 "[図: 简述]" 说明，⛔ 不要凭想象补出图的内容
  · 只转写你**看得见**的内容。看不清就置 has_figure=true 并说明，不要猜

⭐ 页面上若出现「問N から 問M までは，XXX系の問題です。」这类分域说明，
   在该分片里一并写出来：

    {{"pages": [..], "sections": [{{"from": 1, "to": 34, "name": "ストラテジ系"}}], "items": [...]}}

   ⚠️ 这是【官方印在卷子上的考纲域划分】—— 有它就不必让富化阶段去猜 domain，
   直接按题号区间机械映射即可。少抄一条，那一段题的 domain 就得靠猜。
""")


# ─────────────────────────────────────────────────────────────────────
# 写入与合并
# ─────────────────────────────────────────────────────────────────────
def cmd_write(bank: str, src: Path) -> None:
    spec = load_spec(bank)
    data = ast.literal_eval(src.read_text(encoding="utf-8"))
    items = sorted(data["items"], key=lambda x: x["no"])
    if not items:
        sys.exit("✗ 空分片")

    d = pages_dir(bank)
    total = len(list(d.glob("p*.png")))
    ranges = shard_ranges(total, spec.get("first_content_page", 1))
    # 落到哪一片由「这批题写在哪几页」决定 —— 但会话只知道题号，不知道页。
    # 所以让写入方在文件里显式给出页范围，避免猜。
    if "pages" not in data:
        sys.exit("✗ 分片文件必须含 'pages': (起页, 止页)，与 next 打印的一致")
    lo, hi = data["pages"]
    if (lo, hi) not in ranges:
        sys.exit(f"✗ 页范围 {(lo, hi)} 不是合法分片，合法的有 {ranges}")

    out = shard_dir(bank) / shard_name(lo, hi)
    payload = {"pages": [lo, hi], "items": items}
    if data.get("sections"):
        payload["sections"] = data["sections"]
    out.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    errs = check_shard(out, spec)
    print(f"{'✓' if not errs else '✗'} 写入 {out.relative_to(REPO)}  {len(items)} 题")
    for e in errs[:10]:
        print(f"    {e}")


def cmd_merge(bank: str) -> None:
    spec = load_spec(bank)
    d = shard_dir(bank)
    items: dict[int, dict] = {}
    for s in sorted(d.glob("p*.json")):
        if errs := check_shard(s, spec):
            sys.exit(f"✗ {s.name} 未通过校验（{errs[0]}），修好再 merge")
        for it in json.loads(s.read_text(encoding="utf-8"))["items"]:
            items[it["no"]] = it

    answers = load_answers(bank)
    if set(items) != set(answers):
        sys.exit(f"✗ 题号与答案对不上：转写 {len(items)} 题、答案 {len(answers)} 题；"
                 f"仅转写有 {sorted(set(items) - set(answers))[:10]}、"
                 f"仅答案有 {sorted(set(answers) - set(items))[:10]}")

    labels = spec["choice_labels"]
    questions = []
    for no in sorted(items):
        it = items[no]
        questions.append({
            "no": no,
            "session": spec["session"],
            "stem": it["stem"],
            "kind": "single",
            "pick": 1,
            "choices": [{"label": l, "body": it["choices"][l]} for l in labels],
            # 官方解答是唯一来源，且它是权威的 —— 与 AWS 那两个题库
            # 「题库标注 vs 社区投票」的多来源情形不同，这里只有一条主张。
            "claims": [{"source": "bank_label", "answer": answers[no]}],
            "warnings": (["has_figure"] if it.get("has_figure") else []),
        })

    pdf = INBOX / bank / spec["questions_pdf"]
    doc = {
        "bank": {
            "slug": bank,
            "name": spec["bank_name"],
            "locale": spec["locale"],
            "kind": "cert",
            "source": spec["questions_pdf"],
            "extracted_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        },
        "questions": questions,
    }
    out = REPO / "data" / bank / "questions.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(doc, ensure_ascii=False, indent=2), encoding="utf-8")

    figures = [q["no"] for q in questions if "has_figure" in q["warnings"]]
    print(f"✓ {out.relative_to(REPO)}  {len(questions)} 题")
    print(f"  含图的题 {len(figures)} 道{'：' + str(figures) if figures else ''}")
    print(f"  素材 {spec['questions_pdf']}  sha256={sha256(pdf)[:16]}…")


def sha256(p: Path) -> str:
    h = hashlib.sha256()
    with p.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def main() -> None:
    ap = argparse.ArgumentParser(description="扫描版 PDF → questions.json（会话逐片转写）")
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("pages", "status", "check", "merge"):
        sub.add_parser(name).add_argument("bank")
    n = sub.add_parser("next")
    n.add_argument("bank")
    n.add_argument("--count", type=int, default=1)
    w = sub.add_parser("write", help="从 Python 字面量文件写入分片（避开 JSON 转义）")
    w.add_argument("bank")
    w.add_argument("file", type=Path)
    a = ap.parse_args()
    {"pages": lambda: cmd_pages(a.bank),
     "status": lambda: cmd_status(a.bank),
     "next": lambda: cmd_next(a.bank, a.count),
     "write": lambda: cmd_write(a.bank, a.file),
     "check": lambda: cmd_check(a.bank),
     "merge": lambda: cmd_merge(a.bank)}[a.cmd]()


if __name__ == "__main__":
    main()
