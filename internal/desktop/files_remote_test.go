package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/remote"
	remotefs "github.com/yangk/kshell/internal/remote/fs"
)

func newSSHFilesEnv(t *testing.T, run remotefs.Runner) (*App, string) {
	t.Helper()
	env := newFilesEnv(t)
	env.app.opts.RemoteRun = run
	ref := discovery.FormatSSHRef("c1", "/home/u/proj")
	return env.app, ref
}

func TestRemoteListFilesViaRef(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "find") && strings.Contains(cmd, "-maxdepth 1") {
			return []byte("d\t0\t1700000000\tpkg\nf\t12\t1700000001\tmain.go\nd\t0\t1700000002\tnode_modules\n"), nil, nil
		}
		return nil, nil, fmtUnexpected(cmd)
	}
	app, ref := newSSHFilesEnv(t, run)

	nodes, err := app.ListFiles(ref, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Name != "pkg" || !nodes[0].IsDir || nodes[1].Name != "main.go" {
		t.Fatalf("nodes=%+v", nodes)
	}
	if nodes[1].Path != "/home/u/proj/main.go" {
		t.Fatalf("Path=%q", nodes[1].Path)
	}
}

func TestRemoteListFilesRejectsEscape(t *testing.T) {
	app, ref := newSSHFilesEnv(t, func(_ context.Context, _ remote.Connection, _ string, _ []byte) ([]byte, []byte, error) {
		t.Fatal("穿越不应调 Runner")
		return nil, nil, nil
	})
	if _, err := app.ListFiles(ref, "../etc", false); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("期望 errPathOutsideWorkspace, got %v", err)
	}
}

func TestRemoteReadWriteEditRoundTrip(t *testing.T) {
	store := map[string][]byte{"/home/u/proj/readme.md": []byte("hello\n")}
	run := func(_ context.Context, _ remote.Connection, cmd string, stdin []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "mktemp") {
			parts := strings.Split(cmd, "'")
			target := parts[len(parts)-2]
			store[target] = append([]byte(nil), stdin...)
			return nil, nil, nil
		}
		if strings.Contains(cmd, "head -c") {
			for p, data := range store {
				if strings.Contains(cmd, p) {
					return data, nil, nil
				}
			}
			return nil, nil, errors.New("missing")
		}
		return nil, nil, fmtUnexpected(cmd)
	}
	app, ref := newSSHFilesEnv(t, run)

	ec, err := app.ReadFileForEdit(ref, "/home/u/proj/readme.md")
	if err != nil {
		t.Fatal(err)
	}
	if ec.Text != "hello" && ec.Text != "hello\n" && !strings.HasPrefix(ec.Text, "hello") {
		t.Fatalf("Text=%q", ec.Text)
	}
	if err := app.SaveFile(ref, "readme.md", "world\n", "lf"); err != nil {
		t.Fatal(err)
	}
	if string(store["/home/u/proj/readme.md"]) != "world\n" {
		t.Fatalf("write store=%q", store["/home/u/proj/readme.md"])
	}
	if _, err := app.ReadFileForEdit(ref, "/etc/passwd"); !errors.Is(err, errPathOutsideWorkspace) {
		t.Fatalf("越界应拒: %v", err)
	}
}

func TestRemoteReadFileBytes(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "head -c") && strings.Contains(cmd, "pic.png") {
			return png, nil, nil
		}
		return nil, nil, fmtUnexpected(cmd)
	}
	app, ref := newSSHFilesEnv(t, run)
	fb, err := app.ReadFileBytes(ref, "/home/u/proj/pic.png")
	if err != nil {
		t.Fatal(err)
	}
	if fb.Mime != "image/png" || fb.Size != int64(len(png)) {
		t.Fatalf("%+v", fb)
	}
	raw, err := base64.StdEncoding.DecodeString(fb.Base64)
	if err != nil || string(raw) != string(png) {
		t.Fatalf("base64 decode: %v %q", err, raw)
	}
}

func TestRemoteSearchFiles(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "-iname") {
			return []byte("f\tmain.go\n"), nil, nil
		}
		return nil, nil, fmtUnexpected(cmd)
	}
	app, ref := newSSHFilesEnv(t, run)
	hits, err := app.SearchFiles(ref, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Name != "main.go" || hits[0].RelPath != "main.go" {
		t.Fatalf("%+v", hits)
	}
	if hits, _ := app.SearchFiles(ref, "  ", false); len(hits) != 0 {
		t.Fatal("空查询应空")
	}
}

func TestRemoteCreateEntryUnsupported(t *testing.T) {
	app, ref := newSSHFilesEnv(t, nil)
	if _, err := app.CreateEntry(ref, "", "x.go", false); !errors.Is(err, errRemoteFSOpUnsupported) {
		t.Fatalf("got %v", err)
	}
}

func TestLocalListFilesStillWorks(t *testing.T) {
	env := newFilesEnv(t)
	nodes, err := env.app.ListFiles(env.root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("本地列表不应被分流破坏")
	}
}

func fmtUnexpected(cmd string) error {
	return errors.New("unexpected cmd: " + cmd)
}
