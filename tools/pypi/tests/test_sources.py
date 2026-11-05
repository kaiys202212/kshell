"""sources.latest_release 的测试:两个 latest API 的响应全部用 fake 注入。"""

import pytest

from kshell_install import sources
from kshell_install.sources import latest_release

GITCODE_LATEST = sources.GITCODE_LATEST_URL
GITHUB_LATEST = sources.GITHUB_LATEST_URL
ZIP = "kshell-desktop-windows-amd64.zip"


def make_get_json(responses, fail=()):
    """responses: url -> JSON 可序列化对象;fail: 抛异常的 url 集合。"""
    fail = set(fail)

    def get_json(url):
        if url in fail:
            raise OSError("网络错误")
        return responses[url]

    return get_json


def release(tag, assets):
    return {"tag_name": tag, "assets": assets}


ASSETS_FULL = [
    {"name": ZIP, "browser_download_url": f"https://github.com/kaiys202212/kshell/releases/download/v0.1.27/{ZIP}"},
    {"name": "SHA256SUMS", "browser_download_url": "https://github.com/kaiys202212/kshell/releases/download/v0.1.27/SHA256SUMS"},
]


def test_gitcode_priority():
    """两源 tag 并列时保留先到者(GitCode),zip 取其 assets 视为 url。"""
    tag, zip_url, sums_url = latest_release(
        make_get_json({
            GITCODE_LATEST: release("v0.1.27", [
                {"name": ZIP, "browser_download_url": "https://gitcode.example/asset.zip"},
                {"name": "SHA256SUMS", "browser_download_url": "https://gitcode.example/SHA256SUMS"},
            ]),
            GITHUB_LATEST: release("v0.1.27", ASSETS_FULL),
        }),
        ZIP,
    )
    assert tag == "v0.1.27"
    assert zip_url == "https://gitcode.example/asset.zip"
    assert sums_url == "https://gitcode.example/SHA256SUMS"


def test_github_fallback():
    """GitCode 请求失败时用 GitHub。"""
    tag, zip_url, _ = latest_release(
        make_get_json({GITHUB_LATEST: release("v0.1.26", ASSETS_FULL)}, fail=(GITCODE_LATEST,)),
        ZIP,
    )
    assert tag == "v0.1.26"
    assert zip_url == ASSETS_FULL[0]["browser_download_url"]


def test_gitcode_attach_backfill():
    """GitCode 响应缺附件时,zip 与 sums 都用 attach_files URL 补链(只依赖 tag)。"""
    tag, zip_url, sums_url = latest_release(
        make_get_json({GITCODE_LATEST: release("v0.1.27", [])}),
        ZIP,
    )
    assert tag == "v0.1.27"
    assert zip_url == sources.gitcode_attach_url("v0.1.27", ZIP)
    assert sums_url == sources.gitcode_attach_url("v0.1.27", "SHA256SUMS")


def test_cross_source_max_tag():
    """镜像缓存滞后时,取两源中 semver 最大的 tag。"""
    tag, _, _ = latest_release(
        make_get_json({
            GITCODE_LATEST: release("v0.1.25", []),
            GITHUB_LATEST: release("v0.1.27", ASSETS_FULL),
        }),
        ZIP,
    )
    assert tag == "v0.1.27"
    # 对应 zip 必须来自持有新版本的 GitHub 源。
    _, zip_url, _ = latest_release(
        make_get_json({
            GITCODE_LATEST: release("v0.1.25", []),
            GITHUB_LATEST: release("v0.1.27", ASSETS_FULL),
        }),
        ZIP,
    )
    assert zip_url == ASSETS_FULL[0]["browser_download_url"]


def test_gitcode_newer_wins():
    """GitCode 版本更新时回到 GitCode(含补链)。"""
    tag, zip_url, _ = latest_release(
        make_get_json({
            GITCODE_LATEST: release("v0.1.28", []),
            GITHUB_LATEST: release("v0.1.27", ASSETS_FULL),
        }),
        ZIP,
    )
    assert tag == "v0.1.28"
    assert zip_url == sources.gitcode_attach_url("v0.1.28", ZIP)


def test_both_fail_raises():
    with pytest.raises(Exception):
        latest_release(make_get_json({}, fail=(GITCODE_LATEST, GITHUB_LATEST)), ZIP)


def test_github_missing_asset_raises():
    """GitHub 兜底但缺 zip 附件(无法构造补链)时报错。"""
    with pytest.raises(Exception):
        latest_release(
            make_get_json({GITHUB_LATEST: release("v0.1.27", [])}, fail=(GITCODE_LATEST,)),
            ZIP,
        )


def test_canon_and_compare():
    assert sources.canon("0.1.27") == "v0.1.27"
    assert sources.canon(" v0.1.27 ") == "v0.1.27"
    assert sources._compare_tags("v0.1.28", "v0.1.27") > 0
    assert sources._compare_tags("v0.2.0", "v0.1.99") > 0
    assert sources._compare_tags("v1.0.0", "v0.9.9") > 0
    assert sources._compare_tags("v0.1.27", "v0.1.27") == 0
    assert sources._compare_tags("v0.1.28-beta.1", "v0.1.28") < 0
