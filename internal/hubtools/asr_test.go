package hubtools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"testing"
)

func TestTranscribeAudio(t *testing.T) {
	var gotPath, gotAuth string
	var gotModel, gotFile string
	var gotWAV []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("解析 multipart: %v", err)
		}
		gotModel = r.FormValue("model")
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("缺少 file 字段: %v", err)
			return
		}
		defer f.Close()
		gotFile = hdr.Filename
		gotWAV, _ = io.ReadAll(f)
		io.WriteString(w, `{"text":"识别出的内容"}`)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL + "/v1", APIKey: "key-1"}
	text, err := TranscribeAudio(context.Background(), cfg, "asr-model-1", []byte("WAVDATA"))
	if err != nil {
		t.Fatalf("TranscribeAudio: %v", err)
	}
	if text != "识别出的内容" {
		t.Errorf("text = %q", text)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Errorf("路径 = %q, 期望 /v1/audio/transcriptions", gotPath)
	}
	if gotAuth != "Bearer key-1" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotModel != "asr-model-1" {
		t.Errorf("model 字段 = %q", gotModel)
	}
	if gotFile != "audio.wav" {
		t.Errorf("文件名 = %q, 期望 audio.wav", gotFile)
	}
	if string(gotWAV) != "WAVDATA" {
		t.Errorf("WAV 内容 = %q", gotWAV)
	}
}

func TestTranscribeAudioEndpointFromChatCompletions(t *testing.T) {
	// BaseURL 带 /chat/completions 尾巴（chat 客户端同款配置复用）时应剥掉再拼。
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, `{"text":"ok"}`)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL + "/v1/chat/completions", APIKey: "k"}
	text, err := TranscribeAudio(context.Background(), cfg, "m", []byte("x"))
	if err != nil {
		t.Fatalf("TranscribeAudio: %v", err)
	}
	if text != "ok" || gotPath != "/v1/audio/transcriptions" {
		t.Errorf("path=%q text=%q", gotPath, text)
	}
}

func TestTranscribeAudioErrors(t *testing.T) {
	// 模型缺失：不发请求直接报错。
	cfg := Config{BaseURL: "http://unused", APIKey: "k"}
	if _, err := TranscribeAudio(context.Background(), cfg, "  ", []byte("x")); err == nil {
		t.Error("模型为空应报错")
	}
	// BaseURL 缺失。
	cfg2 := Config{APIKey: "k"}
	if _, err := TranscribeAudio(context.Background(), cfg2, "m", []byte("x")); err == nil {
		t.Error("BaseURL 为空应报错")
	}
	// 服务端 500。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg3 := Config{BaseURL: srv.URL, APIKey: "k"}
	if _, err := TranscribeAudio(context.Background(), cfg3, "m", []byte("x")); err == nil {
		t.Error("HTTP 500 应报错")
	}
	// 响应无 text 字段 → 空串不报错（语义：转写为空）。
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{}`)
	}))
	defer srv2.Close()
	cfg4 := Config{BaseURL: srv2.URL, APIKey: "k"}
	text, err := TranscribeAudio(context.Background(), cfg4, "m", []byte("x"))
	if err != nil || text != "" {
		t.Errorf("无 text 响应应返回空串, got %q, %v", text, err)
	}
}

func TestTranscribeViaChat(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string           `json:"role"`
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		}
		if json.Unmarshal(body, &req) == nil {
			gotModel = req.Model
			raw, _ := json.Marshal(req.Messages)
			var msgs []map[string]any
			_ = json.Unmarshal(raw, &msgs)
			gotBody = map[string]any{"messages": msgs}
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"聊天模型转写结果"}}]}`)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL + "/v1/chat/completions", APIKey: "key-2"}
	text, err := TranscribeViaChat(context.Background(), cfg, "glm-4-voice", []byte("WAVBYTES"))
	if err != nil {
		t.Fatalf("TranscribeViaChat: %v", err)
	}
	if text != "聊天模型转写结果" {
		t.Errorf("text = %q", text)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("路径 = %q", gotPath)
	}
	if gotAuth != "Bearer key-2" || gotModel != "glm-4-voice" {
		t.Errorf("鉴权/模型错误: %q %q", gotAuth, gotModel)
	}
	// content 数组：text 块 + input_audio 块（base64 音频，format=wav）。
	msgs, _ := gotBody["messages"].([]map[string]any)
	if len(msgs) == 0 {
		t.Fatal("缺少 messages")
	}
	contents, _ := msgs[len(msgs)-1]["content"].([]any)
	if len(contents) != 2 {
		t.Fatalf("user content 块数 = %v", contents)
	}
	audio, _ := contents[1].(map[string]any)
	if audio["type"] != "input_audio" {
		t.Errorf("第二块 type = %v", audio["type"])
	}
	ia, _ := audio["input_audio"].(map[string]any)
	if ia == nil || ia["format"] != "wav" || ia["data"] != "V0FWQllURVM=" {
		t.Errorf("input_audio 块错误: %v", ia)
	}
	// 提示词要求只输出转写文本。
	txt, _ := contents[0].(map[string]any)
	if t2, _ := txt["text"].(string); !strings.Contains(t2, "转写") {
		t.Errorf("text 块提示词缺失: %v", txt)
	}
}

