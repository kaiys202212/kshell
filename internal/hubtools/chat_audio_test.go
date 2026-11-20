package hubtools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTranscribeViaChatRejectsRefusal 覆盖 2026-11-20 真机缺陷：网关对
// input_audio 返回 200 但模型拒答（「没有收到语音内容」），拒答被当成转写
// 文本投递给 agent。拒答必须显式化为 ErrChatAudioUnsupported。
func TestTranscribeViaChatRejectsRefusal(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"中文-我没有收到", "我这边没有收到语音内容，当前对话里看不到任何音频附件，所以暂时无法转写。请把语音文件发上来，或重新上传一次。"},
		{"中文-真机200拒答", "我还没有收到需要转写的语音内容，因为当前对话里没有附带音频文件或可识别的语音信息。请把语音文件上传，或者把语音内容以文字形式发给我，我再帮你逐字转写。"},
		{"中文-短拒答", "无法转写：没有收到音频。"},
		{"英文-no-audio", "I didn't receive any audio content in this conversation, so I cannot transcribe it. Please upload the file again."},
		{"英文-please-resend", "There is no audio attached. Please resend the voice message."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `{"choices":[{"message":{"content":`+jsonString(tc.body)+`}}]}`)
			}))
			defer srv.Close()
			cfg := Config{BaseURL: srv.URL, APIKey: "k"}
			_, err := TranscribeViaChat(context.Background(), cfg, "m", []byte("x"))
			if !errors.Is(err, ErrChatAudioUnsupported) {
				t.Fatalf("拒答应返回 ErrChatAudioUnsupported, got %v", err)
			}
			if !strings.Contains(err.Error(), "chat_audio_unsupported") {
				t.Errorf("错误串应含稳定 key: %v", err)
			}
		})
	}
}

// TestTranscribeViaChatKeepsLegitTranscript 合法转写（即使口语内容里带有
// 「上传」「无法转写」等词）必须照常返回，不被拒答检测误伤。
func TestTranscribeViaChatKeepsLegitTranscript(t *testing.T) {
	cases := []string{
		"分析一下策略评判，拉取最新的代码，看看昨天晚上的那些改动。",
		"帮我重新上传一下那个安装包，然后跑一次构建。",
		"请把语音文件发给我。",
		"今天的会议要点：无法转写这个词出现在讨论中，但整体是正常转述。",
		strings.Repeat("这是一段很长的正常语音转写内容，", 20) + "里面提到了没有收到语音但整体是正常陈述。",
	}
	for _, text := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"choices":[{"message":{"content":`+jsonString(text)+`}}]}`)
		}))
		cfg := Config{BaseURL: srv.URL, APIKey: "k"}
		got, err := TranscribeViaChat(context.Background(), cfg, "m", []byte("x"))
		srv.Close()
		if err != nil {
			t.Fatalf("合法转写不应报错: %q %v", text, err)
		}
		if got != text {
			t.Fatalf("转写被改写: got %q want %q", got, text)
		}
	}
}

// TestIsTranscriptRejection 表驱动校验拒答识别边界：只命中「模型声称没收到
// 音频」的短文本，不误伤正常转写。
func TestIsTranscriptRejection(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"我这边没有收到语音内容，当前对话里看不到任何音频附件", true},
		{"没有收到语音", true},
		{"未收到音频附件", true},
		{"无法转写这段内容", true},
		{"你还没有提供语音", true},
		{"请把语音文件发上来", true},
		{"请重新上传一次", true},
		{"I didn't receive any audio", true},
		{"No audio was provided.", true},
		{"cannot transcribe this", true},
		{"Please upload the file", true},
		{"", false},
		{"分析一下策略评判", false},
		{"帮我把文件重新上传到服务器", false},
		{"请把语音文件发给我", false},
		{"我没看到你的语音消息，再发一次", false},
		{"今天的会议要点：无法转写这个词出现在讨论中，但整体是正常转述", false},
		{strings.Repeat("正常转写内容", 40) + "没有收到语音", false},
	}
	for _, tc := range cases {
		if got := isTranscriptRejection(tc.text); got != tc.want {
			t.Errorf("isTranscriptRejection(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

// jsonString 把任意字符串编码为 JSON 字符串字面量（测试拼响应体用）。
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}