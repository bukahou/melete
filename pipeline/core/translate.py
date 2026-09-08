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

# 平假名 + 片假名。⭐ 与下面的简体字黑名单一起构成「这是不是中文原文」的判据。
KANA = re.compile(r"[぀-ゟ゠-ヿ]")
CJK = re.compile(r"[一-鿿]")

# ⭐ 日语里【根本不存在】的简体字。命中即判为「中文原文被抄了过来」。
#
# 为什么要有这一条（2026-09-08 加）：原来只用「有汉字却无假名」做判据，
# 于是「亚马逊 S3」的正确日译「Amazon S3」（零假名零汉字）被判成没翻译，
# 而「S3 标准」→「S3 標準」同样被拒。⚠️ 闸门在逼译者往正确译文里硬塞假名，
# ⛔ 那正是「为了过闸而改内容」——我自己在假名哨兵那条警告过的自毁形状。
#
# 简体字判据反过来【更锋利】：它不管长短，只问「这个字日语里有没有」。
# 实测本题库 5226 条原文，98.5% 含至少一个黑名单字；
# 与下面的「汉字≥6 需有假名」合起来，两条都漏掉的只剩 0.8%（都是极短串）。
# ⛔ 注意「双」「区」「数」「据」这类【日语也有】的字不得入表 —— 会误伤正确译文。
#
# ⛔⛔ 这份清单【已封闭，不要再加字】。理由是实测出来的，不是保守：
#     220 字里 205 个的边际贡献是 0（删掉它闸门一条也不多漏）；
#     而剩下 40 条逃逸文本里的汉字全是日语也在用的（使用 / 与 / 配置 / 器 / 全球）——
#     ⇒ 想再提高覆盖，只能靠加会误伤正确日译的字。清单已经到顶了。
#
# ⚠️ 2026-09-08 两次踩坑，教训是分开的两条：
#
# 【一】靠人眼挑的清单必然含杂质。第一版混进了「阻」——阻止/阻害是**常用漢字**，
#      闸门把正确的「アクセスを阻止する」判成了中文原文（「解析」会话撞出）。
#      而靠人眼再挑一遍，只会挑出被撞到的那一个。
#
# 【二】⭐ **机械可执行 ≠ 测对了东西。**
#      我改用 Shift-JIS 试编码筛，它机械、可复现、零人眼参与 —— 照样错了：
#      它回答的是「这个字**能不能**用日文字符集编码」，而闸门要问的是
#      「这个字**在不在**日文里用」。JIS X 0208 有 6355 字，含约 4200 个
#      常用漢字表以外的生僻码位，「个 从 价 凭 网」正落在那一区 ——
#      于是 5 个真简体字被误删。（同一个会话指出的，判据是常用漢字表。）
#
# ⇒ 正确判据是【常用漢字表（2136 字）】，不是 JIS X 0208。
#   ⚠️ 本机没有可靠的常用漢字数据源，而抓一份验证不了的清单更危险
#      （被摘要过、恰好漏掉「阻」的表，会让同样的错误静默通过）。
#   ⭐ 但清单已封闭 ⇒ 这条判据不在关键路径上了。真要加字：
#      ① 确认它不在常用漢字表  ② 确认它在 IPA 日语语料里零命中（check 会自动跑）
#      两条都过才加，且必须实测边际贡献 > 0。
SIMPLIFIED_ONLY = frozenset("亚们为义乐习书买产亲仓优传伤备复头实宁审层岁币帮师带库应废开张录惊户执扩报拟损时显术权业东严临举仅计订认讨让训议记讲论设访证评识诉词试详语误说请读课调谈谁谢贝财责账货质购贷费资车转轮软轻输达过运进连选递适远违边还这钟银错键镜锁铁问间闭关阅险隐项顺须领频题顾预页风飞马驱验组织经结给络统继线级纪约纳纸终维缓编缩电长门队阶动势发变处标样检构树桥极测济满灭现环监盘确码离种积稳竞笔简类罗职联胜脑节药获营蓝补见观规视览觉单储启击邻务鲁历纵览个从价凭网")

