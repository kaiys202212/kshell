"use strict";
/**
 * 下载 → SHA-256 校验 → 解压到 ~/.kshell/bin → 拉起 GUI 的安装主流程。
 *
 * 依赖全部可注入:getJson/getBytes/getStream(HTTP)、runExtract(解压命令)、
 * spawnFn(进程拉起),测试用 fake + 临时目录覆盖,不需要真实联网。
 *
 * 取舍说明:zip 已过 SHA-256 校验、内容可信,故直接解到目标 bin 目录;
 * 不采用"临时目录 → 拷贝"两步法,失败时重复运行整包覆盖即可自愈
 * (unzip -o / tar -xf 本身按归档内布局落文件,不会留半成品入口)。
 *
 * 解压信任模型:sha256 校验 + 系统 tar/bsdtar——系统 tar 默认拒绝绝对路径与
 * `..` 成员,故未像 Python 版那样自己做进程内 zip-slip 防护(文件越是往下游
 * 解压环节靠,重复防护的收益越低)。
 */

const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const crypto = require("node:crypto");
const cp = require("node:child_process");
const { Readable } = require("node:stream");
const { pipeline } = require("node:stream/promises");

const sources = require("./sources");

const USER_AGENT = "kshell-install";
const SUMS_NAME = "SHA256SUMS";

class InstallError extends Error {
  constructor(message) {
    super(message);
    this.name = "InstallError";
  }
}

/** 生产下载实现:内置 fetch 流式把响应体写进目标文件(避免整包载入内存)。 */
async function defaultGetStream(url, destPath) {
  let res;
  try {
    res = await fetch(url, { headers: { "User-Agent": USER_AGENT }, redirect: "follow" });
  } catch (exc) {
    throw new InstallError(`下载失败 ${url}:${exc && exc.message ? exc.message : exc}`);
  }
  if (!res.ok) throw new InstallError(`下载失败 ${url}:HTTP ${res.status}`);
  if (!res.body) throw new InstallError(`下载失败 ${url}:响应没有内容`);
  await pipeline(Readable.fromWeb(res.body), fs.createWriteStream(destPath));
}

