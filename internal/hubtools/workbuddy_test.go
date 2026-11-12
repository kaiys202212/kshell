package hubtools

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/workbuddy"
)

func TestReadyWorkbuddyPreset(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"workbuddy 只需模型名", Config{Preset: workbuddy.PresetID, Model: "hy3"}, true},
		{"workbuddy 缺模型名", Config{Preset: workbuddy.PresetID}, false},
		{"workbuddy 端点缺失不算缺", Config{Preset: workbuddy.PresetID, Model: "hy3", BaseURL: "", APIKey: ""}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Ready(tc.cfg); got != tc.want {
				t.Fatalf("Ready(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}

func TestFromModelConfigWorkbuddy(t *testing.T) {
	got, ok := FromModelConfig(platCfg([]config.ModelPlatform{
		{ID: "wb", Name: "WorkBuddy", Preset: workbuddy.PresetID, Models: []string{"hy3", "glm-5.3"}},
	}, "wb", "hy3"))
	if !ok {
		t.Fatal("workbuddy 平台配了模型应就绪")
	}
	if got.Preset != workbuddy.PresetID {
		t.Fatalf("Preset 未透传: %+v", got)
	}
	if got.BaseURL != "" || got.APIKey != "" {
		t.Fatalf("workbuddy 平台端点/密钥应为空（动态凭据）: %+v", got)
	}
	if got.Model != "hy3" {
		t.Fatalf("Model 不符: %+v", got)
	}
}

func TestOnceWorkbuddyBranch(t *testing.T) {
	origCred, origChat := wbLoadCredential, wbChatOnce
	defer func() { wbLoadCredential, wbChatOnce = origCred, origChat }()

	var gotBody []byte
	wbLoadCredential = func() (workbuddy.Credential, error) {
		return workbuddy.Credential{AccessToken: "tok", UID: "u1"}, nil
	}
	wbChatOnce = func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error) {
		gotBody = body
		if cred.AccessToken != "tok" {
			t.Fatalf("凭据未透传: %+v", cred)
		}
		return []byte(`{"choices":[{"message":{"role":"assistant","content":"wb-ok"}}]}`), nil
	}

	c := NewClient(Config{Preset: workbuddy.PresetID, Model: "hy3"})
	resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "你好"}})
	if err != nil {
		t.Fatalf("workbuddy 分支请求失败: %v", err)
	}
	if resp.Text != "wb-ok" {
		t.Fatalf("响应解析不符: %+v", resp)
	}
	if !strings.Contains(string(gotBody), `"model":"hy3"`) {
		t.Fatalf("请求体应带模型名: %s", gotBody)
	}
}

func TestOnceWorkbuddyErrorPropagates(t *testing.T) {
	origCred, origChat := wbLoadCredential, wbChatOnce
	defer func() { wbLoadCredential, wbChatOnce = origCred, origChat }()

	wbLoadCredential = func() (workbuddy.Credential, error) {
		return workbuddy.Credential{}, context.DeadlineExceeded
	}
	c := NewClient(Config{Preset: workbuddy.PresetID, Model: "hy3"})
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}); err == nil {
		t.Fatal("凭据失败应透传错误")
	}
}
