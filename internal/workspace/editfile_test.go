package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadForEdit_EOL探测(t *testing.T) {
	root := t.TempDir()

	crlfPath := filepath.Join(root, "crlf.txt")
	if err := os.WriteFile(crlfPath, []byte("a\r\nb\r\nc"), 0o644); err != nil {
		t.Fatal(err)
	}
	ec, err := ReadForEdit(crlfPath)
	if err != nil {
		t.Fatal(err)
	}
	if ec.EOL != "crlf" || ec.Text != "a\nb\nc" || ec.Size != 7 {
		t.Fatalf("crlf 探测失败：%+v", ec)
	}

	lfPath := filepath.Join(root, "lf.txt")
	if err := os.WriteFile(lfPath, []byte("a\r\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ec, err = ReadForEdit(lfPath); err != nil {
		t.Fatal(err)
	}
	if ec.EOL != "lf" || ec.Text != "a\nb\nc\n" {
		t.Fatalf("lf 探测失败：%+v", ec)
	}
}

// Edit 相关错误一律以 wire key 暴露给前端翻译，不再是中文字面量。
func TestEditErrorsWireKeys(t *testing.T) {
	if ErrBinaryFile.Error() != "err.workspace.binary_not_editable" {
		t.Fatalf("二进制 key = %q", ErrBinaryFile.Error())
	}
	if ErrIsDirectory.Error() != "err.workspace.dir_not_editable" {
		t.Fatalf("目录 key = %q", ErrIsDirectory.Error())
	}
	if !strings.HasPrefix(ErrFileTooLarge.Error(), "err.workspace.file_too_big|") {
		t.Fatalf("超限 key = %q", ErrFileTooLarge.Error())
	}
}

func TestReadForEdit_拒绝二进制(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bin.dat")
	if err := os.WriteFile(p, []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadForEdit(p); !errors.Is(err, ErrBinaryFile) {
		t.Fatalf("期望 ErrBinaryFile，实得 %v", err)
	}
}

func TestReadForEdit_超限(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(p, bytesOf(maxEditBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadForEdit(p); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("期望 ErrFileTooLarge，实得 %v", err)
	}
}

func TestReadForEdit_目录(t *testing.T) {
	d := t.TempDir()
	if _, err := ReadForEdit(d); !errors.Is(err, ErrIsDirectory) {
		t.Fatalf("期望 ErrIsDirectory，实得 %v", err)
	}
}

func TestSaveEdit_行尾还原与往返(t *testing.T) {
	root := t.TempDir()

	t.Run("crlf往返", func(t *testing.T) {
		p := filepath.Join(root, "win.txt")
		if err := os.WriteFile(p, []byte("a\r\nb\r\nc"), 0o644); err != nil {
			t.Fatal(err)
		}
		ec, err := ReadForEdit(p)
		if err != nil {
			t.Fatal(err)
		}
		// 原文 "a\r\nb\r\nc" 归一为 "a\nb\nc"，追加 d 后按 crlf 还原
		edited := ec.Text + "d"
		if err := SaveEdit(p, edited, ec.EOL); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(p)
		if string(raw) != "a\r\nb\r\ncd" {
			t.Fatalf("CRLF 应还原：实得 %q", raw)
		}
	})

	t.Run("lf保持", func(t *testing.T) {
		p := filepath.Join(root, "unix.txt")
		if err := SaveEdit(p, "x\ny\n", "lf"); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(p)
		if string(raw) != "x\ny\n" {
			t.Fatalf("LF 应保持：实得 %q", raw)
		}
	})

	t.Run("编辑内容混入crlf不会双写", func(t *testing.T) {
		p := filepath.Join(root, "mixed.txt")
		if err := SaveEdit(p, "a\r\nb\n", "crlf"); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(p)
		if string(raw) != "a\r\nb\r\n" {
			t.Fatalf("应先归一再还原：实得 %q", raw)
		}
	})
}

func TestSaveEdit_保留权限位与原子替换(t *testing.T) {
	p := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(p, []byte("old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveEdit(p, "new\n", "lf"); err != nil {
		t.Fatal(err)
	}
	// 目录里不留临时文件
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("临时文件应已改名，目录内实得 %d 项", len(entries))
	}
}

func TestSaveEdit_超限(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.txt")
	if err := SaveEdit(p, strings.Repeat("a", maxEditBytes+1), "lf"); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("期望 ErrFileTooLarge，实得 %v", err)
	}
}

func TestSaveEdit_非法eol按lf(t *testing.T) {
	p := filepath.Join(t.TempDir(), "eol.txt")
	if err := SaveEdit(p, "a\n", "mac"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if string(raw) != "a\n" {
		t.Fatalf("非法 eol 应回退 lf：实得 %q", raw)
	}
}

func bytesOf(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return b
}
