#!/usr/bin/env python3
"""
导入前的题面文字整理 —— 用户看到的题干与选项，由素材原文依次经过三步得到：

  1. 去掉断行留下的汉字间空格（textrepair.close_cjk_gaps；题库 spec 打开 cjk_gap_repair 才做）
  2. 专有名词归一（termnorm.py；题库有 term_normalize.json 才做）
  3. 勘误（$MELETE_DATA_ROOT/<bank>/errata.json；逐题逐处、审过的修正）

三步都只改进库的文字，questions.json / enriched.json 一字不动，随时能对照素材原文。
导入（load.py）与逐题校对（proofread.py）都调这里的 FacePreparer，校对者看到的就是用户看到的。

勘误按「整理后的文字」定位：find 必须在该字段里恰好出现一次，否则整批拒绝 ——
规则表或素材变了之后勘误对不上时，要当场暴露，不能悄悄跳过（跳过 = 用户又看到错字）。

    python3 pipeline/core/facetext.py <bank>           # 预览：三步各改了多少处；勘误不成立时退出码 1
    python3 pipeline/core/facetext.py <bank> --diff    # 逐条列出勘误前后
"""
import argparse
import json
import sys
from dataclasses import dataclass
from pathlib import Path

from termnorm import load_normalizer
from textrepair import close_cjk_gaps

ERRATA_FILE = "errata.json"
# term 机翻的产品 / 功能名 → 官方英文名 · typo 错字 · garble 抽取残留（断开的词、多余字符）
# misalign 选项文字串位 · wording 译法错到影响理解
ERRATA_KINDS = ("term", "typo", "garble", "misalign", "wording")
ERRATUM_KEYS = {"no", "field", "find", "replace", "kind", "basis"}


@dataclass(frozen=True)
class Erratum:
    no: int
    field: str      # "stem" 或 "choice:<字母>"
    find: str
    replace: str
    kind: str
    basis: str      # 依据：为什么这样改


class ErrataError(Exception):
    """勘误表格式不对，或有勘误在整理后的文字里定位不到唯一一处。"""

    def __init__(self, problems: list[str]):
        super().__init__(f"{len(problems)} 个问题")
        self.problems = problems


# ---------- 字段 ----------

def field_names(q: dict) -> list[str]:
    return ["stem"] + [f"choice:{c['label']}" for c in q["choices"]]


def get_field(q: dict, field: str) -> str | None:
    if field == "stem":
        return q["stem"]
    if field.startswith("choice:"):
        label = field.split(":", 1)[1]
        hits = [c for c in q["choices"] if c["label"] == label]
        return hits[0]["body"] if len(hits) == 1 else None
    return None


def set_field(q: dict, field: str, text: str) -> None:
    if field == "stem":
        q["stem"] = text
        return
    label = field.split(":", 1)[1]
    next(c for c in q["choices"] if c["label"] == label)["body"] = text


# ---------- 勘误 ----------

def parse_errata(doc: dict) -> list[Erratum]:
    """校验勘误表的格式。任何一条不合格都整表拒绝。"""
    problems, out = [], []
    items = doc.get("items")
    if not isinstance(items, list):
        raise ErrataError(["缺少 items 数组"])
    for i, it in enumerate(items):
        where = f"第 {i + 1} 条"
        if not isinstance(it, dict) or set(it) != ERRATUM_KEYS:
            problems.append(f"{where}: 字段应为 {sorted(ERRATUM_KEYS)}")
            continue
        types_ok = isinstance(it["no"], int) and all(
            isinstance(it[k], str) for k in ("field", "find", "replace", "kind", "basis"))
        if not types_ok:
            problems.append(f"{where}: no 应为整数，其余字段应为字符串")
            continue
        where = f"{where}（#{it['no']} {it['field']}）"
        if not it["find"]:
            problems.append(f"{where}: find 为空")
        if it["find"] == it["replace"]:
            problems.append(f"{where}: find 与 replace 相同")
        if it["kind"] not in ERRATA_KINDS:
            problems.append(f"{where}: kind 应为 {ERRATA_KINDS} 之一")
        if not it["basis"].strip():
            problems.append(f"{where}: 没写依据")
        out.append(Erratum(**it))
    if problems:
        raise ErrataError(problems)
    return out


def load_errata(data_dir: Path) -> list[Erratum]:
    path = data_dir / ERRATA_FILE
    if not path.exists():
        return []
    return parse_errata(json.loads(path.read_text(encoding="utf-8")))


def locate(text: str, find: str) -> list[int]:
    """find 在 text 里所有出现的起点（允许重叠计数，用来判断「恰好一次」）。"""
    out, pos = [], 0
    while (i := text.find(find, pos)) >= 0:
        out.append(i)
        pos = i + 1
    return out


