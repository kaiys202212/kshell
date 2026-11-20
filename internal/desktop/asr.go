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
	running  bool
	log      strings.Builder
	errText  string
	progress ASRProgressView
}

// ASRProgressView 是安装进度快照：Phase 为当前阶段标识，Pct 为阶段内百分比，
// Overall 为按各文件体积加权的总体百分比（下载阶段 ≤99，留 100 给完成）。
type ASRProgressView struct {
	Phase   string `json:"phase"`
	Pct     int    `json:"pct"`
	Overall int    `json:"overall"`
}

// 下载/解压阶段标识（与 asrlocal.Options.Progress 的 phase 参数一致）。
const (
	asrPhaseRuntime = "runtime"
	asrPhaseModel   = "model.int8.onnx"
	asrPhaseTokens  = "tokens.txt"
	asrPhaseExtract = "extract"
)

// ASRStatusView 是设置页「语音转写」卡片的数据源：安装状态 + 持久化配置。
type ASRStatusView struct {
	Installed   bool            `json:"installed"`
	Partial     bool            `json:"partial"`
	EnginePath  string          `json:"enginePath"`
	ModelPath   string          `json:"modelPath"`
	Version     string          `json:"version"`
	SizeMB      int64           `json:"sizeMB"`
	Supported   bool            `json:"supported"`
	Platform    string          `json:"platform"`
	Enabled     bool            `json:"enabled"`
	PreferCloud bool            `json:"preferCloud"`
	Language    string          `json:"language"`
	ITN         bool            `json:"itn"`
	Mirrors     []string        `json:"mirrors"`
	Command     string          `json:"command"`
	Running     bool            `json:"running"`
	Error       string          `json:"error"`
	Progress    ASRProgressView `json:"progress"`
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
	view.Progress = a.asrJob.progress
	a.mu.Unlock()
	return view
}

// asrPhaseLabel 返回阶段的中文日志标签（日志与设置页共用同一套阶段名）。
func asrPhaseLabel(phase string) string {
	switch phase {
	case asrPhaseRuntime:
		return "下载语音引擎"
	case asrPhaseModel:
		return "下载语音模型"
	case asrPhaseTokens:
		return "下载词表"
	case asrPhaseExtract:
		return "解压语音引擎"
	}
	return "下载语音模型"
}

// asrOverallPct 把某阶段的已下载字节换算成总体百分比：按 runtime、模型、词表
// 三份文件的体积加权；解压固定 99，避免下载完成时进度条先满再等解压。
func asrOverallPct(phase string, done, runtimeSize, totalBytes int64) int {
	if phase == asrPhaseExtract {
		return 99
	}
	if totalBytes <= 0 {
		return 0
	}
	var base int64
	switch phase {
	case asrPhaseRuntime:
		base = 0
	case asrPhaseModel:
		base = runtimeSize
	case asrPhaseTokens:
		base = runtimeSize + asrlocal.ModelFiles[asrPhaseModel].Size
	default:
		return 0
	}
	pct := int((base + done) * 100 / totalBytes)
	if pct > 99 {
		pct = 99
	}
	if pct < 0 {
		pct = 0
	}
	return pct
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
		} else {
			a.asrJob.progress = ASRProgressView{Phase: "done", Overall: 100}
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

	asset, _ := asrlocal.RuntimeAsset(runtime.GOOS, runtime.GOARCH)
	totalBytes := asset.Size + asrlocal.ModelFiles[asrPhaseModel].Size + asrlocal.ModelFiles[asrPhaseTokens].Size
	lastLogPct := map[string]int{}
	opts := asrlocal.Options{
		Mirrors: cfg.Mirrors,
		Logf:    logf,
		Progress: func(phase string, done, total int64) {
			pct := 0
			if total > 0 {
				pct = int(done * 100 / total)
				if pct > 100 {
					pct = 100
				}
			}
			next := ASRProgressView{Phase: phase, Pct: pct, Overall: asrOverallPct(phase, done, asset.Size, totalBytes)}
			// 进度快照供切页/重连回填；只有变化才推事件（1% 粒度足够平滑）。
			a.mu.Lock()
			changed := a.asrJob.progress != next
			a.asrJob.progress = next
			a.mu.Unlock()
			if changed {
				a.Emit("asr:install:progress", map[string]any{
					"phase": next.Phase, "pct": next.Pct, "overall": next.Overall,
				})
			}
			// 日志仍按阶段 ≥5% 节流，避免几十行刷屏（进度条由结构化事件驱动）。
			if total <= 0 || (pct-lastLogPct[phase] < 5 && pct != 0) {
				return
			}
			lastLogPct[phase] = pct
			if pct >= 100 {
				pct = 99 // 100 留给完成提示
			}
			logf(fmt.Sprintf("%s %d%%", asrPhaseLabel(phase), pct))
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
