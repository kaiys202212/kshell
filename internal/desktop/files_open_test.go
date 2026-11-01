package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenInDefaultAppRejectsOutside(t *testing.T) {
	env := newFilesEnv(t)
	if err := env.app.OpenInDefaultApp(env.root, filepath.Join(env.root, "..", "x.html")); err == nil {
		t.Fatal("越界应失败")
	}
}

// 前端聊天附件分派（chatAttachment 的 'default' 分支）传的是相对路径，
// resolveWorkspaceFile 须先拼回工作区根再校验，否则一律误判越界。
func TestOpenInDefaultAppAcceptsRelativePath(t *testing.T) {
	env := newFilesEnv(t)

	var opened string
	prev := openLocalFile
	openLocalFile = func(abs string) error {
		opened = abs
		return nil
	}
	t.Cleanup(func() { openLocalFile = prev })

	if err := env.app.OpenInDefaultApp(env.root, "readme.md"); err != nil {
		t.Fatalf("相对路径应可打开: %v", err)
	}
	if filepath.Base(opened) != "readme.md" {
		t.Fatalf("opened = %q, want readme.md", opened)
	}
}

// 相对路径支持不得引入穿越漏洞：../ 仍须拒绝。
func TestOpenInDefaultAppRejectsRelativeEscape(t *testing.T) {
	env := newFilesEnv(t)
	if err := env.app.OpenInDefaultApp(env.root, filepath.Join("..", "x.html")); err == nil {
		t.Fatal("相对越界应失败")
	}
}

func TestOpenInDefaultAppRejectsMissing(t *testing.T) {
	env := newFilesEnv(t)
	if err := env.app.OpenInDefaultApp(env.root, filepath.Join(env.root, "no-such.html")); err == nil {
		t.Fatal("不存在应失败")
	}
}

func TestOpenInDefaultAppRejectsDir(t *testing.T) {
	env := newFilesEnv(t)
	dir := filepath.Join(env.root, "subdir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := env.app.OpenInDefaultApp(env.root, dir); err == nil {
		t.Fatal("目录应失败")
	}
}

func TestOpenInDefaultAppOpensResolvedFile(t *testing.T) {
	env := newFilesEnv(t)
	path := filepath.Join(env.root, "page.html")
	if err := os.WriteFile(path, []byte("<html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}

	var opened string
	prev := openLocalFile
	openLocalFile = func(abs string) error {
		opened = abs
		return nil
	}
	t.Cleanup(func() { openLocalFile = prev })

	if err := env.app.OpenInDefaultApp(env.root, path); err != nil {
		t.Fatalf("OpenInDefaultApp: %v", err)
	}
	if opened != path {
		// Windows 上 EvalSymlinks 可能规范化盘符/分隔符，只比基名与后缀
		if filepath.Base(opened) != "page.html" {
			t.Fatalf("opened = %q, want path ending page.html (got base %q)", opened, filepath.Base(opened))
		}
	}
}

func TestOpenInDefaultAppPropagatesOSError(t *testing.T) {
	env := newFilesEnv(t)
	path := filepath.Join(env.root, "page.html")
	if err := os.WriteFile(path, []byte("<p>x</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("open failed")
	prev := openLocalFile
	openLocalFile = func(string) error { return boom }
	t.Cleanup(func() { openLocalFile = prev })

	if err := env.app.OpenInDefaultApp(env.root, path); !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
}