# 汉字多到这个数还一个假名都没有，才判为可疑。
# 低于它的多是「S3 標準」「Amazon S3」这类纯名词，正确日译本就可能零假名。
KANA_REQUIRED_FROM = 6
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
    # 值归一成列表：写成字符串是「只有一种正确写法」的简写
    terms = {k: ([v] if isinstance(v, str) else list(v))
             for k, v in g.get("terms", {}).items() if not k.startswith("_")}

    # not_terms：**这几个字连在一起不是一个词**。
    #
    # 中文没有词边界 ⇒ 假命中是结构性的，不是偶发。
    # 例：「监控应用程序并发出警报」里的「并发」是「并」+「发出」（= 并且发出），
    # 不是 concurrency ——正确译文里【不该也不可能】出现「同時実行」。
    # 全库「并发」20 处：真 17 · 假 3 ⇒ ⛔ 词条不能删（删了放走 17 处真漂移），
    # 也不能靠给「并发出」配译法解决（「并发送一份报告」与「并发送到 AWS」日文是不同动词，
    # 一个词条要靠不断追加写法才够 —— 正是规则 1 判死刑的形态）。
    #
    # ⭐ 与被驳回的「本题豁免」的区别，就是这条机制存在的理由：
    #   豁免说的是「**这道题**放我过去」；not_terms 说的是「**这两个字**不是词」。
    #   后者是一句关于语言的可复查断言，对全部 41 片一次生效。
    not_terms = [t for t in g.get("not_terms", []) if isinstance(t, str) and t]

    # ⛔ 防滥用：not_terms 不得用来把一个【真词条】关掉。
    # 没有这道锁的话，「负载均衡器」被搬进 not_terms 就等于静默删掉一条闸，
    # 而 diff 上看起来只是「加了一行」。
    # ⛔ 词条键不得含空白 —— 它会【静默失效】，这是最坏的一种失败：
    #    check 拿【折叠掉空白后】的原文匹配（见 check_item 里 _whitespace 那段），
    #    所以键里带空格的词条永远匹配不上，而且不报任何错，看着像在工作。
    # ⚠️ 「预置 IOPS」要写成「预置IOPS」。这条锁是 2026-09-08 「解析」会话
    #    加第一个含拉丁字母的键时发现的 —— 它自己绕过去了，但下一个人不会知道。
    # ⭐ keep 不受此限：它匹配的是【未折叠】的原文，服务名里的空格是必须保留的。
    if spaced := sorted(k for k in [*terms, *not_terms] if re.search(r"\s", k)):
        sys.exit(f"✗ {rel(p)}: 词条键含空白 {spaced} —— ⛔ 它永远匹配不上（匹配前原文已折叠空白），"
                 f"\n  且不会报错，看着像在工作。把键里的空格去掉即可（「预置 IOPS」→「预置IOPS」）。")
    if clash := sorted(set(not_terms) & set(terms)):
        sys.exit(f"✗ {rel(p)}: {clash} 同时出现在 terms 和 not_terms —— "
                 f"⛔ not_terms 是「这几个字不是词」，不是关闭词条的开关。"
                 f"\n  真要停用某个词条，就把它从 terms 里删掉，让 diff 看得见。")
    for t in not_terms:
        if not any(k in t and k != t for k in terms):
            print(f"⚠ not_terms 的「{t}」不包含任何词条，是个空操作 —— 多半写错了")
    return {"terms": terms, "keep": list(g.get("keep", [])), "not_terms": not_terms}


