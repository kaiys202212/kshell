"use strict";
/**
 * 平台探测:把当前机器映射到官方支持的发布产物。
 *
 * 产物矩阵(与 CI 发布一致,见 internal/update/check.go 的 PackageZipName):
 * - windows/amd64 → zip 内含 kshell-desktop.exe
 * - linux/amd64   → zip 内含裸二进制 kshell-desktop
 * - darwin/arm64  → zip 内含完整 kshell-desktop.app 目录
 *
 * 矩阵之外(GitHub Actions 目前不出这些包)直接报错,不再走下载。
 */

/** 支持的 (os, arch) 组合;zip 名 = kshell-desktop-{os}-{arch}.zip。 */
const ZIPS = {
  "windows-amd64": "kshell-desktop-windows-amd64.zip",
  "linux-amd64": "kshell-desktop-linux-amd64.zip",
  "darwin-arm64": "kshell-desktop-darwin-arm64.zip",
};

/** zip 内的可执行文件名(相对于解压根)。 */
const BINARIES = {
  windows: "kshell-desktop.exe",
  linux: "kshell-desktop",
  darwin: "kshell-desktop.app",
};

class UnsupportedPlatform extends Error {
  constructor(message) {
    super(message);
    this.name = "UnsupportedPlatform";
  }
}

/** 把各平台对同一架构的叫法归一成发布产物里的写法。 */
function canonArch(machine) {
  const m = String(machine || "").toLowerCase();
  const table = {
    x86_64: "amd64",
    amd64: "amd64",
    x64: "amd64",
    aarch64: "arm64",
    arm64: "arm64",
  };
  return table[m] || m;
}

/** 把 process.platform 风格(win32)归一成发布产物的 os 名(windows)。 */
function canonOs(osName) {
  const o = String(osName || "").toLowerCase();
  if (o.startsWith("win")) return "windows";
  return o; // linux / darwin 原样;其它由矩阵校验拒绝。
}

/**
 * 返回 {os, arch, zipName, binaryName}。
 * osName/arch 留空时取真实机器(默认 osName=process.platform、arch=process.arch;
 * 测试可直接传值,不依赖运行平台)。
 */
function detect(osName, arch) {
  const os = canonOs(osName || process.platform);
  const a = canonArch(arch || process.arch);
  const zipName = ZIPS[`${os}-${a}`];
  if (!zipName) {
    throw new UnsupportedPlatform(
      `暂不支持的平台 ${os}/${a}:官方发布包仅含 windows-amd64 / linux-amd64 / darwin-arm64`
    );
  }
  return { os, arch: a, zipName, binaryName: BINARIES[os] };
}

module.exports = { ZIPS, BINARIES, UnsupportedPlatform, canonArch, canonOs, detect };
