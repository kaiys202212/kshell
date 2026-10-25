package main

import (
	"testing"
	"testing/fstest"
)

func TestStartHiddenFromArgs(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"kshell.exe"}, false},                               // 正常启动
		{[]string{"kshell.exe", "--autostart-hidden"}, true},          // 自启动静默拉起
		{[]string{"kshell.exe", "agent-hook"}, false},                 // 隐藏子命令不受影响
		{[]string{"kshell.exe", "--autostart-hidden", "extra"}, true}, // 参数只认 os.Args[1]
		{[]string{"kshell.exe", "x", "--autostart-hidden"}, false},    // 非 Args[1] 不算
	}
	for _, tc := range cases {
		if got := startHiddenFromArgs(tc.args); got != tc.want {
			t.Fatalf("startHiddenFromArgs(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestDesktopOptionsStartHidden(t *testing.T) {
	src := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	if got := desktopOptions(src, true).StartHidden; !got {
		t.Fatal("startHidden=true 应透传给 Wails StartHidden")
	}
	if got := desktopOptions(src, false).StartHidden; got {
		t.Fatal("startHidden=false 不应设置 StartHidden")
	}
}
