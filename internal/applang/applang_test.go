package applang

import "testing"

func TestTDefaultsToEnglish(t *testing.T) {
	Set("en")
	if got := T("tray.exit"); got != "Quit" {
		t.Fatalf("en tray.exit = %q, want Quit", got)
	}
}

func TestTChinese(t *testing.T) {
	Set("zh-CN")
	defer Set("en") // 还原，避免测试间串扰
	if got := T("tray.exit"); got != "退出" {
		t.Fatalf("zh tray.exit = %q, want 退出", got)
	}
}

func TestUnknownKeyReturnsKey(t *testing.T) {
	Set("en")
	if got := T("no.such.key"); got != "no.such.key" {
		t.Fatalf("未知 key = %q, want 原样返回", got)
	}
}

func TestResolveNonSystemPassthrough(t *testing.T) {
	if got := Resolve("en"); got != "en" {
		t.Fatalf("Resolve(en) = %q", got)
	}
	if got := Resolve("zh-CN"); got != "zh-CN" {
		t.Fatalf("Resolve(zh-CN) = %q", got)
	}
}
