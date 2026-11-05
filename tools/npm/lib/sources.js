"use strict";
/**
 * latest release 发现:GitCode 国内主源优先,GitHub 兜底。
 *
 * 两个源都暴露 Gitee v5 风格 API(tag_name + assets[].{name,browser_download_url})。
 * 跨源取 semver 最大的 tag,防止某个镜像缓存滞后导致"一次只升一个小版本"。
 *
 * GitCode 附件常常不在 assets 里,但附件下载链接只依赖 tag 即可构造:
 * /releases/<tag>/attach_files/<fileName>/download(公开仓库免登录),
 * 所以 GitCode 源上的 zip/sums URL 一定可构造,不必因 assets 缺失而抛弃该候选。
 */

const GITCODE_OWNER = "abraveheart2023";
const GITCODE_REPO = "kshell";
const GITHUB_OWNER = "kaiys202212";
const GITHUB_REPO = "kshell";

const GITCODE_LATEST_URL = `https://api.gitcode.com/api/v5/repos/${GITCODE_OWNER}/${GITCODE_REPO}/releases/latest`;
const GITHUB_LATEST_URL = `https://api.github.com/repos/${GITHUB_OWNER}/${GITHUB_REPO}/releases/latest`;

const SUMS_NAME = "SHA256SUMS";

// 候选按源顺序产生;同名 tag 保留先到者(源链靠前者优先)。
const SOURCES = [
  ["gitcode", GITCODE_LATEST_URL],
  ["github", GITHUB_LATEST_URL],
];

class ReleaseError extends Error {
  constructor(message) {
    super(message);
    this.name = "ReleaseError";
  }
}

/** GitCode release 附件公开下载链,只依赖 tag。 */
function gitcodeAttachUrl(tag, fileName) {
  return `https://api.gitcode.com/api/v5/repos/${GITCODE_OWNER}/${GITCODE_REPO}/releases/${tag}/attach_files/${fileName}/download`;
}

/** 把 `0.1.27`、` v0.1.27 ` 统一成 `v0.1.27`;口径与 Go 侧 canon 一致。 */
function canon(tag) {
  let t = String(tag || "").trim();
  if (t && !t.startsWith("v")) t = `v${t}`;
  return t;
}

/**
 * tag → 可比较的排序键;非法返回 null。
 *
 * 遵循 semver:核心 = 主.次.补 三个数字段;
 * 预发布(-beta.1)低于同核心的正式版;预发布段数字比字母"更小"且纯比数字。
 * 手写解析,避免引入 semver 依赖(尽力而为,预发布低于正式)。
 */
function parseTag(tag) {
  const t = canon(tag);
  if (!t.startsWith("v")) return null;
  const dash = t.indexOf("-");
  const coreStr = dash === -1 ? t.slice(1) : t.slice(1, dash);
  const preStr = dash === -1 ? "" : t.slice(dash + 1);
  const parts = coreStr.split(".");
  const nums = [];
  for (const part of parts) {
    if (!/^\d+$/.test(part)) return null;
    nums.push(parseInt(part, 10));
  }
  if (nums.length !== 3) return null;
  const preKey = [];
  if (preStr) {
    for (const ident of preStr.split(".")) {
      if (/^\d+$/.test(ident)) preKey.push([0, parseInt(ident, 10), ""]);
      else preKey.push([1, 0, ident]);
    }
  } else {
    preKey.push([2, 0, ""]); // 正式版永远高于任何预发布
  }
  return { nums, preKey };
}

function comparePreKey(a, b) {
  const n = Math.max(a.length, b.length);
  for (let i = 0; i < n; i++) {
    const x = a[i] || [0, 0, ""];
    const y = b[i] || [0, 0, ""];
    if (x[0] !== y[0]) return x[0] < y[0] ? -1 : 1;
    if (x[1] !== y[1]) return x[1] < y[1] ? -1 : 1;
    if (x[2] !== y[2]) return x[2] < y[2] ? -1 : 1;
  }
  return 0;
}

/** tag 比较:-1/0/1;非法 tag 一律低于合法 tag。 */
function compareTags(a, b) {
  const ka = parseTag(a);
  const kb = parseTag(b);
  if (!ka && !kb) return 0;
  if (!ka) return -1;
  if (!kb) return 1;
  for (let i = 0; i < 3; i++) {
    if (ka.nums[i] !== kb.nums[i]) return ka.nums[i] < kb.nums[i] ? -1 : 1;
  }
  return comparePreKey(ka.preKey, kb.preKey);
}

/**
 * 遍历所有源,返回 {tag, zipUrl, sumsUrl}。
 *
 * 优先级:tag 严格更大者胜;并列时先到者(GitCode)保留。
 * 全部源失败/均缺 zip 附件 → ReleaseError。
 * getJson(url) 返回 Promise<release JSON>(生产走 fetch,测试用 fake)。
 */
async function latestRelease(getJson, zipName) {
  let best = null;
  let lastErr = null;
  for (const [name, url] of SOURCES) {
    let rel;
    try {
      rel = await getJson(url);
    } catch (exc) {
      // 网络层失败:换下一个源。
      lastErr = new ReleaseError(`${name} 查询失败:${exc && exc.message ? exc.message : exc}`);
      continue;
    }
    if (!rel || typeof rel !== "object") {
      lastErr = new ReleaseError(`${name} 返回的不是 release JSON`);
      continue;
    }
    const tag = String(rel.tag_name || "").trim();
    const assets = Array.isArray(rel.assets) ? rel.assets : [];
    let zipUrl = "";
    let sumsUrl = "";
    for (const asset of assets) {
      if (!asset) continue;
      if (asset.name === zipName) zipUrl = asset.browser_download_url || "";
      else if (asset.name === SUMS_NAME) sumsUrl = asset.browser_download_url || "";
    }
    // GitCode 附件缺失时用 attach 链补,只依赖 tag。
    if (name === "gitcode" && tag) {
      if (!zipUrl) zipUrl = gitcodeAttachUrl(tag, zipName);
      if (!sumsUrl) sumsUrl = gitcodeAttachUrl(tag, SUMS_NAME);
    }
    if (!zipUrl) {
      lastErr = new ReleaseError(`${name} 发布 ${tag || "?"} 缺少附件 ${zipName}`);
      continue;
    }
    if (best === null || compareTags(tag, best.tag) > 0) {
      best = { tag, zipUrl, sumsUrl };
    }
  }
  if (best === null) {
    throw lastErr || new ReleaseError("没有可用的发布源");
  }
  return best;
}

module.exports = {
  GITCODE_LATEST_URL,
  GITHUB_LATEST_URL,
  SUMS_NAME,
  SOURCES,
  ReleaseError,
  gitcodeAttachUrl,
  canon,
  compareTags,
  latestRelease,
};
