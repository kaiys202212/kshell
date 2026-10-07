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

// TestCatalogEveryKeyTranslates 遍历两张 catalog 的每条 key，断言在 en 与 zh-CN
// 运行态下 T 都不返回空串、也不原样返回 key——防止「缺 key 时调用方拿 key 自拼
// 期望值」的假阳性（如 notify 测试用 applang.T(...) 构造期望）。覆盖 tray/dialog/
// toast/terminal/role 全部命名空间。
func TestCatalogEveryKeyTranslates(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			Set(lang)
			t.Cleanup(func() { Set("en") })
			for _, catalog := range []map[string]string{en, zhCN} {
				for k := range catalog {
					if got := T(k); got == "" || got == k {
						t.Errorf("[%s] key %q 未翻译（返回 %q）", lang, k, got)
					}
				}
			}
		})
	}
}

// TestCallerReferencedKeysExist 锁定调用方实际引用的 key（grep `applang.T(` 全仓确认：
// notify.go / projects.go / shell.go / tray.go / providers/transcript.go），逐个断言已注册
// ——缺 key 时 T 原样返回 key，这里即判红。与 TestCatalogEveryKeyTranslates 互补：后者只
// 遍历 catalog 自身，从两张表同时删掉某个被调用的 key 时它发现不了，本测试能发现。
func TestCallerReferencedKeysExist(t *testing.T) {
	keys := []string{
		"toast.task_done",
		"toast.task_error",
		"toast.waiting_confirm",
		"dialog.pick_project_dir",
		"terminal.title",
		"role.user",
		"role.assistant",
		"tray.show_main",
		"tray.exit",
		"tray.show_main_tip",
		"tray.exit_tip",
	}
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			Set(lang)
			t.Cleanup(func() { Set("en") })
			for _, k := range keys {
				if got := T(k); got == "" || got == k {
					t.Errorf("[%s] 调用方引用的 key %q 未注册（返回 %q）", lang, k, got)
				}
			}
		})
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
