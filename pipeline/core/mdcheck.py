"""
解析正文（Markdown）的强调标记检查：渲染之后会不会露出星号。

两个客户端都按 CommonMark 渲染解析，并采用 CJK 友好的强调规则（网页 remark-cjk-friendly；
规格 github.com/tats-u/markdown-cjk-friendly 的 specification.md，iOS 各自对齐）。
本模块按这套规则如实走一遍：先判每串星号能开、能闭（左右侧界定，中日文标点不算「标点」那一支），
再按 CommonMark 的配对算法（含「3 的倍数」规则）配对；配不上的星号渲染出来就是字面的 *，即为问题。

  emphasis_errors(text)  每个行内单元（段落、列表项、表格单元格）里会露出来的星号
  structure_diff(a, b)   两段 Markdown 的骨架（段落、表格行、代码块、标题、引用行）是否一致
  strip_markers(text)    去掉全部强调标记（含转义的 \\*），用于「只改标记、不改文字」的比对

字面的星号（通配符 *.example.com、SSE-*）写成 \\* 。
"""
import re
import unicodedata

_BLOCK_START = re.compile(r"^(#{1,6}\s|[-+*]\s+|\d+[.)]\s+)")
_QUOTE = re.compile(r"^>\s?")
_FENCE = re.compile(r"^(```|~~~)")
_CODE_SPAN = re.compile(r"`[^`]*`")
_RUN = re.compile(r"\*+")


def _is_emoji_presentation(ch: str) -> bool:
    """默认以 emoji 显示的字符（✅ ❌ ⭐ 💰）：宽字符里落在符号与 emoji 区段的那些。规格把它们排除在 CJK 之外。"""
    cp = ord(ch)
    return unicodedata.east_asian_width(ch) in ("W", "F") and (0x2190 <= cp <= 0x2BFF or 0x1F000 <= cp <= 0x1FAFF)


def _is_cjk(ch: str) -> bool:
    cp = ord(ch)
    hangul = 0x1100 <= cp <= 0x11FF or 0x3130 <= cp <= 0x318F or 0xAC00 <= cp <= 0xD7AF
    return hangul or (unicodedata.east_asian_width(ch) in ("W", "F", "H") and not _is_emoji_presentation(ch))


def _is_punct(ch: str) -> bool:
    """CommonMark 0.31 的 Unicode 标点：P* 与 S* 类。"""
    return unicodedata.category(ch)[0] in ("P", "S")


def _neighbor(u: str, i: int, step: int) -> str | None:
    """星号串旁边起作用的那个字符；行首行尾算空白（None）。标准变体选择符 U+FE00–FE0E 跟着前一个字符算。"""
    j = i + step
    if not 0 <= j < len(u):
        return None
    if step < 0 and 0xFE00 <= ord(u[j]) <= 0xFE0E and j > 0:
        return u[j - 1]
    return u[j]


def _flanking(before: str | None, after: str | None) -> tuple[bool, bool]:
    """(left-flanking, right-flanking)，按 CJK 友好规格的修订定义。"""
    ws = lambda c: c is None or c.isspace()  # noqa: E731
    ncp = lambda c: c is not None and _is_punct(c) and not _is_cjk(c)  # 非 CJK 标点  # noqa: E731
    cjk = lambda c: c is not None and _is_cjk(c)  # noqa: E731
    left = not ws(after) and (not ncp(after) or ws(before) or ncp(before) or cjk(before))
    right = not ws(before) and (not ncp(before) or ws(after) or ncp(after) or cjk(after))
    return left, right


def inline_units(text: str) -> list[str]:
    """
    切成行内单元：强调标记只在单元内部配对（段落可跨行，列表项、表格单元格、标题各自独立）。
    围栏代码块整块跳过；引用的 > 前缀去掉后按普通行处理。
    """
    units: list[str] = []
    cur: list[str] = []
    in_fence = False

    def flush() -> None:
        if cur:
            units.append(" ".join(cur))
            cur.clear()

    for line in text.split("\n"):
        s = line.strip()
        if _FENCE.match(s):
            flush()
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        while _QUOTE.match(s):
            s = _QUOTE.sub("", s, count=1).strip()
        if not s:
            flush()
        elif s.startswith("|"):
            flush()
            units.extend(s.strip("|").split("|"))
        elif _BLOCK_START.match(s):
            flush()
            cur.append(_BLOCK_START.sub("", s, count=1))
        else:
            cur.append(s)
    flush()
    return units


