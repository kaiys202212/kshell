package desktop

import (
	"errors"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

// parseModelSlash 识别整串形如 "/model <arg>" 的输入。
// 只有「整串就是一条 /model 指令」才算（避免误伤正文里提到 /model 的消息）。
func TestParseModelSlash(t *testing.T) {
	cases := []struct {
		in    string
		arg   string
		isCmd bool
	}{
		{"/model claude", "claude", true},
		{"  /model   claude  ", "claude", true},
		{"/model", "", true},
		{"/model ", "", true},
		{"/models", "", false},
		{"请把 /model claude 改掉", "", false},
		{"hello", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		arg, isCmd := parseModelSlash(c.in)
		if arg != c.arg || isCmd != c.isCmd {
			t.Errorf("parseModelSlash(%q) = (%q, %v), want (%q, %v)", c.in, arg, isCmd, c.arg, c.isCmd)
		}
	}
}

// checkModelSlash 判定该 /model 参数能否直接投递：
// 空参数（查询）与内置别名放行；平台模型名放行；其余拒绝。
func TestCheckModelSlash(t *testing.T) {
	cfg := config.ModelConfig{
		Enabled: true,
		Platforms: []config.ModelPlatform{
			{ID: "p1", Name: "P", Models: []string{"glm-5.3-flash", "kimi-k3"}},
		},
	}
	cases := []struct {
		arg     string
		wantErr bool
	}{
		{"", false},                 // 纯查询
		{"sonnet", false},           // 内置别名，CLI 能解析
		{"opus[1m]", false},         // 内置别名变体
		{"haiku-1m", false},         // 内置别名变体
		{"glm-5.3-flash", false},    // 平台模型名
		{"kimi-k3", false},          // 平台模型名
		{"claude", true},            // 裸名：CLI 不解析 → 网关 400
		{"gpt-5", true},             // 不在平台列表
		{"claude-sonnet-4-6", true}, // 不在平台列表
	}
	for _, c := range cases {
		err := checkModelSlash(c.arg, cfg)
		if (err != nil) != c.wantErr {
			t.Errorf("checkModelSlash(%q) err=%v, wantErr=%v", c.arg, err, c.wantErr)
		}
	}
}

// 拒绝时的稳定 key 应带参数，便于前端提示具体是哪个值不可用。
func TestCheckModelSlashErrorKey(t *testing.T) {
	err := checkModelSlash("claude", config.ModelConfig{})
	if err == nil {
		t.Fatal("claude 应被拒绝")
	}
	want := "err.chat.model_not_available|claude"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, errChatModelNotAvailable) {
		t.Errorf("应可 errors.Is 识别为 errChatModelNotAvailable")
	}
}
