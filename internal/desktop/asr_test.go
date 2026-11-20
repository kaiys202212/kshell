package desktop

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/asrlocal"
	"github.com/yangk/kshell/internal/config"
)

func newASRTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	return NewAppWith(Options{
		Config: config.Default(),
		Layout: config.Layout{
			Root:   dir,
			Config: filepath.Join(dir, "config.yaml"),
			Cache:  filepath.Join(dir, "cache"),
		},
		Home: dir,
	})
}

func TestASRStatusNotInstalled(t *testing.T) {
	app := newASRTestApp(t)
	st := app.GetASRStatus()
	if st.Installed || st.Partial {
		t.Fatalf("空目录不应就绪: %+v", st)
	}
	if st.Language != "auto" || !st.ITN {
		t.Fatalf("默认配置错误: %+v", st)
	}
	if st.Running {
		t.Fatal("初始不应有安装任务")
	}
}

func TestSaveASRSettingsPersists(t *testing.T) {
	app := newASRTestApp(t)
	st, err := app.SaveASRSettings(ASRSettingsInput{
		Enabled: true, PreferCloud: true, Language: "zh", ITN: false,
		Mirrors: []string{"https://m.example/{url}"}, Command: "my-asr {wav}",
	})
	if err != nil {
		t.Fatalf("SaveASRSettings: %v", err)
	}
	if !st.Enabled || !st.PreferCloud || st.Language != "zh" || st.ITN {
		t.Fatalf("状态回显错误: %+v", st)
	}
	// 落盘：重新加载配置应保持。
	cfg, err := config.Load(app.snapshot().Layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ASR.Enabled || !cfg.ASR.PreferCloud || cfg.ASR.Language != "zh" || !cfg.ASR.NoITN {
		t.Fatalf("配置未持久化: %+v", cfg.ASR)
	}
	if cfg.ASR.Command != "my-asr {wav}" || len(cfg.ASR.Mirrors) != 1 {
		t.Fatalf("命令/镜像未持久化: %+v", cfg.ASR)
	}
}

func TestLocalTranscriberGating(t *testing.T) {
	app := newASRTestApp(t)
	// 未启用：nil。
	if fn := app.localTranscriber(); fn != nil {
		t.Fatal("未启用时应返回 nil")
	}
	// 启用但未安装：nil（回落云端链）。
	if _, err := app.SaveASRSettings(ASRSettingsInput{Enabled: true, Language: "auto", ITN: true}); err != nil {
		t.Fatal(err)
	}
	if fn := app.localTranscriber(); fn != nil {
		t.Fatal("未安装时应返回 nil")
	}
	// 启用 + 自定义命令：即使没安装引擎也应返回闭包。
	if _, err := app.SaveASRSettings(ASRSettingsInput{
		Enabled: true, Language: "auto", ITN: true, Command: "my-asr {wav}",
	}); err != nil {
		t.Fatal(err)
	}
	if fn := app.localTranscriber(); fn == nil {
		t.Fatal("自定义命令应可用")
	}
}

func TestGetASRInstallLogEmpty(t *testing.T) {
	app := newASRTestApp(t)
	if log := app.GetASRInstallLog(); log != "" {
		t.Fatalf("初始日志应为空: %q", log)
	}
}

// TestInstallASRLocalReturnsPromptly 守住回归：InstallASRLocal 若在持有 a.mu 时再调
// runCtx()（内部重复加同一把不可重入锁），绑定会永久挂起，进而卡死整个 App。
func TestInstallASRLocalReturnsPromptly(t *testing.T) {
	app := newASRTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.mu.Lock()
	app.ctx = ctx
	app.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- app.InstallASRLocal() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("InstallASRLocal: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("InstallASRLocal 超过 2s 未返回：疑似持有 a.mu 时调 runCtx 造成自死锁")
	}

	// 立即取消下载并等任务收尾，避免 goroutine 写盘与 TempDir 清理竞争。
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for app.GetASRStatus().Running && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if app.GetASRStatus().Running {
		t.Fatal("后台安装任务未在 2s 内结束")
	}
}

// TestASROverallPct 覆盖体积加权的总体进度换算：三份文件接续、解压与封顶。
func TestASROverallPct(t *testing.T) {
	const runtimeSize = 1000
	model := asrlocal.ModelFiles[asrPhaseModel].Size
	tokens := asrlocal.ModelFiles[asrPhaseTokens].Size
	total := runtimeSize + model + tokens

	cases := []struct {
		name  string
		phase string
		done  int64
		want  int
	}{
		{"引擎起点", asrPhaseRuntime, 0, 0},
		{"引擎半程", asrPhaseRuntime, runtimeSize / 2, int(int64(runtimeSize/2) * 100 / total)},
		{"模型起点", asrPhaseModel, 0, int(runtimeSize * 100 / total)},
		{"模型完成封顶 99", asrPhaseModel, model, 99},
		{"词表完成封顶 99", asrPhaseTokens, tokens, 99},
		{"解压固定 99", asrPhaseExtract, 0, 99},
		{"未知阶段", "mystery", 5, 0},
	}
	for _, c := range cases {
		if got := asrOverallPct(c.phase, c.done, runtimeSize, total); got != c.want {
			t.Fatalf("%s: asrOverallPct(%q,%d) = %d, want %d", c.name, c.phase, c.done, got, c.want)
		}
	}
	if got := asrOverallPct(asrPhaseRuntime, 10, runtimeSize, 0); got != 0 {
		t.Fatalf("总大小为 0 时应返回 0, 得到 %d", got)
	}
}

// TestASRStatusReportsProgress 保证切页回来时进度条能按状态回填。
func TestASRStatusReportsProgress(t *testing.T) {
	app := newASRTestApp(t)
	app.mu.Lock()
	app.asrJob = asrInstallJob{
		running:  true,
		progress: ASRProgressView{Phase: asrPhaseModel, Pct: 40, Overall: 25},
	}
	app.mu.Unlock()
	st := app.GetASRStatus()
	if !st.Running {
		t.Fatal("状态应标记安装中")
	}
	if st.Progress != (ASRProgressView{Phase: asrPhaseModel, Pct: 40, Overall: 25}) {
		t.Fatalf("进度回填错误: %+v", st.Progress)
	}
}

func TestInstallASRLocalRequiresLayout(t *testing.T) {
	app := NewAppWith(Options{Config: config.Default()})
	if err := app.InstallASRLocal(); err == nil {
		t.Fatal("未装配 Layout 应报错")
	}
}
