"""
文本层缺陷的**确定性**修复 —— 只修「信息损失可无歧义还原」的那类。

目前只有一类：pdftotext 抽取时 fi / fl / ffi / ffl 连字丢失
（PDF 字体缺 ToUnicode 映射，连字被抽成一个空格）：

    Configure → Con gure      traffic → tra c      file → le

SAP-C02 实测 76.7% 的题中招（SAA 中文版只有 4 题，那边用了专项正则）。

为什么用**封闭词表**而不是系统词典：
  · 可复现 —— 产物不随机器上 /usr/share/dict 的版本变化
  · 可审计 —— 词表是从语料生成、逐词肉眼核过的（见 docs/design/active/data-pipeline.md）
  · 零误报 —— 词典法会把 "a n" 还原成 "flan"、把 sts:AssumeRole 的 sts 还原成 fists

新题库若出现词表外的连字词：先在语料里统计，核过再加进来。
本模块零题库特有逻辑。
"""
import re

LIGATURES = ("ffi", "ffl", "fi", "fl")   # 长的在前，避免 ffi 被 fi 抢先

# 连字丢失后能被无歧义还原的词（小写）。分两组只是为了读者好核对，算法不区分。
LIGATURE_WORDS = frozenset(
    # 连字在词中：左右都有残片（Con|gure、tra|c）
    """artificial certificate certificates config configs configuration configurations configure configured
    configures configuring confirm confirmation confirmed confirms define defines defining
    definition efficiency efficient misconfigured misconfiguration preconfigured reconfigured efficiently identifiable identification identified identifies
    indefinitely modification modified modifies notification notifications notifies office
    officer offices offload predefined prefix prefixes profile profiles reconfigure significant
    significantly simplified specific specifically specification specifications specified specifies
    sufficient traffic unified verification verified workflow workflows
    defined insufficient confidential affinity scientific staffing notified identifier
    find flag flags fine difficult flash""".split()
    # ⚠ 词典法会提议 "a field"→afield：左右都是真词时不能合并。词表只收核过的项，这就是不用词典的原因。
    # 连字在词首：只剩右残片（|le、|eet、|rewall）
    + """field fields figure figured figures figuring file files filter filtered filtering filters
    final finalizes finance financial finding findings finds finish finished finishes firewall
    firewalls first five fixed flagship fleet fleets flexibility flexible flood flow flows""".split()
)

# 词中形态：左残片 + 空格 + 右残片，两侧都不能再连着字母
_INTERNAL = re.compile(r"(?<![A-Za-z])([A-Za-z]+) ([a-z]+)(?![A-Za-z])")
# 词首形态：独立小写 token。前面只能是行首/空白/引号/括号，后面只能是行尾/空白/标点 ——
# 显式排除冒号与斜杠等，避免 `sts:AssumeRole` 这类标识符被当成残片
# 后接允许连字符（field-level）与冒号（types of files:）。冒号曾被排除是为了 sts:AssumeRole，
# 但真正的保险是封闭词表（fists 不在表里），不是标点。
_INITIAL = re.compile(r"(?:^|(?<=[\s(\"']))([a-z]{2,})(?=$|[\s.,;:)!?\"'\-/0-9@])")   # 也允许后接数字/@：nance1@example.com


def _recombine(left: str, right: str) -> str | None:
    for lig in LIGATURES:
        word = left + lig + right
        if word.lower() in LIGATURE_WORDS:
            return word
    return None


def restore_ligatures(text: str) -> tuple[str, list[str]]:
    """
    返回 (修复后的文本, 修复登记)。登记形如 "Con gure→Configure"，供落进 repairs 字段审计。

    先修词中形态再修词首形态：`Con gure` 若先跑词首规则，`gure` 会被单独还原成 `figure`。
    """
    repairs: list[str] = []

    def fix_initial(m: re.Match) -> str:
        word = _recombine("", m.group(1))
        if word is None:
            return m.group(0)
        repairs.append(f"{m.group(1)}→{word}")
        return word

    # 词中形态不能用 re.sub：`SNS noti cation` 会先配到 `SNS noti`，配对失败后
    # 扫描指针已越过 `noti`，真正的 `noti cation` 永远轮不到。改为手动推进 ——
    # 失败时只前进到空格之后，让右残片有机会成为下一对的左残片。
    out, pos = [], 0
    while (m := _INTERNAL.search(text, pos)):
        word = _recombine(m.group(1), m.group(2))
        if word is None:
            nxt = m.start(2)          # 跳到右残片开头，重新配对
            out.append(text[pos:nxt]); pos = nxt
            continue
        repairs.append(f"{m.group(0)}→{word}")
        out.append(text[pos:m.start()]); out.append(word); pos = m.end()
    out.append(text[pos:])
    text = "".join(out)
    text = _INITIAL.sub(fix_initial, text)
    return text, repairs
