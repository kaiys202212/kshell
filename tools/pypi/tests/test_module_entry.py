"""python -m kshell_install.cli 必须真正执行 main。

回归背景:cli.py 曾缺 `__main__` 守卫,`python -m kshell_install.cli ...`
静默 exit 0 且什么都不装(冒烟实测 EXIT=0、bin 目录为空)。
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

SRC = Path(__file__).resolve().parents[1] / "src"


def test_python_dash_m_cli_prints_help() -> None:
    env = dict(os.environ, PYTHONPATH=str(SRC))
    proc = subprocess.run(
        [sys.executable, "-m", "kshell_install.cli", "-h"],
        capture_output=True,
        text=True,
        env=env,
    )
    assert proc.returncode == 0
    output = proc.stdout + proc.stderr
    assert "一键安装器" in output
    assert "--no-run" in output
