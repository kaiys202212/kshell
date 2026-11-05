"""installer 的测试:HTTP 用 fake bytes,文件系统用 pytest 临时目录。

darwin 的 launch 用 `open` 命令(不真实拉进程),windows/linux 的 spawn
通过注入桩函数验证参数,不打真实子进程。
"""

import hashlib
import subprocess
import zipfile
from pathlib import Path

import pytest

from kshell_install import installer
from kshell_install.installer import InstallError, extract, verify


def fake_get_bytes(urls):
    def get_bytes(url):
        return urls[url]

    return get_bytes


def make_zip(tmp_path, name, members):
    """members: zip 内成员名 -> 文件内容(bytes 或 marker 目录)。"""
    zip_path = tmp_path / name
    with zipfile.ZipFile(zip_path, "w") as zf:
        for member, content in members.items():
            zf.writestr(member, content)
    return zip_path


def sums_for(zip_path, name):
    digest = hashlib.sha256(zip_path.read_bytes()).hexdigest()
    return f"{digest}  {name}\n"


# ---------- download ----------


def test_download_writes_temp_file(tmp_path):
    content = b"PK\x03\x04fakezip"
    p = installer.download(fake_get_bytes({"u": content}), "u")
    try:
        assert p.exists()
        assert p.read_bytes() == content
    finally:
        p.unlink()


# ---------- verify ----------


def test_verify_ok(tmp_path):
    zip_path = make_zip(tmp_path, "z.zip", {"kshell-desktop.exe": b"bin"})
    # verify 内部需要先读文件再校验,这里手动重建(verify 约定吃 sums 文本)。
    body = sums_for(zip_path, "z.zip")
    verify(zip_path, body, "z.zip")  # 不抛即通过


def test_verify_mismatch_deletes_and_raises(tmp_path):
    zip_path = make_zip(tmp_path, "z.zip", {"kshell-desktop.exe": b"bin"})
    bad_body = ("deadbeef" + "0" * 56) + "  z.zip\n"
    with pytest.raises(InstallError):
        verify(zip_path, bad_body, "z.zip")
    assert not zip_path.exists(), "校验失败必须删除临时文件,不能留下未验证的文件"


def test_verify_sums_missing_zip_name(tmp_path):
    zip_path = make_zip(tmp_path, "z.zip", {"a": b"1"})
    with pytest.raises(InstallError):
        verify(zip_path, "0" * 64 + "  other.zip\n", "z.zip")
    assert not zip_path.exists(), "sums 里没有该条目时同样必须删掉未验证的包"


def test_verify_ignores_garbage_lines(tmp_path):
    zip_path = make_zip(tmp_path, "z.zip", {"kshell-desktop.exe": b"bin"})
    body = (
        "注释行不带哈希\n"
        + sums_for(zip_path, "z.zip")
        + "\n"
    )
    verify(zip_path, body, "z.zip")


# ---------- extract ----------


def test_extract_windows_binary(tmp_path):
    zip_path = make_zip(tmp_path, "kshell-desktop-windows-amd64.zip",
                        {"kshell-desktop.exe": b"MZbin"})
    bin_dir = tmp_path / "bin"
    p = extract(zip_path, bin_dir, "kshell-desktop.exe")
    assert p == bin_dir / "kshell-desktop.exe"
    assert p.read_bytes() == b"MZbin"


def test_extract_darwin_app_tree(tmp_path):
    zip_path = make_zip(tmp_path, "kshell-desktop-darwin-arm64.zip", {
        "kshell-desktop.app/Contents/MacOS/kshell-desktop": b"macho",
        "kshell-desktop.app/Contents/Info.plist": b"plist",
    })
    bin_dir = tmp_path / "bin"
    p = extract(zip_path, bin_dir, "kshell-desktop.app")
    assert p == bin_dir / "kshell-desktop.app"
    assert (p / "Contents/MacOS/kshell-desktop").read_bytes() == b"macho"


def test_extract_rejects_zip_slip(tmp_path):
    """成员路径含 .. 时不允许写出 bin 目录之外;全部成员危险时按缺入口报错。"""
    zip_path = tmp_path / "evil.zip"
    with zipfile.ZipFile(zip_path, "w") as zf:
        zf.writestr("../evil.exe", b"x")
    bin_dir = tmp_path / "bin"
    with pytest.raises(InstallError):
        extract(zip_path, bin_dir, "kshell-desktop.exe")
    # 危险成员被丢弃,bin 目录外不留任何文件。
    assert not (tmp_path / "evil.exe").exists()


