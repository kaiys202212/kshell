package desktop

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
)

// 同步失败必须留痕：models.json 被外部改坏（BOM/损坏）时 Sync 若静默失败，
// 注入冻结将无迹可循（2026-11-01 codebuddy 双前缀排查）。断言 SyncAll 的
// 错误经 log.Printf 落日志。
func TestSyncAgentModelsLogsFailure(t *testing.T) {
	// 用独立 App：home 下 codebuddy models.json 为目录 → ReadFile 报错。
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codebuddy", "models.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := newChatTestApp(t, nil, nil)
	env.app.mu.Lock()
	env.app.opts.Home = home
	env.app.tools = append(env.app.tools, discovery.Tool{ID: "codebuddy", Installed: true})
	env.app.opts.Config.Model = config.ModelConfig{
		Enabled:         true,
		DefaultPlatform: "p1",
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", OpenAIBaseURL: "https://x.example/v1", APIKey: "k", Models: []string{"m1"}},
		},
	}
	env.app.mu.Unlock()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	env.app.syncAgentModels()

	if !strings.Contains(buf.String(), "agentsync") {
		t.Fatalf("同步失败应写含 agentsync 的日志, got %q", buf.String())
	}
}
