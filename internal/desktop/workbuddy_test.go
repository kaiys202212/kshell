package desktop

import (
	"errors"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/workbuddy"
)

func TestWorkbuddyScanViewFromError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		installed  bool
		credOK     bool
		wantErrKey string
	}{
		{"未安装", errors.New("err.workbuddy.not_installed"), false, false, "err.workbuddy.not_installed"},
		{"exe 缺失视为未安装", errors.New("err.workbuddy.exe_not_found"), false, false, "err.workbuddy.exe_not_found"},
		{"key 不可用但已安装", errors.New("err.workbuddy.key_unavailable|spawn"), true, false, "err.workbuddy.key_unavailable|spawn"},
		{"凭据文件损坏", errors.New("err.workbuddy.bad_auth_file|json"), true, false, "err.workbuddy.bad_auth_file|json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := workbuddyScanOnError(tc.err)
			if v.Installed != tc.installed {
				t.Fatalf("Installed = %v, want %v", v.Installed, tc.installed)
			}
			if v.CredentialOK {
				t.Fatal("失败路径 CredentialOK 应为 false")
			}
			if v.ErrKey != tc.wantErrKey {
				t.Fatalf("ErrKey = %q", v.ErrKey)
			}
			if len(v.Models) != 0 {
				t.Fatal("失败路径不应有模型")
			}
		})
	}
}

func TestWorkbuddyScanViewExpiredToken(t *testing.T) {
	// 过期 token：凭据解出但不可用，ErrKey 固定为 token_expired。
	cred := workbuddy.Credential{AccessToken: "t", ExpiresAtMs: 1}
	v, ok := workbuddyScanCredentialState(cred)
	if ok {
		t.Fatal("过期凭据应不可用")
	}
	if !strings.Contains(v.ErrKey, "err.workbuddy.token_expired") {
		t.Fatalf("ErrKey 不符: %q", v.ErrKey)
	}
}
