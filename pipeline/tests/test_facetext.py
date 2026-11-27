"""题面整理：断行空格、勘误、三步的先后。运行：python3 -m unittest discover -s pipeline/tests"""
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "core"))

from facetext import ErrataError, FacePreparer, apply_errata, parse_errata  # noqa: E402
from textrepair import close_cjk_gaps  # noqa: E402


def question(no=1, stem="题干", **bodies):
    bodies = bodies or {"A": "选项A", "B": "选项B"}
    return {"no": no, "stem": stem, "choices": [{"label": k, "body": v} for k, v in bodies.items()]}


def erratum(no=1, field="stem", find="题干", replace="题面", kind="typo", basis="测试"):
    return {"no": no, "field": field, "find": find, "replace": replace, "kind": kind, "basis": basis}


class CloseCjkGapsTest(unittest.TestCase):
    def test_removes_gaps_between_han_and_fullwidth_punct(self):
        self.assertEqual(close_cjk_gaps("重新路由流 量。 哪种方案 （选择两项。）"),
                         ("重新路由流量。哪种方案（选择两项。）", 3))
        self.assertEqual(close_cjk_gaps("文 件 系 统"), ("文件系统", 3))

    def test_keeps_spaces_next_to_ascii(self):
        text = "使用 AWS Systems Manager 对 Amazon EC2 实例"
        self.assertEqual(close_cjk_gaps(text), (text, 0))

    def test_keeps_spaces_after_kana(self):
        # 日文里假名记号后的空格有意义（「ア ハードウェア」）
        text = "ア ハードウェア と 企業"
        self.assertEqual(close_cjk_gaps(text), (text, 0))


class ParseErrataTest(unittest.TestCase):
    def test_accepts_well_formed(self):
        self.assertEqual(len(parse_errata({"items": [erratum()]})), 1)

    def test_rejects_bad_entries(self):
        bad = [
            {**erratum(), "extra": 1},
            erratum(kind="style"),
            erratum(find=""),
            erratum(replace="题干"),
            erratum(basis="  "),
            {**erratum(), "no": "1"},
        ]
        for it in bad:
            with self.subTest(it=it), self.assertRaises(ErrataError):
                parse_errata({"items": [it]})


class ApplyErrataTest(unittest.TestCase):
    def test_applies_by_position_within_one_field(self):
        qs = [question(stem="甲乙丙丁")]
        errata = parse_errata({"items": [erratum(find="丁", replace="D"), erratum(find="甲", replace="A")]})
        log = apply_errata(qs, errata)
        self.assertEqual(qs[0]["stem"], "A乙丙D")
        self.assertEqual(len(log), 2)

    def test_choice_field_and_deletion(self):
        qs = [question(A="选项A多余", B="选项B")]
        apply_errata(qs, parse_errata({"items": [erratum(field="choice:A", find="多余", replace="")]}))
        self.assertEqual(qs[0]["choices"][0]["body"], "选项A")

    def test_rejects_and_changes_nothing(self):
        cases = {
            "找不到": [erratum(find="没有这段")],
            "出现两次": [erratum(find="X")],
            "互相重叠": [erratum(find="乙X"), erratum(find="X丙")],
            "题号不存在": [erratum(no=99)],
            "字段不存在": [erratum(field="choice:Z", find="选项")],
        }
        for name, items in cases.items():
            qs = [question(stem="乙X丙X")]
            good = erratum(field="choice:A", find="选项A", replace="甲")
            with self.subTest(name), self.assertRaises(ErrataError):
                apply_errata(qs, parse_errata({"items": [good] + items}))
            # 整批拒绝：同批里本来能成立的那条也不能生效
            self.assertEqual(qs[0]["choices"][0]["body"], "选项A")
            self.assertEqual(qs[0]["stem"], "乙X丙X")


class FacePreparerOrderTest(unittest.TestCase):
    """断行空格 → 专有名词 → 勘误：勘误按整理后的文字定位。"""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = Path(self.tmp.name)
        self.spec_dir, self.data_dir = root / "spec", root / "data"
        self.spec_dir.mkdir()
        self.data_dir.mkdir()
        (self.spec_dir / "term_normalize.json").write_text(json.dumps({"rules": [
            {"to": "AWS Systems Manager", "aliases": ["Systems Manager"], "need_prefix": True, "from": ["系统管理器"]},
        ]}), encoding="utf-8")

    def tearDown(self):
        self.tmp.cleanup()

    def write_errata(self, items):
        (self.data_dir / "errata.json").write_text(json.dumps({"items": items}, ensure_ascii=False), encoding="utf-8")

    def test_errata_see_text_after_gaps_and_terms(self):
        self.write_errata([erratum(find="使用 AWS Systems Manager 管理流量", replace="使用 AWS Systems Manager 管理流量。")])
        qs = [question(stem="使用 AWS 系统 管理器 管理流 量", A="状态 管理器")]
        rep = FacePreparer({"cjk_gap_repair": True}, self.spec_dir, self.data_dir).prepare(qs)
        self.assertEqual(qs[0]["stem"], "使用 AWS Systems Manager 管理流量。")
        self.assertEqual(qs[0]["choices"][0]["body"], "状态管理器")
        self.assertEqual(len(rep.errata_log), 1)
        self.assertGreater(rep.gap_fixes, 0)

    def test_gap_repair_is_opt_in(self):
        qs = [question(stem="流 量")]
        FacePreparer({}, self.spec_dir, self.data_dir).prepare(qs)
        self.assertEqual(qs[0]["stem"], "流 量")

    def test_stale_erratum_stops_everything(self):
        # 勘误写的是整理前的文字（「系统管理器」已被归一成英文）⇒ 定位不到，必须报错而不是跳过
        self.write_errata([erratum(find="AWS 系统管理器", replace="AWS Systems Manager")])
        with self.assertRaises(ErrataError):
            FacePreparer({}, self.spec_dir, self.data_dir).prepare([question(stem="使用 AWS 系统管理器")])


if __name__ == "__main__":
    unittest.main()
