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

func TestTooltipDefaultsToEnglish(t *testing.T) {
	Set("en")
	if got := T("tray.show_main_tip"); got != "Show kshell main window" {
		t.Fatalf("en tray.show_main_tip = %q", got)
	}
	if got := T("tray.exit_tip"); got != "Quit kshell" {
		t.Fatalf("en tray.exit_tip = %q", got)
	}
}

func TestTooltipChinese(t *testing.T) {
	Set("zh-CN")
	defer Set("en") // 还原，避免测试间串扰
	if got := T("tray.show_main_tip"); got != "显示 kshell 主窗口" {
		t.Fatalf("zh tray.show_main_tip = %q", got)
	}
	if got := T("tray.exit_tip"); got != "退出 kshell" {
		t.Fatalf("zh tray.exit_tip = %q", got)
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

func TestCatalogKeySetsMatch(t *testing.T) {
	for k := range en {
		if _, ok := zhCN[k]; !ok {
			t.Errorf("zhCN 缺少 key %q（中文用户会看到英文混排）", k)
		}
	}
	for k := range zhCN {
		if _, ok := en[k]; !ok {
			t.Errorf("en 缺少 key %q", k)
		}
	}
}

func TestSetInvalidFallsBackToEnglish(t *testing.T) {
	Set("bogus")
	defer Set("en") // 还原，避免测试间串扰
	if got := T("tray.exit"); got != "Quit" {
		t.Fatalf("Set(bogus) 后 tray.exit = %q, want Quit", got)
	}
}

func TestResolveSystemValueDomain(t *testing.T) {
	got := Resolve("system")
	if got != "en" && got != "zh-CN" {
		t.Fatalf("Resolve(system) = %q, want en 或 zh-CN", got)
	}
}
