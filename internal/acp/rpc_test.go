package acp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestCodecRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	c := newCodec(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`+"\n"), &buf)

	m, err := c.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if m.JSONRPC != "2.0" || m.Method != "initialize" || m.ID == nil || *m.ID != "1" {
		t.Fatalf("unexpected message: %+v", m)
	}
	var p struct{ ProtocolVersion int `json:"protocolVersion"` }
	if err := json.Unmarshal(m.Params, &p); err != nil || p.ProtocolVersion != 1 {
		t.Fatalf("params decode: %v %+v", err, p)
	}

	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": 2, "result": map[string]any{}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("want single newline-terminated line, got %q", out)
	}
	if strings.Contains(strings.TrimSuffix(out, "\n"), "\n") {
		t.Fatalf("embedded newline: %q", out)
	}
}

func TestCodecStreamsAcrossReads(t *testing.T) {
	r := iotest.OneByteReader(strings.NewReader(`{"jsonrpc":"2.0","method":"session/update","params":{}}` + "\n"))
	c := newCodec(r, io.Discard)
	if _, err := c.read(); err != nil {
		t.Fatalf("read across chunks: %v", err)
	}
}

func TestCodecBadJSON_SurfacesError(t *testing.T) {
	c := newCodec(strings.NewReader("not json\n"), io.Discard)
	if _, err := c.read(); err == nil {
		t.Fatal("want decode error")
	}
}

// 字符串 id 走 idString.UnmarshalJSON 的引号分支，归一后应可直接比较。
func TestCodecStringID(t *testing.T) {
	c := newCodec(strings.NewReader(`{"jsonrpc":"2.0","id":"abc","method":"m"}`+"\n"), io.Discard)
	m, err := c.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if m.ID == nil || *m.ID != "abc" {
		t.Fatalf("want string id abc, got %+v", m.ID)
	}
}

// errWriter 模拟写出失败，用于验证错误向调用方传播而非被吞掉。
type errWriter struct{ err error }

func (w errWriter) Write(p []byte) (int, error) { return 0, w.err }

func TestCodecWriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	c := newCodec(strings.NewReader(""), errWriter{err: sentinel})
	err := c.write(map[string]any{"jsonrpc": "2.0", "id": 1})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want write error %v, got %v", sentinel, err)
	}
}

// 通知无 id（响应则带 id），且错误响应需解出 Code/Message。
func TestCodecNotificationAndErrorResponse(t *testing.T) {
	c := newCodec(strings.NewReader(
		`{"jsonrpc":"2.0","method":"session/update","params":{}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"Method not found"}}`+"\n",
	), io.Discard)

	note, err := c.read()
	if err != nil {
		t.Fatalf("read notification: %v", err)
	}
	if note.ID != nil || note.Method != "session/update" {
		t.Fatalf("unexpected notification: %+v", note)
	}

	resp, err := c.read()
	if err != nil {
		t.Fatalf("read error response: %v", err)
	}
	if resp.ID == nil || *resp.ID != "2" {
		t.Fatalf("want response id 2, got %+v", resp.ID)
	}
	if resp.Error == nil || resp.Error.Code != -32601 || resp.Error.Message != "Method not found" {
		t.Fatalf("unexpected error response: %+v", resp)
	}
}
