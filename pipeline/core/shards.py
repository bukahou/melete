"""
分片式人工智能产物的存取：按题号切片、一片一个文件、文件存在且校验通过即完成。

逐片生成要跨多个会话和额度周期，所以状态只看磁盘上的文件，不另记进度清单。
各阶段（逐项点评等）只提供「单题怎么校验」，切片、读写、合并、状态统计都在这里。

  ShardStore      一个阶段在一个题库下的分片目录
  load_items      从 Python 字面量文件读出 ITEMS 列表（只做字面量求值，不执行代码）
  write_json      先写临时文件再改名，进程中途被杀也不会留下半个文件
"""
import ast
import json
import os
import tempfile
from collections.abc import Callable
from datetime import datetime, timezone
from pathlib import Path

ItemCheck = Callable[[dict], list[str]]


def write_json(path: Path, doc: dict) -> None:
    """原子写 JSON：同目录临时文件 + os.replace。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix=f".{path.name}.", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(doc, f, ensure_ascii=False, indent=1)
            f.write("\n")
        os.replace(tmp, path)
    except BaseException:
        Path(tmp).unlink(missing_ok=True)
        raise


def load_items(src: Path) -> list:
    """
    读输入文件里的 `ITEMS = [...]`。

    用 Python 字面量而不是 JSON：长文本放进三引号里，换行、引号、反斜杠都不用转义。
    只对赋值右侧做 ast.literal_eval —— 文件里的任何函数调用、表达式都会被拒绝，不会执行。
    """
    tree = ast.parse(src.read_text(encoding="utf-8"), filename=str(src))
    for node in tree.body:
        if (isinstance(node, ast.Assign) and len(node.targets) == 1
                and isinstance(node.targets[0], ast.Name) and node.targets[0].id == "ITEMS"):
            items = ast.literal_eval(node.value)
            if not isinstance(items, list):
                raise ValueError("ITEMS 必须是列表")
            return items
    raise ValueError("文件里没有 `ITEMS = [...]` 赋值")


class ShardStore:
    """一个阶段在一个题库下的分片目录。题号区间固定，与题号是否连续无关。"""

    def __init__(self, root: Path, numbers: list[int], size: int = 25):
        if not numbers:
            raise ValueError("题号列表为空")
        self.root = root
        self.numbers = sorted(numbers)
        lo, hi = self.numbers[0], self.numbers[-1]
        self.ranges = [(s, min(s + size - 1, hi)) for s in range(lo, hi + 1, size)]

    @staticmethod
    def name(rng: tuple[int, int]) -> str:
        return f"{rng[0]:04d}-{rng[1]:04d}.json"

    def path(self, rng: tuple[int, int]) -> Path:
        return self.root / self.name(rng)

    def range_of(self, no: int) -> tuple[int, int]:
        rng = next((r for r in self.ranges if r[0] <= no <= r[1]), None)
        if rng is None:
            raise KeyError(f"#{no} 不属于任何分片区间")
        return rng

    def members(self, rng: tuple[int, int]) -> list[int]:
        return [n for n in self.numbers if rng[0] <= n <= rng[1]]

    def read(self, rng: tuple[int, int]) -> dict | None:
        p = self.path(rng)
        if not p.exists():
            return None
        return json.loads(p.read_text(encoding="utf-8"))

    def check(self, rng: tuple[int, int], wanted: set[int], check_item: ItemCheck) -> list[str]:
        """
        校验一片。wanted 是这一片里应该有产物的题号（可能少于区间内全部题号：
        不需要这一阶段的题由调用方排除）。
        """
        try:
            doc = self.read(rng)
        except json.JSONDecodeError as e:
            return [f"JSON 解析失败: {e}"]
        if doc is None:
            return ["分片不存在"]
        items = doc.get("items", [])
        got = [it.get("no") for it in items]
        errs = []
        if missing := wanted - set(got):
            errs.append(f"缺题号 {sorted(missing)}")
        if extra := set(got) - wanted:
            errs.append(f"多出不该有的题号 {sorted(extra)}")
        if len(got) != len(set(got)):
            errs.append("同一题号出现了多次")
        for it in items:
            if it.get("no") in wanted:
                errs += [f"#{it['no']}: {e}" for e in check_item(it)]
        return errs

    def write(self, items: list[dict], meta: dict) -> list[tuple[tuple[int, int], int, int]]:
        """
        把一批条目按题号路由进各自的分片；同题号覆盖，其余保留。
        返回 [(区间, 本次写入条数, 该片现有条数)]。调用方负责先校验。
        """
        buckets: dict[tuple[int, int], list[dict]] = {}
        for it in items:
            buckets.setdefault(self.range_of(it["no"]), []).append(it)
        out = []
        for rng, batch in sorted(buckets.items()):
            try:
                doc = self.read(rng) or {}
            except json.JSONDecodeError:
                doc = {}
            merged = {i["no"]: i for i in doc.get("items", [])}
            merged.update({i["no"]: i for i in batch})
            doc = {
                **meta,
                "range": list(rng),
                "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
                "items": [merged[k] for k in sorted(merged)],
            }
            write_json(self.path(rng), doc)
            out.append((rng, len(batch), len(doc["items"])))
        return out
