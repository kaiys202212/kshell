package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeUpdateEnvelope(t *testing.T) {
	raw := json.RawMessage(`{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"type":"text","text":"hi"}}}`)
	sid, upd, err := decodeUpdateParams(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sid != "s1" {
		t.Fatalf("sessionId = %q", sid)
	}
	var head struct {
		SessionUpdate string `json:"sessionUpdate"`
		MessageID     string `json:"messageId"`
	}
	if err := json.Unmarshal(upd, &head); err != nil || head.SessionUpdate != "agent_message_chunk" || head.MessageID != "m1" {
		t.Fatalf("update head: %v %+v", err, head)
	}
}

func TestDecodeRequestPermissionParams(t *testing.T) {
	raw := json.RawMessage(`{"sessionId":"s1","toolCall":{"toolCallId":"c1","title":"Write file","kind":"edit"},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"}]}`)
	p, err := decodeRequestPermissionParams(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.SessionID != "s1" || p.ToolCall.ToolCallID != "c1" || len(p.Options) != 1 || p.Options[0].OptionID != "allow" {
		t.Fatalf("unexpected params: %+v", p)
	}
}

func TestMarshalOutboundParams(t *testing.T) {
	// 空切片必须序列化为 []，不能是 null，否则 agent 会把 mcpServers 当成缺失。
	b, err := json.Marshal(NewSessionParams{Cwd: "/x", McpServers: []any{}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"cwd":"/x"`) {
		t.Fatalf("missing cwd: %s", s)
	}
	if !strings.Contains(s, `"mcpServers":[]`) {
		t.Fatalf("mcpServers not []: %s", s)
	}

	// 经 requestEnvelope 往返，确认 json tag 一致。
	env := requestEnvelope{JSONRPC: jsonrpcVersion, ID: "1", Method: "session/new", Params: NewSessionParams{Cwd: "/x", McpServers: []any{}}}
	eb, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var got struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(eb, &got); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if got.JSONRPC != jsonrpcVersion || got.ID != "1" || got.Method != "session/new" {
		t.Fatalf("envelope head: %+v (%s)", got, eb)
	}
	var p NewSessionParams
	if err := json.Unmarshal(got.Params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p.Cwd != "/x" || p.McpServers == nil {
		t.Fatalf("round-trip params: %+v", p)
	}
}

func TestMarshalPermissionResult(t *testing.T) {
	b, err := json.Marshal(RequestPermissionResult{Outcome: PermissionOutcome{Outcome: "selected", OptionID: "allow"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"outcome":"selected"`) {
		t.Fatalf("missing outcome: %s", s)
	}
	if !strings.Contains(s, `"optionId":"allow"`) {
		t.Fatalf("missing optionId: %s", s)
	}
}

// session/new 响应必须能解析 agent 报告的可用模型（v1 models 字段与 configOptions 双形态）。
func TestUnmarshalNewSessionResultModels(t *testing.T) {
	raw := `{"sessionId":"s1","models":{"availableModels":[{"modelId":"m1","name":"Model One"},{"modelId":"m2","name":"Two","description":"d2"}],"currentModelId":"m1"},"configOptions":[{"id":"model","type":"select","currentValue":"m1","options":[{"value":"m1","name":"Model One"}]}]}`
	var r NewSessionResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.SessionID != "s1" {
		t.Fatalf("sessionId = %q", r.SessionID)
	}
	if r.Models == nil || len(r.Models.AvailableModels) != 2 || r.Models.CurrentModelID != "m1" {
		t.Fatalf("models = %+v", r.Models)
	}
	if r.Models.AvailableModels[0].ModelID != "m1" || r.Models.AvailableModels[0].Name != "Model One" {
		t.Fatalf("model[0] = %+v", r.Models.AvailableModels[0])
	}
	if r.Models.AvailableModels[1].Description != "d2" {
		t.Fatalf("model[1] = %+v", r.Models.AvailableModels[1])
	}
	if len(r.ConfigOptions) != 1 || r.ConfigOptions[0].ID != "model" || len(r.ConfigOptions[0].Options) != 1 {
		t.Fatalf("configOptions = %+v", r.ConfigOptions)
	}
}

// agent 不报 models 时必须保持 nil，不能误造空结构。
func TestUnmarshalNewSessionResultBare(t *testing.T) {
	var r NewSessionResult
	if err := json.Unmarshal([]byte(`{"sessionId":"s1"}`), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Models != nil || len(r.ConfigOptions) != 0 {
		t.Fatalf("unexpected fields: %+v", r)
	}
}

// ContentBlock 序列化必须覆盖 image / resource_link / resource 各形态，text 保持原样。
func TestMarshalContentBlocks(t *testing.T) {
	text, _ := json.Marshal([]ContentBlock{{Type: "text", Text: "hi"}})
	if string(text) != `[{"type":"text","text":"hi"}]` {
		t.Fatalf("text block = %s", text)
	}
	image, _ := json.Marshal([]ContentBlock{{Type: "image", Data: "AAAA", MimeType: "image/png"}})
	if !strings.Contains(string(image), `"type":"image"`) || !strings.Contains(string(image), `"data":"AAAA"`) || !strings.Contains(string(image), `"mimeType":"image/png"`) {
		t.Fatalf("image block = %s", image)
	}
	link, _ := json.Marshal([]ContentBlock{{Type: "resource_link", URI: "file:///C:/x.png", Name: "x.png"}})
	if !strings.Contains(string(link), `"type":"resource_link"`) || !strings.Contains(string(link), `"uri":"file:///C:/x.png"`) || !strings.Contains(string(link), `"name":"x.png"`) {
		t.Fatalf("resource_link block = %s", link)
	}
	res, _ := json.Marshal([]ContentBlock{{Type: "resource", Resource: &ResourceContents{URI: "file:///a.txt", MimeType: "text/plain", Blob: "AAA"}}})
	if !strings.Contains(string(res), `"resource":{"uri":"file:///a.txt"`) || !strings.Contains(string(res), `"blob":"AAA"`) {
		t.Fatalf("resource block = %s", res)
	}
}

func TestMarshalSetModelParams(t *testing.T) {
	b, err := json.Marshal(SetModelParams{SessionID: "s1", ModelID: "m2"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"sessionId":"s1","modelId":"m2"}` {
		t.Fatalf("set_model params = %s", b)
	}
}
