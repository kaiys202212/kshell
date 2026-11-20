package asrlocal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrNotInstalled 表示本地引擎未安装或文件不全（上层据此回落其它转写来源）。
var ErrNotInstalled = errors.New("err.asrlocal.not_installed")

// defaultTimeout 是单次转写的超时：引擎冷启动约 1.5s，长语音仍远小于此值。
const defaultTimeout = 120 * time.Second

// stderrTailLimit 是错误里附带 stderr 的字符上限（按字节截尾，够定位即可）。
const stderrTailLimit = 600

// RunOptions 是转写参数。
type RunOptions struct {
	// Language 取值 auto/zh/en/ja/ko/yue；空串按 auto。
	Language string
	// ITN 开启逆文本规整（数字、标点更符合书写习惯）。
	ITN bool
	// NumThreads 为 0 时按 CPU 核数取 1~4。
	NumThreads int
	// Timeout 为 0 时用 defaultTimeout。
	Timeout time.Duration
}

// cmdRunner 执行外部命令并分别返回 stdout/stderr（测试注入点）。
type cmdRunner func(ctx context.Context, exe string, args, env []string) (stdout, stderr string, err error)

// runCmd 是生产实现。
var runCmd cmdRunner = execRunner

// execRunner 启动子进程，stdout/stderr 分开收集。
func execRunner(ctx context.Context, exe string, args, env []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = env
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

// Transcribe 调用本地 sherpa-onnx 把 WAV 转成文字。返回空串表示无人声/静音。
func Transcribe(ctx context.Context, rootDir string, wav []byte, opts RunOptions) (string, error) {
	st := Detect(rootDir)
	if !st.Installed {
		return "", ErrNotInstalled
	}
	tmp, err := os.CreateTemp("", "kshell-asr-*.wav")
	if err != nil {
		return "", fmt.Errorf("创建临时音频: %w", err)
	}
	wavPath := tmp.Name()
	defer os.Remove(wavPath)
	if _, err := tmp.Write(wav); err != nil {
		tmp.Close()
		return "", fmt.Errorf("写入临时音频: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := buildArgs(st.EnginePath, filepath.Join(st.ModelPath, "tokens.txt"),
		filepath.Join(st.ModelPath, "model.int8.onnx"), wavPath, opts)
	stdout, stderr, err := runCmd(actx, st.EnginePath, args, engineEnv(st.EnginePath))
	if err != nil {
		if errors.Is(actx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("本地转写超时（%s）", timeout)
		}
		return "", fmt.Errorf("本地转写失败: %v; stderr: %s", err, tailBytes(stderr, stderrTailLimit))
	}
	text, perr := parseOfflineJSON(stdout)
	if perr != nil {
		return "", fmt.Errorf("%w; stderr: %s", perr, tailBytes(stderr, stderrTailLimit))
	}
	return text, nil
}

// TranscribeCommand 用用户自定义命令转写（配置 asr.command）：模板按空白拆分，
// {wav} 占位替换成临时 WAV 路径（不支持引号转义，路径含空格请用 {wav} 占位而
// 非自行拼接），stdout 去首尾空白整体作为文本。
func TranscribeCommand(ctx context.Context, tmpl string, wav []byte, timeout time.Duration) (string, error) {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return "", errors.New("err.asrlocal.empty_command")
	}
	if !strings.Contains(tmpl, "{wav}") {
		return "", errors.New("err.asrlocal.command_missing_wav")
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	tmp, err := os.CreateTemp("", "kshell-asr-*.wav")
	if err != nil {
		return "", fmt.Errorf("创建临时音频: %w", err)
	}
	wavPath := tmp.Name()
	defer os.Remove(wavPath)
	if _, err := tmp.Write(wav); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	fields := strings.Fields(strings.ReplaceAll(tmpl, "{wav}", wavPath))
	if len(fields) == 0 {
		return "", errors.New("err.asrlocal.empty_command")
	}
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr, err := runCmd(actx, fields[0], fields[1:], os.Environ())
	if err != nil {
		return "", fmt.Errorf("自定义转写命令失败: %v; stderr: %s", err, tailBytes(stderr, stderrTailLimit))
	}
	return strings.TrimSpace(stdout), nil
}

// buildArgs 构造 sherpa-onnx-offline 参数（exe 本身不参与）。
func buildArgs(exe, tokens, model, wavPath string, opts RunOptions) []string {
	lang := strings.TrimSpace(opts.Language)
	if lang == "" {
		lang = "auto"
	}
	threads := opts.NumThreads
	if threads <= 0 {
		threads = runtime.NumCPU()
		if threads > 4 {
			threads = 4
		}
		if threads < 1 {
			threads = 1
		}
	}
	itn := "false"
	if opts.ITN {
		itn = "true"
	}
	return []string{
		"--tokens=" + tokens,
		"--sense-voice-model=" + model,
		"--sense-voice-language=" + lang,
		"--sense-voice-use-itn=" + itn,
		"--num-threads=" + fmt.Sprint(threads),
		wavPath,
	}
}

// engineEnv 在继承环境的基础上补充动态库搜索路径：官方 Linux/macOS 包把
// onnxruntime 放在 bin 同级的 lib/，部分发行版需要显式指定。
func engineEnv(exePath string) []string {
	env := os.Environ()
	base := filepath.Dir(filepath.Dir(exePath)) // <stem>/bin/exe → <stem>
	libDir := filepath.Join(base, "lib")
	switch runtime.GOOS {
	case "linux":
		return appendEnv(env, "LD_LIBRARY_PATH", libDir)
	case "darwin":
		return appendEnv(env, "DYLD_LIBRARY_PATH", libDir)
	default:
		return env
	}
}

// appendEnv 把 dir 前置到既有环境变量（保留原值，便于用户自带库）。
func appendEnv(env []string, key, dir string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			old := strings.TrimPrefix(e, prefix)
			if old != "" {
				env[i] = prefix + dir + string(os.PathListSeparator) + old
			} else {
				env[i] = prefix + dir
			}
			return env
		}
	}
	return append(env, prefix+dir)
}

// parseOfflineJSON 从 sherpa-onnx-offline 的输出里取最后一条含 text 的 JSON。
// 引擎把大量调试信息直接打到 stderr，stdout 只在本轮结束时输出结果 JSON；
// 批量输入时每文件一行，取最后一行即可。
func parseOfflineJSON(stdout string) (string, error) {
	lines := strings.Split(stdout, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			continue
		}
		var out struct {
			Text *string `json:"text"`
		}
		if err := json.Unmarshal([]byte(line), &out); err == nil && out.Text != nil {
			return *out.Text, nil
		}
	}
	return "", errors.New("本地转写输出中未找到结果 JSON")
}

// tailBytes 截取字符串尾部（按字节），避免超长 stderr 灌进错误串。
func tailBytes(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return "…" + s[len(s)-limit:]
}