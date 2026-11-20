package asrlocal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

)

const sampleStdout = `Loaded
Creating recognizer ...
recognizer created in 1.5 s
Started
Done!

/tmp/voice.wav
----
num threads: 2
decoding method: greedy_search
Elapsed seconds: 0.330 s
{"lang": "<|zh|>", "emotion": "<|NEUTRAL|>", "event": "<|Speech|>", "text": "分析一下策略平台，拉取最新的代码。", "tokens":["分"]}
`

func TestParseOfflineJSON(t *testing.T) {
	text, err := parseOfflineJSON(sampleStdout)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if text != "分析一下策略平台，拉取最新的代码。" {
		t.Fatalf("text = %q", text)
	}
	// 多段结果取最后一行（批量输入时每文件一行 JSON）。
	multi := "{\"text\":\"第一段\"}\n{\"text\":\"第二段\"}\n"
	if got, _ := parseOfflineJSON(multi); got != "第二段" {
		t.Fatalf("多段应取最后: %q", got)
	}
	// 空文本是合法结果（静音/无人声）。
	if got, err := parseOfflineJSON(`{"text":""}`); err != nil || got != "" {
		t.Fatalf("空文本: %q %v", got, err)
	}
	// 无 JSON 行报错。
	if _, err := parseOfflineJSON("no json here\n"); err == nil {
		t.Fatal("无 JSON 应报错")
	}
	// 非法 JSON 行跳过，继续往前找合法行。
	mixed := "{\"text\":\"ok\"}\n{\"broken\": \n"
	if got, err := parseOfflineJSON(mixed); err != nil || got != "ok" {
		t.Fatalf("应跳过坏行: %q %v", got, err)
	}
}

func TestBuildArgs(t *testing.T) {
	args := buildArgs(engineName(), "/m/tokens.txt", "/m/model.int8.onnx", "/tmp/a.wav", RunOptions{Language: "zh", ITN: true, NumThreads: 4})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--tokens=/m/tokens.txt",
		"--sense-voice-model=/m/model.int8.onnx",
		"--sense-voice-language=zh",
		"--sense-voice-use-itn=true",
		"--num-threads=4",
		"/tmp/a.wav",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("参数缺少 %q: %v", want, args)
		}
	}
	// 默认语言 auto、线程数至少 1。
	args = buildArgs(engineName(), "t", "m", "w.wav", RunOptions{})
	if !strings.Contains(strings.Join(args, " "), "--sense-voice-language=auto") {
		t.Errorf("默认语言应为 auto: %v", args)
	}
	if !strings.Contains(strings.Join(args, " "), "--num-threads=1") && runtime.NumCPU() < 2 {
		t.Errorf("线程数下限 1: %v", args)
	}
}

func TestTranscribeInvokesRunner(t *testing.T) {
	dir, _ := installFixture(t)
	if err := Install(context.Background(), dir, Options{}); err != nil {
		t.Fatal(err)
	}
	var gotExe string
	var gotArgs, gotEnv []string
	var wavSeen string
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	runCmd = func(ctx context.Context, exe string, args, env []string) (string, string, error) {
		gotExe, gotArgs, gotEnv = exe, args, env
		// 末尾参数是临时 wav 路径：确认调用时文件真实存在。
		if st, err := os.Stat(args[len(args)-1]); err == nil {
			b, _ := os.ReadFile(args[len(args)-1])
			wavSeen = string(b)
			_ = st
		}
		return sampleStdout, "", nil
	}

	text, err := Transcribe(context.Background(), dir, []byte("RIFFfake"), RunOptions{Language: "auto"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "分析一下策略平台，拉取最新的代码。" {
		t.Fatalf("text = %q", text)
	}
	if !strings.HasSuffix(gotExe, engineName()) {
		t.Fatalf("exe = %q", gotExe)
	}
	if wavSeen != "RIFFfake" {
		t.Fatalf("临时 WAV 内容不符: %q", wavSeen)
	}
	if _, err := os.Stat(gotArgs[len(gotArgs)-1]); err == nil {
		t.Fatal("临时 WAV 应在调用后删除")
	}
	if runtime.GOOS == "linux" {
		found := false
		for _, e := range gotEnv {
			if strings.HasPrefix(e, "LD_LIBRARY_PATH=") {
				found = true
			}
		}
		if !found {
			t.Fatalf("Linux 应注入 LD_LIBRARY_PATH: %v", gotEnv)
		}
	}
}

func TestTranscribeNotInstalled(t *testing.T) {
	_, err := Transcribe(context.Background(), t.TempDir(), []byte("x"), RunOptions{})
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("未安装应返回 ErrNotInstalled, got %v", err)
	}
}

func TestTranscribeRunnerError(t *testing.T) {
	dir, _ := installFixture(t)
	if err := Install(context.Background(), dir, Options{}); err != nil {
		t.Fatal(err)
	}
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	runCmd = func(ctx context.Context, exe string, args, env []string) (string, string, error) {
		return "", "boom: model load failed", errors.New("exit status 1")
	}
	_, err := Transcribe(context.Background(), dir, []byte("x"), RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "model load failed") {
		t.Fatalf("错误应包含 stderr 尾部: %v", err)
	}
	_ = filepath.Separator
}
func TestTranscribeCommand(t *testing.T) {
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	var gotExe string
	var gotArgs []string
	var wavBody string
	runCmd = func(ctx context.Context, exe string, args, env []string) (string, string, error) {
		gotExe, gotArgs = exe, args
		b, _ := os.ReadFile(args[len(args)-1])
		wavBody = string(b)
		return "  自定义结果\n", "", nil
	}
	text, err := TranscribeCommand(context.Background(), "my-asr --lang zh {wav}", []byte("RIFFxx"), 0)
	if err != nil {
		t.Fatalf("TranscribeCommand: %v", err)
	}
	if text != "自定义结果" {
		t.Fatalf("text = %q", text)
	}
	if gotExe != "my-asr" || len(gotArgs) != 3 || gotArgs[0] != "--lang" || gotArgs[1] != "zh" {
		t.Fatalf("命令解析错误: %s %v", gotExe, gotArgs)
	}
	if wavBody != "RIFFxx" {
		t.Fatalf("WAV 内容 = %q", wavBody)
	}
	// 缺 {wav} 占位直接报错（避免用户命令漏传文件却茫然）。
	if _, err := TranscribeCommand(context.Background(), "my-asr", []byte("x"), 0); err == nil {
		t.Fatal("缺 {wav} 应报错")
	}
	if _, err := TranscribeCommand(context.Background(), "  ", []byte("x"), 0); err == nil {
		t.Fatal("空命令应报错")
	}
}
