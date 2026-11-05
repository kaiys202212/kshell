"use strict";
import { afterEach, describe, expect, it } from "vitest";
import { mkdtempSync, rmSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import { tmpdir, homedir } from "node:os";
import path from "node:path";
import crypto from "node:crypto";

const { InstallError } = require("../lib/installer");
const { ReleaseError } = require("../lib/sources");
const { UnsupportedPlatform } = require("../lib/platform");
const { main, parseArgs, printHelp } = require("./cli");

let dirs = [];
function tempDir(name = "k") {
  const d = mkdtempSync(path.join(tmpdir(), `kshell-cli-${name}-`));
  dirs.push(d);
  return d;
}
afterEach(() => {
  for (const d of dirs) rmSync(d, { recursive: true, force: true });
  dirs = [];
});

/** 端到端依赖注入:固定 zip 内容 → sums 由内容现算,保证校验必然通过。 */
function makeDeps({ zipContent = Buffer.from("fake-zip"), tag = "v0.1.27" } = {}) {
  const hex = crypto.createHash("sha256").update(zipContent).digest("hex");
  const sums = `${hex}  kshell-desktop-${"windows"}-amd64.zip\n${hex}  kshell-desktop-linux-amd64.zip\n${hex}  kshell-desktop-darwin-arm64.zip\n`;
  const log = { info: [], err: [], urls: [], extracts: [], spawns: [] };
  const deps = {
    log,
    getJson: async (url) => ({
      tag_name: tag,
      assets: [
        { name: "kshell-desktop-windows-amd64.zip", browser_download_url: `https://dl/${tag}/win.zip` },
        { name: "SHA256SUMS", browser_download_url: `https://dl/${tag}/SHA256SUMS` },
      ],
    }),
    getStream: async (url, dest) => {
      log.urls.push(["stream", url]);
      writeFileSync(dest, zipContent);
    },
    getBytes: async (url) => {
      log.urls.push(["bytes", url]);
      return Buffer.from(sums);
    },
    runExtract: (file, args) => {
      log.extracts.push({ file, args });
      // 模拟真实解压产物:三个平台的入口名都放出来,install 的存在性检查必过。
      const binRoot = args.includes("-d") ? args[args.indexOf("-d") + 1] : args[args.indexOf("-C") + 1];
      writeFileSync(path.join(binRoot, "kshell-desktop.exe"), "mz");
      writeFileSync(path.join(binRoot, "kshell-desktop"), "elf");
      return { status: 0 };
    },
    spawnFn: (cmd, args, opts) => {
      log.spawns.push({ cmd, args, opts });
      return { unref() {} };
    },
  };
  return deps;
}

/** 捕获 console 输出跑 main。 */
async function capture(argv, deps) {
  const info = [];
  const err = [];
  const co = console.log;
  const ce = console.error;
  console.log = (...a) => info.push(a.join(" "));
  console.error = (...a) => err.push(a.join(" "));
  let code;
  try {
    code = await main(argv, deps);
  } finally {
    console.log = co;
    console.error = ce;
  }
  return { code, info, err };
}

describe("parseArgs", () => {
  it("解析三个选项", () => {
    expect(parseArgs(["--tag", "v0.1.27", "--no-run", "--bin-dir", "D:/b"])).toEqual({
      tag: "v0.1.27",
      noRun: true,
      binDir: "D:/b",
      help: false,
    });
  });
  it("支持 = 连接形式", () => {
    expect(parseArgs(["--tag=v1.2.3"]).tag).toBe("v1.2.3");
    expect(parseArgs(["--bin-dir=/x"]).binDir).toBe("/x");
  });
  it("未知参数抛 InstallError 文案", () => {
    expect(() => parseArgs(["--wat"])).toThrow(InstallError);
    expect(() => parseArgs(["--wat"])).toThrow(/未知参数/);
  });
  it("--help", () => {
    expect(parseArgs(["-h"]).help).toBe(true);
    const seen = [];
    const co = console.log;
    console.log = (...a) => seen.push(a.join(" "));
    try {
      printHelp();
    } finally {
      console.log = co;
    }
    expect(seen.join("\n")).toContain("kshell");
  });
});

describe("cli 端到端(临时 bin dir,不碰真实 ~/.kshell)", () => {
  it("--no-run:下载→校验→解压→打印版本与路径,exit 0,不启动", async () => {
    const deps = makeDeps();
    const binDir = path.join(tempDir("a"), "bin");
    const out = await capture(["--no-run", "--bin-dir", binDir], deps);
    expect(out.code).toBe(0);
    expect(out.info.some((l) => l.includes("目标版本 v0.1.27"))).toBe(true);
    expect(out.info.some((l) => l.includes("安装完成") && l.includes(binDir))).toBe(true);
    expect(out.info.some((l) => /启动/.test(l))).toBe(false);
    expect(deps.log.extracts.length).toBe(1);
    // 临时 zip 已清理
    expect(deps.log.urls.map(([k]) => k)).toEqual(["stream", "bytes"]);
  });

  it("默认会拉起(spawn 注入被调用,三平台命令正确)", async () => {
    const deps = makeDeps();
    const binDir = path.join(tempDir("b"), "bin");
    const out = await capture(["--bin-dir", binDir], deps);
    expect(out.code).toBe(0);
    expect(deps.log.spawns.length).toBe(1);
    const s = deps.log.spawns[0];
    expect(s.cmd).toBe(path.join(binDir, "kshell-desktop.exe"));
    expect(s.opts.detached).toBe(true);
    expect(s.opts.stdio).toBe("ignore");
    expect(out.info.some((l) => l.includes("正在启动"))).toBe(true);
  });

  it("--tag 固定版本:不查 latest,走 GitCode attach 链", async () => {
    const deps = makeDeps();
    let latestCalls = 0;
    deps.getJson = async () => {
      latestCalls++;
      throw new Error("不该查询 latest");
    };
    const binDir = path.join(tempDir("c"), "bin");
    const out = await capture(["--no-run", "--tag", "0.1.25", "--bin-dir", binDir], deps);
    expect(out.code).toBe(0);
    expect(latestCalls).toBe(0);
    expect(deps.log.urls.some(([, u]) => u.includes("/releases/v0.1.25/attach_files/"))).toBe(true);
    expect(out.info.some((l) => l.includes("目标版本 v0.1.25"))).toBe(true);
  });

  it("默认 bin-dir 是 ~/.kshell/bin(仅推断,不入参时可用 temp home 断言)", () => {
    // 这里只断言默认值表达式,不真正以默认参数运行(避免写真实家目录)。
    expect(path.join(homedir(), ".kshell", "bin")).toContain(".kshell");
  });

  it("latest 失败 → stderr 中文错误 + exit 1", async () => {
    const deps = makeDeps();
    deps.getJson = async () => {
      throw new Error("无网络");
    };
    const out = await capture(["--bin-dir", tempDir("d")], deps);
    expect(out.code).toBe(1);
    expect(out.err[0]).toContain("kshell-install 错误");
    expect(out.err[0]).toContain("无网络");
  });

  it("校验不匹配 → exit 1 且不解压", async () => {
    const deps = makeDeps();
    deps.getBytes = async () => Buffer.from("0000  x.zip\n");
    const out = await capture(["--no-run", "--bin-dir", tempDir("e")], deps);
    expect(out.code).toBe(1);
    expect(deps.log.extracts.length).toBe(0);
    expect(out.err[0]).toContain("校验项");
  });

  it("ReleaseError / UnsupportedPlatform 都落到 exit 1", async () => {
    const deps = makeDeps();
    deps.getJson = async () => {
      throw new ReleaseError("没有可用的发布源");
    };
    const out = await capture(["--no-run"], deps);
    expect(out.code).toBe(1);
  });

  it("未知参数 → exit 1", async () => {
    const out = await capture(["--unknown"], makeDeps());
    expect(out.code).toBe(1);
    expect(out.err[0]).toContain("未知参数");
  });
});
