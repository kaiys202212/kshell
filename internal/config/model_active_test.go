package config

import (
	"os"
	"testing"
)

// 无平台时 ActiveModel 必须清空：否则它会被当成「kshell 托管过模型」的唯一
// 痕迹，让启动同步每次清掉用户或 ccswitch 写入的 ANTHROPIC_*。
func TestModelActiveModelClearedWithoutPlatforms(t *testing.T) {
	p := tempPaths(t)
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "model:\n  enabled: false\n  active_model: m1\n"
	if err := os.WriteFile(p.Config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model.ActiveModel != "" {
		t.Fatalf("无平台时 ActiveModel 应清空，got %q", got.Model.ActiveModel)
	}
}
