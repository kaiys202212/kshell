"""cli 端到端测试:HTTP 与 spawn 全部打桩,文件走真实临时目录。

平台固定成 windows/amd64(cli 内引用 platform.detect,单测替换)。
"""

import hashlib
import io
import zipfile
from pathlib import Path

import pytest

from kshell_install import cli
from kshell_install.platform import UnsupportedPlatform

ZIP = "kshell-desktop-windows-amd64.zip"
TAG = "v0.1.27"
EXE_CONTENT = b"fakebin"


def real_zip_bytes(members):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        for name, data in members.items():
            zf.writestr(name, data)
    return buf.getvalue()


class FakeNet:
    def __init__(self):
        self.json_calls = []
        self.bytes_calls = []
        # 用"真 zip"当下载体,extract 才走完全流程。
        self.content = real_zip_bytes({"kshell-desktop.exe": EXE_CONTENT})
        self.zip_name = ZIP

    def get_json(self, url):
        self.json_calls.append(url)
        return {"tag_name": TAG, "assets": []}

    def get_bytes(self, url):
        self.bytes_calls.append(url)
        if "SHA256SUMS" in url:
            digest = hashlib.sha256(self.content).hexdigest()
            return f"{digest}  {self.zip_name}\n".encode()
        return self.content


class FakeSpawner:
    """记录 launch 调用参数,不真拉进程。"""

    def __init__(self):
        self.calls = []

    def __call__(self, *args, **kwargs):
        self.calls.append(args[-1])
        return self


@pytest.fixture
def env(monkeypatch, tmp_path):
    net = FakeNet()
    spawner = FakeSpawner()
    # 把"家目录"压到测试临时目录,任何流程都不会碰真实 ~/.kshell。
    monkeypatch.setattr(Path, "home", classmethod(lambda cls: tmp_path))
    # 用薄包装转发,后续替换 net.get_* 时 cli 仍能看到新实现。
    monkeypatch.setattr(cli, "get_json", lambda url: net.get_json(url))
    monkeypatch.setattr(cli, "get_bytes", lambda url: net.get_bytes(url))
    monkeypatch.setattr(cli.installer, "launch", spawner)
    # 平台固定 windows/amd64,与 FakeNet 的 zip 名一致。
    monkeypatch.setattr(cli, "detect", lambda: ("windows", "amd64", ZIP, "kshell-desktop.exe"))
    return net, spawner


def test_full_install_default(env, tmp_path, capsys):
    net, spawner = env
    rc = cli.main([])
    assert rc == 0
    out = capsys.readouterr().out
    assert TAG in out
    bin_exe = tmp_path / ".kshell" / "bin" / "kshell-desktop.exe"
    assert bin_exe.exists(), "默认应装到 ~ 下 .kshell/bin(tmp 中即 tmp_path)"
    assert bin_exe.read_bytes() == EXE_CONTENT
    # 临时下载文件应清理干净。
    leftovers = [p for p in tmp_path.rglob("*") if p.name.endswith(".zip") and ".kshell" not in p.parts]
    assert not leftovers
    assert len(spawner.calls) == 1 and str(spawner.calls[0]).endswith("kshell-desktop.exe")
    # JSON 查询走 GitCode 下行下载走补链 URL。
    assert any("api.gitcode.com" in u for u in net.json_calls)
    assert any("/attach_files/" in u for u in net.bytes_calls)


def test_bin_dir_override(env, tmp_path, capsys):
    net, spawner = env
    target = tmp_path / "custom" / "bin"
    rc = cli.main(["--bin-dir", str(target), "--no-run"])
    assert rc == 0
    assert (target / "kshell-desktop.exe").exists()
    assert spawner.calls == [], "--no-run 不应拉起 GUI"


def test_fixed_tag_skips_latest(env, tmp_path, capsys):
    net, _ = env
    rc = cli.main(["--tag", TAG, "--no-run"])
    assert rc == 0
    assert net.json_calls == [], "--tag 固定版本不应请求 latest API"
    out = capsys.readouterr().out
    assert TAG in out


def test_bad_zip_checksum_fails(env, tmp_path, capsys):
    net, _ = env
    net.get_bytes = lambda url: (b"0" * 64 + f"  {net.zip_name}\n".encode()) \
        if "SHA256SUMS" in url else net.content
    with pytest.raises(SystemExit) as ei:
        cli.main(["--no-run"])
    assert ei.value.code == 1
    err = capsys.readouterr().err
    assert "校验" in err or "SHA" in err or "sha256" in err.lower()


def test_unsupported_platform(monkeypatch, capsys):
    def boom():
        raise UnsupportedPlatform("freebsd/amd64")

    monkeypatch.setattr(cli, "detect", boom)
    with pytest.raises(SystemExit) as ei:
        cli.main([])
    assert ei.value.code == 1
    err = capsys.readouterr().err
    assert "freebsd" in err


def test_network_error_exits_1(monkeypatch, tmp_path, capsys):
    def broken_get_json(url):
        raise OSError("连接超时")

    monkeypatch.setattr(cli, "get_json", broken_get_json)
    monkeypatch.setattr(cli, "detect",
                        lambda: ("windows", "amd64", ZIP, "kshell-desktop.exe"))
    with pytest.raises(SystemExit) as ei:
        cli.main([])
    assert ei.value.code == 1
    assert "超时" in capsys.readouterr().err
