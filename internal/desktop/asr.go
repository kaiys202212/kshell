package desktop

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yangk/kshell/internal/asrlocal"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hubtools"
)

var (
	errASRInstallBusy = errors.New("err.asr.install_in_progress")
	errASRNotReady    = errors.New("err.asr.not_ready")
)

// asrInstallJob 是本机语音引擎安装任务的快照（日志供前端轮询与流式事件共用）。
type asrInstallJob struct {
	running bool
	log     strings.Builder
	errText string
}

// ASRStatusView 是设置页「语音转写」卡片的数据源：安装状态 + 持久化配置。
type ASRStatusView struct {
	Installed   bool     `json:"installed"`
	Partial     bool     `json:"partial"`
	EnginePath  string   `json:"enginePath"`
	ModelPath   string   `json:"modelPath"`
	Version     string   `json:"version"`
	SizeMB      int64    `json:"sizeMB"`
	Supported   bool     `json:"supported"`
	Platform    string   `json:"platform"`
	Enabled     bool     `json:"enabled"`
	PreferCloud bool     `json:"preferCloud"`
	Language    string   `json:"language"`
	ITN         bool     `json:"itn"`
	Mirrors     []string `json:"mirrors"`
	Command     string   `json:"command"`
	Running     bool     `json:"running"`
	Error       string   `json:"error"`
}

// ASRSettingsInput 是设置页提交的本地转写配置（不含安装动作）。
type ASRSettingsInput struct {
	Enabled     bool     `json:"enabled"`
	PreferCloud bool     `json:"preferCloud"`
	Language    string   `json:"language"`
	ITN         bool     `json:"itn"`
	Mirrors     []string `json:"mirrors"`
	Command     string   `json:"command"`
}

// asrRootDir 返回本地引擎根目录（~/.kshell/asr）；Layout 未装配时返回空串。
func (a *App) asrRootDir() string {
	root := a.snapshot().Layout.Root
	if root == "" {
		return ""
	}
	return filepath.Join(root, "asr")
}

// GetASRStatus 汇总安装状态与配置，供设置页渲染。
func (a *App) GetASRStatus() ASRStatusView {
	cfg := a.snapshot().Config.ASR
	view := ASRStatusView{
		Supported:   true,
		Platform:    runtime.GOOS + "/" + runtime.GOARCH,
		Enabled:     cfg.Enabled,
		PreferCloud: cfg.PreferCloud,
		Language:    cfg.Language,
		ITN:         cfg.ITNEnabled(),
		Mirrors:     append([]string(nil), cfg.Mirrors...),
		Command:     cfg.Command,
	}
	if _, err := asrlocal.RuntimeAsset(runtime.GOOS, runtime.GOARCH); err != nil {
		view.Supported = false
	}
	dir := a.asrRootDir()
	if dir != "" {
		st := asrlocal.Detect(dir)
		view.Installed = st.Installed
		view.Partial = st.Partial
		view.EnginePath = st.EnginePath
		view.ModelPath = st.ModelPath
		view.Version = st.Version
		view.SizeMB = st.SizeBytes / (1 << 20)
	}
	a.mu.Lock()
	view.Running = a.asrJob.running
	view.Error = a.asrJob.errText
	a.mu.Unlock()
	return view
}

// GetASRInstallLog 返回当前安装任务的完整日志：设置页切走再回来时用它恢复
// 进度展示（事件只覆盖挂载期间的增量）。
func (a *App) GetASRInstallLog() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.asrJob.log.String()
}

