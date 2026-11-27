"""英文原文里的假命中：keep_drop（同时是普通词的服务名）与英文 not_terms。运行：python3 -m unittest discover -s pipeline/tests"""
import io
import sys
import unittest
from contextlib import redirect_stdout
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "core"))

from translate import glossary_errors, merge_keep  # noqa: E402


class MergeKeepTest(unittest.TestCase):
    def test_drop_removes_derived_bare_name_and_keeps_full_name(self):
        keep = merge_keep(["Amazon Connect"], ["Connect", "Direct Connect", "S3"], ["Connect"])
        self.assertEqual(keep, ["Amazon Connect", "Direct Connect", "S3"])

    def test_unused_drop_is_reported(self):
        out = io.StringIO()
        with redirect_stdout(out):
            merge_keep([], ["S3"], ["Conect"])
        self.assertIn("Conect", out.getvalue())


class VerbConnectTest(unittest.TestCase):
    SRC = ["Create a transit gateway. Connect the existing transit gateway to the VPC."]
    DST = ["Transit Gateway を作成します。既存の Transit Gateway を VPC に接続します。"]

    def test_bare_connect_in_keep_flags_the_verb(self):
        glo = {"terms": {}, "not_terms": [], "keep": ["Connect"]}
        self.assertTrue(glossary_errors(self.SRC, self.DST, glo))

    def test_after_drop_verb_passes_and_full_name_is_still_required(self):
        glo = {"terms": {}, "not_terms": [], "keep": merge_keep(["Amazon Connect"], ["Connect"], ["Connect"])}
        self.assertEqual(glossary_errors(self.SRC, self.DST, glo), [])
        src = ["Use Amazon Connect for the contact center."]
        self.assertTrue(glossary_errors(src, ["コンタクトセンターに Connect を使用します。"], glo))
        self.assertEqual(glossary_errors(src, ["コンタクトセンターに Amazon Connect を使用します。"], glo), [])


class EnglishNotTermTest(unittest.TestCase):
    GLO = {"terms": {"replica": ["レプリカ"], "read replica": ["リードレプリカ"]}, "not_terms": ["replicat"], "keep": []}

    def test_replicate_is_not_replica(self):
        self.assertEqual(glossary_errors(["Configure S3 replication to replicate the objects."],
                                         ["S3 レプリケーションを設定してオブジェクトをレプリケートします。"], self.GLO), [])

    def test_real_replica_in_the_same_text_is_still_required(self):
        src = ["Replicate the data and add a replica in another AZ."]
        self.assertTrue(glossary_errors(src, ["データをレプリケートし、別の AZ に複製を追加します。"], self.GLO))
        self.assertEqual(glossary_errors(src, ["データをレプリケートし、別の AZ にレプリカを追加します。"], self.GLO), [])


if __name__ == "__main__":
    unittest.main()
