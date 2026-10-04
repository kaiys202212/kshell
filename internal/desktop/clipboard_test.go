package desktop

import "testing"

func TestFormatClipboardPaste_PrefersCopiedFiles(t *testing.T) {
	got := formatClipboardPaste("hello", []string{`D:\shot.png`}, `C:\tmp\img.png`)
	if got.Path != `D:\shot.png` || got.Text != "" {
		t.Fatalf("got %+v, want Path=copied file and empty Text", got)
	}
}

func TestFormatClipboardPaste_PrefersTextOverBitmap(t *testing.T) {
	// Word 等会同时放 Unicode 文本和 DIB；有文本时绝不能改贴成图片路径
	got := formatClipboardPaste("copied text", nil, `C:\tmp\img.png`)
	if got.Text != "copied text" || got.Path != "" {
		t.Fatalf("got %+v, want Text only", got)
	}
}

func TestFormatClipboardPaste_ImageWhenNoText(t *testing.T) {
	got := formatClipboardPaste("  ", nil, `C:\tmp\kshell-paste.png`)
	if got.Path != `C:\tmp\kshell-paste.png` || got.Text != "" {
		t.Fatalf("got %+v, want image path", got)
	}
}

func TestFormatClipboardPaste_Empty(t *testing.T) {
	got := formatClipboardPaste("", nil, "")
	if got.Text != "" || got.Path != "" {
		t.Fatalf("got %+v, want empty", got)
	}
}
