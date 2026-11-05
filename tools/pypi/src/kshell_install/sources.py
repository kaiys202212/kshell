"""latest release 发现:GitCode 国内主源优先,GitHub 兜底。

两个源都暴露 Gitee v5 风格 API(tag_name + assets[].{name,browser_download_url})。
跨源取 semver 最大的 tag,防止某个镜像缓存滞后导致"一次只升一个小版本"。

GitCode 附件常常不在 assets 里,但附件下载链接只依赖 tag 即可构造:
/releases/<tag>/attach_files/<fileName>/download(公开仓库免登录),
所以 GitCode 源上的 zip/sums URL 一定可构造,不必因 assets 缺失而抛弃该候选。
"""

from __future__ import annotations

import json
from typing import Callable

GITCODE_OWNER = "abraveheart2023"
GITCODE_REPO = "kshell"
GITHUB_OWNER = "kaiys202212"
GITHUB_REPO = "kshell"

GITCODE_LATEST_URL = (
    f"https://api.gitcode.com/api/v5/repos/{GITCODE_OWNER}/{GITCODE_REPO}/releases/latest"
)
GITHUB_LATEST_URL = (
    f"https://api.github.com/repos/{GITHUB_OWNER}/{GITHUB_REPO}/releases/latest"
)

SUMS_NAME = "SHA256SUMS"

#: get_json(url) -> release JSON 的注入协议(测试用 fake,生产走 urllib)。
GetJSON = Callable[[str], dict]

# 候选按源顺序产生;同名 tag 保留先到者(源链靠前者优先)。
_SOURCES = (("gitcode", GITCODE_LATEST_URL), ("github", GITHUB_LATEST_URL))


def gitcode_attach_url(tag: str, file_name: str) -> str:
    """GitCode release 附件公开下载链,只依赖 tag。"""
    return (
        f"https://api.gitcode.com/api/v5/repos/{GITCODE_OWNER}/{GITCODE_REPO}"
        f"/releases/{tag}/attach_files/{file_name}/download"
    )


class ReleaseError(Exception):
    """所有源都取不到可用发布(网络失败或缺 zip 附件)。"""


def canon(tag: str) -> str:
    """把 `0.1.27`、` v0.1.27 ` 统一成 `v0.1.27`;口径与 Go 侧 canon 一致。"""
    t = (tag or "").strip()
    if t and not t.startswith("v"):
        t = "v" + t
    return t


def _parse(tag: str):
    """tag → 可比较的排序键;非法返回 None。

    遵循 semver:核心 = 主.次.补 三个数字段;
    预发布(-beta.1)低于同核心的正式版;预发布段数字比字母"更小"且纯比数字。
    """
    t = canon(tag)
    if not t.startswith("v"):
        return None
    core, _, pre = t[1:].partition("-")
    nums: list[int] = []
    for part in core.split("."):
        try:
            nums.append(int(part))
        except ValueError:
            return None
    if len(nums) != 3:
        return None
    pre_key: tuple[tuple[int, int, str], ...] = ()
    if pre:
        for ident in pre.split("."):
            if ident.isdigit():
                pre_key += ((0, int(ident), ""),)
            else:
                pre_key += ((1, 0, ident),)
    else:
        pre_key = ((2, 0, ""),)  # 正式版永远高于任何预发布
    # 排序键 = 核心数字 + 逐段三元组;正式版的 (2,…) 段天然压过 (0/1,…)。
    return (tuple(nums), pre_key)


def _compare_tags(a: str, b: str) -> int:
    ka, kb = _parse(a), _parse(b)
    if ka is None and kb is None:
        return 0
    if ka is None:
        return -1
    if kb is None:
        return 1
    if ka == kb:
        return 0
    return -1 if ka < kb else 1


def latest_release(get_json: GetJSON, want_zip: str) -> tuple[str, str, str]:
    """遍历所有源,返回 (tag, zip_url, sums_url)。

    优先级:tag 严格更大者胜;并列时先到者(GitCode)保留。
    全部源失败/均缺 zip 附件 → ReleaseError。
    """
    best: tuple[str, str, str] | None = None
    last_err: Exception | None = None
    for name, url in _SOURCES:
        try:
            rel = get_json(url)
        except Exception as exc:  # 网络层失败:换下一个源。
            last_err = ReleaseError(f"{name} 查询失败:{exc}")
            continue
        try:
            tag = (rel.get("tag_name") or "").strip()
            assets = rel.get("assets") or []
        except AttributeError:
            last_err = ReleaseError(f"{name} 返回的不是 release JSON")
            continue
        zip_url = sums_url = ""
        for asset in assets:
            asset_name = asset.get("name")
            if asset_name == want_zip:
                zip_url = asset.get("browser_download_url") or ""
            elif asset_name == SUMS_NAME:
                sums_url = asset.get("browser_download_url") or ""
        # GitCode 附件缺失时用 attach 链补,只依赖 tag。
        if name == "gitcode" and tag:
            if not zip_url:
                zip_url = gitcode_attach_url(tag, want_zip)
            if not sums_url:
                sums_url = gitcode_attach_url(tag, SUMS_NAME)
        if not zip_url:
            last_err = ReleaseError(f"{name} 发布 {tag or '?'} 缺少附件 {want_zip}")
            continue
        if best is None or _compare_tags(tag, best[0]) > 0:
            best = (tag, zip_url, sums_url)
    if best is None:
        raise last_err or ReleaseError("没有可用的发布源")
    return best
