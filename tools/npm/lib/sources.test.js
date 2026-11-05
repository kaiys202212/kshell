"use strict";
import { describe, expect, it } from "vitest";

const {
  GITCODE_LATEST_URL,
  GITHUB_LATEST_URL,
  SUMS_NAME,
  ReleaseError,
  canon,
  compareTags,
  gitcodeAttachUrl,
  latestRelease,
} = require("./sources");

const ZIP = "kshell-desktop-windows-amd64.zip";

/** 造一个 release JSON;无 browser_download_url 时表示附件缺失。 */
function rel(tag, { zip = true, sums = true, noUrl = false } = {}) {
  const assets = [];
  if (zip) assets.push({ name: ZIP, browser_download_url: noUrl ? "" : `https://dl/${tag}/${ZIP}` });
  if (sums)
    assets.push({ name: SUMS_NAME, browser_download_url: noUrl ? "" : `https://dl/${tag}/SHA256SUMS` });
  return { tag_name: tag, assets };
}

describe("canon", () => {
  it("补 v 前缀并去空白", () => {
    expect(canon("0.1.27")).toBe("v0.1.27");
    expect(canon(" v0.1.27 ")).toBe("v0.1.27");
    expect(canon("")).toBe("");
  });
});

describe("compareTags 手写 semver", () => {
  it("核心数字逐段比较", () => {
    expect(compareTags("v0.1.27", "v0.1.26")).toBe(1);
    expect(compareTags("v0.2.0", "v0.1.99")).toBe(1);
    expect(compareTags("v1.0.0", "v0.9.9")).toBe(1);
    expect(compareTags("v0.1.27", "v0.1.27")).toBe(0);
    expect(compareTags("v0.1.3", "v0.1.27")).toBe(-1); // 数字比较不是字符串比较
  });
  it("预发布低于同核心正式版", () => {
    expect(compareTags("v1.0.0-beta.1", "v1.0.0")).toBe(-1);
    expect(compareTags("v1.0.0-beta.2", "v1.0.0-beta.1")).toBe(1);
    expect(compareTags("v1.0.0-rc.1", "v1.0.0-beta.9")).toBe(1); // rc > beta(字母比大小)
    expect(compareTags("v1.0.0-2", "v1.0.0-1")).toBe(1); // 预发布段纯数字比较
  });
  it("非法 tag 一律低于合法 tag", () => {
    expect(compareTags("release-2024", "v0.0.1")).toBe(-1);
  });
});

describe("latestRelease 源顺序与兜底", () => {
  it("两个源都带上:按 semver 取最大 tag", async () => {
    const calls = [];
    const getJson = async (url) => {
      calls.push(url);
      if (url === GITCODE_LATEST_URL) return rel("v0.1.20");
      return rel("v0.1.27");
    };
    const out = await latestRelease(getJson, ZIP);
    expect(calls).toEqual([GITCODE_LATEST_URL, GITHUB_LATEST_URL]); // 顺序遍历
    expect(out.tag).toBe("v0.1.27");
    expect(out.zipUrl).toBe("https://dl/v0.1.27/" + ZIP);
    expect(out.sumsUrl).toBe("https://dl/v0.1.27/SHA256SUMS");
  });

  it("tag 并列时先到者(GitCode)保留", async () => {
    const getJson = async (url) => rel(url === GITCODE_LATEST_URL ? "v0.1.27" : "v0.1.27");
    const out = await latestRelease(getJson, ZIP);
    expect(out.zipUrl).toBe("https://dl/v0.1.27/" + ZIP);
  });

  it("GitCode 失败 → GitHub 兜底", async () => {
    const getJson = async (url) => {
      if (url === GITCODE_LATEST_URL) throw new Error("connection reset");
      return rel("v0.1.26");
    };
    const out = await latestRelease(getJson, ZIP);
    expect(out.tag).toBe("v0.1.26");
  });

  it("GitCode assets 缺 zip → attach 链补全(zip 和 sums 都补)", async () => {
    const getJson = async (url) => {
      if (url === GITCODE_LATEST_URL) return { tag_name: "v0.1.27", assets: [] };
      throw new Error("github down");
    };
    const out = await latestRelease(getJson, ZIP);
    expect(out.zipUrl).toBe(gitcodeAttachUrl("v0.1.27", ZIP));
    expect(out.sumsUrl).toBe(gitcodeAttachUrl("v0.1.27", SUMS_NAME));
  });

  it("GitCode 只补缺失项,已有 browser_download_url 不覆盖", async () => {
    const getJson = async (url) => {
      if (url === GITCODE_LATEST_URL)
        return {
          tag_name: "v0.1.27",
          assets: [{ name: ZIP, browser_download_url: "https://gitcode-real/zip" }], // 无 sums
        };
      throw new Error("github down");
    };
    const out = await latestRelease(getJson, ZIP);
    expect(out.zipUrl).toBe("https://gitcode-real/zip");
    expect(out.sumsUrl).toBe(gitcodeAttachUrl("v0.1.27", SUMS_NAME));
  });

  it("GitHub assets 缺 zip 时不补链(只对 GitCode 生效)", async () => {
    const getJson = async (url) => {
      if (url === GITCODE_LATEST_URL) throw new Error("down");
      return { tag_name: "v0.1.27", assets: [{ name: SUMS_NAME, browser_download_url: "https://s" }] };
    };
    await expect(latestRelease(getJson, ZIP)).rejects.toThrow(ReleaseError);
    await expect(latestRelease(getJson, ZIP)).rejects.toThrow(/缺少附件/);
  });

  it("全部源失败 → ReleaseError(带最后一个错误)", async () => {
    const getJson = async () => {
      throw new Error("boom");
    };
    await expect(latestRelease(getJson, ZIP)).rejects.toThrow(ReleaseError);
    await expect(latestRelease(getJson, ZIP)).rejects.toThrow(/boom/);
  });

  it("返回非 JSON(如防刷页面)→ 换源", async () => {
    const getJson = async (url) => (url === GITCODE_LATEST_URL ? "<html>rate limit</html>" : rel("v0.0.9"));
    const out = await latestRelease(getJson, ZIP);
    expect(out.tag).toBe("v0.0.9");
  });
});
