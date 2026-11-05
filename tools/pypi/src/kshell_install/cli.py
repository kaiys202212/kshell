"""kshell-install 命令行入口:pip 直装后一条命令装好桌面版。

参考 Go 侧 internal/update 的源顺序与附件补链规则;这里只负责"装",
不负责已有安装的升级(桌面端自带更新器)。
"""

from __future__ import annotations

import argparse
import json
import sys
import urllib.request
from pathlib import Path

from kshell_install import installer, sources
from kshell_install.platform import UnsupportedPlatform, detect

USER_AGENT = "kshell-install"


def get_bytes(url: str) -> bytes:
    """生产实现:urllib 读取 URL 正文,带固定 User-Agent。"""
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(req, timeout=120) as resp:
        return resp.read()


def get_json(url: str) -> dict:
    """生产实现:在 get_bytes 之上解析 JSON。"""
    return json.loads(get_bytes(url).decode("utf-8"))


def _build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="kshell-install",
        description="kshell 桌面版一键安装器:下载官方 zip → 校验 SHA-256 → 解压到 ~/.kshell/bin → 启动。",
    )
    p.add_argument(
        "--tag",
        default="",
        help="固定安装某一发布版本(如 v0.1.27);缺省时自动取最新 release。",
    )
    p.add_argument(
        "--no-run",
        action="store_true",
        help="只安装不启动(缺省会拉起 kshell 桌面窗口)。",
    )
    p.add_argument(
        "--bin-dir",
        default="",
        help="安装目录(缺省 %%USERPROFILE%% 或 ~ 下的 .kshell/bin)。",
    )
    return p


def main(argv: list[str] | None = None) -> int:
    args = _build_parser().parse_args(argv)
    try:
        os_name, _arch, zip_name, binary_name = detect()
        bin_dir = Path(args.bin_dir) if args.bin_dir else Path.home() / ".kshell" / "bin"

        if args.tag:
            # 固定版本:不再查 latest,GitCode 附件链只依赖 tag 即可构造。
            tag = sources.canon(args.tag)
            zip_url = sources.gitcode_attach_url(tag, zip_name)
            sums_url = sources.gitcode_attach_url(tag, sources.SUMS_NAME)
        else:
            print("正在查询最新版本……")
            tag, zip_url, sums_url = sources.latest_release(get_json, zip_name)
        print(f"目标版本 {tag}")

        print(f"正在下载 {zip_name}……")
        zip_path = installer.download(get_bytes, zip_url)
        try:
            print("正在校验 SHA-256……")
            sums_text = get_bytes(sums_url).decode("utf-8", errors="replace")
            installer.verify(zip_path, sums_text, zip_name)
            print(f"正在解压到 {bin_dir}……")
            entry = installer.extract(zip_path, bin_dir, binary_name)
        finally:
            zip_path.unlink(missing_ok=True)

        print(f"安装完成:kshell 桌面版 {tag} -> {entry}")
        if args.no_run:
            return 0
        print("正在启动……")
        installer.launch(os_name, entry)
        return 0
    except (installer.InstallError, sources.ReleaseError, UnsupportedPlatform) as exc:
        print(f"kshell-install 错误:{exc}", file=sys.stderr)
        raise SystemExit(1) from None
    except KeyboardInterrupt:
        print("已取消", file=sys.stderr)
        raise SystemExit(1) from None
