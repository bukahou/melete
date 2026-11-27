"""逐项点评翻译的校验规则。运行：python3 -m unittest discover -s pipeline/tests"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "core"))

import json  # noqa: E402
import tempfile  # noqa: E402

from notes_i18n import check_item, load_translations, note_fp  # noqa: E402

GLO = {"terms": {"区域": ["リージョン"], "存储桶": ["バケット"]}, "not_terms": [], "keep": ["Systems Manager", "S3"]}
SRC = {
    "no": 1,
    "choices": {
        "A": "Systems Manager 只负责调度补丁任务，不能替代应用层的健康检查。",
        "B": "把存储桶复制到另一个区域只解决了容灾，没有解决访问延迟。",
    },
    "dispute": None,
}
SRC_DISPUTED = {**SRC, "dispute": "题库标注选 B，但跨区域复制解决的是容灾而不是访问延迟，题干要的是降低延迟，所以 AI 判 A。"}
FP = note_fp("q0000001", SRC)
GOOD = {
    "A": "Systems Manager はパッチ作業のスケジュールを担うだけで、アプリケーション層のヘルスチェックの代わりにはならない。",
    "B": "バケットを別のリージョンにレプリケートしても解決するのは災害対策だけで、アクセスのレイテンシーは解決しない。",
}
GOOD_DISPUTE = ("問題集の正解は B だが、クロスリージョンレプリケーションが解決するのは災害対策でありアクセスのレイテンシーではない。"
                "設問が求めているのはレイテンシーの低減なので、AI は A と判定した。")


def item(choices=None, dispute=None, fp=FP):
    return {"no": 1, "fp": fp, "choices": dict(GOOD if choices is None else choices), "dispute": dispute}


class CheckItemTest(unittest.TestCase):
    def test_faithful_translation_passes(self):
        self.assertEqual(check_item(item(), SRC, FP, GLO), [])
        fp = note_fp("q0000001", SRC_DISPUTED)
        self.assertEqual(check_item(item(dispute=GOOD_DISPUTE, fp=fp), SRC_DISPUTED, fp, GLO), [])

    def test_stale_fingerprint_rejected(self):
        errs = check_item(item(fp="deadbeef"), SRC, FP, GLO)
        self.assertEqual(len(errs), 1)
        self.assertIn("fp 不符", errs[0])

    def test_fingerprint_follows_source_notes(self):
        self.assertEqual(note_fp("q0000001", SRC), FP)
        edited = {**SRC, "choices": {**SRC["choices"], "A": SRC["choices"]["A"] + "补一句。"}}
        self.assertNotEqual(note_fp("q0000001", edited), FP)
        self.assertNotEqual(note_fp("q0000002", SRC), FP)
        self.assertNotEqual(note_fp("q0000001", SRC_DISPUTED), FP)

    def test_copied_chinese_rejected(self):
        errs = check_item(item({"A": SRC["choices"]["A"], "B": GOOD["B"]}), SRC, FP, GLO)
        self.assertTrue(any("choices[A]" in e and "简体字" in e for e in errs), errs)

    def test_english_name_must_stay(self):
        katakana = {"A": GOOD["A"].replace("Systems Manager", "システムズマネージャー"), "B": GOOD["B"]}
        errs = check_item(item(katakana), SRC, FP, GLO)
        self.assertTrue(any("Systems Manager" in e for e in errs), errs)

    def test_glossary_term_enforced(self):
        wrong = {"A": GOOD["A"], "B": GOOD["B"].replace("リージョン", "地域")}
        errs = check_item(item(wrong), SRC, FP, GLO)
        self.assertTrue(any("区域" in e for e in errs), errs)

    def test_dispute_parity(self):
        errs = check_item(item(dispute=GOOD_DISPUTE), SRC, FP, GLO)
        self.assertTrue(any("dispute 应为 None" in e for e in errs), errs)
        fp = note_fp("q0000001", SRC_DISPUTED)
        errs = check_item(item(fp=fp), SRC_DISPUTED, fp, GLO)
        self.assertTrue(any("dispute 为空" in e for e in errs), errs)

    def test_labels_must_match_source(self):
        errs = check_item(item({"A": GOOD["A"]}), SRC, FP, GLO)
        self.assertTrue(any("≠ 原文点评的选项" in e for e in errs), errs)

    def test_plain_text_and_length(self):
        marked = {"A": "**" + GOOD["A"] + "**", "B": GOOD["B"]}
        self.assertTrue(any("纯文本" in e for e in check_item(item(marked), SRC, FP, GLO)))
        short = {"A": "スケジューラーにすぎない。", "B": GOOD["B"]}
        self.assertTrue(any("疑似漏译" in e for e in check_item(item(short), SRC, FP, GLO)))

    def test_structure(self):
        it = item()
        del it["fp"]
        self.assertTrue(check_item(it, SRC, FP, GLO))
        self.assertTrue(check_item({**item(), "choices": ["A"]}, SRC, FP, GLO))



class LoadTranslationsTest(unittest.TestCase):
    """导入前的过期判断：译文的指纹要与当前的原文点评、素材题面对得上。"""

    def write(self, root: Path, items: list) -> None:
        doc = {"bank": "demo", "locale": "ja", "source_locale": "zh", "complete": False, "items": items}
        (root / "choice_notes.ja.json").write_text(json.dumps(doc, ensure_ascii=False), encoding="utf-8")

    def test_fresh_and_stale_items(self):
        source = {1: SRC, 2: {**SRC, "no": 2}}
        raw_fp = {1: "q0000001", 2: "q0000002"}
        fresh = {**item(), "no": 1}
        # 原文点评改过：按旧原文算出的指纹对不上
        old_src = {**SRC, "choices": {**SRC["choices"], "A": "旧的点评。"}}
        stale = {**item(fp=note_fp("q0000002", old_src)), "no": 2}
        with tempfile.TemporaryDirectory() as d:
            self.write(Path(d), [fresh, stale])
            docs, bad = load_translations(Path(d), source, raw_fp)
        self.assertEqual(bad, ["choice_notes.ja.json #2"])
        self.assertEqual([doc["locale"] for doc in docs], ["ja"])
        self.assertEqual(sorted(docs[0]["items"]), [1])

    def test_question_without_source_notes_is_stale(self):
        with tempfile.TemporaryDirectory() as d:
            self.write(Path(d), [{**item(), "no": 9}])
            _, bad = load_translations(Path(d), {1: SRC}, {1: "q0000001"})
        self.assertEqual(bad, ["choice_notes.ja.json #9"])

    def test_source_notes_file_itself_is_not_a_translation(self):
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "choice_notes.json").write_text("{}", encoding="utf-8")
            docs, bad = load_translations(Path(d), {1: SRC}, {1: "q0000001"})
        self.assertEqual((docs, bad), ([], []))


if __name__ == "__main__":
    unittest.main()
