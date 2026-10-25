package version

import "testing"

func TestCurrentDefaultsToDev(t *testing.T) {
	if Current() != "dev" {
		t.Fatalf("Current() = %q, want dev", Current())
	}
}

func TestIsRelease(t *testing.T) {
	// 保存/恢复原值，避免影响其它用例。
	orig := Version
	defer func() { Version = orig }()

	cases := map[string]bool{
		"":          false, // 空串视为非发布
		"dev":       false,
		"unknown":   false,
		"  dev  ":   false, // 可 trim
		"DEV":       false, // 大小写不敏感
		"Unknown":   false,
		"v1.2.3":    true,
		"1.2.3":     true,
		"v0.0.1-rc": true,
	}
	for v, want := range cases {
		Version = v
		if got := IsRelease(); got != want {
			t.Fatalf("IsRelease(Version=%q) = %v, want %v", v, got, want)
		}
	}
}
