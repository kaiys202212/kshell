package hubtools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"

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
