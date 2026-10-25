//go:build darwin

package appearance

import "testing"

func TestDetectOSThemePlatformDarwin(t *testing.T) {
	theme := detectOSThemePlatform()
	if theme != ThemeLight && theme != ThemeDark {
		t.Fatalf("返回非法主题: %q", theme)
	}
}
