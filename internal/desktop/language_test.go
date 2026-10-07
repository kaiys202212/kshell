package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/applang"
	"github.com/yangk/kshell/internal/config"
)

// langLayout 造一份落在临时目录的 Layout（照 appearance_test 的构造方式）。
func langLayout(t *testing.T) config.Layout {
	t.Helper()
	dir := t.TempDir()
	return config.Layout{
		Root:    dir,
		Config:  filepath.Join(dir, "config.yaml"),
		Cache:   filepath.Join(dir, "cache"),
		Locales: filepath.Join(dir, "locales"),
	}
}

func TestGetLanguageDefaultsToEn(t *testing.T) {
	a := NewAppWith(Options{Config: config.Default()})
	got := a.GetLanguage()
	if got.Configured != "en" {
		t.Fatalf("configured = %q, want en", got.Configured)
	}
	if got.Resolved != "en" {
		t.Fatalf("resolved = %q, want en", got.Resolved)
	}
}

func TestSetLanguagePersistsAndEmits(t *testing.T) {
	layout := langLayout(t)
	var events []string
	var lastPayload any
	a := NewAppWith(Options{
		Config: config.Default(),
		Layout: layout,
		Emit: func(name string, data ...any) {
			events = append(events, name)
			if len(data) > 0 {
				lastPayload = data[0]
			}
		},
	})
	t.Cleanup(func() { applang.Set("en") }) // 语言是进程级运行态，测完还原

	if err := a.SetLanguage("zh-CN"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	got := a.GetLanguage()
	if got.Configured != "zh-CN" || got.Resolved != "zh-CN" {
		t.Fatalf("language = %+v, want configured/resolved zh-CN", got)
	}
	if len(events) == 0 || events[len(events)-1] != "language:changed" {
		t.Fatalf("events = %v, want last language:changed", events)
	}
	if info, ok := lastPayload.(LanguageInfo); !ok || info.Configured != "zh-CN" {
		t.Fatalf("payload = %#v, want LanguageInfo{Configured: zh-CN}", lastPayload)
	}

	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != "zh-CN" {
		t.Fatalf("persisted = %q, want zh-CN", loaded.Language)
	}
}

// TestSetLanguageNotReadyDoesNotPollute 仿 TestSetAppearanceModeWithoutLayoutIsNotReady：
// 无 Layout 时 saveConfig 返回 errNotReady，失败路径不得污染 applang 运行态、
// 不得触碰托盘刷新、不得改内存配置。
func TestSetLanguageNotReadyDoesNotPollute(t *testing.T) {
	a := NewAppWith(Options{Config: config.Default()}) // 无 Layout → errNotReady
	orig := refreshTrayTextFn
	calls := 0
	refreshTrayTextFn = func() { calls++ }
	t.Cleanup(func() {
		refreshTrayTextFn = orig
		applang.Set("en")
	})
	applang.Set("en") // 显式归零，防其它测试串扰

	if err := a.SetLanguage("zh-CN"); err != errNotReady {
		t.Fatalf("err = %v, want errNotReady", err)
	}
	if got := applang.T("tray.exit"); got != "Quit" {
		t.Fatalf("运行态被污染：tray.exit = %q, want Quit", got)
	}
	if calls != 0 {
		t.Fatalf("refresh 次数 = %d, want 0", calls)
	}
	if got := a.GetLanguage(); got.Configured != "en" || got.Resolved != "en" {
		t.Fatalf("内存配置被污染：language = %+v, want en", got)
	}
}

func TestSetLanguageKeepsExternalLocaleCode(t *testing.T) {
	layout := langLayout(t)
	a := NewAppWith(Options{
		Config: config.Default(),
		Layout: layout,
		Emit:   func(string, ...any) {},
	})
	t.Cleanup(func() { applang.Set("en") })

	// 外部语言包引入的语言（如 ja）可被选择并持久化，Resolved 原样直通
	if err := a.SetLanguage("ja"); err != nil {
		t.Fatalf("SetLanguage(ja): %v", err)
	}
	got := a.GetLanguage()
	if got.Configured != "ja" || got.Resolved != "ja" {
		t.Fatalf("language = %+v, want configured/resolved ja", got)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != "ja" {
		t.Fatalf("persisted = %q, want ja", loaded.Language)
	}
	// Go 直显文案对非 zh-CN 语言回落英文（applang.Set 语义）
	if applang.T("tray.exit") != "Quit" {
		t.Fatalf("直显文案 = %q, want Quit（英文回落）", applang.T("tray.exit"))
	}
}

func TestSetLanguageInvalidFallsBackToEn(t *testing.T) {
	layout := langLayout(t)
	a := NewAppWith(Options{
		Config: config.Default(),
		Layout: layout,
		Emit:   func(string, ...any) {},
	})
	t.Cleanup(func() { applang.Set("en") })

	if err := a.SetLanguage("bogus"); err != nil {
		t.Fatalf("SetLanguage(bogus): %v", err)
	}
	if got := a.GetLanguage(); got.Configured != "en" || got.Resolved != "en" {
		t.Fatalf("language = %+v, want configured/resolved en", got)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != "en" {
		t.Fatalf("persisted = %q, want en", loaded.Language)
	}

	// 含非法字符的值不匹配 locale 码模式，同样回落 en
	if err := a.SetLanguage("bogus!"); err != nil {
		t.Fatalf("SetLanguage(bogus!): %v", err)
	}
	if got := a.GetLanguage(); got.Configured != "en" || got.Resolved != "en" {
		t.Fatalf("bogus! 后 language = %+v, want en", got)
	}
	loaded, err = config.Load(layout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Language != "en" {
		t.Fatalf("bogus! persisted = %q, want en", loaded.Language)
	}
}

func TestSetLanguageSystemResolves(t *testing.T) {
	layout := langLayout(t)
	a := NewAppWith(Options{
		Config: config.Default(),
		Layout: layout,
		Emit:   func(string, ...any) {},
	})
	t.Cleanup(func() { applang.Set("en") })

	if err := a.SetLanguage("system"); err != nil {
		t.Fatalf("SetLanguage(system): %v", err)
	}
	got := a.GetLanguage()
	if got.Configured != "system" {
		t.Fatalf("configured = %q, want system", got.Configured)
	}
	if got.Resolved != "en" && got.Resolved != "zh-CN" {
		t.Fatalf("resolved = %q, want en or zh-CN", got.Resolved)
	}
}

func TestSetLanguageRefreshesTrayOnlyWhenActive(t *testing.T) {
	layout := langLayout(t)
	a := NewAppWith(Options{
		Config: config.Default(),
		Layout: layout,
		Emit:   func(string, ...any) {},
	})
	orig := refreshTrayTextFn
	calls := 0
	refreshTrayTextFn = func() { calls++ }
	t.Cleanup(func() {
		refreshTrayTextFn = orig
		applang.Set("en")
	})

	// 托盘未激活：不得触碰菜单刷新
	if err := a.SetLanguage("zh-CN"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if calls != 0 {
		t.Fatalf("tray inactive 时 refresh 次数 = %d, want 0", calls)
	}

	// 托盘激活：语言切换后刷新一次（句柄为 nil 时 refreshTrayText 自身也是安全无操作）
	a.trayActive = true
	if err := a.SetLanguage("system"); err != nil {
		t.Fatalf("SetLanguage(system): %v", err)
	}
	if calls != 1 {
		t.Fatalf("tray active 时 refresh 次数 = %d, want 1", calls)
	}
}

func TestLoadExternalLocalesMissingDirReturnsEmpty(t *testing.T) {
	layout := langLayout(t)
	layout.Locales = filepath.Join(t.TempDir(), "nope") // 明确不存在的目录
	a := NewAppWith(Options{Config: config.Default(), Layout: layout})

	got, err := a.LoadExternalLocales()
	if err != nil {
		t.Fatalf("LoadExternalLocales: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty map", got)
	}
}

func TestLoadExternalLocalesReadsValidAndSkipsOthers(t *testing.T) {
	layout := langLayout(t)
	dir := layout.Locales
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	valid := `{"hello":"world"}`
	if err := os.WriteFile(filepath.Join(dir, "xx.json"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	// 非法 JSON 跳过
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 非 .json 后缀跳过
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 子目录跳过（内含 .json 也不读）
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "y.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewAppWith(Options{Config: config.Default(), Layout: layout})
	got, err := a.LoadExternalLocales()
	if err != nil {
		t.Fatalf("LoadExternalLocales: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries (%v), want exactly 1", len(got), got)
	}
	if got["xx.json"] != valid {
		t.Fatalf("xx.json = %q, want %q", got["xx.json"], valid)
	}
}

func TestLoadExternalLocalesReadErrorReturnsNil(t *testing.T) {
	layout := langLayout(t)
	// locales 指向普通文件（非目录）→ ReadDir 失败且非「目录不存在」
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	layout.Locales = file

	a := NewAppWith(Options{Config: config.Default(), Layout: layout})
	got, err := a.LoadExternalLocales()
	if err == nil {
		t.Fatal("ReadDir 失败应返回错误")
	}
	if got != nil {
		t.Fatalf("出错时应返回 nil map, got %#v", got)
	}
}