func TestTranscribeViaChatAudioUnsupported(t *testing.T) {
	// 400 + 响应体含 input_audio（火山等网关对不支持音频的模型的真实报错形态）
	// → 哨兵 ErrChatAudioUnsupported，不再透传网关原始报错。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":"InvalidParameter","message":"The parameter `+"`messages.content.input_audio`"+` specified in the request are not valid: audio input is not supported by this model. Request id: 021792..."}}`)
	}))
	defer srv.Close()
	cfg := Config{BaseURL: srv.URL, APIKey: "k"}
	_, err := TranscribeViaChat(context.Background(), cfg, "glm-5.3-flash", []byte("x"))
	if !errors.Is(err, ErrChatAudioUnsupported) {
		t.Fatalf("应返回 ErrChatAudioUnsupported, got %v", err)
	}
	if !strings.Contains(err.Error(), "err.hubtools.chat_audio_unsupported") {
		t.Errorf("错误串应含稳定 key: %v", err)
	}

	// 400 + 与音频无关的其他参数错 → 普通错误，不误判。
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":"InvalidParameter","message":"The parameter temperature is not valid"}}`)
	}))
	defer srv2.Close()
	cfg2 := Config{BaseURL: srv2.URL, APIKey: "k"}
	_, err = TranscribeViaChat(context.Background(), cfg2, "m", []byte("x"))
	if err == nil || errors.Is(err, ErrChatAudioUnsupported) {
		t.Fatalf("无关 400 不应判为音频不支持: %v", err)
	}

	// 404（部分网关对未知字段报 404）+ input_audio 文案 → 哨兵。
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"message":"Unknown parameter: input_audio"}}`)
	}))
	defer srv3.Close()
	cfg3 := Config{BaseURL: srv3.URL, APIKey: "k"}
	_, err = TranscribeViaChat(context.Background(), cfg3, "m", []byte("x"))
	if !errors.Is(err, ErrChatAudioUnsupported) {
		t.Fatalf("404+input_audio 应返回哨兵, got %v", err)
	}
}

func TestTranscribeAutoPriority(t *testing.T) {
	// asr 优先：asrModel 非空时走 /audio/transcriptions。
	var asrHits, chatHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			asrHits++
			io.WriteString(w, `{"text":"ASR 结果"}`)
			return
		}
		chatHits++
		io.WriteString(w, `{"choices":[{"message":{"content":"chat 结果"}}]}`)
	}))
	defer srv.Close()
	cfg := Config{BaseURL: srv.URL, APIKey: "k"}

	text, err := TranscribeAuto(context.Background(), cfg, "asr-m", "chat-m", []byte("x"))
	if err != nil || text != "ASR 结果" {
		t.Fatalf("asr 优先失败: %q %v", text, err)
	}
	if asrHits != 1 || chatHits != 0 {
		t.Fatalf("asr=%d chat=%d", asrHits, chatHits)
	}

	// asrModel 留空：回落 chat。
	text, err = TranscribeAuto(context.Background(), cfg, "", "chat-m", []byte("x"))
	if err != nil || text != "chat 结果" {
		t.Fatalf("chat 回落失败: %q %v", text, err)
	}
	if chatHits != 1 {
		t.Fatalf("chat 调用数 = %d", chatHits)
	}

	// 两者皆空：报错。
	if _, err := TranscribeAuto(context.Background(), cfg, "  ", "", []byte("x")); err == nil {
		t.Error("双空应报错")
	}
}
