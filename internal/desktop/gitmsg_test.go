package desktop

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
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
