"""路径解析的唯一归属地。

本仓是**公开仓**，题库数据不在其中 —— 它在私有的 config 仓里，
由 `MELETE_DATA_ROOT` 指出位置。

两个根，别混：

    REPO         代码仓根。`pipeline/banks/<bank>/parse.py` 与各 spec 在这里，
                 它们是代码，跟着仓库走。
    data_root()  题库数据根。questions.json / enriched.json / transcribed/ 在这里，
                 它们是数据，在仓库之外。

⛔ MELETE_DATA_ROOT 必填，【没有默认值】。

理由不是洁癖。一个"合理的默认值"（比如回落到 REPO/"data"）会让脚本把
28 MB 题库写回**公开仓** —— 正是把数据挪出去要避免的那件事本身。
忘配就报错退出，比猜一个路径安全。

同样的判断在 backend 侧也成立（见 CLAUDE.md 的凭证纪律：
凭证类一律无默认值 + required，生产忘配就启动失败，
优于"默默连了个错库/错环境"）。这里错的不是库，是仓。

设置位置：config 私有仓 `local/ubuntu/env/melete.env`，开发机 zshrc 自动加载。
已开着的终端要立刻生效：
    source ~/work/github/config/local/ubuntu/env/melete.env
"""

import os
import sys
from pathlib import Path

# 代码仓根。本文件在 <repo>/pipeline/core/paths.py，故 parents[2]。
REPO = Path(__file__).resolve().parents[2]

_ENV = "MELETE_DATA_ROOT"


def data_root() -> Path:
    """题库数据根目录。未配置或不存在则直接退出。

    ⛔ 不返回默认值，不自动创建根目录 —— 见模块 docstring。
    (`<root>/<bank>/` 这一层由各脚本按需创建，那是正常产物；
     根目录本身不存在则说明配错了，不该悄悄造一个出来。)
    """
    raw = os.environ.get(_ENV)
    if not raw:
        sys.exit(
            f"✗ 未设 {_ENV} —— 题库数据不在本仓库里（本仓是公开仓）。\n"
            f"  它在私有的 config 仓 banks/melete/。设置方式：\n"
            f"    source ~/work/github/config/local/ubuntu/env/melete.env\n"
            f"  ⛔ 本脚本刻意不猜默认路径：猜错会把题库写回公开仓。"
        )
    root = Path(raw).expanduser().resolve()
    if not root.is_dir():
        sys.exit(f"✗ {_ENV} 指向的目录不存在：{root}")
    return root


def bank_dir(bank: str) -> Path:
    """某个题库的数据目录 `<data_root>/<bank>`。"""
    return data_root() / bank


def rel(path: Path) -> str:
    """给人看的路径。

    落在哪个根下就相对哪个根显示，都不在就给绝对路径 ——
    ⛔ 不能无脑 `relative_to(REPO)`：数据已经不在仓库里，那样会抛 ValueError。

    ⚠️ 这里【不】调用 data_root()：那个函数在未配置时会 sys.exit，
       而一个纯粹的显示函数不该有终止进程的副作用 ——
       尤其它常出现在错误信息里，那正是配置可能有问题的时候。
    """
    path = Path(path)
    roots = [(REPO, "")]
    raw = os.environ.get(_ENV)
    if raw:
        roots.append((Path(raw).expanduser().resolve(), "$MELETE_DATA_ROOT/"))
    for root, label in roots:
        try:
            return label + str(path.relative_to(root))
        except ValueError:
            continue
    return str(path)
