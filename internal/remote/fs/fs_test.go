package fs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/remote"
)

func TestWithinRootRejectsEscape(t *testing.T) {
	if err := WithinRoot("/ws", "/ws/../etc"); err == nil {
		t.Fatal("期望拒绝 /ws/../etc 逃出工作区根")
	}
	if err := WithinRoot("/ws", "/etc"); err == nil {
		t.Fatal("期望拒绝 /etc")
	}
	// /wsmore 不能因前缀误判为落在 /ws 内
	if err := WithinRoot("/ws", "/wsmore"); err == nil {
		t.Fatal("期望拒绝前缀相似但非子路径")
	}
}

func TestWithinRootAllowsInside(t *testing.T) {
	if err := WithinRoot("/ws", "/ws"); err != nil {
		t.Fatalf("根自身应允许: %v", err)
	}
	if err := WithinRoot("/ws", "/ws/foo"); err != nil {
		t.Fatalf("子路径应允许: %v", err)
	}
	if err := WithinRoot("/ws", "/ws/foo/../bar"); err != nil {
		t.Fatalf("清理后仍在根内应允许: %v", err)
	}
}

func TestListDirParsesFindPrintf(t *testing.T) {
	var gotCmd string
	f := &FS{
		Conn: remote.Connection{ID: "c1", Host: "h"},
		Run: func(_ context.Context, _ remote.Connection, cmd string, _ []byte) (stdout, stderr []byte, err error) {
			gotCmd = cmd
			// 稳定格式：type\tsize\tmodunix\tname（与实现约定一致，单测不连真 SSH）
			out := "d\t4096\t1700000000\tsub\n" +
				"f\t12\t1700000001\tfile.txt\n" +
				"l\t0\t1700000002\tlink\n"
			return []byte(out), nil, nil
		},
	}
	entries, err := f.ListDir(context.Background(), "/home/u/proj")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotCmd, "/home/u/proj") {
		t.Fatalf("命令应包含目标目录: %q", gotCmd)
	}
	if len(entries) != 3 {
		t.Fatalf("entries=%d %+v", len(entries), entries)
	}
	if entries[0].Name != "sub" || !entries[0].IsDir || entries[0].Size != 4096 {
		t.Fatalf("dir entry: %+v", entries[0])
	}
	if entries[1].Name != "file.txt" || entries[1].IsDir || entries[1].Size != 12 {
		t.Fatalf("file entry: %+v", entries[1])
	}
	if entries[1].ModTime.Unix() != 1700000001 {
		t.Fatalf("modtime: %v", entries[1].ModTime)
	}
	if entries[2].Name != "link" || entries[2].IsDir {
		t.Fatalf("symlink 按非目录处理: %+v", entries[2])
	}
}

func TestListDirRunnerError(t *testing.T) {
	f := &FS{
		Run: func(_ context.Context, _ remote.Connection, _ string, _ []byte) ([]byte, []byte, error) {
			return nil, []byte("boom"), errors.New("err.ssh.exec_failed")
		},
	}
	_, err := f.ListDir(context.Background(), "/tmp")
	if err == nil {
		t.Fatal("期望透传 Runner 错误")
	}
}

func TestListDirRejectsEmptyDir(t *testing.T) {
	f := &FS{
		Run: func(_ context.Context, _ remote.Connection, _ string, _ []byte) ([]byte, []byte, error) {
			t.Fatal("空路径不应调用 Runner")
			return nil, nil, nil
		},
	}
	if _, err := f.ListDir(context.Background(), ""); err == nil {
		t.Fatal("期望拒绝空目录")
	}
}

func TestListDirParseFailure(t *testing.T) {
	f := &FS{
		Run: func(_ context.Context, _ remote.Connection, _ string, _ []byte) ([]byte, []byte, error) {
			return []byte("not-a-valid-line\n"), nil, nil
		},
	}
	_, err := f.ListDir(context.Background(), "/tmp")
	if err == nil {
		t.Fatal("期望解析失败")
	}
	if !strings.Contains(err.Error(), "err.remote.fs_parse") {
		t.Fatalf("期望 wire key: %v", err)
	}
}

