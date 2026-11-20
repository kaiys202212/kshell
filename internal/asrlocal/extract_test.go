package asrlocal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExtractRuntimeKeepsBinAndLibOnly(t *testing.T) {
	dest := t.TempDir()
	if err := extractRuntime(filepath.Join("testdata", "runtime-good.tar.bz2"), dest); err != nil {
		t.Fatalf("extractRuntime: %v", err)
	}
	for _, rel := range []string{"bin/sherpa-onnx-offline", "bin/libdep.so", "lib/libx.so"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	for _, rel := range []string{"include/x.h", "LICENSE"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err == nil {
			t.Errorf("不应解出 %s", rel)
		}
	}
}

func TestExtractRuntimePreservesExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不保留 Unix 权限位")
	}
	dest := t.TempDir()
	if err := extractRuntime(filepath.Join("testdata", "runtime-good.tar.bz2"), dest); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dest, "bin", "sherpa-onnx-offline"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("可执行位丢失: %v", st.Mode())
	}
}

func TestExtractRuntimeRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	err := extractRuntime(filepath.Join("testdata", "runtime-evil.tar.bz2"), dest)
	if err == nil {
		t.Fatal("越界条目应报错")
	}
	// 越界文件不得落到 dest 之外。
	parent := filepath.Dir(dest)
	if _, statErr := os.Stat(filepath.Join(parent, "escape.txt")); statErr == nil {
		t.Fatal("恶意条目逃出了目标目录")
	}
}