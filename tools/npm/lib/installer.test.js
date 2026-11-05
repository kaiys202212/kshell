"use strict";
import { afterEach, describe, expect, it, vi } from "vitest";
import { mkdtempSync, readFileSync, rmSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import crypto from "node:crypto";

const { InstallError, download, extract, install, launch, verify, windowsTarPath } = require("./installer");
const { GITCODE_LATEST_URL } = require("./sources");

let dirs = [];
function tempDir() {
  const d = mkdtempSync(path.join(tmpdir(), "kshell-npm-test-"));
  dirs.push(d);
  return d;
}
afterEach(() => {
  for (const d of dirs) rmSync(d, { recursive: true, force: true });
  dirs = [];
  vi.unstubAllGlobals();
});

const BIN_NAME = "kshell-desktop.exe"; // windows 安装器入口约定名
const ZIP = "kshell-desktop-windows-amd64.zip";

function fakeGetStream(content, tmpRoot) {
  // 把"下载"实现为直接写一段内容,模拟真实流式落盘。
  let i = 0;
  const callUrls = [];
  const fn = async (url, destPath) => {
    callUrls.push(url);
    if (!destPath) throw new Error("fake 期望有目标路径");
    writeFileSync(destPath, content);
    i++;
  };
  return { fn, callUrls, count: () => i };
}

describe("download", () => {
  it("写入临时文件并返回路径", async () => {
    const { fn } = fakeGetStream(Buffer.from("hello zip"));
    const p = await download("https://dl/x.zip", fn);
    expect(typeof p).toBe("string");
    expect(readFileSync(p).toString()).toBe("hello zip");
    rmSync(p);
  });

  it("getStream 抛错 → 包成中文 InstallError 且不残留文件", async () => {
    const boom = async () => {
      throw new Error("网络断了");
    };
    await expect(download("https://dl/x.zip", boom)).rejects.toThrow(InstallError);
    await expect(download("https://dl/x.zip", boom)).rejects.toThrow(/下载失败/);
  });
});

describe("verify", () => {
  it("双空格标准格式校验通过", () => {
    const zip = path.join(tempDir(), "a.zip");
    const buf = Buffer.from("zip-bytes");
    writeFileSync(zip, buf);
    const hex = crypto.createHash("sha256").update(buf).digest("hex");
    verify(zip, `${hex}  kshell-desktop-windows-amd64.zip\n`, "kshell-desktop-windows-amd64.zip");
    expect(existsSync(zip)).toBe(true); // 通过时不删
  });

  it("兜底单空格与二进制标记(*)", () => {
    const zip = path.join(tempDir(), "a.zip");
    writeFileSync(zip, "x");
    const hex = crypto.createHash("sha256").update("x").digest("hex");
    verify(zip, `${hex} *kshell-desktop-windows-amd64.zip`, "kshell-desktop-windows-amd64.zip");
  });

  it("不匹配 → 删临时文件并抛 InstallError", () => {
    const zip = path.join(tempDir(), "a.zip");
    writeFileSync(zip, "tampered");
    const goodHex = crypto.createHash("sha256").update("expected").digest("hex");
    expect(() => verify(zip, `${goodHex}  a.zip`, "a.zip")).toThrow(/校验不匹配/);
    expect(existsSync(zip)).toBe(false);
  });

  it("sums 缺条目 → 删临时文件并抛错", () => {
    const zip = path.join(tempDir(), "a.zip");
    writeFileSync(zip, "data");
    expect(() => verify(zip, "00  other.bin", "a.zip")).toThrow(/没有 .* 的校验项/);
    expect(existsSync(zip)).toBe(false);
  });
});

/** 假 unzip/tar:status 可配置,并记录调用;可模拟成功时把入口文件写出来。 */
function fakeRun({ status = 0, touchDir = null, touchName = "kshell-desktop" } = {}) {
  const calls = [];
  const fn = (file, args, opts) => {
    calls.push({ file, args });
    if (touchDir && status === 0) {
      writeFileSync(path.join(String(touchDir), touchName), "binary");
    }
    return { status };
  };
  fn.calls = calls;
  return fn;
}

describe("extract 命令链选型", () => {
  it("windows → 系统 System32 tar.exe(bsdtar)-xf <zip> -C <binDir>", () => {
    const run = fakeRun();
    const out = extract({ zipPath: "a.zip", binDir: "C:\\k\\bin", osName: "windows", runExtract: run });
    expect(out.usedFallback).toBe(false);
    expect(run.calls).toEqual([
      { file: windowsTarPath(), args: ["-xf", "a.zip", "-C", "C:\\k\\bin"] },
    ]);
  });

  it("windowsTarPath() 指向 %SystemRoot%\\System32\\tar.exe;缺 SystemRoot 兜底 C:\\Windows", () => {
    vi.stubEnv("SystemRoot", "D:\\WinNT");
    expect(windowsTarPath()).toBe(path.join("D:\\WinNT", "System32", "tar.exe"));
    vi.stubEnv("SystemRoot", "");
    expect(windowsTarPath()).toBe(path.join("C:\\Windows", "System32", "tar.exe"));
  });

  it("darwin → tar -xf", () => {
    const run = fakeRun();
    extract({ zipPath: "a.zip", binDir: "/tmp/bin", osName: "darwin", runExtract: run });
    expect(run.calls).toEqual([{ file: "tar", args: ["-xf", "a.zip", "-C", "/tmp/bin"] }]);
  });

  it("linux → 先 unzip -o;成功即结束", () => {
    const run = fakeRun();
    extract({ zipPath: "a.zip", binDir: "/tmp/bin", osName: "linux", runExtract: run });
    expect(run.calls.length).toBe(1);
    expect(run.calls[0]).toEqual({ file: "unzip", args: ["-o", "a.zip", "-d", "/tmp/bin"] });
  });

  it("linux unzip 成功但退出码非 0 → 真解压失败,直接抛错不降级 tar", () => {
    let n = 0;
    const run = () => {
      n++;
      return { status: 1, stderr: Buffer.from("unzip: cannot find zipfile") };
    };
    try {
      extract({ zipPath: "a.zip", binDir: "/tmp/bin", osName: "linux", runExtract: run });
      throw new Error("should not reach");
    } catch (exc) {
      expect(exc).toBeInstanceOf(InstallError);
      expect(exc.message).toContain("unzip 报错");
      expect(exc instanceof InstallError).toBe(true);
      expect(exc.message).toContain("cannot find zipfile"); // 带 stderr
      expect(n).toBe(1); // 不再尝试 tar
    }
  });

  it("linux unzip 缺失(ENOENT, error.code) → 降级 tar -xf", () => {
    let n = 0;
    const run = (file) => {
      n++;
      if (file === "unzip") return { status: null, error: { code: "ENOENT" } };
      return { status: 0 };
    };
    const out = extract({ zipPath: "a.zip", binDir: "/tmp/bin", osName: "linux", runExtract: run });
    expect(out.usedFallback).toBe(true);
    expect(n).toBe(2);
  });

  it("linux unzip 缺失(仅 status===null) → 降级 tar -xf", () => {
    const run = (file) => (file === "unzip" ? { status: null } : { status: 0 });
    const out = extract({ zipPath: "a.zip", binDir: "/tmp/bin", osName: "linux", runExtract: run });
    expect(out.usedFallback).toBe(true);
  });

  it("linux unzip 缺失且 tar 也失败 → 中文提示装 unzip", () => {
    const run = (file) =>
      file === "unzip"
        ? { status: null, error: { code: "ENOENT" } }
        : { status: 1, stderr: Buffer.from("tar: invalid") };
    expect(() =>
      extract({ zipPath: "a.zip", binDir: "/tmp/bin", osName: "linux", runExtract: run })
    ).toThrow(InstallError);
    try {
      extract({ zipPath: "a.zip", binDir: "/tmp/bin", osName: "linux", runExtract: run });
      throw new Error("should not reach");
    } catch (exc) {
      expect(exc instanceof InstallError).toBe(true);
      expect(exc.message).toContain("unzip");
      expect(exc.message).toContain("tar: invalid");
      expect(exc.message).toContain("重试");
    }
  });

  it("windows tar 失败(status 非 0/null)直接抛错(无降级),文案不变", () => {
    const run = fakeRun({ status: 1 });
    expect(() =>
      extract({ zipPath: "a.zip", binDir: "b", osName: "windows", runExtract: run })
    ).toThrow(/解压失败/);
  });

  it("windows 系统自带 tar 缺失(status===null)→ 报解压失败并提示未找到系统 tar", () => {
    const run = () => ({ status: null, stderr: Buffer.from("") });
    expect(() =>
      extract({ zipPath: "a.zip", binDir: "b", osName: "windows", runExtract: run })
    ).toThrow(/解压失败/);
    try {
      extract({ zipPath: "a.zip", binDir: "b", osName: "windows", runExtract: run });
      throw new Error("should not reach");
    } catch (exc) {
      expect(exc instanceof InstallError).toBe(true);
      expect(exc.message).toContain("未找到系统自带 tar");
    }
  });
});

describe("launch 三平台参数", () => {
  it("windows/linux:直接 spawn exe,detached + unref,cwd 家目录", () => {
    for (const osName of ["windows", "linux"]) {
      let got;
      const child = launch(osName, "/home/u/.kshell/bin/kshell-desktop", (cmd, args, opts) => {
        got = { cmd, args, opts };
        return { unref() { got.unrefd = true; } };
      });
      expect(got.cmd).toBe(path.resolve("/home/u/.kshell/bin/kshell-desktop"));
      expect(got.args).toEqual([]);
      expect(got.opts.detached).toBe(true);
      expect(got.opts.stdio).toBe("ignore");
      expect(got.opts.cwd).toBeTruthy();
      expect(got.unrefd).toBe(true);
      expect(child).toEqual({ unref: expect.any(Function) });
    }
  });

  it("darwin:open <appPath>", () => {
    let got;
    const child = launch("darwin", "/Applications/kshell-desktop.app", (cmd, args, opts) => {
      got = { cmd, args, opts };
      return { unref() {} };
    });
    expect(got.cmd).toBe("open");
    expect(got.args).toEqual([path.resolve("/Applications/kshell-desktop.app")]);
    expect(got.opts.detached).toBe(true);
    expect(got.opts.stdio).toBe("ignore");
    expect(got.opts.cwd).toBeUndefined();
    expect(typeof child.unref).toBe("function");
  });

  it("spawn 抛错 → 包成中文 InstallError", () => {
    const boom = () => {
      throw new Error("被拦截");
    };
    try {
      launch("windows", "x.exe", boom);
      throw new Error("should not reach");
    } catch (exc) {
      expect(exc instanceof InstallError).toBe(true);
      expect(exc.message).toContain("无法启动");
      expect(exc.message).toContain("请到安装目录手动运行或重装");
    }
  });
});

describe("install 无 getJson 注入 → 用 sources.defaultGetJson(全局 fetch)", () => {
  it("不传 getJson 时走生产 fetch 拉 gitcode latest,整流程 tag 正确", async () => {
    const fetchUrls = [];
    vi.stubGlobal("fetch", async (url) => {
      fetchUrls.push(url);
      return {
        ok: true,
        status: 200,
        text: async () =>
          JSON.stringify({
            tag_name: "v0.1.27",
            assets: [{ name: ZIP, browser_download_url: "https://dl/v0.1.27/zip" }],
          }),
      };
    });
    const urls = [];
    const stream = fakeGetStream(Buffer.from("zip-bytes"), tempDir());
    const streamFn = async (url, dest) => {
      urls.push(url);
      await stream.fn(url, dest);
    };
    const sumsHex = crypto.createHash("sha256").update(Buffer.from("zip-bytes")).digest("hex");
    await install({
      // 注意:故意不传 getJson —— 走生产默认 defaultGetJson
      getStream: streamFn,
      getBytes: async () => Buffer.from(`${sumsHex}  ${ZIP}\n`),
      runExtract: (file, args) => {
        // 把"解压"模拟成直接写入口文件,fs.existsSync(entry) 检查就能过。
        const dest = args.includes("-C") ? args[args.indexOf("-C") + 1] : args[args.indexOf("-d") + 1];
        writeFileSync(path.join(dest, BIN_NAME), "entry");
        return { status: 0, stderr: Buffer.from("") };
      },
      binDir: tempDir(),
      os: "windows",
      zipName: ZIP,
      binaryName: BIN_NAME,
    });
    // latest 查询走的是生产 defaultGetJson;latestRelease 按设计遍历全部源(防镜像滞后),
    // 第一个请求必须打在 gitcode 主源上。
    expect(fetchUrls.length).toBe(2);
    expect(fetchUrls[0]).toBe(GITCODE_LATEST_URL);
    // zip 落盘用的是 release JSON 里的 browser_download_url。
    expect(urls.length).toBe(1);
    expect(urls[0]).toBe("https://dl/v0.1.27/zip");
  });
});
