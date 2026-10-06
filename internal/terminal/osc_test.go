package terminal

import (
	"bytes"
	"strings"
	"testing"
)

// feedAll 把 data 按指定切片依次喂给扫描器，返回全部命中。
// splitAt 为空表示整块喂入；否则按给定的每片字节数切割（最后一个分片可能不足）。
func feedAll(sc *oscScanner, data string, per int) []oscHit {
	var hits []oscHit
	b := []byte(data)
	for len(b) > 0 {
		n := per
		if n <= 0 || n > len(b) {
			n = len(b)
		}
		hits = append(hits, sc.Feed(b[:n])...)
		b = b[n:]
	}
	return hits
}

func TestOSCScannerDetect(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    []string // 期望命中摘要，空切片表示不应命中
	}{
		{
			name:  "OSC9 BEL 终止",
			input: "\x1b]9;任务完成\x07",
			want:  []string{"任务完成"},
		},
		{
			name:  "OSC9 ST 终止",
			input: "\x1b]9;waiting for input\x1b\\",
			want:  []string{"waiting for input"},
		},
		{
			name:  "OSC777 notify BEL 终止",
			input: "\x1b]777;notify;Gemini 标题;正文内容\x07",
			want:  []string{"Gemini 标题"},
		},
		{
			name:  "OSC777 notify ST 终止",
			input: "\x1b]777;notify;标题;带;分号的正文\x1b\\",
			want:  []string{"标题"},
		},
		{
			name:  "OSC9;4 进度排除",
			input: "\x1b]9;4;running;50\x07",
			want:  nil,
		},
		{
			name:  "OSC9;4 无进度值也排除",
			input: "\x1b]9;4\x07",
			want:  nil,
		},
		{
			name:  "OSC9;40% 不是进度（仅 9;4; 前缀排除）",
			input: "\x1b]9;40%\x07",
			want:  []string{"40%"},
		},
		{
			name:  "其它 OSC 编号不命中（窗口标题）",
			input: "\x1b]0;window title\x07",
			want:  nil,
		},
		{
			name:  "OSC777 非 notify 子命令不命中",
			input: "\x1b]777;update;title\x07",
			want:  nil,
		},
		{
			name:  "普通文本零命中",
			input: "hello world\r\n$ ",
			want:  nil,
		},
		{
			name:  "孤立 ESC 不命中",
			input: "\x1b[2J\x1b[Hprompt> ",
			want:  nil,
		},
		{
			name:  "前后夹普通文本的一次命中",
			input: "before\x1b]9;done\x07after",
			want:  []string{"done"},
		},
		{
			name:  "单块多次命中按序返回",
			input: "\x1b]9;first\x07mid\x1b]9;second\x07",
			want:  []string{"first", "second"},
		},
		{
			name:  "OSC9 空文本不命中",
			input: "\x1b]9;\x07",
			want:  nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sc := newOSCScanner()
			hits := feedAll(sc, c.input, 0)
			if len(hits) != len(c.want) {
				t.Fatalf("命中数 = %d (%+v), 期望 %d", len(hits), hits, len(c.want))
			}
			for i, w := range c.want {
				if hits[i].Summary != w {
					t.Fatalf("命中[%d].Summary = %q, 期望 %q", i, hits[i].Summary, w)
				}
			}
		})
	}
}

// TestOSCScannerRawPreserved 验证 Raw 是含 ESC 引导与终止符的完整原文。
func TestOSCScannerRawPreserved(t *testing.T) {
	sc := newOSCScanner()
	hits := sc.Feed([]byte("\x1b]9;ok\x07"))
	if len(hits) != 1 || hits[0].Raw != "\x1b]9;ok\x07" {
		t.Fatalf("BEL 终止 Raw = %+v", hits)
	}

	sc = newOSCScanner()
	hits = sc.Feed([]byte("\x1b]777;notify;t;b\x1b\\"))
	if len(hits) != 1 || hits[0].Raw != "\x1b]777;notify;t;b\x1b\\" {
		t.Fatalf("ST 终止 Raw = %+v", hits)
	}
}

// TestOSCScannerByteByByte 验证序列跨 chunk 边界：1 字节一片喂入仍能识别。
func TestOSCScannerByteByByte(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"OSC9 BEL 跨块", "\x1b]9;跨块完成\x07", "跨块完成"},
		{"OSC9 ST 跨块", "\x1b]9;split\x1b\\", "split"},
		{"OSC777 跨块", "\x1b]777;notify;t;body\x07", "t"},
		{"ESC 在块尾", "abc\x1b", ""},
		{"ESC] 在块尾", "abc\x1b]9;x", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sc := newOSCScanner()
			hits := feedAll(sc, c.input, 1)
			if c.want == "" {
				if len(hits) != 0 {
					t.Fatalf("不应命中, got %+v", hits)
				}
				return
			}
			if len(hits) != 1 || hits[0].Summary != c.want {
				t.Fatalf("命中 = %+v, 期望 %q", hits, c.want)
			}
		})
	}
}

// TestOSCScannerPendingCap 验证无终止符时的内存安全：超限丢弃缓冲，
// 且丢弃后扫描器能恢复，继续识别后续完整序列。
func TestOSCScannerPendingCap(t *testing.T) {
	sc := newOSCScanner()
	// 超过 4KB 上限的无终止符序列
	long := "\x1b]9;" + strings.Repeat("x", 8192)
	if hits := feedAll(sc, long, 100); len(hits) != 0 {
		t.Fatalf("无终止符超长序列不应命中, got %+v", hits)
	}
	// 缓冲必须已丢弃（而不是攒着 8KB）：再喂一个完整序列仍能命中
	hits := sc.Feed([]byte("\x1b]9;after\x07"))
	if len(hits) != 1 || hits[0].Summary != "after" {
		t.Fatalf("超限丢弃后应恢复识别, got %+v", hits)
	}
}

// TestOSCScannerDoesNotTouchInput 验证只观察不消费：输入切片内容不被改写。
func TestOSCScannerDoesNotTouchInput(t *testing.T) {
	sc := newOSCScanner()
	in := []byte("keep\x1b]9;me\x07intact")
	snapshot := bytes.Clone(in)
	sc.Feed(in)
	if !bytes.Equal(in, snapshot) {
		t.Fatalf("扫描器不得改写输入: %q vs %q", in, snapshot)
	}
}

// TestOSCScannerSplitAcrossFeeds 验证命中序列被任意切割（含终止符拆两半）仍能拼回识别。
func TestOSCScannerSplitAcrossFeeds(t *testing.T) {
	sc := newOSCScanner()
	var hits []oscHit
	hits = append(hits, sc.Feed([]byte("log\x1b]777;not"))...)
	hits = append(hits, sc.Feed([]byte("ify;标题;正\x07more"))...)
	if len(hits) != 1 || hits[0].Summary != "标题" {
		t.Fatalf("跨块 777 命中 = %+v", hits)
	}
	// 后续普通文本不应残留旧状态
	if got := sc.Feed([]byte("plain")); len(got) != 0 {
		t.Fatalf("结算后不应再命中: %+v", got)
	}
}