func TestReadFileRejectsEscape(t *testing.T) {
	f := &FS{
		Run: func(_ context.Context, _ remote.Connection, _ string, _ []byte) ([]byte, []byte, error) {
			t.Fatal("穿越不应调用 Runner")
			return nil, nil, nil
		},
	}
	_, err := f.ReadFile(context.Background(), "/ws", "/ws/../etc/passwd", 1024)
	if err == nil {
		t.Fatal("期望拒绝路径穿越")
	}
	if !strings.Contains(err.Error(), "err.remote.path_escape") {
		t.Fatalf("wire key: %v", err)
	}
}

func TestReadFileEnforcesLimit(t *testing.T) {
	f := &FS{
		Run: func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
			if !strings.Contains(cmd, "head -c 5") {
				t.Fatalf("应按 limit+1 截断读取: %s", cmd)
			}
			return []byte("hello!"), nil, nil // 6 > limit 4
		},
	}
	_, err := f.ReadFile(context.Background(), "/ws", "a.txt", 4)
	if err == nil || !strings.Contains(err.Error(), "err.remote.fs_too_large") {
		t.Fatalf("期望超限错误, got %v", err)
	}
}

func TestWriteFileRoundTripViaFake(t *testing.T) {
	store := map[string][]byte{}
	f := &FS{
		Conn: remote.Connection{ID: "c1"},
		Run: func(_ context.Context, _ remote.Connection, cmd string, stdin []byte) ([]byte, []byte, error) {
			if strings.Contains(cmd, "mktemp") && strings.Contains(cmd, "mv") {
				// 写：从命令里抠目标路径（最后一个单引号段）
				parts := strings.Split(cmd, "'")
				if len(parts) < 2 {
					t.Fatalf("写命令缺引号路径: %s", cmd)
				}
				target := parts[len(parts)-2]
				store[target] = append([]byte(nil), stdin...)
				return nil, nil, nil
			}
			if strings.Contains(cmd, "head -c") {
				for p, data := range store {
					if strings.Contains(cmd, p) {
						lim := 1025
						if len(data) > lim {
							return data[:lim], nil, nil
						}
						return data, nil, nil
					}
				}
				return nil, nil, errors.New("missing")
			}
			return nil, nil, fmt.Errorf("unexpected: %s", cmd)
		},
	}
	payload := []byte("hello remote\n")
	if err := f.WriteFile(context.Background(), "/ws", "note.txt", payload); err != nil {
		t.Fatal(err)
	}
	got, err := f.ReadFile(context.Background(), "/ws", "/ws/note.txt", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("回读 = %q", got)
	}
	if err := f.WriteFile(context.Background(), "/ws", "../etc/x", []byte("x")); err == nil {
		t.Fatal("写穿越应拒绝")
	}
}

func TestSearchParsesFindIname(t *testing.T) {
	var gotCmd string
	f := &FS{
		Run: func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
			gotCmd = cmd
			return []byte("f\tmain.go\nd\tpkg\n"), nil, nil
		},
	}
	hits, err := f.Search(context.Background(), "/ws", "Main", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotCmd, "-iname") || !strings.Contains(gotCmd, "head -n 2000") {
		t.Fatalf("命令应含 -iname 与上限: %s", gotCmd)
	}
	if len(hits) != 2 || hits[0].Name != "main.go" || hits[0].RelPath != "main.go" || hits[0].Path != "/ws/main.go" {
		t.Fatalf("hits=%+v", hits)
	}
	if !hits[1].IsDir || hits[1].RelPath != "pkg" {
		t.Fatalf("dir hit: %+v", hits[1])
	}
}
