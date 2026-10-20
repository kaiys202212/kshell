package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeLastAssistantModelTakesLatestReal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	body := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","message":{"role":"assistant","model":"old-m","content":"a"}}`,
		`{"type":"assistant","message":{"role":"assistant","model":"<synthetic>","content":"x"}}`,
		`{"type":"assistant","message":{"role":"assistant","model":"glm-5-3-flash","content":"b"}}`,
		``,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ClaudeLastAssistantModel(path); got != "glm-5-3-flash" {
		t.Fatalf("got %q want glm-5-3-flash", got)
	}
}

func TestClaudeLastAssistantModelSkipsSyntheticOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	body := `{"type":"assistant","message":{"model":"<synthetic>","content":"x"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ClaudeLastAssistantModel(path); got != "" {
		t.Fatalf("仅 synthetic 应返回空, got %q", got)
	}
}

func TestClaudeLastAssistantModelMissingFile(t *testing.T) {
	if got := ClaudeLastAssistantModel(filepath.Join(t.TempDir(), "gone.jsonl")); got != "" {
		t.Fatalf("缺失文件应返回空, got %q", got)
	}
}