// InstallASRLocal 启动本地引擎安装任务：下载 runtime + 模型、校验、解压。
// 成功后自动把 asr.enabled 置真并落盘，安装完成即可用。
func (a *App) InstallASRLocal() error {
	dir := a.asrRootDir()
	if dir == "" {
		return errASRNotReady
	}
	if _, err := asrlocal.RuntimeAsset(runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	// runCtx 内部也加 a.mu，必须在拿锁之前取：Go 的 Mutex 不可重入，持锁再调
	// 会让绑定永久挂起，进而卡死所有依赖 a.mu 的 Wails 调用。
	ctx := a.runCtx()
	a.mu.Lock()
	if a.asrJob.running {
		a.mu.Unlock()
		return errASRInstallBusy
	}
	a.asrJob = asrInstallJob{running: true}
	cfg := a.opts.Config.ASR
	a.mu.Unlock()

	go a.execASRInstall(ctx, dir, cfg)
	return nil
}

// execASRInstall 在后台执行安装：日志同时进任务缓冲与 asr:install:log 事件。
func (a *App) execASRInstall(ctx context.Context, dir string, cfg config.ASRConfig) {
	var runErr error
	defer func() {
		a.mu.Lock()
		a.asrJob.running = false
		if runErr != nil {
			a.asrJob.errText = runErr.Error()
		}
		a.mu.Unlock()
		payload := map[string]any{"ok": runErr == nil}
		if runErr != nil {
			payload["error"] = runErr.Error()
		}
		a.Emit("asr:install:done", payload)
	}()

	logf := func(line string) {
		a.mu.Lock()
		a.asrJob.log.WriteString(line)
		a.asrJob.log.WriteByte('\n')
		a.mu.Unlock()
		a.Emit("asr:install:log", map[string]any{"text": line})
	}

	lastPct := map[string]int{}
	opts := asrlocal.Options{
		Mirrors: cfg.Mirrors,
		Logf:    logf,
		Progress: func(phase string, done, total int64) {
			// 进度节流：同一阶段百分比变化 ≥5 才发，避免几百行刷屏。
			if total <= 0 {
				return
			}
			pct := int(done * 100 / total)
			if pct >= 100 {
				pct = 99 // 100 留给「下载完成」日志
			}
			if pct-lastPct[phase] < 5 && pct != 0 {
				return
			}
			lastPct[phase] = pct
			label := "下载语音模型"
			switch phase {
			case "runtime":
				label = "下载语音引擎"
			case "model.int8.onnx":
				label = "下载语音模型"
			case "tokens.txt":
				label = "下载词表"
			}
			logf(fmt.Sprintf("%s %d%%", label, pct))
		},
	}
	runErr = asrlocal.Install(ctx, dir, opts)
	if runErr == nil {
		// 安装成功即启用：用户点了安装就是要用，不必再勾一次开关。
		if err := a.saveConfig(func(c *config.Config) { c.ASR.Enabled = true }); err != nil {
			logf("保存配置失败：" + err.Error())
		}
	}
}

// UninstallASRLocal 删除本地引擎目录；同时关闭 asr.enabled（避免继续尝试不可用的引擎）。
func (a *App) UninstallASRLocal() error {
	dir := a.asrRootDir()
	if dir == "" {
		return errASRNotReady
	}
	a.mu.Lock()
	if a.asrJob.running {
		a.mu.Unlock()
		return errASRInstallBusy
	}
	a.mu.Unlock()
	if err := asrlocal.Remove(dir); err != nil {
		return err
	}
	return a.saveConfig(func(c *config.Config) { c.ASR.Enabled = false })
}

// SaveASRSettings 持久化本地转写配置并返回最新状态。语言与镜像在 config 归一
// 化里兜底；命令为空表示使用内置引擎。
func (a *App) SaveASRSettings(in ASRSettingsInput) (ASRStatusView, error) {
	if err := a.saveConfig(func(c *config.Config) {
		c.ASR.Enabled = in.Enabled
		c.ASR.PreferCloud = in.PreferCloud
		c.ASR.Language = strings.TrimSpace(in.Language)
		c.ASR.NoITN = !in.ITN
		c.ASR.Mirrors = append([]string(nil), in.Mirrors...)
		c.ASR.Command = in.Command
	}); err != nil {
		return ASRStatusView{}, err
	}
	return a.GetASRStatus(), nil
}

// localTranscriber 组装本地转写闭包（weixinTranscriber 注入用）：
// 未启用/未安装/自定义命令非法时返回 nil，让上层回落云端链。
func (a *App) localTranscriber() hubtools.LocalTranscriber {
	snap := a.snapshot()
	cfg := snap.Config.ASR
	if !cfg.Enabled {
		return nil
	}
	dir := a.asrRootDir()
	if dir == "" {
		return nil
	}
	opts := asrlocal.RunOptions{Language: cfg.Language, ITN: cfg.ITNEnabled()}
	if cmd := strings.TrimSpace(cfg.Command); cmd != "" {
		return func(ctx context.Context, wav []byte) (string, error) {
			text, err := asrlocal.TranscribeCommand(ctx, cmd, wav, 0)
			if err != nil {
				return "", err
			}
			return text, nil
		}
	}
	if !asrlocal.Detect(dir).Installed {
		return nil
	}
	return func(ctx context.Context, wav []byte) (string, error) {
		text, err := asrlocal.Transcribe(ctx, dir, wav, opts)
		if errors.Is(err, asrlocal.ErrNotInstalled) {
			return "", hubtools.ErrLocalNotInstalled
		}
		return text, err
	}
}
