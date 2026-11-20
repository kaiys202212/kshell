package asrlocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installFixture 用假资产表 + 本地文件服务模拟完整安装（避免真实下载 250MB）。
func installFixture(t *testing.T) (dir string, opts Options) {
	t.Helper()
	dir = t.TempDir()

	archive, err := os.ReadFile(filepath.Join("testdata", "runtime-good.tar.bz2"))
	if err != nil {
		t.Fatal(err)
	}
	model := []byte(strings.Repeat("onnx", 500))
	tokens := []byte("token\n")

	sum := func(b []byte) string {
		h := sha256.Sum256(b)
		return hex.EncodeToString(h[:])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/runtime.tar.bz2"):
			w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "/model.int8.onnx"):
			w.Write(model)
		case strings.HasSuffix(r.URL.Path, "/tokens.txt"):
			w.Write(tokens)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	// 用测试资产表替换全局表（结束后恢复）。
	origAssets := runtimeAssets
	origFiles := ModelFiles
	origHosts := modelHosts
	t.Cleanup(func() {
		runtimeAssets = origAssets
		ModelFiles = origFiles
		modelHosts = origHosts
	})
	key := runtime.GOOS + "/" + runtime.GOARCH
	runtimeAssets = map[string]Asset{
		key: {Stem: "sherpa-onnx-vtest", File: "runtime.tar.bz2", SHA256: sum(archive), URLs: []string{srv.URL + "/runtime.tar.bz2"}},
	}
	ModelFiles = map[string]ModelFileMeta{
		"model.int8.onnx": {sum(model), int64(len(model))},
		"tokens.txt":     {sum(tokens), int64(len(tokens))},
	}
	modelHosts = []string{srv.URL + "/"}

	return dir, Options{}
}

func TestInstallAndDetect(t *testing.T) {
	dir, opts := installFixture(t)
	if s := Detect(dir); s.Installed {
		t.Fatalf("安装前不应就绪: %+v", s)
	}
	if err := Install(context.Background(), dir, opts); err != nil {
		t.Fatalf("Install: %v", err)
	}
	s := Detect(dir)
	if !s.Installed {
		t.Fatalf("安装后应就绪: %+v", s)
	}
	if s.EnginePath == "" || s.ModelPath == "" {
		t.Fatalf("状态缺路径: %+v", s)
	}
	if _, err := os.Stat(s.EnginePath); err != nil {
		t.Fatalf("引擎不存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.ModelPath, "tokens.txt")); err != nil {
		t.Fatalf("模型文件缺失: %v", err)
	}
	if s.SizeBytes <= 0 {
		t.Fatalf("占用大小应 > 0: %+v", s)
	}
	if s.Version != RuntimeVersion {
		t.Fatalf("版本 = %q", s.Version)
	}

	// 幂等：再次安装不应失败（命中哈希跳过）。
	if err := Install(context.Background(), dir, Options{}); err != nil {
		t.Fatalf("重复安装: %v", err)
	}
}

func TestInstallUnsupportedPlatform(t *testing.T) {
	orig := runtimeAssets
	t.Cleanup(func() { runtimeAssets = orig })
	runtimeAssets = map[string]Asset{}
	err := Install(context.Background(), t.TempDir(), Options{})
	if err == nil || !strings.Contains(err.Error(), "unsupported_platform") {
		t.Fatalf("不支持平台应报错: %v", err)
	}
}

func TestRemove(t *testing.T) {
	dir, _ := installFixture(t)
	if err := Install(context.Background(), dir, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if s := Detect(dir); s.Installed || s.Partial {
		t.Fatalf("卸载后应无残留: %+v", s)
	}
}

func TestDetectPartial(t *testing.T) {
	dir := t.TempDir()
	engine := filepath.Join(dir, "runtime", "x", "bin", engineName())
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := Detect(dir)
	if s.Installed || !s.Partial {
		t.Fatalf("只有引擎应判 partial: %+v", s)
	}
}