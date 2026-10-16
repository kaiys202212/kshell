package providers

import (
	"encoding/json"
	"reflect"
	"testing"
)

// 解析 args 里 --settings 的值（最后一个），返回对象与是否存在。
func settingsArgObj(t *testing.T, args []string) (map[string]any, bool) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--settings" {
			var m map[string]any
			if err := json.Unmarshal([]byte(args[i+1]), &m); err != nil {
				t.Fatalf("--settings 值应为合法 JSON: %v (%q)", err, args[i+1])
			}
			return m, true
		}
	}
	return nil, false
}

func TestMergeSettingsArgAppendsWhenAbsent(t *testing.T) {
	obj := map[string]any{"env": map[string]any{"A": "1"}}
	out := MergeSettingsArg([]string{"--resume", "s1"}, obj)
	// 前置：--settings 是会话级全局 flag，前置维持「注入参数全部在前」不变量
	if !reflect.DeepEqual(out[2:], []string{"--resume", "s1"}) {
		t.Fatalf("原参数应保留在尾部: %v", out)
	}
	if out[0] != "--settings" {
		t.Fatalf("应前置 --settings: %v", out)
	}
	m, ok := settingsArgObj(t, out)
	if !ok {
		t.Fatalf("应有 --settings: %v", out)
	}
	if !reflect.DeepEqual(m, obj) {
		t.Fatalf("settings = %+v, want %+v", m, obj)
	}
}

func TestMergeSettingsArgMergesWithTheme(t *testing.T) {
	args := []string{"--settings", `{"theme":"dark"}`}
	obj := map[string]any{"env": map[string]any{"ANTHROPIC_MODEL": "m"}}
	out := MergeSettingsArg(args, obj)
	m, ok := settingsArgObj(t, out)
	if !ok {
		t.Fatalf("应保留 --settings: %v", out)
	}
	// 只允许一组 --settings，且同时含主题与 env
	if len(out) != 2 {
		t.Fatalf("应合并为单组参数: %v", out)
	}
	if m["theme"] != "dark" {
		t.Fatalf("主题键应保留: %+v", m)
	}
	env, _ := m["env"].(map[string]any)
	if env["ANTHROPIC_MODEL"] != "m" {
		t.Fatalf("env 键应合入: %+v", m)
	}
}

func TestMergeSettingsArgEnvKeyLevelMerge(t *testing.T) {
	// 既有 env 有别的键：键级浅合并，同名注入键覆盖，其余保留
	args := []string{"--settings", `{"theme":"light","env":{"KEEP":"yes","ANTHROPIC_MODEL":"old"}}`}
	obj := map[string]any{"env": map[string]any{"ANTHROPIC_MODEL": "new"}}
	out := MergeSettingsArg(args, obj)
	m, _ := settingsArgObj(t, out)
	env, _ := m["env"].(map[string]any)
	if env["KEEP"] != "yes" || env["ANTHROPIC_MODEL"] != "new" {
		t.Fatalf("env 应键级合并: %+v", m)
	}
}

func TestMergeSettingsArgFileValueSkipped(t *testing.T) {
	args := []string{"--settings", `C:\tmp\s.json`, "--other"}
	out := MergeSettingsArg(args, map[string]any{"env": map[string]any{"A": "1"}})
	if !reflect.DeepEqual(out, args) {
		t.Fatalf("值为文件路径时应原样返回: %v", out)
	}
}

func TestMergeSettingsArgBadJSONSkipped(t *testing.T) {
	args := []string{"--settings", "{bad", "--other"}
	out := MergeSettingsArg(args, map[string]any{"env": map[string]any{"A": "1"}})
	if !reflect.DeepEqual(out, args) {
		t.Fatalf("非法 JSON 时应原样返回: %v", out)
	}
}