def apply_errata(qs: list[dict], errata: list[Erratum]) -> list[str]:
    """
    就地套用勘误；返回登记行。任何一条定位不到唯一一处、或同一字段里互相重叠，都整批拒绝（ErrataError）。
    同一字段的多条勘误按原文位置一次替换，彼此不受先后顺序影响。
    """
    qmap = {q["no"]: q for q in qs}
    by_field: dict[tuple[int, str], list[Erratum]] = {}
    problems = []
    for e in errata:
        if e.no not in qmap:
            problems.append(f"#{e.no}: 题号不在题库中")
            continue
        if get_field(qmap[e.no], e.field) is None:
            problems.append(f"#{e.no}: 没有字段 {e.field}")
            continue
        by_field.setdefault((e.no, e.field), []).append(e)

    plan: list[tuple[int, str, list[tuple[int, Erratum]]]] = []
    for (no, field), items in by_field.items():
        text = get_field(qmap[no], field)
        spans = []
        for e in items:
            hits = locate(text, e.find)
            if len(hits) != 1:
                problems.append(f"#{no} {field}: {e.find!r} 出现 {len(hits)} 次（应恰好 1 次）")
                continue
            spans.append((hits[0], e))
        spans.sort(key=lambda s: s[0])
        for (a, ea), (b, _) in zip(spans, spans[1:]):
            if a + len(ea.find) > b:
                problems.append(f"#{no} {field}: 勘误 {ea.find!r} 与后一条重叠")
        plan.append((no, field, spans))
    if problems:
        raise ErrataError(problems)

    log = []
    for no, field, spans in plan:
        text = get_field(qmap[no], field)
        for start, e in reversed(spans):
            text = text[:start] + e.replace + text[start + len(e.find):]
            log.append(f"#{no} {field}: {e.find!r} → {e.replace!r}（{e.kind}）")
        set_field(qmap[no], field, text)
    return log


# ---------- 三步合起来 ----------

@dataclass
class FaceReport:
    gap_fixes: int = 0
    gap_questions: int = 0
    term_notes: list[str] | None = None
    errata_log: list[str] | None = None


class FacePreparer:
    """题面整理的唯一入口：导入与校对都用它，保证校对者看到的就是用户看到的。"""

    def __init__(self, spec: dict, spec_dir: Path, data_dir: Path):
        self.gap_repair = bool(spec.get("cjk_gap_repair"))
        self.normalizer = load_normalizer(spec_dir)
        self.errata = load_errata(data_dir)

    def prepare(self, qs: list[dict]) -> FaceReport:
        """就地整理题干、选项（以及解析里的断行空格）。勘误不成立时抛 ErrataError。"""
        rep = FaceReport(term_notes=[], errata_log=[])
        if self.gap_repair:
            for q in qs:
                n = 0
                q["stem"], k = close_cjk_gaps(q["stem"])
                n += k
                for c in q["choices"]:
                    c["body"], k = close_cjk_gaps(c["body"])
                    n += k
                e = q.get("enrichment") or {}
                if e.get("explanation"):
                    e["explanation"], k = close_cjk_gaps(e["explanation"])
                    n += k
                rep.gap_fixes += n
                rep.gap_questions += 1 if n else 0
        if self.normalizer is not None:
            for q in qs:
                rep.term_notes += self.normalizer.normalize_question(q)
        rep.errata_log = apply_errata(qs, self.errata)
        return rep


def main() -> None:
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    from paths import REPO, bank_dir  # noqa: E402

    ap = argparse.ArgumentParser(description="预览题面整理的效果（只读，不写任何文件）")
    ap.add_argument("bank")
    ap.add_argument("--diff", action="store_true", help="逐条列出勘误前后")
    args = ap.parse_args()

    spec_dir = REPO / "pipeline" / "banks" / args.bank
    spec_path = spec_dir / "enrich_spec.json"
    spec = json.loads(spec_path.read_text(encoding="utf-8")) if spec_path.exists() else {}
    qs = json.loads((bank_dir(args.bank) / "enriched.json").read_text(encoding="utf-8"))["questions"]
    try:
        prep = FacePreparer(spec, spec_dir, bank_dir(args.bank))
        rep = prep.prepare(qs)
    except ErrataError as e:
        print(f"✗ 勘误表不成立（{len(e.problems)} 个问题）：")
        for p in e.problems[:40]:
            print(f"    {p}")
        sys.exit(1)
    touched = len({n.split(" ", 1)[0] for n in rep.term_notes})
    print(f"断行空格  {rep.gap_fixes} 处（{rep.gap_questions} 题）" if prep.gap_repair else "断行空格  未打开")
    print(f"专有名词  {len(rep.term_notes)} 处（{touched} 题）")
    print(f"勘误      {len(rep.errata_log)} 条（共 {len(prep.errata)} 条）")
    if args.diff:
        for line in rep.errata_log:
            print(f"    {line}")


if __name__ == "__main__":
    main()
