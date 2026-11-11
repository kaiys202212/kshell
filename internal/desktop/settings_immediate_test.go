package desktop

import (
	"testing"

	"github.com/yangk/kshell/internal/config"
)

// SetModelEnabled 立即落盘启用开关；关闭后防抖同步走清理分支（Enabled=false）。
func TestSetModelEnabledImmediate(t *testing.T) {
	app, layout, _ := newSyncApp(t)
	setSyncPlatform(t, app)

	if err := app.SetModelEnabled(false); err != nil {
		t.Fatalf("SetModelEnabled(false): %v", err)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.Enabled {
		t.Fatal("Enabled 应已落盘为 false")
	}

	if err := app.SetModelEnabled(true); err != nil {
		t.Fatalf("SetModelEnabled(true): %v", err)
	}
	loaded, err = config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Model.Enabled {
		t.Fatal("Enabled 应已落盘为 true")
	}
}

// DeleteModelPlatform 立即删除平台：默认平台回落第一个；悬空 ActiveModel 清空。
func TestDeleteModelPlatformImmediate(t *testing.T) {
	app, layout, _ := newSyncApp(t)
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p1",
		ActiveModel:     "m1",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P1", OpenAIBaseURL: "https://p1.example", Models: []string{"m1"}},
			{ID: "p2", Name: "P2", OpenAIBaseURL: "https://p2.example", Models: []string{"m2"}},
		},
	}
	app.mu.Unlock()

	if err := app.DeleteModelPlatform("p1"); err != nil {
		t.Fatalf("DeleteModelPlatform: %v", err)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Model.Platforms) != 1 || loaded.Model.Platforms[0].ID != "p2" {
		t.Fatalf("p1 应被删除: %#v", loaded.Model.Platforms)
	}
	if loaded.Model.DefaultPlatform != "p2" {
		t.Fatalf("默认平台应回落 p2, got %q", loaded.Model.DefaultPlatform)
	}
	if loaded.Model.ActiveModel != "" {
		t.Fatalf("悬空 ActiveModel 应清空, got %q", loaded.Model.ActiveModel)
	}

	// 归属正确的 ActiveModel 保留。
	app.mu.Lock()
	app.opts.Config.Model = config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p1",
		ActiveModel:     "m1",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P1", OpenAIBaseURL: "https://p1.example", Models: []string{"m1"}},
			{ID: "p2", Name: "P2", OpenAIBaseURL: "https://p2.example", Models: []string{"m2"}},
		},
	}
	app.mu.Unlock()
	if err := app.DeleteModelPlatform("p2"); err != nil {
		t.Fatalf("DeleteModelPlatform(p2): %v", err)
	}
	loaded, err = config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.ActiveModel != "m1" {
		t.Fatalf("归属正确的 ActiveModel 应保留, got %q", loaded.Model.ActiveModel)
	}
}

// 删除不存在的平台报错且不改配置。
func TestDeleteModelPlatformUnknown(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)

	if err := app.DeleteModelPlatform("nope"); err == nil {
		t.Fatal("未知平台应报错")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.opts.Config.Model.Platforms) != 1 {
		t.Fatalf("内存配置不应被改动: %#v", app.opts.Config.Model.Platforms)
	}
}

// Save 失败时内存配置不得被改坏（浅拷贝写穿陷阱）。
func TestDeleteModelPlatformSaveFailureLeavesMemoryIntact(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)
	app.mu.Lock()
	app.opts.Config.Model.Platforms = append(app.opts.Config.Model.Platforms,
		config.ModelPlatform{ID: "p2", Name: "P2", OpenAIBaseURL: "https://p2.example"})
	// Config 路径指向目录 → config.Save 必失败
	app.opts.Layout.Config = t.TempDir()
	app.mu.Unlock()

	if err := app.DeleteModelPlatform("p2"); err == nil {
		t.Fatal("Save 应失败")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.opts.Config.Model.Platforms) != 2 {
		t.Fatalf("Save 失败不得改坏内存配置, got %#v", app.opts.Config.Model.Platforms)
	}
}

// 防抖同步确有被调度：SetModelEnabled 后调度器已创建并排入任务。
// 清理分支本身的落盘效果由 agentsync 包既有用例覆盖（claude_test.go 等）。
func TestSetModelEnabledSchedulesSync(t *testing.T) {
	app, _, _ := newSyncApp(t)
	setSyncPlatform(t, app)

	if err := app.SetModelEnabled(false); err != nil {
		t.Fatalf("SetModelEnabled: %v", err)
	}
	if app.agentSyncSched == nil {
		t.Fatal("调度器应已创建")
	}
}
