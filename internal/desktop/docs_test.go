package desktop

import (
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

// newDocsApp 组装一个带临时 Layout 的 App，供文档/已读版本接口测试。
func newDocsApp(t *testing.T) (*App, config.Layout) {
	t.Helper()
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	app := NewAppWith(Options{Config: config.Default(), Layout: layout})
	return app, layout
}

func TestGetHelpDocRoutesByLang(t *testing.T) {
	app, _ := newDocsApp(t)
	zh := app.GetHelpDoc("zh-CN")
	en := app.GetHelpDoc("en")
	if zh == "" || en == "" {
		t.Fatal("help 文档 zh-CN 与 en 均应非空")
	}
	if zh == en {
		t.Fatal("zh-CN 与 en 内容应不同")
	}
	if got := app.GetHelpDoc("fr"); got != en {
		t.Fatal("非 zh-CN 应回退英文版")
	}
}

func TestGetChangelogDocRoutesByLang(t *testing.T) {
	app, _ := newDocsApp(t)
	zh := app.GetChangelogDoc("zh-CN")
	en := app.GetChangelogDoc("en")
	if zh == "" || en == "" {
		t.Fatal("changelog 文档 zh-CN 与 en 均应非空")
	}
	if zh == en {
		t.Fatal("zh-CN 与 en 内容应不同")
	}
	if got := app.GetChangelogDoc(""); got != en {
		t.Fatal("空 lang 应回退英文版")
	}
}

func TestMarkVersionSeenPersists(t *testing.T) {
	app, layout := newDocsApp(t)
	if got := app.GetLastSeenVersion(); got != "" {
		t.Fatalf("初始 LastSeenVersion 应为空, got %q", got)
	}
	if err := app.MarkVersionSeen("v1.2.3"); err != nil {
		t.Fatalf("MarkVersionSeen: %v", err)
	}
	if got := app.GetLastSeenVersion(); got != "v1.2.3" {
		t.Fatalf("GetLastSeenVersion = %q, want v1.2.3", got)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LastSeenVersion != "v1.2.3" {
		t.Fatalf("落盘 lastseenversion = %q, want v1.2.3", loaded.LastSeenVersion)
	}
}

func TestMarkVersionSeenNotReady(t *testing.T) {
	app := NewAppWith(Options{Config: config.Default()})
	if err := app.MarkVersionSeen("v1.2.3"); err == nil {
		t.Fatal("未装配 Layout 应返回错误")
	}
}