def load_untranslated(bank: str, locale: str) -> dict:
    """
    ⭐ 有意【不翻】的题：题号 → 理由。

    ⚠️ 这不是「豁免」，两者的差别是结构性的，别混：
        豁免      = 翻了，但跳过校验        ⇒ 译文进库，问题【看不见】
        untranslated = 【不翻】，并记下理由  ⇒ 界面显示原文并标注，损失【看得见】
    ⇒ 它没法被用来把一条可疑译文塞过闸门 —— 用它的代价就是那道题没有译文，
      而覆盖率统计会一直显示这个缺口。**自限的机制才是安全的机制。**

    为什么需要它：#627 被判定为「术语表表达不了」而不翻之后，那一片会【永远】校验失败。
    ⚠️ 一个永远亮着的红灯等于没有红灯 —— 与「误报比漏报更伤」同一条道理。
    ⇒ 有意的缺口必须能被表达成一个绿色状态，否则它会淹掉真正的失败。
    """
    p = REPO / "pipeline" / "banks" / bank / f"untranslated.{locale}.json"
    if not p.exists():
        return {}
    raw = json.loads(p.read_text(encoding="utf-8"))
    return {int(k): v for k, v in raw.items() if not k.startswith("_")}


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
    def looks_untranslated(text: str) -> str | None:
        bad = SIMPLIFIED_ONLY & set(text)
        if bad:
            return f"含简体字「{''.join(sorted(bad))[:6]}」—— 日语里没有这些字，中文原文被抄了过来"
        if len(CJK.findall(text)) >= KANA_REQUIRED_FROM and not KANA.search(text):
            return "汉字这么多却一个假名都没有 —— 多半是没翻译"
        return None

    checked = [("stem", stem), *((f"choices[{k}]", v) for k, v in sorted(choices.items()))]
    if isinstance(expl, str):
        checked.append(("explanation", expl))
    for name, text in checked:
        if isinstance(text, str) and text.strip():
            if why := looks_untranslated(text):
                errs.append(f"{name} {why}")

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
    # 术语表的两条语义：
    #
    # ① 值可以是列表 = 这个词有多个【都正确】的写法。「负载均衡器」在产品名里是
    #    Application Load Balancer，在普通名词位置是ロードバランサー ——
    #    强行二选一会把正确的译文判成错的。⛔ 但列表要短：说不出「为什么都对」
    #    就别多加一项，那是在把闸门自己拆掉。
    #
    # ② ⭐ 最长匹配优先：长词命中后【吃掉】它占的那段原文，短词不再对那段提要求。
    #
    # ⚠️ 中文没有词边界，子串匹配必然出这个问题（2026-09-08 由「解析」会话撞出）：
    # 「S3 单区域不频繁访问」里的「区域」是 **Zone 不是 Region**，
    # 官方日译是「S3 One Zone-IA」，正确译文里【不该】出现「リージョン」。
    # 而旧逻辑要求它出现 ⇒ 要过闸只能把 Zone 错译成 Region，
    # ⛔ 又是「为了过闸而写错内容」。本题库有 35 道题会撞上。
    # ⇒ 这是通用缺陷，不是「单区域」一个词的事：任何「X区域」「X主题」类复合词都会重犯。
    # not_terms 与 terms 一起按长度排 —— 假命中串（「并发出」3 字）必须排在
    # 被它包住的词条（「并发」2 字）之前，才能先把那段原文吃掉。
    ordered = sorted([*glo["terms"], *glo["not_terms"]], key=len, reverse=True)
    # ⚠️⚠️ 术语匹配用【折叠掉空白】的原文，keep 那一侧⛔ 绝不能折叠。
    #
    # 素材是 PDF 转出来的，换行会在字中间留下空格：
    #     「配置 Amazon 简单队列服 务（Amazon SQS）」   ← 服/务 之间
    # ⇒ 复合词「简单队列服务」匹配不上 ⇒ 最长匹配没吃掉这段 ⇒ 短词「队列」接管
    # ⇒ 要求译文含キュー，而正确日译就是 Amazon Simple Queue Service，不含也不该含。
    #
    # ⭐ 同一个空格，落在【普通词条】上是漏报（闸门不响，兜底还在），
    #   落在【复合词条】上是误报（长词没吃掉，短词接管）—— 复合词的全部作用就是压住短词。
    #   ⚠️ 但它失效的方向是让闸门【变严】不是变松，所以一定会撞在译者身上，不会静默通过。
    # 实测：折叠后多命中 104 处，其中复合词 3 处是有害的那种。
    #
    # ⛔ keep 用的必须是【未折叠】的原文：52 个含空格的服务名里，
    #   折叠后有 28 个会匹配不上（Data Lifecycle Manager → DataLifecycleManager）。
    #   ⇒ 折叠若做在 joined_src 上，等于悄悄关掉一半服务名的保护。
    # ⛔ 译文侧也不折叠：约定译词本身含空格（Simple Queue Service / Load Balancer），
    #   而译文是人写的，不会有 PDF 换行伪影。
    unconsumed = re.sub(r"\s+", "", joined_src)
    for src_term in ordered:
        if src_term in unconsumed:
            accepted = glo["terms"].get(src_term)
            if accepted is None:
                # not_terms：吃掉这段，⛔ 不对译文提任何要求
                unconsumed = unconsumed.replace(src_term, "\x00" * len(src_term))
                continue
            if not any(d in joined_dst for d in accepted):
                errs.append(f"原文有「{src_term}」，译文里找不到约定译词"
                            + "（" + " / ".join(f"「{d}」" for d in accepted) + "）")
            # 吃掉：更短的子串不再对这一段提要求
            unconsumed = unconsumed.replace(src_term, "\x00" * len(src_term))
    # keep：服务名等必须原样保留，⛔ 不许意译也不许改写成假名。
    #
    # ⚠️ 拉丁字母的服务名必须按【词边界】匹配，⛔ 不能裸子串 ——
    # 解析正文里全是 IAM 策略名与 API 名（`AmazonS3ReadOnlyAccess` / `GetCostForecast`
    # / `Launch Configuration`），裸子串会把 S3 / Forecast / Config 全都「找到」。
    # 后果不是多一次检查，是【要求译文含一个本题根本没提的服务名】：
    # 实测加上解析后有 **48/1019** 道题会因此被误拦（只看题面时仅 5 道，
    # 所以题面这一趟 500 题一次都没撞上 —— 它是【只在第二趟才爆】的那类）。
    # ⭐ 源文与译文两侧用同一条规则，否则会一边宽一边严。
    for term in glo["keep"]:
        if _standalone(term, joined_src) and not _standalone(term, joined_dst):
            errs.append(f"「{term}」是必须原样保留的名字，译文里没有")
    return errs


