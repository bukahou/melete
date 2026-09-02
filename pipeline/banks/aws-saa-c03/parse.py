#!/usr/bin/env python3
"""
Melete 数据导入管道 — 第一段：PDF 文本 → 结构化 JSON

输入：pdftotext -layout 产出的纯文本
输出：questions.json（稳定中间格式，可 review / 可进 git / 可 diff）

设计要点：
  · 答案不是单一字段，而是一组带来源的「主张」(claims)
  · 解析异常不静默丢弃，全部记入 warnings 并在报告中统计
"""
import json
import re
import sys
from datetime import datetime, timezone

# 页眉/页脚噪声：出现在题号行尾或独立成行
NOISE = re.compile(r"(主题\s*\d+|Topic\s*\d+|考试\s*[A-Z]|\x0c)")
# 题头两种形态：中文「问题 #N」为主，8 道题（108/129/253/326/429/718/939/960）
# 是英文「Question #N」但正文仍为中文 —— 曾因只认中文题头漏抓这 8 道，
# 且它们的整题内容被吞进前一题（#252 的投票分布出现 200% 即此根因）。
Q_SPLIT = re.compile(r"^\s*\x0c?\s*(?:问题|Question)\s*#(\d+)\b", re.M)
OPT_START = re.compile(r"^\s{0,6}([A-F])\.\s*(.*)$")
ANS_LINE = re.compile(r"正确答案\s*[：:]\s*([^\n]*)$", re.M)
# 西里尔字母混入拉丁选项字母（题库脏点）
CYRILLIC = str.maketrans({"А":"A","В":"B","С":"C","Е":"E","Ѕ":"S","Р":"P","Д":"D","Ғ":"F"})
VOTE_PAIR = re.compile(r"([A-F]+)\s*[（(]\s*(\d+)\s*%\s*[）)]")
PICK_N = re.compile(r"[（(]\s*选择(两|三|四|2|3|4|２|３)\s*[个项]?[。．.]?\s*[）)]")
CN_NUM = {"两": 2, "三": 3, "四": 4, "2": 2, "3": 3, "4": 4, "２": 2, "３": 3}


# 「ﬁ」连字（U+FB01）在文本层丢失，「file 系统/服务器」只剩「le 系统/服务器」。
# 全库实证 4 处（#260/#283/#332/#800），受害词仅 file —— 按已验证模式修复并登记。
# 专有名词被当普通词组直译 —— 撞词误译之外的第四类译文缺陷。
# 首例由富化侧发现：#886 的「Babel shell」实为 Babelfish for Aurora PostgreSQL。
# 全库扫描又找到 Redshift→「红移」、Snowball→「雪球」等。
# 危害：产品名失真使题目无法与真实 AWS 服务对应，学习者据此形成错误记忆。
PRODUCT_MISTRANSLATION = re.compile(
    r"Babel\s*(shell|鱼)"      # Babelfish
    r"|红移"                    # Redshift
    r"|雪球|雪锥"               # Snowball / Snowcone
    r"|开放搜索"                # OpenSearch
    r"|湖泊?形成"               # Lake Formation
)

# Lambda 并发术语（provisioned 与 reserved 的中译在本题库里不统一，见下方检测处注释）
CONCURRENCY_TERM = re.compile(r"(预留|预置|预配置|预配)\s*并发")

# 指代性引用短语：题干说「以下 JSON / 以下策略」，正文却是图片没抓到
REF_PHRASE = re.compile(
    r"以下\s*(JSON|IAM\s*策略|策略|政策)|如下(所示)?的?\s*(JSON|策略)|following\s+(JSON|policy)")

FILE_LIGATURE = re.compile(r"(?<![A-Za-z])le(?=\s*(?:系统|服务器))")


def clean(line: str) -> str:
    return NOISE.sub("", line).rstrip()


