#!/usr/bin/env python3
"""
专有名词归一 —— 把素材里被机翻成中文的产品名还原成官方英文名。

    python3 pipeline/core/termnorm.py aws-saa-c03            # 预览：每条规则命中几次、改成什么
    python3 pipeline/core/termnorm.py aws-saa-c03 --diff     # 逐处列出「原文 → 归一后」

为什么需要：SAA-C03 的素材是第三方机翻的中文版，同一个服务在不同题里叫法不一 ——
「S3 深度存档冰川存储」与「S3 Glacier 深度存档」是同一个东西，「简单队列服务」就是 SQS。
学习者在考场和工作里看到的都是英文名，中文译名只会多一层换算，叫法不一还会让人以为是两样东西。

为什么在【导入时】做、而不是改写 questions.json / enriched.json（用户裁定 2026-11-15，方案 B③）：
  · 原始产物一字不动，随时能对照素材原文
  · 规则是封闭词表 + 逐处登记，与 textrepair.py 的连字修复同一路数：可复现、可审计
范围（裁定 B①②）：只动产品名与存储类名；「存储桶」「区域」「安全组」这类概念名词
AWS 官方中文文档本身就这么写，不动。有通用简称的用简称（Amazon EBS），没有的用官方英文名。

规则表：pipeline/banks/<slug>/term_normalize.json（没有这个文件 = 该题库不归一）。
本模块零题库特有逻辑。
"""
import argparse
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path

CJK = re.compile(r"[㐀-鿿豈-﫿]")
# 素材是 pdftotext 抽出来的，断行处会在词中间留一个空格（「亚马 逊」「成本报 告」）——
# 匹配时每个字之间都容忍空白
_GAP = r"\s*"
# 括注：「弹性块存储（Amazon EBS）」里的「（Amazon EBS）」
_PAREN = re.compile(r"\s*[（(]\s*([^（）()]{1,60}?)\s*[)）]")


def _flex(literal: str) -> str:
    """字面量 → 容忍字间空白的正则。ASCII 词内部不插空白（不把 EBS 拆成 E B S）。"""
    out, prev_ascii = [], False
    for ch in literal:
        if ch.isspace():
            out.append(_GAP)
            prev_ascii = False
            continue
        is_ascii = ch.isascii() and ch.isalnum()
        if out and not (is_ascii and prev_ascii) and out[-1] != _GAP:
            out.append(_GAP)
        out.append(re.escape(ch))
        prev_ascii = is_ascii
    return "".join(out)


def _key(s: str) -> str:
    """比较括注是否只是重复了一遍名字用：去空白、小写、去 Amazon/AWS 前缀。"""
    s = re.sub(r"\s+", "", s).lower()
    return re.sub(r"^(amazon|aws)", "", s)


@dataclass
class Rule:
    to: str
    variants: list[str]
    aliases: set[str]       # 括注里出现这些（归一比较后）就收掉，不留「Amazon EBS（EBS）」
    kind: str               # service | storage-class
    need_prefix: bool       # 译名本身是普通词（「命令行界面」）时，只在带 AWS/亚马逊 前缀时才认
    not_followed_by: str    # 后面紧跟这些就不认：「到 AWS 配置三个 VPN」里的「配置」是动词，不是 AWS Config
    pattern: re.Pattern | None = None


