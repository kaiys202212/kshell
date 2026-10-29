package agentsync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func TestInputFromModelCarriesDefaultPlatform(t *testing.T) {
	in := InputFromModel(config.ModelConfig{Enabled: true, DefaultPlatform: "p1"})
	if in.DefaultPlatformID != "p1" {
		t.Fatalf("DefaultPlatformID = %q, want p1", in.DefaultPlatformID)
	}
}

// Managed 决定是否允许改写各工具自有模型配置：只有 kshell 留有配置痕迹
//（启用、平台、默认平台或 ActiveModel）才算接管过；从未配置过必须原样保留。
func TestSyncInputManaged(t *testing.T) {
	cases := []struct {
		name string
		in   SyncInput
		want bool
	}{
		{"空配置", SyncInput{}, false},
		{"仅空白", SyncInput{DefaultPlatformID: " ", ActiveModel: "  "}, false},
		{"启用", SyncInput{Enabled: true}, true},
		{"有平台", SyncInput{Platforms: []config.ModelPlatform{{ID: "p1"}}}, true},
		{"仅 ActiveModel 残留", SyncInput{ActiveModel: "m"}, true},
		{"仅 DefaultPlatform 残留", SyncInput{DefaultPlatformID: "p1"}, true},
	}
	for _, c := range cases {
		if got := c.in.Managed(); got != c.want {
			t.Fatalf("%s: Managed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestInputFromModelCarriesActiveModel(t *testing.T) {
	in := InputFromModel(config.ModelConfig{Enabled: true, ActiveModel: "glm-5.3-flash"})
	if in.ActiveModel != "glm-5.3-flash" {
		t.Fatalf("ActiveModel = %q", in.ActiveModel)
	}
}

// 未配置过模型时 SyncAll 不得创建任何工具配置文件：Claude 只写登录标记
//（~/.claude.json），settings.json / opencode.json / models.json 都不应出现。
func TestSyncAllUnmanagedDoesNotCreateModelFiles(t *testing.T) {
	home := t.TempDir()
	if err := SyncAll(home, SyncInput{}, []string{"opencode", "codebuddy", "claude"}); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	for _, rel := range []string{
		filepath.Join(".claude", "settings.json"),
		filepath.Join(".config", "opencode", "opencode.json"),
		filepath.Join(".codebuddy", "models.json"),
	} {
		if _, err := os.Stat(filepath.Join(home, rel)); !os.IsNotExist(err) {
			t.Fatalf("不应创建 %s: err=%v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err != nil {
		t.Fatalf("应写入登录标记: %v", err)
	}
}

// 并发写同一目标（TUI 与桌面端/多个 goroutine 同时同步）必须全部成功，
// 文件保持完整 JSON 且不留下临时文件。Windows 上 rename 会因 sharing violation
// 失败，writeAtomic 需要退避重试；这里用真实并发锁住该行为。
func TestWriteAtomicConcurrentWritersAllSucceed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	const writers = 4
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"writer":%d,"pad":%q}`, i, strings.Repeat("x", 1024))
			errs <- writeAtomic(path, []byte(payload))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发写不应失败: %v", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("并发写后文件应仍是完整 JSON: %v (%q)", err, raw)
	}
	if _, ok := doc["writer"]; !ok {
		t.Fatalf("缺少 writer 字段: %v", doc)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".settings.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("不应残留临时文件: %v", leftovers)
	}
}
