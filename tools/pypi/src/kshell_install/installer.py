"""下载 → SHA-256 校验 → 解压到 ~/.kshell/bin → 拉起 GUI 的安装主流程。

依赖全部可注入:get_bytes(HTTP)、spawn(进程拉起)、文件系统走标准库,
测试用 fake + pytest 临时目录覆盖,不需要真实联网。
"""

from __future__ import annotations

import hashlib
import os
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path
from typing import Callable

#: get_bytes(url) -> 正文 bytes(注:全文读取;计算完整性校验本来就要全量,不值得引入分片协议)。
GetBytes = Callable[[str], bytes]

#: spawn(args, **kwargs) -> Popen 的注入协议。
Spawn = Callable[..., object]


class InstallError(Exception):
    """安装过程中任意一步失败(校验不匹配/zip 损坏/二进制缺失等)。"""


def download(get_bytes: GetBytes, url: str) -> Path:
    """下载到系统临时目录的文件中;返回路径,由调用方在用毕删除。"""
    try:
        data = get_bytes(url)
    except Exception as exc:
        raise InstallError(f"下载失败 {url}:{exc}") from exc
    fd, name = tempfile.mkstemp(suffix=".zip", prefix="kshell-install-")
    with Path(name).open("wb") as f:
        f.write(data)
    os.close(fd)  # mkstemp 的底层句柄显式关闭,否则 Windows 上后续 unlink 会失败。
    return Path(name)


def verify(zip_path: Path, sums_text: str, zip_name: str) -> None:
    """校验 zip 的 sha256 是否与 SHA256SUMS 中同名条目一致。

    不匹配(或 sums 里根本没有该文件)时删除临时 zip 并抛错——
    绝不让一个未验证的安装包解压到用户目录。
    """
    # 每行 `<hex>  <filename>`;双空格分隔是 sha256sum 标准格式,兜底单空格。
    expected = ""
    for line in sums_text.strip().splitlines():
        line = line.strip()
        if not line or len(line) < 66:
            continue
        head, _, rest = line.partition("  ")
        if not rest:
            head, _, rest = line.partition(" ")
        if not rest or " " in rest:
            continue  # 带空格的文件名不在本仓库的发布约定里,跳过。
        if rest.strip().lstrip("*") == zip_name:
            expected = head.strip().lower()
    if not expected:
        raise InstallError(f"SHA256SUMS 中没有 {zip_name} 的校验项")
    digest = hashlib.sha256()
    with zip_path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            digest.update(chunk)
    if digest.hexdigest() != expected:
        zip_path.unlink(missing_ok=True)
        raise InstallError(
            f"SHA-256 校验不匹配:下载的 {zip_name} 已删除,请重试或手动下载"
        )


def extract(zip_path: Path, bin_dir: Path, binary_name: str) -> Path:
    """解压 zip 到 bin_dir,返回其中应作为入口的路径(二进制或 .app 目录)。

    做最基础的 zip-slip 防护:成员路径解析后必须仍在 bin_dir 内。
    """
    bin_dir.mkdir(parents=True, exist_ok=True)
    resolved_root = bin_dir.resolve()
    with zipfile.ZipFile(zip_path) as zf:
        for info in zf.infolist():
            target = (bin_dir / info.filename).resolve()
            if not str(target).startswith(str(resolved_root)):
                continue  # 危险成员(../、绝对路径)直接丢弃,不写出根目录。
            zf.extract(info, bin_dir)
    extracted = bin_dir / binary_name
    if not extracted.exists():
        raise InstallError(
            f"zip 内没有预期的入口 {binary_name},解压已在 {bin_dir} 但内容异常"
        )
    # linux 裸二进制需要可执行位;macOS 的 .app 里 Mach-O 也一并补上。
    if sys.platform != "win32":
        if extracted.is_file() or binary_name == "kshell-desktop.app":
            for p in ([extracted] if extracted.is_file() else extracted.rglob("*")):
                if p.is_file():
                    p.chmod(p.stat().st_mode | 0o111)
    return extracted


def _default_spawn(args, **kwargs):
    """真实的进程拉起;launch 会按平台补好分离参数。"""
    return subprocess.Popen(args, **kwargs)


def launch(os_name: str, binary_path: Path, spawn: Spawn | None = None) -> object:
    """异步拉起 GUI:脚本退出后进程必须在(分离式启动,不等它)。

    - windows:DETACHED_PROCESS|CREATE_NEW_PROCESS_GROUP|CREATE_NO_WINDOW,
      不留控制台窗口;cwd 归到用户目录避免继承安装器所在盘符的 cwd。
    - linux:start_new_session=True 脱离会话组。
    - darwin:`open <app>` 本身立即返回。
    """
    spawn = spawn or _default_spawn
    target = str(binary_path.resolve())
    if os_name == "windows":
        return spawn(
            [target],
            cwd=str(Path.home()),
            creationflags=subprocess.DETACHED_PROCESS
            | subprocess.CREATE_NEW_PROCESS_GROUP
            | subprocess.CREATE_NO_WINDOW,
            close_fds=True,
        )
    if os_name == "linux":
        return spawn([target], cwd=str(Path.home()), start_new_session=True)
    if os_name == "darwin":
        return spawn(["open", target])
    raise InstallError(f"未知平台 {os_name}")


def latest(os_name: str, bin_dir: Path) -> Path:
    """按平台给出"装好之后"的入口路径(不触文件系统,给调用方/测试参考)。"""
    from kshell_install.platform import BINARIES

    return bin_dir / BINARIES[os_name]
