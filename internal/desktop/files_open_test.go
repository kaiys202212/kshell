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
