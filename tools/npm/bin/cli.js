#!/usr/bin/env node
"use strict";
/**
 * kshell-install 命令行入口:npx 直装后一条命令装好桌面版。
 *
 * 参考 Go 侧 internal/update 的源顺序与附件补链规则;这里只负责"装",
 * 不负责已有安装的升级(桌面端自带更新器)。
 */

const os = require("node:os");
const path = require("node:path");

const installerLib = require("../lib/installer");
const platformLib = require("../lib/platform");
const sources = require("../lib/sources");

/**
 * 手写参数解析(不引依赖):
 *   --tag <vX.Y.Z>   固定安装某一发布版本;缺省自动取最新 release
 *   --no-run         只安装不启动(缺省会拉起桌面窗口)
 *   --bin-dir <path> 安装目录;缺省 ~/.kshell/bin
 */
function parseArgs(argv) {
  const args = { tag: "", noRun: false, binDir: "", help: false };
  const list = Array.isArray(argv) ? argv : [];
  for (let i = 0; i < list.length; i++) {
    const a = list[i];
    if (a === "--no-run") args.noRun = true;
    else if (a === "--tag") args.tag = list[++i] ?? "";
    else if (a.startsWith("--tag=")) args.tag = a.slice(6);
    else if (a === "--bin-dir") args.binDir = list[++i] ?? "";
    else if (a.startsWith("--bin-dir=")) args.binDir = a.slice(10);
    else if (a === "-h" || a === "--help") args.help = true;
    else if (a === "--") continue;
    else {
      throw new installerLib.InstallError(`未知参数 ${a};用法:kshell-install [--tag vX.Y.Z] [--no-run] [--bin-dir <path>]`);
    }
  }
  return args;
}

function printHelp() {
  console.log(
    [
      "kshell 桌面版一键安装器:下载官方 zip → 校验 SHA-256 → 解压到安装目录 → 启动。",
      "用法:kshell-install [选项]",
      "  --tag <vX.Y.Z>    固定安装某一发布版本(如 v0.1.27);缺省自动取最新 release",
      "  --no-run          只安装不启动(缺省会拉起 kshell 桌面窗口)",
      "  --bin-dir <path>  安装目录(缺省 ~/.kshell/bin)",
      "  -h / --help       显示本帮助",
    ].join("\n")
  );
}

/** 默认安装目录:~/.kshell/bin;独立纯函数便于测试断言。 */
function defaultBinDir() {
  return path.join(os.homedir(), ".kshell", "bin");
}

/**
 * 安装主流程;依赖可注入(getJson/getBytes/getStream/runExtract/spawnFn),
 * 测试传 fake 即可全离线跑通。返回退出码。
 */
async function main(argv = [], deps = {}) {
  let args;
  try {
    args = parseArgs(argv);
  } catch (exc) {
    console.error(`kshell-install 错误:${exc.message}`);
    return 1;
  }
  if (args.help) {
    printHelp();
    return 0;
  }
  try {
    const { os: osName, zipName, binaryName } = platformLib.detect();
    const binDir = args.binDir ? path.resolve(args.binDir) : defaultBinDir();

    if (!args.tag) console.log("正在查询最新版本……");
    const { spawnFn, ...restDeps } = deps;
    // spawnFn 只留给下方的 launch:启动唯一归 CLI 层,不通过 install 双轨触发。
    const { tag, entry } = await installerLib.install({
      ...restDeps,
      tag: args.tag,
      binDir,
      os: osName,
      zipName,
      binaryName,
    });
    console.log(`目标版本 ${tag}`);
    console.log(`安装完成:kshell 桌面版 ${tag} -> ${entry}`);
    if (args.noRun) return 0;
    console.log("正在启动……");
    installerLib.launch(osName, entry, deps.spawnFn);
    console.log("已启动。");
    return 0;
  } catch (exc) {
    const known =
      exc instanceof installerLib.InstallError ||
      exc instanceof sources.ReleaseError ||
      exc instanceof platformLib.UnsupportedPlatform;
    console.error(`kshell-install 错误:${exc && exc.message ? exc.message : exc}`);
    if (!known) console.error("(意外错误,可携带本输出反馈)");
    return 1;
  }
}

/* 直接执行时才运行 main(require 时不跑,便于测试)。 */
if (require.main === module) {
  main(process.argv.slice(2)).then(
    (code) => {
      if (code !== 0) process.exitCode = code;
    },
    (exc) => {
      console.error(`kshell-install 错误:${exc && exc.stack ? exc.stack : exc}`);
      process.exitCode = 1;
    }
  );
}

module.exports = { parseArgs, main, printHelp, defaultBinDir };