def _standalone(term: str, text: str) -> bool:
    """term 是否作为【独立记号】出现，而不是嵌在更长的单词里。

    非拉丁词（中文/假名）没有词边界概念，退回裸子串。
    """
    if not re.fullmatch(r"[A-Za-z0-9 ._-]+", term):
        return term in text
    return re.search(rf"(?<![A-Za-z0-9]){re.escape(term)}(?![A-Za-z0-9])", text) is not None


def check_shard(path: Path, qmap: dict, glo: dict, skip: dict | None = None) -> list[str]:
    errs = []
    try:
        doc = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as e:
        return [f"JSON 解析失败: {e}"]
    items = doc.get("items")
    if not isinstance(items, list):
        return ["items 缺失或不是数组"]
    rng = doc.get("range")
    skip = skip or {}
    want = sorted(n for n in qmap if rng and rng[0] <= n <= rng[1] and n not in skip)
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
    glo["_skip"] = load_untranslated(bank, locale)
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
        broken = bool(check_shard(p, qmap, glo, glo["_skip"]))
        (bad if broken else done).append(r)
        for it in json.loads(p.read_text(encoding="utf-8")).get("items", []):
            # ⚠️ 未填满的片也计进来。片没填满 = 校验不过 = 不算「完成」，
            # 这是照抄 enrich.py 的语义（片存在即完成，⛔ 不维护进度清单）。
            # 但只报「完成片数」会让跨天做到一半的人以为工作丢了 ——
            # 已经翻好的 5 题明明在盘上，进度却显示 0。⇒ 两个数都报。
            n_part = 1
            n_stem += n_part
            if it.get("explanation"):
                n_expl += 1
    skip = glo["_skip"]
    total = len(qmap) - len(skip)
    n_has_expl = sum(1 for q in qmap.values() if explanation_of(q))
    print(f"题库      {bank}   目标语言 {locale}")
    print(f"术语表    {len(glo['terms'])} 条约定译词 · {len(glo['keep'])} 个保留原样的名字")
    print(f"分片      {len(ranges)} 片 × {SHARD_SIZE} 题")
    print(f"题面进度  {n_stem}/{total} 题  ({n_stem/total*100:.1f}%)   ← 不翻就没法做题")
    if bad:
        print(f"          （其中 {len(bad)} 片未填满，算在题面进度里但不算完成片）")
    print(f"解析进度  {n_expl}/{n_has_expl} 题  ({n_expl/max(n_has_expl,1)*100:.1f}%)   ← 缺了回退中文并标注")
    if skip:
        print(f"有意不翻  {len(skip)} 题 —— {sorted(skip)}（理由见 untranslated.{locale}.json，"
              f"界面会显示原文并标注）")
    print(f"校验失败  {len(bad)} 片" + (f"  → {[shard_name(r) for r in bad]}" if bad else ""))
    print(f"待处理    {len(todo)} 片")
    # ⚠️ 「下一片」必须与 cmd_next 取的是【同一片】。
    # 曾经 status 只看「不存在的片」而 next 还会把「存在但没填满的片」算进去 ——
    # status 说下一片是 0026，next 给的却是 0001。跨天做的人照着 status 走就做错片，
    # 而两边都不报错。⇒ 顺序在这里定死：先补没填满/坏掉的，再开新片。
    up_next = (bad + todo)
    if up_next:
        first = min(up_next)
        print(f"\n下一片    {shard_name(first)}   （跑 translate.py next {bank} --locale {locale}）")