def _unit_errors(unit: str) -> list[str]:
    # 代码片段内容换成中性字符、保留两侧反引号（旁边星号的侧界定看的是反引号）；转义的星号是字面
    u = _CODE_SPAN.sub("`x`", unit).replace("\\*", "x")
    runs = []
    for m in _RUN.finditer(u):
        left, right = _flanking(_neighbor(u, m.start(), -1), _neighbor(u, m.end() - 1, 1))
        runs.append({"len": len(m.group(0)), "orig": len(m.group(0)), "open": left, "close": right,
                     "at": m.start(), "dead": False})
    # CommonMark「process emphasis」：从左往右找闭合，向左找最近的可开的同类标记；
    # 两者之一可开可闭时，长度之和是 3 的倍数（且不都是 3 的倍数）就不能配。
    # 长度取【剩余】长度：网页渲染用的 micromark 这样算（cmark 取原始长度，两者只在一串被部分用掉后才有分歧）
    for ci, closer in enumerate(runs):
        if not closer["close"]:
            continue
        while closer["len"] > 0:
            oi = next((k for k in range(ci - 1, -1, -1)
                       if runs[k]["open"] and runs[k]["len"] > 0 and not runs[k]["dead"]
                       and not ((runs[k]["close"] or closer["open"])
                                and (runs[k]["len"] + closer["len"]) % 3 == 0
                                and not (runs[k]["len"] % 3 == 0 and closer["len"] % 3 == 0))), None)
            if oi is None:
                break
            use = 2 if runs[oi]["len"] >= 2 and closer["len"] >= 2 else 1
            runs[oi]["len"] -= use
            closer["len"] -= use
            for k in range(oi + 1, ci):   # 夹在中间、还没配上的标记从此作废，渲染成字面星号
                runs[k]["dead"] = True
    errs = []
    for r in runs:
        if r["orig"] >= 4:
            # 四个以上连写：各渲染器拆分、配对的结果不一致（上面那条长度规则就在这里分岔），一律算问题
            errs.append(f"连续 {r['orig']} 个星号（…{u[max(0, r['at'] - 8):r['at'] + r['orig'] + 8]}…）")
            continue
        # 既不能开也不能闭（两侧都是空白，如「5 * 3」）的星号本来就是字面字符，不算问题
        if r["len"] > 0 and (r["open"] or r["close"]):
            ctx = u[max(0, r["at"] - 8):r["at"] + r["orig"] + 8]
            errs.append(f"「{'*' * r['len']}」会原样显示（…{ctx}…）")
    return errs


def code_block_errors(text: str) -> list[str]:
    """围栏代码块里连写的星号：代码块不渲染强调，** 会原样显示（单个 * 多是通配符，不报）。"""
    out, in_fence = [], False
    for line in text.split("\n"):
        s = line.strip()
        while _QUOTE.match(s):
            s = _QUOTE.sub("", s, count=1).strip()
        if _FENCE.match(s):
            in_fence = not in_fence
        elif in_fence and "**" in s:
            out.append(f"代码块里的「**」会原样显示：「{s[:30]}…」")
    return out


def emphasis_errors(text: str) -> list[str]:
    """全文的强调标记问题，每条带上出问题的单元开头，便于定位。"""
    out = code_block_errors(text)
    for unit in inline_units(text):
        for err in _unit_errors(unit):
            head = unit.strip()[:30]
            out.append(f"{err}：「{head}…」")
    return out


def structure(text: str) -> dict[str, int]:
    """Markdown 的骨架计数：段落、表格行、代码块围栏、标题、引用行。译文应与原文逐项相同。"""
    lines = [ln.strip() for ln in text.split("\n")]
    return {
        "段落": len([b for b in re.split(r"\n[ \t]*\n", text.strip()) if b.strip()]),
        "表格行": sum(1 for ln in lines if ln.startswith("|")),
        "代码块围栏": sum(1 for ln in lines if _FENCE.match(ln)),
        "标题": sum(1 for ln in lines if re.match(r"#{1,6}\s", ln)),
        "引用行": sum(1 for ln in lines if ln.startswith(">")),
    }


def structure_diff(src: str, dst: str) -> list[str]:
    """译文与原文骨架不同的地方：漏译、合并或拆分了段落、表格、代码块。"""
    a, b = structure(src), structure(dst)
    return [f"{k}：原文 {a[k]}，译文 {b[k]}" for k in a if a[k] != b[k]]


def strip_markers(text: str) -> str:
    """去掉全部强调标记与星号转义：两段文字去掉之后相同 ⇔ 只改了标记。"""
    return text.replace("\\*", "").replace("*", "")