/** 生产文本读取实现(用于 SHA256SUMS 这类小文件)。 */
async function defaultGetBytes(url) {
  let res;
  try {
    res = await fetch(url, { headers: { "User-Agent": USER_AGENT }, redirect: "follow" });
  } catch (exc) {
    throw new InstallError(`下载失败 ${url}:${exc && exc.message ? exc.message : exc}`);
  }
  if (!res.ok) throw new InstallError(`下载失败 ${url}:HTTP ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

/**
 * 下载到系统临时目录的文件中;返回路径,由调用方在用毕删除。
 * getStream(url, destPath) 注入:生产走 fetch 流式写盘,测试直接拷文件。
 */
async function download(url, getStream = defaultGetStream) {
  const tmpPath = path.join(
    os.tmpdir(),
    `kshell-install-${Date.now()}-${Math.random().toString(36).slice(2)}.zip`
  );
  try {
    await getStream(url, tmpPath);
  } catch (exc) {
    try {
      fs.unlinkSync(tmpPath);
    } catch {
      /* 临时文件本来就是刚建的,清不干净也不致命 */
    }
    throw exc instanceof InstallError
      ? exc
      : new InstallError(`下载失败 ${url}:${exc && exc.message ? exc.message : exc}`);
  }
  return tmpPath;
}

/**
 * 校验 zip 的 sha256 是否与 SHA256SUMS 中同名条目一致。
 *
 * 不匹配(或 sums 里根本没有该文件)时删除临时 zip 并抛错——
 * 绝不让一个未验证的安装包解压到用户目录。
 */
function verify(zipPath, sumsText, zipName) {
  // 每行 `<hex>  <filename>`;双空格分隔是 sha256sum 标准格式,兜底单空格。
  // hex 段用定长 64 位十六进制硬校验,格式不对的行整行跳过。
  let expected = "";
  for (const rawLine of String(sumsText || "").trim().split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line) continue;
    let sep = line.indexOf("  ");
    let rest;
    if (sep !== -1) {
      rest = line.slice(sep + 2);
    } else {
      sep = line.indexOf(" ");
      if (sep === -1) continue;
      rest = line.slice(sep + 1);
    }
    const hexSeg = line.slice(0, sep).trim();
    if (!/^[0-9a-fA-F]{64}$/.test(hexSeg)) continue;
    rest = rest.trim();
    if (!rest || rest.includes(" ")) continue; // 带空格的文件名不在本仓库的发布约定里,跳过。
    const name = rest.replace(/^\*/, "");
    if (name === zipName) expected = hexSeg.toLowerCase();
  }
  if (!expected) {
    // 与"校验不匹配"同策略:没验过的包同样删掉,不留临时垃圾。
    try {
      fs.unlinkSync(zipPath);
    } catch {
      /* 已删则忽略 */
    }
    throw new InstallError(`SHA256SUMS 中没有 ${zipName} 的校验项`);
  }
  // 计算完整性校验本来就要全量读;zip 通常几十 MB,一次性读入可接受。
  const digest = crypto.createHash("sha256").update(fs.readFileSync(zipPath)).digest("hex");
  if (digest !== expected) {
    // 删除失败(如被杀软临时占用)不得顶掉真正的错误文案。
    try {
      fs.unlinkSync(zipPath);
    } catch {
      /* 已删则忽略 */
    }
    throw new InstallError(`SHA-256 校验不匹配:下载的 ${zipName} 已删除,请重试或手动下载`);
  }
}

/** 真实的同步外部命令执行;extract 会按平台补好命令与参数。stderr 须捕获,失败时带进错误文案。 */
function defaultRunExtract(file, args, opts) {
  return cp.spawnSync(file, args, { stdio: ["ignore", "ignore", "pipe"], ...opts });
}

/**
 * Windows 解压必须显式走系统自带的 bsdtar(%SystemRoot%\System32\tar.exe,
 * 认盘符路径且能解 zip),绝不能裸调 "tar" 靠 PATH 命中:Git Bash/MSYS 环境
 * PATH 先命中 GNU tar,它把 C:\... 解析成远程主机,报
 * "tar: Cannot connect to C: resolve failed"。
 */
function windowsTarPath() {
  const winRoot = process.env.SystemRoot || String.raw`C:\Windows`;
  return path.join(winRoot, "System32", "tar.exe");
}

/**
 * 用系统 tar/unzip 解压 zip 到 binDir。
 *
 * 选链:
 * - windows:System32 下的 tar.exe(系统自带 bsdtar,认 zip)
 * - darwin: tar -xf ...(bsdtar)
 * - linux:  优先 unzip -o <zip> -d <binDir>;命令缺失(ENOENT/status===null)时才降级
 *           tar -xf,最终再找不到路就提示装 unzip;unzip 存在但解压报错则立即抛 stderr
 *
 * runExtract(file, args, opts) 注入:测试用 fake 模拟命令结果与落盘。
 * 返回 {usedFallback};失败抛 InstallError。
 */
function extract({ zipPath, binDir, osName, runExtract = defaultRunExtract }) {
  fs.mkdirSync(binDir, { recursive: true });
  let first;
  if (osName === "linux") {
    // 优先 unzip(-o 覆盖同名文件,对裸二进制最稳)。
    first = runExtract("unzip", ["-o", zipPath, "-d", binDir], {});
  } else {
    const tarFile = osName === "windows" ? windowsTarPath() : "tar";
    first = runExtract(tarFile, ["-xf", zipPath, "-C", binDir], {});
  }
  const stderr = first && first.stderr ? String(first.stderr) : "";
  if (first && first.status === 0) return { usedFallback: false };
  if (osName !== "linux") {
    // Windows 上系统自带 tar 缺失(status===null/ENOENT)时直接报错,不引入任何兜底。
    const missing =
      !first || first.status === null || (first.error && first.error.code === "ENOENT");
    if (missing) {
      throw new InstallError(
        `解压失败:${zipPath} -> ${binDir},未找到系统自带 tar(应存在于 System32),请重试或手动下载`
      );
    }
    throw new InstallError(
      `解压失败:${zipPath} -> ${binDir}${stderr ? `,${stderr.trim()}` : ",请重试或手动下载"}`
    );
  }
  // 命令缺失是预期的降级路径:spawnSync 对 ENOENT 返回 status===null(并带 error.code);
  // 带 error 更稳(注入的 fake 可能只给 status)。unzip 存在但退出码非 0 则是真解压失败,
  // 直接抛带 stderr 的错误,绝不用 tar 去碰一个解压报错过的包。
  const missing =
    !first ||
    first.status === null ||
    (first.error && first.error.code === "ENOENT");
  if (!missing) {
    throw new InstallError(
      `解压失败(unzip 报错):${zipPath} -> ${binDir}${stderr ? `:${stderr.trim()}` : ",请重试或手动下载"}`
    );
  }
  const second = runExtract("tar", ["-xf", zipPath, "-C", binDir], {});
  const stderr2 = second && second.stderr ? String(second.stderr) : "";
  if (second && second.status === 0) return { usedFallback: true };
  throw new InstallError(
    `解压失败(unzip 缺失且 tar 也不可用,${stderr2 ? `tar: ${stderr2.trim()};` : ""}请先安装 unzip(如 sudo apt install unzip)后重试`
  );
}

/** 真实的 GUI 拉起:detached + stdio ignore + unref,脚本退出后进程必须在。 */
function defaultSpawn(cmd, args, opts) {
  return cp.spawn(cmd, args, opts);
}

/**
 * 分离式拉起 GUI,不等它;返回 Promise:
 * - windows/linux:直接 spawn 可执行文件({detached:true, stdio:'ignore'}).unref(),
 *   cwd 归到用户家目录,避免继承安装器所在目录。
 * - darwin:`open <app>` 本身立即返回。
 * - `spawn` 事件即认为已启动,resolve child;
 * - `error` 事件(node spawn 对 ENOENT 不抛同步异常,而是异步发 error,如可执行文件
 *   缺失/被杀软拦截)reject 成中文 InstallError;
 * - spawnFn 同步抛错同样包装成中文 InstallError 并 reject。
 * 仍然 unref:能否启动由事件感知,但本进程不会被 GUI 拖住。
 */
function launch(osName, entryPath, spawnFn = defaultSpawn) {
  const target = path.resolve(String(entryPath));
  let cmd;
  let args;
  const opts = { detached: true, stdio: "ignore", cwd: os.homedir() };
  if (osName === "darwin") {
    cmd = "open";
    args = [target];
    delete opts.cwd; // open 自身就是转发器,无需改 cwd
  } else {
    cmd = target;
    args = [];
  }
  let child;
  try {
    child = spawnFn(cmd, args, opts);
    if (child && typeof child.unref === "function") child.unref();
  } catch (exc) {
    return Promise.reject(
      new InstallError(
        `无法启动 ${target}:${exc && exc.message ? exc.message : exc},请到安装目录手动运行或重装`
      )
    );
  }
  return new Promise((resolve, reject) => {
    if (!child || typeof child.on !== "function") {
      // 注入的 fake 没有 on():无从感知事件,保持旧行为直接成功。
      resolve(child);
      return;
    }
    child.on("error", (exc) => {
      reject(
        new InstallError(
          `无法启动 ${target}:${exc && exc.message ? exc.message : exc},请到安装目录手动运行或重装`
        )
      );
    });
    child.on("spawn", () => resolve(child));
  });
}

/**
 * 完整安装流:latest/tag → download → verify → extract → 入口检查。
 * 返回 {tag, entry};启动(launch)只归 CLI 层调用,本函数不做任何拉起。
 * 依赖注入:
 * - getJson(url) → Promise<release JSON>(查 latest 用)
 * - getStream(url, destPath)(zip 大文件流式下载)
 * - getBytes(url) → Promise<Buffer>(SHA256SUMS 小文件)
 * - runExtract(file, args, opts)(解压命令,可打桩)
 */
async function install(opts) {
  const {
    getJson = sources.defaultGetJson,
    getStream = defaultGetStream,
    getBytes = defaultGetBytes,
    runExtract,
    tag = "",
    binDir,
    os: osName,
    zipName,
    binaryName,
  } = opts;

  let tagOut = tag;
  let zipUrl;
  let sumsUrl;
  if (tag) {
    // 固定版本:不再查 latest,GitCode 附件链只依赖 tag 即可构造。
    tagOut = sources.canon(tag);
    zipUrl = sources.gitcodeAttachUrl(tagOut, zipName);
    sumsUrl = sources.gitcodeAttachUrl(tagOut, SUMS_NAME);
  } else {
    const rel = await sources.latestRelease(getJson, zipName);
    tagOut = rel.tag;
    zipUrl = rel.zipUrl;
    sumsUrl = rel.sumsUrl;
  }

  const zipPath = await download(zipUrl, getStream);
  try {
    const sumsText = (await getBytes(sumsUrl)).toString("utf8");
    verify(zipPath, sumsText, zipName);
    extract({ zipPath, binDir, osName, runExtract });
  } finally {
    try {
      fs.unlinkSync(zipPath);
    } catch {
      /* 已删则忽略 */
    }
  }
  const entry = path.join(binDir, binaryName);
  if (!fs.existsSync(entry)) {
    throw new InstallError(
      `zip 内没有预期的入口 ${binaryName},解压已在 ${binDir} 但内容异常`
    );
  }
  return { tag: tagOut, entry };
}

module.exports = {
  USER_AGENT,
  SUMS_NAME,
  InstallError,
  defaultGetStream,
  defaultGetBytes,
  defaultRunExtract,
  defaultSpawn,
  download,
  verify,
  windowsTarPath,
  extract,
  launch,
  install,
};
