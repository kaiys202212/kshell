"""platform.detect 的映射测试(不触网、不依赖真实运行平台)。"""

import pytest

from kshell_install.platform import UnsupportedPlatform, detect


def test_windows_amd64():
    assert detect(os_name="windows", arch="amd64") == (
        "windows",
        "amd64",
        "kshell-desktop-windows-amd64.zip",
        "kshell-desktop.exe",
    )


def test_linux_amd64():
    assert detect(os_name="linux", arch="amd64") == (
        "linux",
        "amd64",
        "kshell-desktop-linux-amd64.zip",
        "kshell-desktop",
    )


def test_darwin_arm64():
    assert detect(os_name="darwin", arch="arm64") == (
        "darwin",
        "arm64",
        "kshell-desktop-darwin-arm64.zip",
        "kshell-desktop.app",
    )


# 矩阵外的组合一律不支持。
UNSUPPORTED = [
    ("windows", "arm64"),
    ("linux", "arm64"),
    ("darwin", "amd64"),
    ("freebsd", "amd64"),
]


@pytest.mark.parametrize("os_name,arch", UNSUPPORTED)
def test_unsupported_raises(os_name, arch):
    with pytest.raises(UnsupportedPlatform):
        detect(os_name=os_name, arch=arch)


def test_arch_normalize():
    # 常见变体写法应归一化。
    assert detect(os_name="windows", arch="AMD64")[1] == "amd64"
    assert detect(os_name="darwin", arch="aarch64")[1] == "arm64"
