package providers

import (
	"os"
	"path/filepath"
	"testing"
)

// writeAuthFixture 在 home/relDir 下造目录;content 非空时写入 cli-config.json。
// content 为空串表示「目录在但登录配置文件缺失」。
func writeAuthFixture(t *testing.T, home, relDir, content string) {
	t.Helper()
	dir := filepath.Join(home, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if content == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "cli-config.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// loggedInConfig 是本机实测的 cli-config.json 登录形态(键结构一致,值已脱敏)。
// 2026-10 起 cursor-agent 把 userId 写成 JSON number；字符串形态仍兼容。
const loggedInConfig = `{"version":"1.0","authInfo":{"email":"a@b.c","displayName":"u","userId":164059570,"authId":"aid"}}`

// writeAuthBrokenConfig 在 home/relDir 下造出「cli-config.json 是个同名目录」的
// 异常现场:os.ReadFile 会稳定报出**非 NotExist** 的读错误(POSIX 为 is a
// directory,Windows 为 Access is denied),用于锁定「文件在却读不出来必须
// AuthUnknown,不得回落 AuthOut」的保守口径。
func writeAuthBrokenConfig(t *testing.T, home, relDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, relDir, "cli-config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// ProbeAuth 是无参接口,只能拿三种结论之一;绝不能返回零值空串。
func TestCursorProbeAuthReturnsKnownStatus(t *testing.T) {
	got := Cursor{}.ProbeAuth()
	switch got {
	case AuthIn, AuthOut, AuthUnknown:
	default:
		t.Fatalf("ProbeAuth() = %q, want 三态之一", got)
	}
}

// TestCursorLoginCmd 锁定登录命令的裸命令形态:不带绝对路径,BinName 的实际
// 解析(PATH 探测、Windows shim 兜底)由 desktop/launcher 层负责;给空命令时
// desktop 层应判不支持,这里只保证声明本身正确。
func TestCursorLoginCmd(t *testing.T) {
	got := Cursor{}.LoginCmd()
	want := []string{"cursor-agent", "login"}
	if len(got) != len(want) {
		t.Fatalf("LoginCmd() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LoginCmd() = %#v, want %#v", got, want)
		}
	}
}

func TestProbeCursorAuth(t *testing.T) {
	cases := []struct {
		name  string
		dirs  []string // DetectSpec.ConfigDirs,支持 ~ 前缀按 home 展开
		setup func(t *testing.T, home string)
		want  AuthStatus
	}{
		{
			name: "已登录:authInfo.userId 为非零数字(当前 CLI)",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", loggedInConfig)
			},
			want: AuthIn,
		},
		{
			name: "已登录:authInfo.userId 为非空字符串(旧形态)",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":{"userId":"uid-1"}}`)
			},
			want: AuthIn,
		},
		{
			name: "未登录:authInfo 为空对象",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":{}}`)
			},
			want: AuthOut,
		},
		{
			name: "未登录:authInfo 缺 userId",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":{"email":"a@b.c"}}`)
			},
			want: AuthOut,
		},
		{
			name: "未登录:userId 为空串",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":{"userId":""}}`)
			},
			want: AuthOut,
		},
		{
			name: "未登录:userId 为数字 0",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":{"userId":0}}`)
			},
			want: AuthOut,
		},
		{
			name: "未登录:userId 类型异常(布尔)",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":{"userId":true}}`)
			},
			want: AuthOut,
		},
		{
			name: "未登录:无 authInfo 键",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"version":"1.0","model":"gpt-5"}`)
			},
			want: AuthOut,
		},
		{
			name: "未登录:authInfo 为 null(视为无 authInfo)",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":null}`)
			},
			want: AuthOut,
		},
		{
			name: "未登录:config 目录在但从未生成 cli-config.json",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", "")
			},
			want: AuthOut,
		},
		{
			name: "未知:JSON 损坏",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":`)
			},
			want: AuthUnknown,
		},
		{
			name: "未知:authInfo 不是对象",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":"logged-in"}`)
			},
			want: AuthUnknown,
		},
		{
			name:  "未知:没有任何 config 目录(工具没装过)",
			dirs:  []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {},
			want:  AuthUnknown,
		},
		{
			name: "多目录:首个目录缺文件,命中第二个即返回",
			dirs: []string{"~/.cursor-a", "~/.cursor-b"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor-a", "")
				writeAuthFixture(t, home, ".cursor-b", loggedInConfig)
			},
			want: AuthIn,
		},
		{
			name: "未知:唯一目录的 cli-config.json 读取失败(非不存在)",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", "")
				writeAuthBrokenConfig(t, home, ".cursor")
			},
			want: AuthUnknown,
		},
		{
			name: "多目录:首目录读取失败,直接返回不看后续(即使后续已登录)",
			dirs: []string{"~/.cursor-a", "~/.cursor-b"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor-a", "")
				writeAuthBrokenConfig(t, home, ".cursor-a")
				writeAuthFixture(t, home, ".cursor-b", loggedInConfig)
			},
			want: AuthUnknown,
		},
		{
			name: "多目录:首目录 JSON 损坏,以其结论即返(first-hit-wins)",
			dirs: []string{"~/.cursor-a", "~/.cursor-b"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor-a", `{"authInfo":`)
				writeAuthFixture(t, home, ".cursor-b", loggedInConfig)
			},
			want: AuthUnknown,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			c.setup(t, home)
			got := probeCursorAuth(DetectSpec{ConfigDirs: c.dirs}, home)
			if got != c.want {
				t.Fatalf("probeCursorAuth = %q, want %q", got, c.want)
			}
		})
	}
}

func TestProbeOpencodeAuth(t *testing.T) {
	cases := []struct {
		name    string
		content string
		absent  bool
		want    AuthStatus
	}{
		{name: "文件不存在", absent: true, want: AuthOut},
		{name: "空对象", content: `{}`, want: AuthOut},
		{name: "值为 null", content: `{"deepseek":null}`, want: AuthOut},
		{name: "有 api 凭据", content: `{"deepseek":{"type":"api","key":"sk-x"}}`, want: AuthIn},
		{name: "有 oauth 凭据", content: `{"x":{"type":"oauth","refresh":"r"}}`, want: AuthIn},
		{name: "损坏 JSON", content: `{`, want: AuthUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if !tc.absent {
				dir := filepath.Join(home, ".local", "share", "opencode")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := probeOpencodeAuth(home); got != tc.want {
				t.Fatalf("probeOpencodeAuth() = %q, want %q", got, tc.want)
			}
		})
	}
}
