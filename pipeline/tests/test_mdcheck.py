"""解析正文强调标记的检查。运行：python3 -m unittest discover -s pipeline/tests"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "core"))

from mdcheck import emphasis_errors, strip_markers, structure_diff  # noqa: E402


class EmphasisErrorsTest(unittest.TestCase):
    def test_well_formed_markup_passes(self):
        ok = [
            "**判据：**题干说「**必须**」，所以选 A。",
            "**「重点」**之后的内容",
            "有 **`s3:PutObjectLegalHold`** 权限的人",
            "列表：\n- **A**：对\n- **B**：错",
            "| **频率** | 每月第一天 |\n|---|---|",
            "> **引用里的重点：第一行\n> 第二行收尾。**",
            "为 \\*.example.com 申请通配符证书，SSE-\\* 都不满足",
            "运算 5 * 3 两侧有空格，是字面星号",
            "***不丢失灵活性** 是关键*",
            "**题干问 *generate a **TCO report*** → 锁定 **Migration Evaluator**，排掉其余。**",
            "```\n{\"Action\": \"s3:*\", \"Resource\": \"*\"}\n```",
            "**BYOD（Bring Your Own Device）**は，私物端末の業務利用。",
        ]
        for text in ok:
            with self.subTest(text=text[:20]):
                self.assertEqual(emphasis_errors(text), [])

    def test_broken_markup_flagged(self):
        broken = [
            "**题干说「必须**可供使用* 6 个月」——之后只需保存。**",     # 配不上对
            "**题干说的是 API Gateway 的**区域**端点，所以要创建**区域级** Web ACL。**",  # 整段加粗里再套加粗
            "**一句话记忆：**缓存**内容**，加速**连接****。三条都指向它。**",  # 四个星号连写
            "任何服务器端加密（SSE-*）都不满足",                          # 未转义的通配符
            "要用 **DNS64\n+ NAT 网关的 NAT64** 翻译",                     # 列表项打断了加粗
            "```\n② 规划：**数据探索**\n```",                               # 代码块不渲染加粗
            "**a *b** c",                                                # 交叉后剩一个配不上
        ]
        for text in broken:
            with self.subTest(text=text[:20]):
                self.assertTrue(emphasis_errors(text))

    def test_follows_cjk_friendly_flanking(self):
        # 闭合的 ** 内侧是中日文标点、外侧是汉字：CommonMark 不认，CJK 友好规则认 —— 不算问题
        self.assertEqual(emphasis_errors("**该星号会被识别。**这是因为规则修正了。"), [])
        # 夹在两个汉字之间的 ** 可开可闭，按配对算法先闭合外层：这里不露星号（只是强调位置与原意不同，查不出来）
        self.assertEqual(emphasis_errors("**· 处理端能变大，但**它有上限**，且会把压力传给下游**"), [])
        # 嵌套后剩下的 ** 前后都配不上：露星号
        self.assertTrue(emphasis_errors("**题干说的是 API Gateway 的**区域**端点，所以要创建**区域级** Web ACL。**"))

    def test_error_names_the_unit(self):
        errs = emphasis_errors("第一段没问题。\n\n**第二段坏了")
        self.assertEqual(len(errs), 1)
        self.assertIn("第二段坏了", errs[0])


class StripMarkersTest(unittest.TestCase):
    def test_only_markers_and_escapes_removed(self):
        self.assertEqual(strip_markers("**a** 与 *b*，\\*.example.com"), "a 与 b，.example.com")
        self.assertEqual(strip_markers("**题干**说"), strip_markers("题干**说**"))
        self.assertNotEqual(strip_markers("**题干**说"), strip_markers("**题目**说"))



class StructureDiffTest(unittest.TestCase):
    SRC = "第一段。\n\n| 参数 | 值 |\n|---|---|\n| 频率 | 每月 |\n\n```\n图\n```\n\n> 引用\n\n## 小结\n收尾。"

    def test_same_skeleton_passes(self):
        ja = "第一段落。\n\n| パラメータ | 値 |\n|---|---|\n| 頻度 | 毎月 |\n\n```\n図\n```\n\n> 引用\n\n## まとめ\n締め。"
        self.assertEqual(structure_diff(self.SRC, ja), [])

    def test_dropped_parts_flagged(self):
        no_table_row = self.SRC.replace("| 频率 | 每月 |\n", "")
        merged = self.SRC.replace("第一段。\n\n", "第一段。")
        for dst in (no_table_row, merged, self.SRC.replace("> 引用", "引用")):
            with self.subTest(dst=dst[:12]):
                self.assertTrue(structure_diff(self.SRC, dst))


if __name__ == "__main__":
    unittest.main()
