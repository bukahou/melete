"""逐项点评阶段的校验规则与分片读写。运行：python3 -m unittest discover -s pipeline/tests"""
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "core"))

from choice_notes import check_item, rivals, skip_reason  # noqa: E402
from shards import ShardStore, load_items, write_json  # noqa: E402


def make_question(verdict="BC", bank_label="CD", vote=None, pick=2, labels="ABCDE", **extra):
    claims = [{"source": "bank_label", "answer": bank_label}]
    if vote:
        claims.append({"source": "community_vote", "answer": vote, "confidence": 80})
    return {
        "no": 936, "kind": "multi" if pick > 1 else "single", "pick": pick,
        "stem": "题干", "choices": [{"label": l, "body": f"选项{l}"} for l in labels],
        "claims": claims, "warnings": [],
        "enrichment": {"verdict": verdict, "confidence": "high", "data_issue": None,
                       "reasoning": "理由", "explanation": "解析"},
        **extra,
    }


def notes_for(labels="ABCDE", text="这个选项的原因写在这里，足够长"):
    return {l: text for l in labels}


class RivalsTest(unittest.TestCase):
    def test_bank_label_differs(self):
        self.assertEqual(rivals(make_question()), [("bank_label", "CD")])

    def test_all_agree(self):
        self.assertEqual(rivals(make_question(bank_label="BC", vote="CB")), [])

    def test_malformed_claim_is_not_a_rival(self):
        # 字母数与应选数不符的主张导入时会被丢弃，不能要求为它写分歧说明
        self.assertEqual(rivals(make_question(bank_label="C")), [])
        self.assertEqual(rivals(make_question(bank_label="CZ")), [])


class CheckItemTest(unittest.TestCase):
    def test_valid_disputed(self):
        it = {"no": 936, "choices": notes_for(), "dispute": "题库标注 CD 错在参数存储没有自动轮换能力，所以应选 BC。"}
        self.assertEqual(check_item(it, make_question()), [])

    def test_valid_agreed(self):
        it = {"no": 936, "choices": notes_for(), "dispute": None}
        self.assertEqual(check_item(it, make_question(bank_label="BC")), [])

    def test_missing_dispute_on_disputed_question(self):
        it = {"no": 936, "choices": notes_for(), "dispute": None}
        self.assertTrue(any("dispute" in e for e in check_item(it, make_question())))

    def test_dispute_on_agreed_question(self):
        it = {"no": 936, "choices": notes_for(), "dispute": "多余的分歧说明，这题其实没有分歧。"}
        self.assertTrue(any("应为 None" in e for e in check_item(it, make_question(bank_label="BC"))))

    def test_label_set_must_match(self):
        it = {"no": 936, "choices": notes_for("ABCD"), "dispute": None}
        self.assertTrue(any("≠ 题目选项" in e for e in check_item(it, make_question(bank_label="BC"))))

    def test_markdown_and_newlines_rejected(self):
        for bad in ("用 **加粗** 写的点评内容", "第一行内容写在这里\n第二行内容", "| 表格 | 写法 |"):
            it = {"no": 936, "choices": {**notes_for(), "A": bad}, "dispute": None}
            errs = check_item(it, make_question(bank_label="BC"))
            self.assertTrue(any("纯文本" in e for e in errs), bad)

    def test_length_bounds(self):
        for bad in ("太短", "长" * 151):
            it = {"no": 936, "choices": {**notes_for(), "A": bad}, "dispute": None}
            self.assertTrue(any("字，应在" in e for e in check_item(it, make_question(bank_label="BC"))))

    def test_unexpected_keys(self):
        it = {"no": 936, "choices": notes_for(), "dispute": None, "verdict": "BC"}
        self.assertTrue(check_item(it, make_question(bank_label="BC")))


class SkipReasonTest(unittest.TestCase):
    def test_annotatable(self):
        self.assertIsNone(skip_reason(make_question()))

    def test_data_issue(self):
        q = make_question()
        q["enrichment"].update(verdict="", data_issue="选项缺失")
        self.assertIsNotNone(skip_reason(q))

    def test_duplicate_labels_and_duplicates(self):
        self.assertIsNotNone(skip_reason(make_question(warnings=["duplicate_choice_label_C"])))
        self.assertIsNotNone(skip_reason(make_question(warnings=["duplicate_of_438"])))


class ShardStoreTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_ranges_and_routing(self):
        store = ShardStore(self.root / "s", [1, 2, 30, 51], size=25)
        self.assertEqual(store.ranges, [(1, 25), (26, 50), (51, 51)])
        self.assertEqual(store.range_of(30), (26, 50))

    def test_write_merges_by_number(self):
        store = ShardStore(self.root / "s", list(range(1, 26)))
        store.write([{"no": 1, "v": "a"}, {"no": 2, "v": "b"}], {"stage": "t"})
        store.write([{"no": 2, "v": "c"}], {"stage": "t"})
        items = store.read((1, 25))["items"]
        self.assertEqual([(i["no"], i["v"]) for i in items], [(1, "a"), (2, "c")])

    def test_check_reports_missing_and_item_errors(self):
        store = ShardStore(self.root / "s", list(range(1, 26)))
        store.write([{"no": 1}], {})
        errs = store.check((1, 25), {1, 2}, lambda it: ["坏"])
        self.assertIn("缺题号 [2]", errs)
        self.assertIn("#1: 坏", errs)

    def test_write_json_leaves_no_temp_files(self):
        target = self.root / "out.json"
        write_json(target, {"a": 1})
        self.assertEqual(json.loads(target.read_text(encoding="utf-8")), {"a": 1})
        self.assertEqual([p.name for p in self.root.iterdir()], ["out.json"])


class LoadItemsTest(unittest.TestCase):
    def write(self, text: str) -> Path:
        f = tempfile.NamedTemporaryFile("w", suffix=".py", delete=False, encoding="utf-8")
        f.write(text)
        f.close()
        self.addCleanup(Path(f.name).unlink)
        return Path(f.name)

    def test_reads_literal(self):
        p = self.write('ITEMS = [{"no": 1, "text": """多行\n"引号"\\反斜杠"""}]\n')
        self.assertEqual(load_items(p)[0]["no"], 1)

    def test_rejects_code(self):
        # 只做字面量求值：任何调用都必须被拒绝，而不是被执行
        p = self.write('ITEMS = [__import__("os").getcwd()]\n')
        with self.assertRaises(ValueError):
            load_items(p)

    def test_requires_items(self):
        with self.assertRaises(ValueError):
            load_items(self.write("OTHER = []\n"))


if __name__ == "__main__":
    unittest.main()