class TermNormalizer:
    # 服务类的前缀：被一并吞掉，由 to 自带的前缀代替（「亚马逊弹性块存储」→「Amazon EBS」）
    SERVICE_PREFIX = r"(?:(?:" + "|".join(_flex(p) for p in ("亚马逊网络服务", "亚马逊", "Amazon", "AWS")) + ")" + _GAP + ")"
    # 存储类必须跟在 S3 后面 —— 「EFS 智能分层」「DynamoDB 标准-IA」不是 S3 存储类
    STORAGE_PREFIX = r"(?:(?:" + _flex("亚马逊") + r"|Amazon)" + _GAP + r")?S3" + _GAP

    def __init__(self, table: dict):
        self.rules: list[Rule] = []
        for r in table["rules"]:
            rule = Rule(
                to=r["to"],
                variants=sorted(r["from"], key=len, reverse=True),
                aliases={_key(a) for a in [r["to"], *r.get("aliases", [])]},
                kind=r.get("kind", "service"),
                need_prefix=r.get("need_prefix", False),
                not_followed_by=r.get("not_followed_by", ""),
            )
            alt = "|".join(_flex(v) for v in rule.variants)
            if rule.kind == "storage-class":
                # 「智能分层存储」的「存储」一并收掉，但「存储类别」里的不收（否则剩个孤零零的「类别」）
                rule.pattern = re.compile(f"{self.STORAGE_PREFIX}(?:{alt})(?:{_GAP}存{_GAP}储(?!{_GAP}类))?")
            else:
                pre = self.SERVICE_PREFIX + ("" if rule.need_prefix else "?")
                guard = f"(?!{_GAP}(?:{rule.not_followed_by}))" if rule.not_followed_by else ""
                rule.pattern = re.compile(f"{pre}(?:{alt}){guard}")
            self.rules.append(rule)
        # 收尾：其余「亚马逊 + 英文名」只换前缀（「亚马逊 EC2」→「Amazon EC2」）
        self.amazon_latin = re.compile(_flex("亚马逊") + _GAP + r"(?=[A-Za-z])")
        self.amazon_full = re.compile(_flex("亚马逊网络服务") + r"\s*[（(]\s*AWS\s*[)）]")

    # ---- 单段文本 ----
    def normalize(self, text: str) -> tuple[str, list[tuple[str, str, str]]]:
        """返回 (归一后文本, [(规则 to, 原文片段, 替换为), ...])。"""
        log: list[tuple[str, str, str]] = []
        # 按表里的顺序跑：表里把具体的写在前面（「标准-IA」在「标准」之前）。
        # 同一条规则内的变体已按长度降序，不会出现短的抢在长的前面
        for rule in self.rules:
            text = self._apply(rule, text, log)
        text = self._sub(self.amazon_full, "AWS", text, log, "AWS")
        text = self._sub(self.amazon_latin, "Amazon ", text, log, "Amazon")
        return text, log

    def _apply(self, rule: Rule, text: str, log: list) -> str:
        out, pos = [], 0
        for m in rule.pattern.finditer(text):
            if m.start() < pos:
                continue
            end = m.end()
            # 括注若只是同一个名字（或它的另一种译法）的重复，一并收掉
            pm = _PAREN.match(text, end)
            if pm:
                inner = pm.group(1)
                inner_norm, _ = self.normalize(inner) if CJK.search(inner) else (inner, None)
                if _key(inner) in rule.aliases or _key(inner_norm) in rule.aliases:
                    end = pm.end()
            out.append(text[pos:m.start()])
            out.append(self._spaced(text, m.start(), end, rule.to))
            log.append((rule.to, text[m.start():end], rule.to))
            pos = end
        out.append(text[pos:])
        return "".join(out)

    def _sub(self, pat: re.Pattern, to: str, text: str, log: list, label: str) -> str:
        def repl(m: re.Match) -> str:
            log.append((label, m.group(0), to.strip()))
            before = text[m.start() - 1] if m.start() > 0 else ""
            after = text[m.end()] if m.end() < len(text) else ""
            tail = " " if not to.endswith(" ") and after and CJK.match(after) else ""
            return (" " if before and CJK.match(before) else "") + to + tail
        return pat.sub(repl, text)

    @staticmethod
    def _spaced(text: str, start: int, end: int, word: str) -> str:
        """中英之间补一个空格，与素材原有的排版一致（「使用 Amazon SQS 队列」）。"""
        before = text[start - 1] if start > 0 else ""
        after = text[end] if end < len(text) else ""
        return (" " if before and CJK.match(before) else "") + word + (" " if after and CJK.match(after) else "")

    # ---- 整道题 ----
    def normalize_question(self, q: dict) -> list[str]:
        """就地归一题干与选项；返回登记行（供导入时打印）。"""
        notes = []
        q["stem"], log = self.normalize(q["stem"])
        notes += [f"#{q['no']} 题干: {src!r} → {dst}" for _, src, dst in log]
        for ch in q["choices"]:
            ch["body"], log = self.normalize(ch["body"])
            notes += [f"#{q['no']} {ch['label']}: {src!r} → {dst}" for _, src, dst in log]
        return notes


def load_normalizer(spec_dir: Path) -> TermNormalizer | None:
    path = spec_dir / "term_normalize.json"
    if not path.exists():
        return None
    return TermNormalizer(json.loads(path.read_text(encoding="utf-8")))


def main() -> None:
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    from paths import REPO, bank_dir  # noqa: E402

    ap = argparse.ArgumentParser(description="预览专有名词归一的效果（只读，不写任何文件）")
    ap.add_argument("bank")
    ap.add_argument("--diff", action="store_true", help="逐处列出原文与替换结果")
    args = ap.parse_args()

    norm = load_normalizer(REPO / "pipeline" / "banks" / args.bank)
    if norm is None:
        sys.exit(f"✗ {args.bank} 没有 term_normalize.json")
    qs = json.loads((bank_dir(args.bank) / "enriched.json").read_text(encoding="utf-8"))["questions"]

    from collections import Counter, defaultdict
    hits: Counter = Counter()
    forms: dict[str, Counter] = defaultdict(Counter)
    touched = 0
    for q in qs:
        notes_before = sum(hits.values())
        for text in [q["stem"], *(c["body"] for c in q["choices"])]:
            _, log = norm.normalize(text)
            for to, src, _ in log:
                hits[to] += 1
                forms[to][re.sub(r"\s+", " ", src)] += 1
                if args.diff:
                    print(f"#{q['no']}: {src!r} → {to}")
        touched += sum(hits.values()) > notes_before
    print(f"\n{len(qs)} 题中 {touched} 题有替换，共 {sum(hits.values())} 处\n")
    for to, n in hits.most_common():
        variants = "；".join(f"{s}×{c}" for s, c in forms[to].most_common(6))
        print(f"{n:5d}  {to:<34} ← {variants}")


if __name__ == "__main__":
    main()
