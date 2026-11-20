package asrlocal

import "testing"

func TestRuntimeAssetMapping(t *testing.T) {
	cases := []struct {
		goos, goarch string
		wantFile     string
	}{
		{"windows", "amd64", "sherpa-onnx-v2.0.2-win-x64-shared-MT-Release.tar.bz2"},
		{"windows", "arm64", "sherpa-onnx-v2.0.2-win-arm64-shared-MT-Release.tar.bz2"},
		{"darwin", "amd64", "sherpa-onnx-v2.0.2-osx-universal2-shared.tar.bz2"},
		{"darwin", "arm64", "sherpa-onnx-v2.0.2-osx-universal2-shared.tar.bz2"},
		{"linux", "amd64", "sherpa-onnx-v2.0.2-linux-x64-shared.tar.bz2"},
		{"linux", "arm64", "sherpa-onnx-v2.0.2-linux-aarch64-shared-cpu.tar.bz2"},
	}
	for _, c := range cases {
		a, err := RuntimeAsset(c.goos, c.goarch)
		if err != nil {
			t.Fatalf("%s/%s: %v", c.goos, c.goarch, err)
		}
		if a.File != c.wantFile {
			t.Errorf("%s/%s file = %q, want %q", c.goos, c.goarch, a.File, c.wantFile)
		}
		if a.SHA256 == "" || len(a.SHA256) != 64 {
			t.Errorf("%s/%s sha256 非法: %q", c.goos, c.goarch, a.SHA256)
		}
		if len(a.URLs) == 0 {
			t.Errorf("%s/%s 缺少下载地址", c.goos, c.goarch)
		}
		if a.Stem == "" {
			t.Errorf("%s/%s 缺少目录名", c.goos, c.goarch)
		}
	}
}

func TestRuntimeAssetUnknownPlatform(t *testing.T) {
	for _, p := range [][2]string{{"freebsd", "amd64"}, {"linux", "riscv64"}, {"plan9", "amd64"}} {
		if _, err := RuntimeAsset(p[0], p[1]); err == nil {
			t.Errorf("%s/%s 应报不支持", p[0], p[1])
		}
	}
}

func TestApplyMirrors(t *testing.T) {
	orig := []string{"https://github.com/a/b.tar.bz2", "https://gh-proxy.com/https://github.com/a/b.tar.bz2"}
	// 无镜像：保持原序。
	if got := applyMirrors(orig, nil); len(got) != 2 || got[0] != orig[0] {
		t.Fatalf("无镜像应原样返回: %v", got)
	}
	// {url} 占位：镜像版排在最前，同为占位时按镜像顺序。
	got := applyMirrors(orig[:1], []string{"https://m1.example/{url}", "https://m2.example/{url}"})
	want := []string{
		"https://m1.example/https://github.com/a/b.tar.bz2",
		"https://m2.example/https://github.com/a/b.tar.bz2",
		"https://github.com/a/b.tar.bz2",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 项 = %q, want %q", i, got[i], want[i])
		}
	}
	// 纯前缀镜像：直接拼接。
	got = applyMirrors([]string{"https://github.com/x"}, []string{"https://p.example/"})
	if got[0] != "https://p.example/https://github.com/x" {
		t.Fatalf("前缀拼接失败: %v", got)
	}
	// 空白项被忽略。
	got = applyMirrors([]string{"https://github.com/x"}, []string{"  ", ""})
	if len(got) != 1 {
		t.Fatalf("空白镜像应忽略: %v", got)
	}
}

func TestModelSources(t *testing.T) {
	urls := ModelSources("model.int8.onnx", nil)
	if len(urls) < 2 {
		t.Fatalf("模型应有多个来源: %v", urls)
	}
	seenMirror, seenOfficial := false, false
	for _, u := range urls {
		if u == "https://hf-mirror.com/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/model.int8.onnx" {
			seenMirror = true
		}
		if u == "https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/model.int8.onnx" {
			seenOfficial = true
		}
	}
	if !seenMirror || !seenOfficial {
		t.Fatalf("缺少 hf 镜像或官方源: %v", urls)
	}
	// 用户镜像优先。
	urls = ModelSources("tokens.txt", []string{"https://my.example/{url}"})
	if urls[0] != "https://my.example/https://hf-mirror.com/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/tokens.txt" {
		t.Fatalf("用户镜像应排首位: %v", urls)
	}
}

func TestModelFileMeta(t *testing.T) {
	for _, name := range []string{"model.int8.onnx", "tokens.txt"} {
		meta, ok := ModelFiles[name]
		if !ok {
			t.Fatalf("缺少模型文件元数据 %s", name)
		}
		if len(meta.SHA256) != 64 || meta.Size <= 0 {
			t.Fatalf("%s 元数据非法: %+v", name, meta)
		}
	}
}