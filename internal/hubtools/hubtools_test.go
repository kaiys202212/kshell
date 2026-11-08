package hubtools

import (
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func TestReady(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"空配置不就绪", Config{}, false},
		{"三者齐全就绪", Config{BaseURL: "https://api.example.com/v1", APIKey: "sk-x", Model: "gpt-4o"}, true},
		{"缺 BaseURL", Config{APIKey: "sk-x", Model: "gpt-4o"}, false},
		{"缺 APIKey", Config{BaseURL: "https://api.example.com/v1", Model: "gpt-4o"}, false},
		{"缺 Model", Config{BaseURL: "https://api.example.com/v1", APIKey: "sk-x"}, false},
		{"仅空白不就绪", Config{BaseURL: "  ", APIKey: "sk-x", Model: "m"}, false},
		{"空白会去 trim", Config{BaseURL: " u ", APIKey: "\tk ", Model: "\n m \r"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Ready(tc.cfg); got != tc.want {
				t.Fatalf("Ready(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}

// platCfg 构造平台式 ModelConfig 的便捷函数：三元组位于平台节点。
func platCfg(plats []config.ModelPlatform, defPlatform, activeModel string) config.ModelConfig {
	return config.ModelConfig{Platforms: plats, DefaultPlatform: defPlatform, ActiveModel: activeModel}
}

func TestFromModelConfig(t *testing.T) {
	t.Run("默认平台命中且就绪", func(t *testing.T) {
		got, ok := FromModelConfig(platCfg([]config.ModelPlatform{
			{ID: "p1", OpenAIBaseURL: " https://api.example.com/v1 ", APIKey: "sk-x"},
			{ID: "p2", OpenAIBaseURL: "https://other/v1", APIKey: "sk-y"},
		}, "p1", " gpt-4o "))
		if !ok {
			t.Fatal("期望就绪，实际不就绪")
		}
		if got.BaseURL != "https://api.example.com/v1" {
			t.Errorf("BaseURL 未取默认平台或未去空白: %q", got.BaseURL)
		}
		if got.APIKey != "sk-x" || got.Model != "gpt-4o" {
			t.Errorf("映射字段不符: %+v", got)
		}
	})
	t.Run("DefaultPlatform 空回落第一个平台", func(t *testing.T) {
		got, ok := FromModelConfig(platCfg([]config.ModelPlatform{
			{ID: "p1", OpenAIBaseURL: "https://first/v1", APIKey: "sk-1"},
			{ID: "p2", OpenAIBaseURL: "https://second/v1", APIKey: "sk-2"},
		}, "", "m1"))
		if !ok {
			t.Fatal("期望就绪，实际不就绪")
		}
		if got.BaseURL != "https://first/v1" || got.APIKey != "sk-1" {
			t.Errorf("应取第一个平台: %+v", got)
		}
	})
	t.Run("无平台不就绪", func(t *testing.T) {
		got, ok := FromModelConfig(platCfg(nil, "", "m1"))
		if ok {
			t.Error("无平台期望不就绪")
		}
		if strings.TrimSpace(got.BaseURL) != "" || strings.TrimSpace(got.APIKey) != "" {
			t.Errorf("无平台映射应为空端点: %+v", got)
		}
	})
	t.Run("平台缺 APIKey 不就绪但字段仍映射", func(t *testing.T) {
		got, ok := FromModelConfig(platCfg([]config.ModelPlatform{
			{ID: "p1", OpenAIBaseURL: "https://api.example.com/v1"},
		}, "p1", "gpt-4o"))
		if ok {
			t.Error("平台缺 APIKey 期望不就绪")
		}
		if got.BaseURL != "https://api.example.com/v1" || got.APIKey != "" || got.Model != "gpt-4o" {
			t.Errorf("映射字段不符: %+v", got)
		}
	})
	t.Run("ActiveModel 空/空白不就绪", func(t *testing.T) {
		for _, am := range []string{"", "   "} {
			_, ok := FromModelConfig(platCfg([]config.ModelPlatform{
				{ID: "p1", OpenAIBaseURL: "https://api.example.com/v1", APIKey: "sk-x"},
			}, "p1", am))
			if ok {
				t.Errorf("ActiveModel=%q 期望不就绪", am)
			}
		}
	})
	t.Run("零值不就绪", func(t *testing.T) {
		got, ok := FromModelConfig(config.ModelConfig{})
		if ok {
			t.Error("零值期望不就绪")
		}
		// 字段去空白后应全为空
		if strings.TrimSpace(got.BaseURL) != "" || strings.TrimSpace(got.APIKey) != "" || strings.TrimSpace(got.Model) != "" {
			t.Errorf("零值映射应为空串: %+v", got)
		}
	})
}
