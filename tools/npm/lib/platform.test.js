"use strict";
import { describe, expect, it } from "vitest";

const { UnsupportedPlatform, canonArch, canonOs, detect } = require("./platform");

describe("canonArch", () => {
  it("把常见别名归一化", () => {
    expect(canonArch("x86_64")).toBe("amd64");
    expect(canonArch("AMD64")).toBe("amd64");
    expect(canonArch("x64")).toBe("amd64");
    expect(canonArch("aarch64")).toBe("arm64");
    expect(canonArch("arm64")).toBe("arm64");
  });
});

describe("canonOs", () => {
  it("win32 → windows;linux/darwin 原样", () => {
    expect(canonOs("win32")).toBe("windows");
    expect(canonOs("linux")).toBe("linux");
    expect(canonOs("darwin")).toBe("darwin");
  });
});

describe("detect 平台矩阵", () => {
  it("三个支持的平台返回 zip 与二进制名", () => {
    expect(detect("win32", "x64")).toEqual({
      os: "windows",
      arch: "amd64",
      zipName: "kshell-desktop-windows-amd64.zip",
      binaryName: "kshell-desktop.exe",
    });
    expect(detect("linux", "x86_64").zipName).toBe("kshell-desktop-linux-amd64.zip");
    expect(detect("linux", "x86_64").binaryName).toBe("kshell-desktop");
    expect(detect("darwin", "arm64").zipName).toBe("kshell-desktop-darwin-arm64.zip");
    expect(detect("darwin", "arm64").binaryName).toBe("kshell-desktop.app");
  });

  it("矩阵之外抛 UnsupportedPlatform", () => {
    for (const [osName, arch] of [
      ["win32", "arm64"],
      ["linux", "arm64"],
      ["darwin", "amd64"],
      ["darwin", "x64"],
      ["freebsd", "amd64"],
      ["win32", "ia32"],
    ]) {
      expect(() => detect(osName, arch), `${osName}/${arch}`).toThrow(UnsupportedPlatform);
      expect(() => detect(osName, arch)).toThrow(/暂不支持的平台/);
    }
  });

  it("留空时取真实机器且必须是支持平台(保证本机可安装)", () => {
    const real = detect();
    expect(Object.keys(real)).toEqual(["os", "arch", "zipName", "binaryName"]);
  });
});