def cmd_next(bank: str, locale: str, count: int, with_explanation: bool) -> None:
    doc, qmap, glo, d = _scan(bank, locale)
    todo = [r for r in shard_ranges(list(qmap))
            if not (d / shard_name(r)).exists() or check_shard(d / shard_name(r), qmap, glo, glo["_skip"])]
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

    # ⭐ 已在盘上的条目：本次写入是【增量】，缺的字段沿用旧值。
    #
    # ⚠️ 这条是补解析那一趟能不能跑的前提。旧版只在省略 explanation 时保留旧值，
    # 反过来不成立 —— 省略 stem/choices 就当没有，而 check_item 又要求它们必须有。
    # ⇒ 第二趟为了加一段解析，得把题干和全部选项原样重打一遍；
    #   而**每一次重打都是一次改坏已验收译文的机会**。
    # 现在：先把增量合到旧条目上，再校验【合并后】的整体。
    existing_items: dict[int, dict] = {}
    ranges_all = shard_ranges(list(qmap))
    for rng in ranges_all:
        path = d / shard_name(rng)
        if path.exists():
            try:
                for i in json.loads(path.read_text(encoding="utf-8")).get("items", []):
                    existing_items[i["no"]] = i
            except json.JSONDecodeError:
                pass

    errs = []
    merged_items = []
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
        base = {k: v for k, v in existing_items.get(no, {}).items() if k != "no"}
        incoming = {k: v for k, v in it.items() if k not in ("fp", "no")}
        full = {"no": no, **base, **incoming}
        merged_items.append(full)
        errs += [f"#{no}: {e}" for e in check_item(full, qmap[no], glo)]
    if errs:
        print(f"✗ 校验未通过（{len(errs)} 个问题），未写入任何文件：")
        for e in errs[:25]:
            print(f"    {e}")
        if len(errs) > 25:
            print(f"    …… 另有 {len(errs)-25} 个")
        sys.exit(1)

    ranges = ranges_all
    buckets: dict[tuple[int, int], list] = {}
    for it in merged_items:
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
        # batch 里已经是【合并后】的完整条目（见上面 merged_items），直接覆盖
        merged.update({i["no"]: i for i in batch})
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


def audit_blacklist(locale: str) -> list[str]:
    """
    拿【外部】日语语料反查简体字黑名单 —— 混进日语汉字就在这里暴露。

    ⚠️ 为什么必须用外部语料：本管道产出的译文全都过过闸，按定义不可能含黑名单字 ——
    拿它们反查是循环论证，永远查不出问题。
    这里用的是 IPA 官方真题原文（人写的考卷，⛔ 非任何会话产出）。

    ⚠️ 它是【必要不充分】的：8 万字里查不到不等于不是日语汉字 ——
    「阻」就是这样漏过去的（它是常用漢字，但那份语料里没出现）。
    ⇒ 这条只兜住最明显的一类，⛔ 别把它当成清单的准入判据。
    """
    corpus = ""
    for name in ("transcribed", ""):
        base = bank_dir("ipa-ip") / name if name else bank_dir("ipa-ip")
        if not base.is_dir():
            continue
        for f in sorted(base.glob("*.json")):
            corpus += f.read_text(encoding="utf-8")
    if not corpus:
        return []   # 没有外部语料就不做这项，⛔ 不假装查过
    hits = sorted(c for c in SIMPLIFIED_ONLY if c in corpus)
    return [f"黑名单里的「{c}」出现在 IPA 官方日语真题里 —— 它是日语汉字，混进来了" for c in hits]


