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
  status <bank>          进度总览
  next   <bank>          打印下一个待做分片的题目（喂给解答会话）
  write  <bank> <file>   从 Python 字面量写入分片（**推荐**，见下）
  check  <bank>          校验分片，报告问题
  merge  <bank>          合并全部分片 → enriched.json

为什么有 write 子命令：直接用 heredoc 手写 JSON 三次踩坑（裸换行 ×2、
非法转义 ×1）—— JSON 的转义规则对「含 markdown 表格与中文标点的长解析」
太脆弱。改用 Python 字面量后，三引号字符串里的换行、引号、反斜杠全是字面值，
问题从根上消失。

题库特有的约束（考纲域、concept 种子、服务命名）从
pipeline/banks/<bank>/enrich_spec.json 读取 —— 本模块零题库特有逻辑。
"""
import argparse
import hashlib
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SHARD_SIZE = 25
KEBAB = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")
CONFIDENCE = {"high", "medium", "low"}
# ⚠️ 这里【不含】知识对象轴与 concept 轴的字段名 —— 它们由 spec 决定：
#   · 知识对象轴的字段名来自 spec.tag_types.topic.enrichment_field
#     （AWS 是 "services"，IPA IT パスポート 是 "topics"）
#   · concept 是否要求，由 spec 是否声明 concept 词表决定
# 2026-09-05 之前 services / concepts 是写死在这里的 —— 而 load.py 那边
# enrichment_field 早就可配了。⚠️ 「通用化」只做了一半，第三个题库进来才暴露：
# 一个只在导入端可配、在校验端写死的字段名，等于没有可配。
ITEM_KEYS = {"no", "verdict", "confidence", "reasoning", "explanation",
             "domain", "data_issue"}
# fp 是 write 时的题面指纹门禁，校验后即丢弃，不落进产物。
# notes 是 advisory 级：题目有瑕疵但答案仍可判定时的登记处（如译文撞词）。
# 与 data_issue 的分工：data_issue = 结构性不可判（verdict 必须留空），
# notes = 有毛病但不阻断。可选字段，旧分片没有它也合法。
OPTIONAL_KEYS = {"notes", "fp"}


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


def fingerprint(q: dict) -> str:
    """
    题面指纹 —— 防「凭记忆写」的强制锚点。

    背景（2026-09-02 实发）：富化侧上下文被压缩后，误以为题面还在，
    凭记忆写了 11 道题的解析，选项描述全是脑补的。裁决结论碰巧没错，
    但 reasoning 里的对线内容是错的 —— 而那正是本项目的核心价值。

    为什么用哈希而不是「抄题干前 N 字」：
      · 实测题干前 20 字有 37 题撞车，且**撞的正是同类题**
        （都以「一家公司在 EC2 上运行…」开头）—— 最容易记混的恰恰是这些
      · 选项前缀同样撞车（大量「创建一个 Amazon…」）
      · 哈希 8 位在 1019 题上零撞车，且**无法凭记忆构造** ——
        只有真正读过该题的完整题面才算得出来

    这就是它的全部作用：把「你确实看了真数据」变成可验证的事实。
    """
    raw = q["stem"] + "|" + "|".join(c["label"] + c["body"] for c in q["choices"])
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()[:8]


def claims_of(q: dict) -> dict:
    return {c["source"]: c for c in q["claims"]}


def is_contested(q: dict) -> tuple[bool, list[str]]:
    """通用判据：这道题的答案是否存在不确定性。不含任何题库特有逻辑。"""
    # 选项标签重复的题，字母主张互相不可比 —— 不算分歧（见 load.py 同一处理）
    if any(w.startswith("duplicate_choice_label_") for w in q.get("warnings", [])):
        return False, []
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

def topic_field_of(spec: dict) -> str:
    """知识对象轴在富化产物里的字段名。AWS 是 services，IPA 是 topics。"""
    return spec.get("tag_types", {}).get("topic", {}).get("enrichment_field", "services")


def wants_concepts(spec: dict) -> bool:
    """本题库这一轮要不要产 concept。

    ⚠️ 判据是 spec 里有没有 concept_seeds 这个键，而不是它非不非空 ——
    显式写 "concept_seeds": [] 的意思是「本轮不产」（IPA 2026-09-05），
    与「没写这个键」（旧题库，默认要产）要分得开。
    """
    if "concept_seeds" not in spec:
        return True
    return bool(spec["concept_seeds"])


def topic_vocabulary(spec: dict) -> dict[str, int]:
    """封闭词表 {中分類名: 所属分野编号}；spec 没给就返回空（= 开放词表）。"""
    return {v["name"]: v["field"] for v in spec.get("topic_vocabulary", [])}


def check_item(item: dict, q: dict, spec: dict) -> list[str]:
    errs = []
    # 翻译字段由 spec 决定是否要求：英文题库（如 SAP-C02）声明 translation.locale
    # 即要求每题附中文；中文题库不声明，出现该字段反而报多余。
    # 放在 item 里而非另开产物，是为了让「一题的所有富化结果」始终在同一个对象里。
    want_tr = bool(spec.get("translation"))
    topic_field = topic_field_of(spec)
    want_concepts = wants_concepts(spec)
    required = (ITEM_KEYS | {topic_field}
                | ({"translation"} if want_tr else set())
                | ({"concepts"} if want_concepts else set()))
    extra = set(item) - required - OPTIONAL_KEYS
    missing = required - set(item)
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
    for k in [topic_field] + (["concepts"] if want_concepts else []):
        v = item.get(k)
        if not isinstance(v, list) or not v or not all(isinstance(x, str) and x.strip() for x in v):
            errs.append(f"{k} 须为非空字符串列表")

    # ⭐ 封闭词表：spec 给了 topic_vocabulary 就必须从里面挑，⛔ 不得新造。
    #
    # ⚠️ 一个不被校验的封闭词表【不是封闭的】—— 它只是一句建议，
    # 而建议在几百题的规模上必然被突破。这条校验才是「封闭」二字的全部实现。
    #
    # 账已经吃过一次：SAP-C02 用开放词表产出 1002 个独有 concept、
    # 72% 只挂一道题，而问题在【写的时候】看不出来（每个标签单看都合理），
    # 要到【聚合的时候】才暴露，那时已经几百题写完了。
    vocab = topic_vocabulary(spec)
    if vocab and isinstance(item.get(topic_field), list):
        unknown = [t for t in item[topic_field] if t not in vocab]
        if unknown:
            errs.append(f"{topic_field} 含词表外的值 {unknown} —— "
                        f"本题库的 {topic_field} 是封闭词表（{len(vocab)} 项），不得新造")
        # 交叉校验：topic 必须属于该题 domain 所在的分野。
        # ⭐ 这条是免费的 —— domain 来自卷子上印的题号区间而非 AI 判断，
        # 所以它是一个独立于 AI 的参照物。跨分野的组合说明两者至少一个错了。
        want_field = str(item.get("domain"))
        cross = [t for t in item[topic_field]
                 if t in vocab and str(vocab[t]) != want_field]
        if cross:
            errs.append(f"{topic_field} {cross} 不属于 domain={want_field} 那个分野 —— "
                        f"domain 来自卷面（可信），所以是 {topic_field} 判错了")

    if "notes" in item and (not isinstance(item["notes"], str) or not item["notes"].strip()):
        errs.append("notes 存在时须为非空字符串")
    if want_concepts and isinstance(item.get("concepts"), list):
        bad = [c for c in item["concepts"] if isinstance(c, str) and not KEBAB.match(c)]
        if bad:
            errs.append(f"concepts 须为 kebab-case，违规 {bad}")

    if want_tr:
        errs += check_translation(item["translation"], q)

    # ⭐ 权威答案源：spec 声明了就要求 verdict 与它一致。
    #
    # IPA 的官方解答与 源站 那种题库标注【不是同一种东西】——
    # 后者 38% 与社区投票不一致（这正是 answer_claim 多来源设计的由来），
    # 前者是出题机构自己公布的正解，不存在「它错了」这种情形。
    #
    # ⚠️ 所以对这类题库，verdict ≠ 官方答案不是「有价值的分歧」，而是一个【缺陷信号】，
    # 且它同时覆盖两种缺陷：AI 判错了，或者【我们转写错了】（选项抄串、字母错位）。
    # 后者尤其值钱 —— 转写错误在别处几乎没有检出手段。
    auth = spec.get("authoritative_answer_source")
    if auth and not issue:
        official = next((c["answer"] for c in q.get("claims", []) if c["source"] == auth), None)
        if official and verdict and verdict != official:
            errs.append(
                f"verdict {verdict!r} ≠ 官方解答 {official!r}。本题库的 {auth} 是权威来源，"
                f"不一致只可能是【AI 判错】或【转写错了】—— ⛔ 不要改 verdict 去迎合，"
                f"先对着原图核对选项，确认转写无误后再报给人工")

    contested, _ = is_contested(q)
    if contested and not issue and len(item["reasoning"]) < 80:
        errs.append(f"存在答案分歧的题，reasoning 仅 {len(item['reasoning'])} 字（要求 ≥80，须说明分歧方错在哪）")
    return errs


def check_translation(tr, q: dict) -> list[str]:
    """
    translation = {"stem": str, "choices": {label: str}}。

    选项按 label 逐条对应而非整体一段：导入时要落到 choice 行上，
    且能机械校验「每个选项都译了、没有多译」—— 整段译文做不到这两点。
    """
    if not isinstance(tr, dict):
        return ["translation 须为 {stem, choices} 对象"]
    errs = []
    stem = tr.get("stem")
    if not isinstance(stem, str) or not stem.strip():
        errs.append("translation.stem 为空")
    choices = tr.get("choices")
    labels = {ch["label"] for ch in q["choices"]}
    if not isinstance(choices, dict):
        errs.append("translation.choices 须为 {label: 译文} 对象")
    else:
        got = set(choices)
        if got != labels:
            errs.append(f"translation.choices 的选项 {sorted(got)} ≠ 题目选项 {sorted(labels)}")
        empty = [k for k, v in choices.items() if not isinstance(v, str) or not v.strip()]
        if empty:
            errs.append(f"translation.choices 有空译文 {sorted(empty)}")
    if set(tr) - {"stem", "choices"}:
        errs.append(f"translation 多余字段 {sorted(set(tr) - {'stem', 'choices'})}")
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
        if spec.get("translation"):
            print(f"⚠ 本题库要求每题附 translation（{spec['translation']['locale']}）："
                  f"{{'stem': 题干译文, 'choices': {{label: 选项译文}}}}，见 enrich_spec.md「翻译」\n")
        if vocab := spec.get("topic_vocabulary"):
            field = topic_field_of(spec)
            print(f"⛔ 本题库的 {field} 是【封闭词表】，只能从下面挑，不得新造。\n")
            print(f"⭐ 用法：先找这道题在讲哪一个【小分類】（· 号那一层，共 "
                  f"{sum(len(v.get('items', [])) for v in vocab)} 个），再取它的父中分類。")
            print(f"   ⚠️ 直接在 {len(vocab)} 个中分類里凭印象挑，正是最容易错的做法 ——")
            print(f"   而「同一分野内挑错」这种错，交叉校验【看不见】（它只卡跨分野）。\n")
            for v in vocab:
                items = "、".join(v.get("items", []))
                print(f"  [分野{v['field']}] {v['name']}")
                if items:
                    print(f"        · {items}")
            print()
        for no in sorted(n for n in qmap if r[0] <= n <= r[1]):
            q = qmap[no]
            c = claims_of(q)
            contested, why = is_contested(q)
            print(f"### 问题 #{no}  [{q['kind']} · 应选 {q['pick']} 项]  fp={fingerprint(q)}"
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


def cmd_write(bank: str, src: Path) -> None:
    """
    从 Python 字面量文件写入分片 —— 避开 JSON 的转义地雷。

    输入文件里定义一个 ITEMS 列表，长文本用三引号字符串包起来即可：
    换行、引号、反斜杠、markdown 表格全是字面值，不需要任何转义。
    （直接手写 JSON 已三次踩坑：裸换行 x2、非法转义 x1）

    相比手写 JSON 的三个好处：
      · 转义问题消失
      · 自动路由到正确的分片文件，不用自己算题号区间
      · **写盘前先校验**，坏数据根本不会落地（而非写完再 check 才发现）

    格式示例见 pipeline/banks/<bank>/enrich_spec.md。

    注：用 exec 载入文件。这是本地开发工具，输入文件由使用者自己生成，
    信任级别等同于直接 `python3 that_file.py`。
    """
    doc, spec = load_bank(bank)
    qmap = {q["no"]: q for q in doc["questions"]}

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

    # 先全量校验，一条不过就整批不写 —— 避免半批落地导致分片状态不明
    errs = []
    for it in items:
        no = it.get("no")
        if no not in qmap:
            errs.append(f"#{no}: 题号不在题库中")
            continue
        # 指纹是硬门禁：对不上说明没有基于真实题面来写
        want = fingerprint(qmap[no])
        got = it.get("fp")
        if got != want:
            errs.append(
                f"#{no}: fp 不符（期望 {want}，收到 {got or '缺失'}）"
                f" —— 请先跑 `enrich.py next` 取真实题面，不要凭记忆写")
            continue
        errs += [f"#{no}: {e}" for e in check_item(it, qmap[no], spec)]
    if errs:
        print(f"✗ 校验未通过（{len(errs)} 个问题），未写入任何文件：")
        for e in errs[:25]:
            print(f"    {e}")
        if len(errs) > 25:
            print(f"    …… 另有 {len(errs)-25} 个")
        sys.exit(1)

    # 按题号路由到分片；同一批可以跨片
    ranges = shard_ranges(list(qmap))
    buckets: dict[tuple[int, int], list] = {}
    for it in items:
        rng = next((r for r in ranges if r[0] <= it["no"] <= r[1]), None)
        if rng is None:
            sys.exit(f"✗ #{it['no']} 不属于任何分片区间")
        buckets.setdefault(rng, []).append(it)

    d = shard_dir(bank)
    d.mkdir(parents=True, exist_ok=True)
    for rng, batch in sorted(buckets.items()):
        path = d / shard_name(rng)
        if path.exists():
            try:
                existing = json.loads(path.read_text(encoding="utf-8"))
            except json.JSONDecodeError:
                # 上次写坏的半成品：直接重建，不试图挽救
                existing = {"bank": bank, "range": list(rng), "items": []}
        else:
            existing = {"bank": bank, "range": list(rng), "items": []}

        # 同题号覆盖（允许重跑单题订正），其余保留
        merged = {i["no"]: i for i in existing.get("items", [])}
        # fp 只用于写入门禁，不落进产物 —— 它不是题目数据的一部分
        merged.update({i["no"]: {k: v for k, v in i.items() if k != "fp"} for i in batch})
        existing.update({
            "bank": bank,
            "range": list(rng),
            "model": existing.get("model", "claude (Claude Code 订阅额度)"),
            "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
            "items": [merged[k] for k in sorted(merged)],
        })
        path.write_text(json.dumps(existing, ensure_ascii=False, indent=1), encoding="utf-8")
        want = sum(1 for n in qmap if rng[0] <= n <= rng[1])
        print(f"✓ {shard_name(rng)}  写入 {len(batch)} 题，该片现有 {len(existing['items'])}/{want}")
        # 回显真实题干开头：指纹已挡住错位，这里再给人眼一次确认机会
        for it in sorted(batch, key=lambda x: x["no"]):
            print(f"     #{it['no']}  {qmap[it['no']]['stem'][:42]}…")


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
    w = sub.add_parser("write", help="从 Python 字面量文件写入分片（避开 JSON 转义）")
    w.add_argument("bank")
    w.add_argument("file", type=Path)
    a = ap.parse_args()
    {"status": lambda: cmd_status(a.bank),
     "check": lambda: cmd_check(a.bank),
     "merge": lambda: cmd_merge(a.bank),
     "next": lambda: cmd_next(a.bank, a.count),
     "write": lambda: cmd_write(a.bank, a.file)}[a.cmd]()


if __name__ == "__main__":
    main()
