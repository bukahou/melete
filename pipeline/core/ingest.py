#!/usr/bin/env python3
"""
Melete 素材导入编排器

约定：素材放进 inbox 的题库子目录，处理完由**用户自己**拿走。
本脚本不删除、不移动用户的任何文件 —— 只读取、解析、登记。

    $MELETE_INBOX/<bank-slug>/<任意文件名>
              ↓  ingest.py <bank-slug>
    data/<bank-slug>/questions.json     结构化产物（进 git）
    data/<bank-slug>/source.yaml        素材登记单（进 git，仅元数据）

登记单的意义：素材拿走之后，仓库仍然知道
  · 产物来自哪个文件（文件名 + sha256 + 大小）
  · 什么时候、用哪个版本的解析器生成的
  · 产出了多少题、多少告警
将来拿到新版素材，比对 sha256 即可判断是不是同一份。
"""
import argparse
import hashlib
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
DEFAULT_INBOX = Path(os.environ.get("MELETE_INBOX", Path.home() / "melete-inbox"))


def sha256_of(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def git_rev_of(path: Path) -> str:
    """
    解析器当前的 git 版本，用于可复现性追溯。

    带未提交改动时追加 -dirty（同 `git describe --dirty` 的惯例）：
    否则登记单会声称用了某个 commit 的解析器，而实际跑的是工作区里改过的版本 ——
    那样这份登记单就是在撒谎，可追溯性归零。
    """
    rel = str(path.relative_to(REPO))
    try:
        rev = subprocess.run(
            ["git", "log", "-1", "--format=%h", "--", rel],
            cwd=REPO, capture_output=True, text=True, timeout=10,
        ).stdout.strip() or "uncommitted"
        dirty = subprocess.run(
            ["git", "status", "--porcelain", "--", rel],
            cwd=REPO, capture_output=True, text=True, timeout=10,
        ).stdout.strip()
        return f"{rev}-dirty" if dirty else rev
    except Exception:
        return "unknown"


def extract_text(src: Path, dst: Path) -> str:
    """把素材转成解析器吃的纯文本。返回所用工具的描述。"""
    if src.suffix.lower() == ".pdf":
        subprocess.run(["pdftotext", "-layout", str(src), str(dst)],
                       check=True, capture_output=True)
        ver = subprocess.run(["pdftotext", "-v"], capture_output=True, text=True)
        tool = (ver.stderr or ver.stdout).splitlines()[0].strip()
        return f"{tool} (-layout)"
    if src.suffix.lower() in (".txt", ".md"):
        dst.write_bytes(src.read_bytes())
        return "passthrough"
    raise SystemExit(f"不支持的素材格式: {src.suffix}（目前支持 .pdf / .txt / .md）")


def yaml_dump(doc: dict) -> str:
    """极简 YAML 输出，避免为一个登记单引入 pyyaml 依赖。"""
    def val(v):
        if isinstance(v, str) and (":" in v or v.startswith(" ") or v == ""):
            return json.dumps(v, ensure_ascii=False)
        return v

    out = [f"# Melete 素材登记单 —— 由 pipeline/core/ingest.py 自动生成，请勿手改",
           f"bank: {doc['bank']}", "sources:"]
    for s in doc["sources"]:
        out.append(f"  - filename: {val(s['filename'])}")
        for k in ("bytes", "sha256", "ingested_at", "extract_tool", "parser", "parser_rev"):
            out.append(f"    {k}: {val(s[k])}")
        out.append("    output:")
        for k, v in s["output"].items():
            out.append(f"      {k}: {val(v)}")
    return "\n".join(out) + "\n"


def main() -> None:
    ap = argparse.ArgumentParser(description="从 inbox 导入题库素材")
    ap.add_argument("bank", help="题库 slug，须与 inbox 子目录名和 pipeline/banks/ 下目录名一致")
    ap.add_argument("--inbox", type=Path, default=DEFAULT_INBOX)
    args = ap.parse_args()

    inbox = args.inbox / args.bank
    parser_py = REPO / "pipeline" / "banks" / args.bank / "parse.py"
    out_dir = REPO / "data" / args.bank

    if not parser_py.exists():
        sys.exit(f"✗ 找不到解析器: {parser_py.relative_to(REPO)}\n"
                 f"  新题库需要先写 pipeline/banks/{args.bank}/parse.py")
    if not inbox.is_dir():
        sys.exit(f"✗ 收件区不存在: {inbox}\n"
                 f"  请先创建该目录并把素材放进去。")

    files = sorted(p for p in inbox.iterdir() if p.is_file() and not p.name.startswith("."))
    if not files:
        sys.exit(f"✗ 收件区是空的: {inbox}")
    if len(files) > 1:
        sys.exit(f"✗ 收件区有 {len(files)} 个文件，目前一次只处理一个：\n  " +
                 "\n  ".join(f.name for f in files))

    src = files[0]
    out_dir.mkdir(parents=True, exist_ok=True)
    questions_json = out_dir / "questions.json"

    print(f"素材    {src.name}", flush=True)
    print(f"大小    {src.stat().st_size / 2**20:.1f} MiB")
    digest = sha256_of(src)
    print(f"sha256  {digest}")

    with tempfile.TemporaryDirectory() as td:
        txt = Path(td) / "extracted.txt"
        tool = extract_text(src, txt)
        print(f"抽取    {tool}\n", flush=True)
        r = subprocess.run([sys.executable, str(parser_py), str(txt), str(questions_json)],
                           text=True)
        if r.returncode != 0:
            sys.exit("✗ 解析失败")

    doc = json.loads(questions_json.read_text(encoding="utf-8"))
    qs = doc["questions"]
    manifest = {
        "bank": args.bank,
        "sources": [{
            "filename": src.name,
            "bytes": src.stat().st_size,
            "sha256": digest,
            "ingested_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
            "extract_tool": tool,
            "parser": f"pipeline/banks/{args.bank}/parse.py",
            "parser_rev": git_rev_of(parser_py),
            "output": {
                "questions": len(qs),
                "with_warnings": sum(1 for q in qs if q["warnings"]),
                "multi_select": sum(1 for q in qs if q["kind"] == "multi"),
            },
        }],
    }
    (out_dir / "source.yaml").write_text(yaml_dump(manifest), encoding="utf-8")

    print(f"\n✓ 产物  data/{args.bank}/questions.json")
    print(f"✓ 登记  data/{args.bank}/source.yaml")
    print(f"\n素材已处理完毕，可以把它从收件区拿走了：")
    print(f"  {src}")
    print("（本脚本不会删除或移动你的文件）")


if __name__ == "__main__":
    main()
