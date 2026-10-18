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
const loggedInConfig = `{"version":"1.0","authInfo":{"email":"a@b.c","displayName":"u","userId":"uid-1","authId":"aid"}}`

// Cursor 必须实现 AuthProber,否则 ACP 启动门禁拿不到登录态(编译期断言)。
func TestCursorImplementsAuthProber(t *testing.T) {
	var _ AuthProber = Cursor{}
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

func TestProbeCursorAuth(t *testing.T) {
	cases := []struct {
		name  string
		dirs  []string // DetectSpec.ConfigDirs,支持 ~ 前缀按 home 展开
		setup func(t *testing.T, home string)
		want  AuthStatus
	}{
		{
			name: "已登录:authInfo.userId 非空",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", loggedInConfig)
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
			name: "未登录:userId 不是字符串",
			dirs: []string{"~/.cursor"},
			setup: func(t *testing.T, home string) {
				writeAuthFixture(t, home, ".cursor", `{"authInfo":{"userId":123}}`)
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
