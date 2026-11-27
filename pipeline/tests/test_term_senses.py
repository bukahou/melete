"""按题声明词义（term_senses）。运行：python3 -m unittest discover -s pipeline/tests"""
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "core"))

import translate  # noqa: E402
from translate import glossary_errors  # noqa: E402

GLO = {"terms": {"区域": ["リージョン"], "托管区域": ["ホストゾーン"]}, "not_terms": [], "keep": []}


class GlossaryWithSensesTest(unittest.TestCase):
    SRC = ["在 Simple AD 与 Active Directory 之间启用区域传输。"]

    def test_without_sense_region_is_required(self):
        self.assertTrue(glossary_errors(self.SRC, ["Simple AD と Active Directory の間でゾーン転送を有効にします。"], GLO))

    def test_sense_replaces_requirement_not_skips_it(self):
        senses = {"区域": ["ゾーン"]}
        self.assertEqual(glossary_errors(self.SRC, ["Simple AD と Active Directory の間でゾーン転送を有効にします。"], GLO, senses), [])
        errs = glossary_errors(self.SRC, ["Simple AD と Active Directory の間で転送を有効にします。"], GLO, senses)
        self.assertTrue(any("ゾーン" in e for e in errs), errs)

    def test_longer_terms_still_win(self):
        # 「托管区域」照旧由长词条吃掉，只有剩下的「区域」按声明要求
        src = ["为 200 个区域建托管区域"]
        self.assertEqual(glossary_errors(src, ["200 のゾーンにホストゾーンを作成する"], GLO, {"区域": ["ゾーン"]}), [])


class LoadTermSensesTest(unittest.TestCase):
    def load(self, doc):
        with tempfile.TemporaryDirectory() as d:
            bank = Path(d) / "pipeline" / "banks" / "demo"
            bank.mkdir(parents=True)
            (bank / "term_senses.ja.json").write_text(json.dumps(doc, ensure_ascii=False), encoding="utf-8")
            with mock.patch.object(translate, "REPO", Path(d)):
                return translate.load_term_senses("demo", "ja", GLO)

    def test_valid_file(self):
        got = self.load({"_what": "说明", "741": {"区域": "ゾーン", "_why": "DNS 的 zone transfer"}})
        self.assertEqual(got, {741: {"区域": ["ゾーン"]}})

    def test_rejects_unknown_term_missing_reason_and_empty(self):
        for doc in ({"1": {"新词": ["x"], "_why": "理由"}},       # 不能借此新增词条
                    {"1": {"区域": ["ゾーン"]}},                   # 缺理由
                    {"1": {"_why": "理由"}},                       # 没声明任何词条
                    {"1": {"区域": [""], "_why": "理由"}}):         # 空译词
            with self.subTest(doc=doc), self.assertRaises(SystemExit):
                self.load(doc)


if __name__ == "__main__":
    unittest.main()
