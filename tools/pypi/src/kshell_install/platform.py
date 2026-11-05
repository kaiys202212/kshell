"""平台探测:把当前机器映射到官方支持的发布产物。

产物矩阵(与 CI 发布一致,见 internal/update/check.go 的 PackageZipName):
- windows/amd64 → zip 内含 kshell-desktop.exe
- linux/amd64   → zip 内含裸二进制 kshell-desktop
- darwin/arm64  → zip 内含完整 kshell-desktop.app 目录

矩阵之外(GitHub Actions 目前不出这些包)直接报错,不再走下载。
"""

from __future__ import annotations

import platform as _platform
import sys

#: 支持的 (os, arch) 组合;zip 名 = kshell-desktop-{os}-{arch}.zip。
ZIPS = {
    ("windows", "amd64"): "kshell-desktop-windows-amd64.zip",
    ("linux", "amd64"): "kshell-desktop-linux-amd64.zip",
    ("darwin", "arm64"): "kshell-desktop-darwin-arm64.zip",
}

#: zip 内的可执行文件名(相对于解压根)。
BINARIES = {
    "windows": "kshell-desktop.exe",
    "linux": "kshell-desktop",
    "darwin": "kshell-desktop.app",
}


class UnsupportedPlatform(Exception):
    """当前系统/架构没有对应的官方发布包。"""


def canon_arch(machine: str) -> str:
    """把各平台对同一架构的叫法归一成发布产物里的写法。"""
    m = machine.lower()
    return {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(m, m)


def _default_os() -> str:
    plat = sys.platform
    if plat.startswith("win"):
        return "windows"
    if plat.startswith("linux"):
        return "linux"
    if plat == "darwin":
        return "darwin"
    return plat


def detect(
    os_name: str | None = None,
    arch: str | None = None,
) -> tuple[str, str, str, str]:
    """返回 (os, arch, zip_name, binary_name)。

    os_name/arch 留空时取真实机器(测试可直接传值,不依赖运行平台)。
    """
    os_name = os_name or _default_os()
    arch = canon_arch(arch or _platform.machine())
    zip_prefix = ZIPS.get((os_name, arch))
    if zip_prefix is None:
        raise UnsupportedPlatform(
            f"暂不支持的平台 {os_name}/{arch}:官方发布包仅含 "
            "windows-amd64 / linux-amd64 / darwin-arm64"
        )
    return os_name, arch, zip_prefix, BINARIES[os_name]
