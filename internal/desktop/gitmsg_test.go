package desktop

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/remote"
)

func readyModelConfig(baseURL string) config.ModelConfig {
	return config.ModelConfig{
		Enabled: true,
		Platforms: []config.ModelPlatform{{
			ID:            "p1",
			Name:          "test",
			OpenAIBaseURL: baseURL,
			APIKey:        "k",
			Models:        []string{"m"},
		}},
		ActivePlatformID: "p1",
		ActiveModel:      "m",
	}
}

func gitRunIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGenerateCommitMessage_模型未就绪(t *testing.T) {
	env := newFilesEnv(t)
	_, err := env.app.GenerateCommitMessage(env.root, "", "zh-CN")
	if err == nil || !strings.Contains(err.Error(), "err.git.ai_model_not_ready") {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateCommitMessage_无暂存变更(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	env := newFilesEnv(t)
	env.app.opts.Config.Model = readyModelConfig("http://127.0.0.1:1")
	gitRunIn(t, env.root, "init", "-b", "main")
	gitRunIn(t, env.root, "config", "user.email", "t@t")
	gitRunIn(t, env.root, "config", "user.name", "t")
	gitRunIn(t, env.root, "add", ".")
	gitRunIn(t, env.root, "commit", "-m", "init")

	_, err := env.app.GenerateCommitMessage(env.root, "", "zh-CN")
	if err == nil || !strings.Contains(err.Error(), "err.git.ai_no_staged") {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateCommitMessage_生成并清洗(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	env := newFilesEnv(t)
	gitRunIn(t, env.root, "init", "-b", "main")
	gitRunIn(t, env.root, "config", "user.email", "t@t")
	gitRunIn(t, env.root, "config", "user.name", "t")
	gitRunIn(t, env.root, "add", ".")
	gitRunIn(t, env.root, "commit", "-m", "init")
	writeFile(t, env.root, "main.go", "package main\n\nfunc main() { println(1) }\n")
	gitRunIn(t, env.root, "add", "main.go")

	bodyCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodyCh <- string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, "```\nfeat: 支持 AI 生成提交信息\n```")
	}))
	defer srv.Close()
	env.app.opts.Config.Model = readyModelConfig(srv.URL)

	msg, err := env.app.GenerateCommitMessage(env.root, "", "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "feat: 支持 AI 生成提交信息" {
		t.Fatalf("msg = %q", msg)
	}
	body := <-bodyCh
	if !strings.Contains(body, "+func main() { println(1) }") {
		t.Fatalf("请求体缺少 staged diff: %s", body)
	}
	if !strings.Contains(body, `"model":"m"`) {
		t.Fatalf("请求体未使用平台默认模型: %s", body)
	}
	if !strings.Contains(body, "中文") {
		t.Fatalf("请求体缺少输出语言约束: %s", body)
	}
}

func TestOneLine(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a\nb", "a"},
		{"err.x|arg|more", "err.x arg more"},
		{"  spaced  \n\n", "spaced"},
		{strings.Repeat("好", 300), strings.Repeat("好", 200)},
	}
	for _, c := range cases {
		if got := oneLine(c.in); got != c.want {
			t.Fatalf("oneLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGenerateCommitMessage_模型空回复(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	env := newFilesEnv(t)
	gitRunIn(t, env.root, "init", "-b", "main")
	gitRunIn(t, env.root, "config", "user.email", "t@t")
	gitRunIn(t, env.root, "config", "user.name", "t")
	gitRunIn(t, env.root, "add", ".")
	gitRunIn(t, env.root, "commit", "-m", "init")
	writeFile(t, env.root, "main.go", "package main\n\nfunc main() { println(2) }\n")
	gitRunIn(t, env.root, "add", "main.go")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"   "}}]}`)
	}))
	defer srv.Close()
	env.app.opts.Config.Model = readyModelConfig(srv.URL)

	_, err := env.app.GenerateCommitMessage(env.root, "", "zh-CN")
	if err == nil || !strings.Contains(err.Error(), "err.git.ai_empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateCommitMessage_调用失败(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	env := newFilesEnv(t)
	gitRunIn(t, env.root, "init", "-b", "main")
	gitRunIn(t, env.root, "config", "user.email", "t@t")
	gitRunIn(t, env.root, "config", "user.name", "t")
	gitRunIn(t, env.root, "add", ".")
	gitRunIn(t, env.root, "commit", "-m", "init")
	writeFile(t, env.root, "main.go", "package main\n\nfunc main() { println(2) }\n")
	gitRunIn(t, env.root, "add", "main.go")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom\nline2|pipe")
	}))
	defer srv.Close()
	env.app.opts.Config.Model = readyModelConfig(srv.URL)

	_, err := env.app.GenerateCommitMessage(env.root, "", "zh-CN")
	if err == nil || !strings.HasPrefix(err.Error(), "err.git.ai_failed|") {
		t.Fatalf("err = %v", err)
	}
	if strings.Count(err.Error(), "|") != 1 || strings.ContainsAny(err.Error(), "\r\n") {
		t.Fatalf("错误细节未压成单行单分隔: %q", err)
	}
}

func TestGenerateCommitMessage_远端(t *testing.T) {
	const diff = "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n"
	bodyCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodyCh <- string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, "feat: remote flag")
	}))
	defer srv.Close()

	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		switch {
		case strings.Contains(cmd, "command -v git"):
			return []byte("/usr/bin/git\n"), nil, nil
		case strings.Contains(cmd, "diff --cached") && strings.Contains(cmd, "head -c"):
			return []byte(diff), nil, nil
		}
		return nil, nil, fmtUnexpected(cmd)
	}
	app, ref := newSSHFilesEnv(t, run)
	app.opts.Config.Model = readyModelConfig(srv.URL)

	msg, err := app.GenerateCommitMessage(ref, "", "en")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "feat: remote flag" {
		t.Fatalf("msg = %q", msg)
	}
	body := <-bodyCh
	if !strings.Contains(body, "+new") || !strings.Contains(body, "English") {
		t.Fatalf("请求体缺少远端 diff 或英文约束: %s", body)
	}
}