def cmd_check(bank: str, locale: str) -> None:
    doc, qmap, glo, d = _scan(bank, locale)
    for e in audit_blacklist(locale):
        print(f"🔴 {e}")
    files = sorted(d.glob("*.json")) if d.exists() else []
    if not files:
        sys.exit(f"✗ 还没有任何分片（{rel(d)}）")
    total = 0
    for p in files:
        errs = check_shard(p, qmap, glo, glo["_skip"])
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


def cmd_merge(bank: str, locale: str, partial: bool = False) -> None:
    """
    合并分片。

    默认要求【全部片都通过校验】—— 半成品进不了产物。

    `--partial` 是为了一件具体的事：41 片跨多天，中途想把已经翻好的先导进 dev
    看看真机效果。它**逐条**用 check_item 筛，⛔ 不是「跳过校验」——
    有实质错误的条目照样进不来，放行的只是「这一片还没填满」这一种状态。
    ⚠️ 产物里记 complete=false，导入侧据此知道自己拿到的不是全量。
    """
    doc, qmap, glo, d = _scan(bank, locale)
    items, problems, dropped = {}, 0, 0
    for p in sorted(d.glob("*.json")):
        errs = check_shard(p, qmap, glo, glo["_skip"])
        if errs and not partial:
            problems += 1
            print(f"✗ {p.name} 校验未过，已跳过（{len(errs)} 个问题）")
            continue
        for it in json.loads(p.read_text(encoding="utf-8")).get("items", []):
            if partial:
                # 逐条筛：⛔ 只放行「片没填满」，不放行有实质错误的条目
                if it.get("no") not in qmap or check_item(it, qmap[it["no"]], glo):
                    dropped += 1
                    continue
            items[it["no"]] = it
    if problems:
        sys.exit(f"\n✗ 有 {problems} 片未通过校验，先修好再 merge"
                 f"\n  （只是想把已翻好的先导进 dev 看效果 ⇒ 加 --partial）")
    if dropped:
        print(f"⚠ --partial：{dropped} 条有实质错误，已排除")

    out = bank_dir(bank) / f"translated.{locale}.json"
    payload = {
        "bank": bank,
        "locale": locale,
        "source_locale": doc["bank"].get("locale"),
        # ⭐ 导入侧靠它知道自己拿到的是不是全量 —— ⛔ 别让「部分译文」看起来像「全部译完」
        "complete": len(items) == len(qmap),
        "merged_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "items": [items[k] for k in sorted(items)],
    }
    json.dump(payload, out.open("w", encoding="utf-8"), ensure_ascii=False, indent=1)
    n_expl = sum(1 for i in payload["items"] if i.get("explanation"))
    flag = "" if payload["complete"] else "   ⚠ 部分（complete=false）"
    print(f"✓ {rel(out)}   题面 {len(items)}/{len(qmap)} · 解析 {n_expl}/{len(qmap)}{flag}")


def main() -> None:
    ap = argparse.ArgumentParser(description="Melete 翻译编排器（enriched.json → 译文分片）")
    # ⚠️ --locale 挂在【每个子命令】上而不是顶层：顶层选项必须写在子命令【之前】，
    # 而人手打出来的顺序总是 `translate.py status <bank> --locale ja`。
    # 让它在自然位置能用，比在文档里解释一遍便宜。
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--locale", default="ja", help="目标语言（默认 ja）")
    common.add_argument("bank")
    sub = ap.add_subparsers(dest="cmd", required=True)
    for name in ("status", "check"):
        sub.add_parser(name, parents=[common])
    p = sub.add_parser("merge", parents=[common])
    p.add_argument("--partial", action="store_true",
                   help="逐条筛，放行「片没填满」但不放行有实质错误的条目（产物记 complete=false）")
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
        cmd_merge(a.bank, a.locale, a.partial)


if __name__ == "__main__":
    main()