def parse_block(no: int, body: str) -> dict:
    warnings = []
    if FILE_LIGATURE.search(body):
        body = FILE_LIGATURE.sub("file", body)
        warnings.append("ligature_restored_file")
    lines = [clean(l) for l in body.split("\n")]

    # ---- 切三段：题干 / 选项 / 答案 ----
    # 选项区起点：首选 A 开头；若 A 缺失（#868/#477 实证：源 PDF 里 A 整个没印出来），
    # 退而认第一个出现的选项字母 —— 否则 B/C/D 会被整段误当成题干，
    # 一个素材缺陷（缺 A）被放大成整题不可用。
    opt_at = ans_at = None
    first_any = None
    for i, l in enumerate(lines):
        m = OPT_START.match(l)
        if m and first_any is None:
            first_any = i
        if opt_at is None and m and m.group(1) == "A":
            opt_at = i
        if ans_at is None and "正确答案" in l:
            ans_at = i
    if opt_at is None and first_any is not None:
        warnings.append("choice_a_missing")
        opt_at = first_any
    if opt_at is None:
        warnings.append("no_options")
        opt_at = len(lines)
    if ans_at is None:
        warnings.append("no_answer_line")
        ans_at = len(lines)
    if ans_at < opt_at:
        warnings.append("answer_before_options")

    stem = " ".join(x.strip() for x in lines[:opt_at] if x.strip())
    # stem 内连续大段空白 = 截断/噪声混入的表征（#321 实证：「方案应该是什么　　　
    # 架构师应采取什么措施…」，残句 + 空隙 + 真问句）。全库仅命中该题，零误报。
    # 曾评估「双问句」判据 —— 125 道正常题命中（SAA 题干常含多问句），弃用。
    if re.search(r"[ \u3000]{3,}", stem):
        warnings.append("stem_contains_gap_noise")
    # 题干以冒号收尾 = 它引用的策略/代码块是图片，文本层里不存在（#96 实证：
    # 「创建了以下策略：」后直接是选项 A）。这类题无法独立裁决，必须显式标记。
    if stem.rstrip().endswith(("：", ":")):
        warnings.append("stem_ends_with_colon_missing_block")

    # 更一般的形态：题干出现**指代性引用**（「以下 JSON / 以下 IAM 策略」）
    # 但被指代的正文不在产物里。冒号启发式只抓得住引用恰好收尾的情况（#96），
    # 引用在中部时抓不到（#423/#429/#494 都是「以下策略…（换行）…问句」）。
    # 判据：命中引用短语 且 stem 内无 JSON 特征字符 → 内嵌块丢失。
    if REF_PHRASE.search(stem) and "{" not in stem and '"Effect"' not in stem:
        warnings.append("stem_references_missing_block")

    # SAA 题干必以设问句收尾。末尾 30 字内无问号 = 设问句被截断
    # （#460/#467/#473 实证）。全库仅 6 道、零散分布，非批量排版问题。
    if "？" not in stem[-30:] and "?" not in stem[-30:]:
        warnings.append("stem_missing_question")

    # ---- 选项：字母行开启，缩进续行合并 ----
    # 选项字母同样做西里尔归一：若字母是西里尔的，OPT_START 匹配不到，
    # 该行会被静默当作上一个选项的续行 —— 丢一个选项且不产生任何告警。
    # 本题库实测影响面为 0，这里是给换题库时的防御。
    choices, cur = [], None
    for l in lines[opt_at:ans_at]:
        m = OPT_START.match(l[:2].translate(CYRILLIC) + l[2:])
        if m and (not choices or m.group(1) > choices[-1]["label"]):
            if cur:
                choices.append(cur)
            cur = {"label": m.group(1), "body": m.group(2).strip()}
        elif cur is not None and l.strip():
            cur["body"] += " " + l.strip()
    if cur:
        choices.append(cur)

    # 选项标签必须是从 A 起的连续序列。断档 = 某个选项被上一个吞并
    # （#423：A 显示为「角色 B组」，B 整个消失；#756 缺 C；#125 第五个标成 D）。
    # 这是零误报信号 —— 全库仅命中这 3 道，且都已被其它告警旁证。
    # 与「超长合并选项」判据的区别：那个看长度（误报 33 处），这个看结构（误报 0）。
    labels = [c["label"] for c in choices]
    if labels and labels != [chr(ord("A") + i) for i in range(len(labels))]:
        missing = [chr(ord("A") + i) for i in range(len(labels)) if chr(ord("A") + i) not in labels]
        warnings.append("choice_labels_not_contiguous"
                        + (f"_missing_{''.join(missing)}" if missing else ""))

    # Lambda 并发的译法在中译版里不统一，且与英文原词不是一一对应：
    #   provisioned concurrency（预置并发，消除冷启动）→ 译作「预置并发 / 预配置并发 /
    #     预配置并发性 / 预置并发数」四种
    #   reserved concurrency（预留并发，只划配额，对冷启动无效）→ 译作「预留并发量」
    # 危害不在单点误译，而在**跨题矛盾**：#516 用两者的区别定答案，
    # #597 却把「预留并发量」用在消除冷启动的语境（实为 provisioned），
    # 只读中译版的学习者会得到互相矛盾的判据。故凡出现即登记，交富化侧逐题分辨。
    for ch in choices:
        if CONCURRENCY_TERM.search(ch["body"]):
            warnings.append(f"lambda_concurrency_term_ambiguous_{ch['label']}")

    # 专有名词直译（第四类译文缺陷）：产品名被拆词译成普通词组
    for ch in choices:
        if PRODUCT_MISTRANSLATION.search(ch["body"]):
            warnings.append(f"product_name_mistranslated_{ch['label']}")
    if PRODUCT_MISTRANSLATION.search(stem):
        warnings.append("product_name_mistranslated_stem")

    # 「按需 + Aurora」共现 = Serverless 疑似被误译成「按需」（与 On-Demand 撞词）。
    # 依据：全库 16 处 Aurora Serverless 都正常译作「Serverless/无服务器」，
    # 仅 #511 反常，且源 PDF 原文即如此（中译版自身缺陷，非解析问题）。
    # #93 的「按需使用数据库克隆」是副词用法，需人工分辨，故只提示不断言。
    for ch in choices:
        if "Aurora" in ch["body"] and "按需" in ch["body"]:
            warnings.append(f"suspect_serverless_mistranslation_{ch['label']}")

    # 同题内选项正文归一化后完全相同 = 译文缺陷的结构性表现
    # （实证：Spot Instances 被机翻成「按需实例」与真 On-Demand 撞词，
    #   #84 A≡C、#128 A≡C 且 B≡D）。语义层的裁决交给富化侧，这里只登记。
    normed = {}
    for ch in choices:
        key = re.sub(r"[\s，。、；：（）()「」*·.]", "", ch["body"])
        if key in normed:
            warnings.append(f"duplicate_choice_bodies_{normed[key]}{ch['label']}")
        else:
            normed[key] = ch["label"]

    # 选项边界错切检测（#182 实证：A 吸收了 B 的整句，B 只剩 9 字残句「创建一个亚马逊账户」）。
    # 判据三条同时成立才报，每条都有实测的误报排除对象：
    #   ① 方案描述题（同题存在含句号的选项）—— 排除名词型选项题（#620 整题是存储类型名，天然短）
    #   ② 长度 < 中位数的 25% 且中位数 ≥ 30      —— 排除普通的长短差异
    #   ③ 该选项不以句号结尾（残句特征）        —— 排除完整的短选项（#360「使用接口端点。」#623「配置 AWS WAF。」）
    # 曾评估「过长≥2句号>1.8×中位数」的吞并判据 —— 全库误报 33 处且在 #182 上反而不命中，弃用：
    # 错切的可靠信号是「残句」，不是「长句」。
    if len(choices) >= 3:
        lens = sorted(len(c["body"]) for c in choices)
        median = lens[len(lens) // 2]
        if median >= 30 and any("。" in c["body"] for c in choices):
            for c in choices:
                if len(c["body"]) < median * 0.25 and not c["body"].rstrip().endswith("。"):
                    warnings.append(f"choice_body_suspiciously_short_{c['label']}")

    # ---- 答案主张 ----
    claims = []
    tail = "\n".join(lines[ans_at:])
    m = ANS_LINE.search(tail)
    if m:
        # 只取首个连续块：遇到 2 个以上空格即认为后面是社区徽章（冗余，丢弃）
        head = re.split(r"\s{2,}", m.group(1).translate(CYRILLIC).strip())[0]
        label = "".join(sorted(set(re.findall(r"[A-F]", head))))
        if label:
            claims.append({"source": "bank_label", "answer": label})
        else:
            warnings.append("empty_bank_label")

    if "社区投票分配" in tail:
        seg = tail.split("社区投票分配", 1)[1].translate(CYRILLIC)
        pairs = VOTE_PAIR.findall(seg[:400])
        if pairs:
            dist = {}
            for letters, pct in pairs:
                dist["".join(sorted(set(letters)))] = int(pct)
            top = max(dist.items(), key=lambda kv: kv[1])
            # 吞题哨兵：百分比之和远超 100 说明吞了下一题的投票行
            # （#252 曾因英文题头漏切吞掉 #253 而出现 A:100 + C:100 = 200%）。
            # 阈值留舍入余量：各项独立四舍五入可使合计到 101-102%
            # （#483 的 53+33+15=101 是正常题，曾被 >100 的阈值误报）。
            # 合计 <100 是 源站 长尾截断，同样不告警。
            if sum(dist.values()) > 105:
                warnings.append("vote_distribution_sum_gt_100")
            claims.append({
                "source": "community_vote",
                "answer": top[0],
                "confidence": top[1],
                "distribution": dist,
            })
        else:
            warnings.append("vote_header_without_pairs")

    # ---- 单选/多选 ----
    pm = PICK_N.search(stem)
    pick = CN_NUM.get(pm.group(1), 2) if pm else 1
    kind = "multi" if pick > 1 else "single"

    # ---- 一致性校验 ----
    labels = [c["label"] for c in choices]
    if labels != sorted(labels) or len(set(labels)) != len(labels):
        warnings.append("choice_labels_disordered")
    if len(choices) < 4:
        warnings.append(f"only_{len(choices)}_choices")
    for c in claims:
        if c["source"] in ("bank_label", "community_vote") and len(c["answer"]) != pick:
            warnings.append(f"{c['source']}_len{len(c['answer'])}_expect{pick}")
        for ch in c["answer"]:
            if ch not in labels:
                warnings.append(f"{c['source']}_answer_{ch}_not_in_choices")
    if not stem:
        warnings.append("empty_stem")

    return {
        "no": no,
        "kind": kind,
        "pick": pick,
        "stem": stem,
        "choices": choices,
        "claims": claims,
        "warnings": warnings,
    }


# 题库自身的重复题：同一道题以两个题号出现，其中一份有素材缺陷。
# 用完好的那份补全残缺的那份 —— 这不是"编造数据"，是拿题库自己的另一份副本修它。
#
# #868 缺选项 A（源 PDF 里就没印出来），而 #889 是同一道题的完整副本：
# 题干相似度 0.9927，B/C/D 三选项逐字相同，标注与社区投票均为 B。
# 由富化侧在做题时发现（解析器看不出「两道不同题号是同一道题」）。
DUPLICATE_REPAIRS = [
    {"broken": 868, "source": 889, "labels": ["A"],
     "note": "#868 在源 PDF 中缺选项 A，从同题的 #889 补入"},
]


def repair_from_duplicates(questions: list) -> None:
    """用重复题的完好副本补全残缺题的选项。就地修改，并记入 warnings 保持可追溯。"""
    by_no = {q["no"]: q for q in questions}
    for r in DUPLICATE_REPAIRS:
        broken, src = by_no.get(r["broken"]), by_no.get(r["source"])
        if not broken or not src:
            continue
        have = {c["label"] for c in broken["choices"]}
        added = []
        for lab in r["labels"]:
            if lab in have:
                continue  # 已经有了（比如上游修好了），不覆盖
            donor = next((c for c in src["choices"] if c["label"] == lab), None)
            if donor:
                broken["choices"].append(dict(donor))
                added.append(lab)
        if added:
            broken["choices"].sort(key=lambda c: c["label"])
            broken["warnings"].append(f"choice_repaired_from_{r['source']}_{''.join(added)}")


def main(src: str, dst: str) -> None:
    raw = open(src, encoding="utf-8").read()
    parts = Q_SPLIT.split(raw)
    questions = []
    for i in range(1, len(parts) - 1, 2):
        questions.append(parse_block(int(parts[i]), parts[i + 1]))

    repair_from_duplicates(questions)

    doc = {
        "bank": {
            "slug": "aws-saa-c03",
            "name": "AWS Certified Solutions Architect – Associate (SAA-C03)",
            "locale": "zh",
            "kind": "cert",
            "source": "SAA-C03 中文 题目+答案 新.pdf",
            "extracted_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        },
        "questions": questions,
    }
    json.dump(doc, open(dst, "w", encoding="utf-8"), ensure_ascii=False, indent=1)

    # ---- 质量报告 ----
    from collections import Counter
    warn = Counter(w for q in questions for w in q["warnings"])
    clean_n = sum(1 for q in questions if not q["warnings"])
    nos = sorted(q["no"] for q in questions)
    gaps = sorted(set(range(nos[0], nos[-1] + 1)) - set(nos))
    print(f"题目总数        {len(questions)}  (题号 {nos[0]}–{nos[-1]})")
    if gaps:
        print(f"题号缺失        {gaps}  ← 需与源文档核对是否题库自身缺号")
    print(f"零告警          {clean_n}  ({clean_n/len(questions)*100:.1f}%)")
    print(f"有告警          {len(questions)-clean_n}")
    print(f"多选题          {sum(1 for q in questions if q['kind']=='multi')}")
    src_cnt = Counter(c["source"] for q in questions for c in q["claims"])
    print("\n答案主张来源分布:")
    for k, v in src_cnt.most_common():
        print(f"  {k:<18} {v}")
    print("\n告警明细:")
    for k, v in warn.most_common(15):
        print(f"  {k:<40} {v}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
