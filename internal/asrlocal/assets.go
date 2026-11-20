// Package asrlocal 提供非内置的本地语音转写引擎（sherpa-onnx + SenseVoice）：
// 按需下载固定版本运行时与模型、校验 SHA256、解压到 ~/.kshell/asr，并调用
// sherpa-onnx-offline 把 WAV 转成文字。引擎不随 kshell 二进制分发。
package asrlocal

import (
	"fmt"
	"strings"
)

// RuntimeVersion 是固定的 sherpa-onnx 发布版本：升级引擎必须同时更新资产表
// 与 SHA256，并跑一次真机样本验证。
const RuntimeVersion = "v2.0.2"

// releaseBase 是 GitHub release 资产根地址。
const releaseBase = "https://github.com/k2-fsa/sherpa-onnx/releases/download/" + RuntimeVersion + "/"

// runtimeMirrors 是 runtime 下载的默认镜像（官方在前，代理在后）：
// 国内直连 GitHub 常超时，gh-proxy.com / ghproxy.net 实测可用。
var runtimeMirrors = []string{
	releaseBase,
	"https://gh-proxy.com/" + releaseBase,
	"https://ghproxy.net/" + releaseBase,
}

// Asset 描述一个平台运行时资产。
type Asset struct {
	// Stem 是压缩包解压后的顶层目录名（解压时扁平化，仅保留 bin/ 与 lib/）。
	Stem   string
	File   string
	SHA256 string
	Size   int64
	URLs   []string
}

// runtimeAssets 是平台 → 资产的静态表（SHA256 为 2026-11-20 实测值）。
var runtimeAssets = map[string]Asset{
	"windows/amd64": {
		Stem:   "sherpa-onnx-v2.0.2-win-x64-shared-MT-Release",
		File:   "sherpa-onnx-v2.0.2-win-x64-shared-MT-Release.tar.bz2",
		Size:   28075706,
		SHA256: "55ada3f155344355c4b07445a10f4d96708a96bc483d69641fafc4966b6578ef",
	},
	"windows/arm64": {
		Stem:   "sherpa-onnx-v2.0.2-win-arm64-shared-MT-Release",
		File:   "sherpa-onnx-v2.0.2-win-arm64-shared-MT-Release.tar.bz2",
		Size:   25005230,
		SHA256: "6a902113e0ab0d9cff340fb9d6e20b4e2d7f0eff2a77dccdbd67e346a96057ba",
	},
	// universal2 同时覆盖 Intel 与 Apple Silicon，省一份下载。
	"darwin/amd64": {
		Stem:   "sherpa-onnx-v2.0.2-osx-universal2-shared",
		File:   "sherpa-onnx-v2.0.2-osx-universal2-shared.tar.bz2",
		Size:   46915073,
		SHA256: "a740c6fa303e5a650055183f5b4826ff5eeb5214236c3bc07a370bc01f3876ec",
	},
	"darwin/arm64": {
		Stem:   "sherpa-onnx-v2.0.2-osx-universal2-shared",
		File:   "sherpa-onnx-v2.0.2-osx-universal2-shared.tar.bz2",
		SHA256: "a740c6fa303e5a650055183f5b4826ff5eeb5214236c3bc07a370bc01f3876ec",
	},
	"linux/amd64": {
		Stem:   "sherpa-onnx-v2.0.2-linux-x64-shared",
		File:   "sherpa-onnx-v2.0.2-linux-x64-shared.tar.bz2",
		Size:   29913621,
		SHA256: "e7f65778bfb891ec98db56c2cfc37b968d7f56e57271986a8dd2cd0c21d002c9",
	},
	"linux/arm64": {
		Stem:   "sherpa-onnx-v2.0.2-linux-aarch64-shared-cpu",
		File:   "sherpa-onnx-v2.0.2-linux-aarch64-shared-cpu.tar.bz2",
		Size:   29462814,
		SHA256: "5c715c43c8b2a9e591a7146f6a397419780dc2af78921ed9d36d993333317d43",
	},
}

// RuntimeAsset 返回当前平台的运行时资产；未知平台返回可读错误（由上层提示
// 不支持，而不是让用户下载到错误架构的二进制）。
func RuntimeAsset(goos, goarch string) (Asset, error) {
	a, ok := runtimeAssets[goos+"/"+goarch]
	if !ok {
		return Asset{}, fmt.Errorf("err.asrlocal.unsupported_platform: %s/%s", goos, goarch)
	}
	// 表中未显式给 URL（生产路径）时按默认镜像链拼资产名；测试可注入本地地址。
	if len(a.URLs) == 0 {
		a.URLs = append([]string(nil), runtimeMirrors...)
		for i := range a.URLs {
			a.URLs[i] += a.File
		}
	}
	return a, nil
}

// ModelFileMeta 是模型文件的哈希与大小。
type ModelFileMeta struct {
	SHA256 string
	Size   int64
}

// ModelRepo 是 SenseVoice 模型所在仓库（int8 量化，约 229MB）。
const ModelRepo = "csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17"

// ModelFiles 是模型白名单：只下载这两个文件，不拉整仓。
var ModelFiles = map[string]ModelFileMeta{
	"model.int8.onnx": {"c71f0ce00bec95b07744e116345e33d8cbbe08cef896382cf907bf4b51a2cd51", 239233841},
	"tokens.txt":     {"f449eb28dc567533d7fa59be34e2abca8784f771850c78a47fb731a31429a1dc", 315894},
}

// modelHosts 是模型下载源：hf-mirror 为国内镜像，官方 HF 兜底。
var modelHosts = []string{
	"https://hf-mirror.com/" + ModelRepo + "/resolve/main/",
	"https://huggingface.co/" + ModelRepo + "/resolve/main/",
}

// ModelSources 返回模型文件的下载地址列表（用户镜像优先）。
func ModelSources(name string, mirrors []string) []string {
	urls := make([]string, 0, len(modelHosts))
	for _, h := range modelHosts {
		urls = append(urls, h+name)
	}
	return applyMirrors(urls, mirrors)
}

// applyMirrors 把用户镜像插到原始地址之前：含 {url} 占位则替换，否则按前缀
// 拼接。空白项忽略；用户镜像全不可用时原始地址仍是兜底。
func applyMirrors(urls, mirrors []string) []string {
	var clean []string
	for _, m := range mirrors {
		if m = strings.TrimSpace(m); m != "" {
			clean = append(clean, m)
		}
	}
	if len(clean) == 0 {
		return append([]string(nil), urls...)
	}
	out := make([]string, 0, len(clean)*len(urls)+len(urls))
	for _, m := range clean {
		for _, u := range urls {
			if strings.Contains(m, "{url}") {
				out = append(out, strings.ReplaceAll(m, "{url}", u))
			} else {
				out = append(out, strings.TrimRight(m, "/")+"/"+u)
			}
		}
	}
	return append(out, urls...)
}