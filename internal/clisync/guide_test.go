package clisync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/skills"
)

// installGuide 首写：含宿主目录对照表、内嵌 SKILL.md 全文、绝对路径调用说明、
// 安全边界与版本行。
func TestInstallGuideWrites(t *testing.T) {
	home := t.TempDir()
	state, errKey := installGuide(home, "v1.0.0")
	if state != skillInstalled || errKey != "" {
		t.Fatalf("state=%q errKey=%q, want installed/空", state, errKey)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".kshell", "host-install.md"))
	if err != nil {
		t.Fatalf("说明书未写入: %v", err)
	}
	text := string(raw)
	claudeRoot, _ := skills.TargetRoot("claude", home)
	for _, want := range []string{
		claudeRoot,             // 目录对照表（本机解析路径）
		"- codebuddy: ",        // WorkBuddy 落点条目
		"SKILL.md content",     // 内嵌技能全文的节标题
		"name: kshell",         // SKILL.md frontmatter
		"~/.kshell/bin/kshell", // 绝对路径调用说明
		"Safety boundary",      // 安全边界
		"kshell agent list",    // 验收命令
		"v1.0.0",               // 版本行
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("说明书缺少 %q", want)
		}
	}
}

// installGuide 幂等：同版本重复生成跳过（current）；版本变化重写。
func TestInstallGuideIdempotent(t *testing.T) {
	home := t.TempDir()
	if state, _ := installGuide(home, "v1.0.0"); state != skillInstalled {
		t.Fatalf("首写 state=%q", state)
	}
	if state, _ := installGuide(home, "v1.0.0"); state != stateCurrent {
		t.Fatalf("二写 state=%q, want current", state)
	}
	if state, _ := installGuide(home, "v2.0.0"); state != skillInstalled {
		t.Fatalf("版本变化 state=%q, want installed", state)
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".kshell", "host-install.md"))
	if !strings.Contains(string(raw), "v2.0.0") {
		t.Fatal("版本变化后说明书应更新")
	}
}

// installGuide 目标被目录占用（异常现场）时返回 wire key 错误，不 panic。
func TestInstallGuideTargetBlocked(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".kshell", "host-install.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	state, errKey := installGuide(home, "v1.0.0")
	if state != stateError || !strings.HasPrefix(errKey, "err.hostsync.guide_write|") {
		t.Fatalf("state=%q errKey=%q, want error/err.hostsync.guide_write|", state, errKey)
	}
}
