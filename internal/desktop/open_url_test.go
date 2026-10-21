package desktop

import (
	"errors"
	"strings"
	"testing"
)

func TestOpenExternalURLRejectsDangerous(t *testing.T) {
	a := &App{}
	for _, u := range []string{
		"",
		"   ",
		"javascript:alert(1)",
		"data:text/html,x",
		"vbscript:msg",
		"blob:https://x/1",
		"file:///C:/x.html",
	} {
		if err := a.OpenExternalURL(u); err == nil {
			t.Fatalf("%q 应拒绝", u)
		} else if !strings.Contains(err.Error(), "err.open.url_invalid") {
			t.Fatalf("%q got %v", u, err)
		}
	}
}

func TestOpenExternalURLOpensHTTPS(t *testing.T) {
	a := &App{}
	var got string
	prev := openExternalURL
	openExternalURL = func(u string) error {
		got = u
		return nil
	}
	t.Cleanup(func() { openExternalURL = prev })

	if err := a.OpenExternalURL("https://example.com/a?x=1"); err != nil {
		t.Fatalf("OpenExternalURL: %v", err)
	}
	if got != "https://example.com/a?x=1" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenExternalURLAllowsCustomScheme(t *testing.T) {
	a := &App{}
	var got string
	prev := openExternalURL
	openExternalURL = func(u string) error {
		got = u
		return nil
	}
	t.Cleanup(func() { openExternalURL = prev })

	if err := a.OpenExternalURL("vscode://file/D:/proj/a.ts"); err != nil {
		t.Fatalf("OpenExternalURL: %v", err)
	}
	if got != "vscode://file/D:/proj/a.ts" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenExternalURLAllowsParentheses(t *testing.T) {
	// Wails ValidateAndSanitizeURL 会拒绝对号；本绑定必须放行。
	a := &App{}
	var got string
	prev := openExternalURL
	openExternalURL = func(u string) error {
		got = u
		return nil
	}
	t.Cleanup(func() { openExternalURL = prev })

	u := "https://example.com/file(1).html"
	if err := a.OpenExternalURL(u); err != nil {
		t.Fatalf("OpenExternalURL: %v", err)
	}
	if got != u {
		t.Fatalf("got %q", got)
	}
}

func TestOpenExternalURLPropagatesOSError(t *testing.T) {
	a := &App{}
	boom := errors.New("open failed")
	prev := openExternalURL
	openExternalURL = func(string) error { return boom }
	t.Cleanup(func() { openExternalURL = prev })

	if err := a.OpenExternalURL("https://example.com"); !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
}