def test_extract_rejects_absolute_member(tmp_path):
    """zip 内绝对路径成员(Windows 风格盘符 / posix 根路径)必须整条丢弃。"""
    zip_path = tmp_path / "abs.zip"
    with zipfile.ZipFile(zip_path, "w") as zf:
        zf.writestr("C:/x/evil.exe", b"w")
        zf.writestr("/abs/evil.exe", b"n")
    bin_dir = tmp_path / "bin"
    with pytest.raises(InstallError):
        extract(zip_path, bin_dir, "kshell-desktop.exe")
    # 绝对路径成员既不能落在 bin 内,也不能借 resolve 落到其他位置。
    assert not any("evil.exe" in p.name for p in tmp_path.rglob("*"))


def test_extract_rejects_sibling_prefix_member(tmp_path):
    """兄弟目录前缀撞车:bin2 与 bin 共享字符串前缀,但 bin2 不在 bin 内。"""
    zip_path = tmp_path / "sibling.zip"
    with zipfile.ZipFile(zip_path, "w") as zf:
        zf.writestr("../bin2/x.exe", b"x")
    bin_dir = tmp_path / "bin"
    with pytest.raises(InstallError):
        extract(zip_path, bin_dir, "kshell-desktop.exe")
    assert not (tmp_path / "bin2").exists(), "兄弟目录成员不得写出"


def test_extract_missing_binary_raises(tmp_path):
    zip_path = make_zip(tmp_path, "good.zip", {"other.txt": b"x"})
    with pytest.raises(InstallError):
        extract(zip_path, tmp_path / "bin", "kshell-desktop.exe")


def test_extract_linux_sets_executable(tmp_path, monkeypatch):
    """让 installer 以"linux"视角运行;Windows 的 chmod 不能真置执行位,改打桩验证。"""
    zip_path = make_zip(tmp_path, "kshell-desktop-linux-amd64.zip",
                        {"kshell-desktop": b"elf"})
    bin_dir = tmp_path / "bin"
    chmod_calls = []

    def fake_chmod(self, mode):
        chmod_calls.append(mode)

    monkeypatch.setattr(installer.Path, "chmod", fake_chmod)
    monkeypatch.setattr(installer.sys, "platform", "linux")
    extract(zip_path, bin_dir, "kshell-desktop")
    assert any(m & 0o111 == 0o111 for m in chmod_calls), "应补上可执行位"


# ---------- launch ----------


class FakePopen:
    calls = []

    def __call__(self, args, **kwargs):
        FakePopen.calls.append((args, kwargs))
        return self


@pytest.fixture(autouse=True)
def reset_calls():
    FakePopen.calls = []


def test_launch_windows():
    spawn = FakePopen()
    p = Path(r"D:/bin/kshell-desktop.exe")
    installer.launch("windows", p, spawn=spawn)
    args, kwargs = FakePopen.calls[0]
    assert args == [str(p.resolve())]
    # 分离进程组 + 不弹黑框;脚本退出后进程仍活着。
    assert kwargs["creationflags"] == (
        subprocess.DETACHED_PROCESS
        | subprocess.CREATE_NEW_PROCESS_GROUP
        | subprocess.CREATE_NO_WINDOW
    )
    assert kwargs["close_fds"] is True


def test_launch_linux():
    spawn = FakePopen()
    p = Path("/home/u/.kshell/bin/kshell-desktop")
    installer.launch("linux", p, spawn=spawn)
    args, kwargs = FakePopen.calls[0]
    assert args == [str(p.resolve())]
    assert kwargs["start_new_session"] is True


def test_launch_spawn_failure_wrapped(tmp_path):
    """spawn 抛 OSError(如二进制缺失/权限)时包装为中文 InstallError,不裸 traceback。"""

    def broken_spawn(args, **kwargs):
        raise FileNotFoundError(2, "系统找不到指定的文件", args[0])

    p = Path(r"D:/bin/kshell-desktop.exe")
    with pytest.raises(InstallError) as ei:
        installer.launch("windows", p, spawn=broken_spawn)
    assert "无法启动" in str(ei.value)


def test_launch_darwin_uses_open():
    spawn = FakePopen()
    p = Path("/Users/u/.kshell/bin/kshell-desktop.app")
    installer.launch("darwin", p, spawn=spawn)
    args, _ = FakePopen.calls[0]
    assert args == ["open", str(p.resolve())]
