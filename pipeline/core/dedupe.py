"""
重复题检测 —— 跨题的结构性检查，单题解析时看不见。

两级：
  duplicate_of_N   题干 + 全部选项逐字相同（指纹取修复后文本）。后出现的题挂告警，先出现的是正本。
  same_stem_as_N   仅题干相同、选项不同。可能是同一场景的两道题，也可能是抓取缺陷，留给富化侧判断。

只登记不合并：合并是数据模型层的决策（导入时做），解析器只负责把事实摆出来。
SAP-C02 实测 2 组（#84≡#85；#10≡#438 且两题的题库标注互相矛盾 —— 重复题正是暴露题库自身不一致的地方）。
"""
import hashlib


def _fp(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:8]


def flag_duplicates(questions: list[dict]) -> tuple[int, int]:
    """就地给后出现的重复题追加告警；返回 (完全重复数, 仅题干重复数)。"""
    seen_full: dict[str, int] = {}
    seen_stem: dict[str, int] = {}
    full = stem_only = 0
    for q in sorted(questions, key=lambda x: x["no"]):
        key_full = _fp(q["stem"] + "|" + "|".join(c["label"] + c["body"] for c in q["choices"]))
        key_stem = _fp(q["stem"])
        if key_full in seen_full:
            q["warnings"].append(f"duplicate_of_{seen_full[key_full]}")
            full += 1
        elif key_stem in seen_stem:
            q["warnings"].append(f"same_stem_as_{seen_stem[key_stem]}")
            stem_only += 1
        seen_full.setdefault(key_full, q["no"])
        seen_stem.setdefault(key_stem, q["no"])
    return full, stem_only
