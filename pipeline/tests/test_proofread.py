"""逐题校对阶段的校验规则。运行：python3 -m unittest discover -s pipeline/tests"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "core"))

from proofread import check_item, imported  # noqa: E402

FACE = {"stem": "执行 AWSEC2-PatchLoadBalanacerInstance 文档", "choice:A": "配置状态管理器", "choice:B": "流量 流量"}


def fix(field="stem", find="Balanacer", replace="Balancer", kind="typo", why="运行手册名拼错"):
    return {"field": field, "find": find, "replace": replace, "kind": kind, "why": why}


def item(fixes=(), unfixable=()):
    return {"no": 1, "fixes": list(fixes), "unfixable": list(unfixable)}


class CheckItemTest(unittest.TestCase):
    def test_clean_question_and_valid_fix(self):
        self.assertEqual(check_item(item(), FACE, strict=True), [])
        ok = item([fix(), fix(field="choice:A", find="状态管理器", replace="State Manager", kind="term")],
                  [{"field": "question", "issue": "选项文字可能被截断"}])
        self.assertEqual(check_item(ok, FACE, strict=True), [])

    def test_find_must_occur_exactly_once_when_strict(self):
        for f in (fix(find="不存在的片段"), fix(field="choice:B", find="流量", replace="流量。")):
            with self.subTest(find=f["find"]):
                self.assertTrue(check_item(item([f]), FACE, strict=True))
                self.assertEqual(check_item(item([f]), FACE, strict=False), [])  # 已落盘的分片只查结构

    def test_overlapping_fixes_rejected(self):
        a = fix(find="PatchLoadBalanacer")
        b = fix(find="Balanacer Instance".replace(" ", ""))
        self.assertTrue(any("重叠" in e for e in check_item(item([a, b]), FACE, strict=True)))

    def test_structure_errors(self):
        bad = [
            {"no": 1, "fixes": []},                                   # 缺 unfixable
            item([fix(field="choice:Z")]),                             # 没有这个字段
            item([fix(kind="style")]),                                 # kind 不在枚举里
            item([fix(replace="Balanacer")]),                          # find == replace
            item([fix(why="短")]),                                     # why 太短
            item([fix(replace="Bal\nancer")]),                         # 换行
            item(unfixable=[{"field": "choice:Z", "issue": "看不懂这一段"}]),
            item([fix()] * 13),                                        # 超过上限
        ]
        for it in bad:
            with self.subTest(it=str(it)[:60]):
                self.assertTrue(check_item(it, FACE, strict=False))

    def test_deletion_allowed(self):
        self.assertEqual(check_item(item([fix(field="choice:A", find="配置", replace="", kind="garble")]),
                                    FACE, strict=True), [])


class ImportedTest(unittest.TestCase):
    def test_only_questions_that_reach_the_database(self):
        self.assertTrue(imported({"choices": [{"label": "A"}], "warnings": []}))
        self.assertFalse(imported({"choices": [], "warnings": []}))
        self.assertFalse(imported({"choices": [{"label": "A"}], "warnings": ["duplicate_of_12"]}))


if __name__ == "__main__":
    unittest.main()
