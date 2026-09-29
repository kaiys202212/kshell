package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchFiles(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "src", "internal"))
	mkdirAll(t, filepath.Join(root, "docs"))
	mkdirAll(t, filepath.Join(root, "node_modules", "pkg"))
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "src", "app.go"), "package app\n")
	writeFile(t, filepath.Join(root, "src", "internal", "deep.go"), "package deep\n")
	writeFile(t, filepath.Join(root, "docs", "MAIN.md"), "# main\n")
	writeFile(t, filepath.Join(root, "node_modules", "pkg", "main.go"), "package p\n")
	writeFile(t, filepath.Join(root, ".gitignore"), "ignored.go\n")
	writeFile(t, filepath.Join(root, "ignored.go"), "package ignored\n")

	m := NewMatcher(root, nil, false)

	t.Run("命中文件与目录_大小写不敏感", func(t *testing.T) {
		hits, err := SearchFiles(root, "main", m, 100)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, h := range hits {
			got[h.RelPath] = true
		}
		// node_modules 下的 main.go 必须被内置排除
		if len(hits) != 2 {
			t.Fatalf("期望 2 个命中，实得 %d：%v", len(hits), got)
		}
		for _, want := range []string{"main.go", "docs/MAIN.md"} {
			if !got[want] {
				t.Fatalf("缺少期望命中 %s：%v", want, got)
			}
		}
		if got["src/app.go"] || got["node_modules/pkg/main.go"] {
			t.Fatalf("出现意外命中：%v", got)
		}
	})

	t.Run("递归进入未忽略子目录", func(t *testing.T) {
		hits, err := SearchFiles(root, "deep", m, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 || hits[0].RelPath != filepath.FromSlash("src/internal/deep.go") {
			// RelPath 一律 '/' 分隔
			if len(hits) != 1 || hits[0].RelPath != "src/internal/deep.go" {
				t.Fatalf("期望 src/internal/deep.go，实得 %+v", hits)
			}
		}
	})

	t.Run("尊重gitignore", func(t *testing.T) {
		hits, err := SearchFiles(root, "ignored", m, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 0 {
			t.Fatalf("gitignore 应生效，实得 %+v", hits)
		}
	})

	t.Run("目录也参与匹配", func(t *testing.T) {
		hits, err := SearchFiles(root, "internal", m, 100)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, h := range hits {
			if h.RelPath == "src/internal" && h.IsDir {
				found = true
			}
		}
		if !found {
			t.Fatalf("应命中目录 src/internal：%+v", hits)
		}
	})

	t.Run("上限截断", func(t *testing.T) {
		hits, err := SearchFiles(root, "go", m, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 2 {
			t.Fatalf("期望截断为 2，实得 %d", len(hits))
		}
	})

	t.Run("空查询返回空", func(t *testing.T) {
		hits, err := SearchFiles(root, "  ", m, 100)
		if err != nil {
			t.Fatal(err)
		}
		if hits != nil {
			t.Fatalf("空查询应返回 nil，实得 %+v", hits)
		}
	})

	t.Run("root不存在返回error", func(t *testing.T) {
		if _, err := SearchFiles(filepath.Join(root, "no-such-dir"), "x", m, 10); err == nil {
			t.Fatal("root 不存在应返回错误")
		}
	})
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
