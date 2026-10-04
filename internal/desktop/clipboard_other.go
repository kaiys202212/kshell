//go:build !windows

package desktop

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func readClipboardSnapshot() (clipboardSnapshot, error) {
	if runtime.GOOS == "darwin" {
		return readDarwinClipboard()
	}
	return readUnixClipboard()
}

func readDarwinClipboard() (clipboardSnapshot, error) {
	var snap clipboardSnapshot
	if out, err := exec.Command("pbpaste").Output(); err == nil {
		snap.Text = string(out)
	}
	if strings.TrimSpace(snap.Text) != "" {
		return snap, nil
	}
	f, err := os.CreateTemp("", "kshell-paste-*.png")
	if err != nil {
		return snap, nil
	}
	name := f.Name()
	_ = f.Close()
	script := fmt.Sprintf(`set p to "%s"
try
	set pngData to (the clipboard as «class PNGf»)
	set fref to open for access (POSIX file p) with write permission
	set eof fref to 0
	write pngData to fref
	close access fref
on error
	try
		close access (POSIX file p)
	end try
end try`, name)
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		_ = os.Remove(name)
		return snap, nil
	}
	b, err := os.ReadFile(name)
	_ = os.Remove(name)
	if err == nil && isPNG(b) {
		snap.PNG = b
	}
	return snap, nil
}

func readUnixClipboard() (clipboardSnapshot, error) {
	var snap clipboardSnapshot
	if out, err := exec.Command("wl-paste", "-n").Output(); err == nil {
		snap.Text = string(out)
	} else if out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output(); err == nil {
		snap.Text = string(out)
	}
	if strings.TrimSpace(snap.Text) != "" {
		return snap, nil
	}
	if out, err := exec.Command("wl-paste", "-t", "image/png").Output(); err == nil && isPNG(out) {
		snap.PNG = out
		return snap, nil
	}
	if out, err := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output(); err == nil && isPNG(out) {
		snap.PNG = out
	}
	return snap, nil
}

func isPNG(b []byte) bool {
	return bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G'})
}
