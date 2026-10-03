package appearance

import "testing"

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":        System,
		"system":  System,
		"SYSTEM":  System,
		"bogus":   System,
		"light":   Light,
		" Light ": Light,
		"dark":    Dark,
		"DARK":    Dark,
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Fatalf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveForcedModesIgnoreOS(t *testing.T) {
	prev := detectOSTheme
	detectOSTheme = func() Theme { return ThemeDark }
	t.Cleanup(func() { detectOSTheme = prev })

	if got := Resolve(Light); got != ThemeLight {
		t.Fatalf("Resolve(light) = %q", got)
	}
	if got := Resolve(Dark); got != ThemeDark {
		t.Fatalf("Resolve(dark) = %q", got)
	}
	if got := Resolve(System); got != ThemeDark {
		t.Fatalf("Resolve(system) = %q, want detected dark", got)
	}
}

func TestGenericEnv(t *testing.T) {
	dark := GenericEnv(ThemeDark)
	if dark["COLORFGBG"] != "15;0" {
		t.Fatalf("dark COLORFGBG = %q", dark["COLORFGBG"])
	}
	if dark["COLORTERM"] != "truecolor" {
		t.Fatalf("COLORTERM = %q", dark["COLORTERM"])
	}
	light := GenericEnv(ThemeLight)
	if light["COLORFGBG"] != "0;15" {
		t.Fatalf("light COLORFGBG = %q", light["COLORFGBG"])
	}
}
