package asrlocal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// 目录与文件常量（全部相对 rootDir，即 ~/.kshell/asr）。
const (
	runtimeSubdir = "runtime"
	modelsSubdir  = "models"
	modelDirName  = "sense-voice"
	downloadsDir  = "downloads"
	manifestName  = "version.json"
)

// Options 是安装参数：镜像覆盖、进度与日志回调。
type Options struct {
	Mirrors  []string
	Client   *http.Client
	Logf     func(string)
	Progress func(phase string, done, total int64)
}

// manifest 是安装清单：版本变化时据此判断需要重装。
type manifest struct {
	RuntimeVersion string `json:"runtime_version"`
	Asset          string `json:"asset"`
	ModelRepo      string `json:"model_repo"`
	InstalledAt    string `json:"installed_at"`
}

// Status 是本地引擎状态。Installed 要求引擎与两份模型文件齐全；Partial 表示
// 只装了一部分（上次中断），UI 提示重新安装。
type Status struct {
	Installed  bool  `json:"installed"`
	Partial    bool  `json:"partial"`
	EnginePath string `json:"enginePath"`
	ModelPath  string `json:"modelPath"`
	Version    string `json:"version"`
	SizeBytes  int64 `json:"sizeBytes"`
}

// engineName 返回平台可执行文件名。
func engineName() string {
	if runtime.GOOS == "windows" {
		return "sherpa-onnx-offline.exe"
	}
	return "sherpa-onnx-offline"
}

// Install 下载并安装本地引擎与模型。幂等：已就绪且版本一致时直接返回。
// 安装过程分三段（运行时 → 模型），每段按镜像链回退并做 SHA256 校验。
func Install(ctx context.Context, rootDir string, opts Options) error {
	logf := opts.Logf
	if logf == nil {
		logf = func(string) {}
	}
	client := opts.Client
	if client == nil {
		client = defaultHTTPClient()
	}
	asset, err := RuntimeAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	if st := Detect(rootDir); st.Installed && st.Version == RuntimeVersion {
		logf("本地引擎已是最新版本，跳过下载")
		return nil
	}
	asset.URLs = applyMirrors(asset.URLs, opts.Mirrors)

	dlDir := filepath.Join(rootDir, downloadsDir)
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(dlDir)

	// 1) 运行时
	logf("下载语音引擎 " + runtime.GOOS + "/" + runtime.GOARCH + "（约 " + humanMB(asset.Size) + "）")
	archive := filepath.Join(dlDir, asset.File)
	if err := reportProgress(opts, "runtime"); err != nil {
		return err
	}
	if _, err := fetchFile(ctx, client, asset.URLs, archive, asset.SHA256, asset.Size, progressFor(opts, "runtime"), logf); err != nil {
		return err
	}
	runtimeDir := filepath.Join(rootDir, runtimeSubdir, asset.Stem)
	logf("解压运行时到 " + runtimeDir)
	if err := extractRuntime(archive, runtimeDir); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "bin", engineName())); err != nil {
		return fmt.Errorf("解压后缺少引擎可执行文件: %w", err)
	}

	// 2) 模型
	modelDir := filepath.Join(rootDir, modelsSubdir, modelDirName)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"model.int8.onnx", "tokens.txt"} {
		meta := ModelFiles[name]
		logf("下载语音模型 " + name)
		dst := filepath.Join(modelDir, name)
		urls := ModelSources(name, opts.Mirrors)
		if _, err := fetchFile(ctx, client, urls, dst, meta.SHA256, meta.Size, progressFor(opts, name), logf); err != nil {
			return err
		}
	}

	// 3) 清单
	mf := manifest{
		RuntimeVersion: RuntimeVersion,
		Asset:          asset.File,
		ModelRepo:      ModelRepo,
		InstalledAt:    time.Now().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(rootDir, manifestName), data, 0o644); err != nil {
		return err
	}
	cleanupOldRuntimes(rootDir, asset.Stem)
	logf("本地语音引擎安装完成")
	return nil
}

// cleanupOldRuntimes 删除非当前版本的历史 runtime 目录（升级后释放空间）。
func cleanupOldRuntimes(rootDir, keep string) {
	base := filepath.Join(rootDir, runtimeSubdir)
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep {
			_ = os.RemoveAll(filepath.Join(base, e.Name()))
		}
	}
}

// Detect 扫描安装目录返回状态。引擎位置用「runtime/*/bin/<engine>」扫描而非
// 依赖当前资产表的目录名：旧版本升级中断时也能识别。
func Detect(rootDir string) Status {
	var st Status
	if mf, err := os.ReadFile(filepath.Join(rootDir, manifestName)); err == nil {
		var m manifest
		if json.Unmarshal(mf, &m) == nil {
			st.Version = m.RuntimeVersion
		}
	}
	if entries, err := os.ReadDir(filepath.Join(rootDir, runtimeSubdir)); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(rootDir, runtimeSubdir, e.Name(), "bin", engineName())
			if _, err := os.Stat(p); err == nil {
				st.EnginePath = p
				break
			}
		}
	}
	st.ModelPath = filepath.Join(rootDir, modelsSubdir, modelDirName)
	hasModel := fileExists(filepath.Join(st.ModelPath, "model.int8.onnx"))
	hasTokens := fileExists(filepath.Join(st.ModelPath, "tokens.txt"))
	hasEngine := st.EnginePath != ""
	st.Installed = hasEngine && hasModel && hasTokens
	st.Partial = !st.Installed && (hasEngine || hasModel || hasTokens)
	st.SizeBytes = dirSize(rootDir)
	if st.Installed && st.Version == "" {
		st.Version = RuntimeVersion
	}
	return st
}

// Remove 删除本地引擎目录（仅 kshell 自装的 ~/.kshell/asr）。
func Remove(rootDir string) error {
	return os.RemoveAll(rootDir)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// dirSize 累计目录占用（失败项忽略，仅用于 UI 展示）。
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// defaultHTTPClient 给大文件下载用：不设总超时（由 ctx 控制），只限制响应头
// 等待，避免镜像无响应时长时间挂起。
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

// progressFor 把 fetchFile 的字节进度包装成阶段化回调。
func progressFor(opts Options, phase string) func(done, total int64) {
	if opts.Progress == nil {
		return nil
	}
	// 节流交给 fetchFile（500ms），这里只标注阶段。
	return func(done, total int64) { opts.Progress(phase, done, total) }
}

// reportProgress 在进入某阶段前发一条 0% 进度，让 UI 立刻显示当前阶段。
func reportProgress(opts Options, phase string) error {
	if opts.Progress != nil {
		opts.Progress(phase, 0, 0)
	}
	return nil
}

// humanMB 把字节格式化为 MB 字符串。
func humanMB(n int64) string {
	return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
}