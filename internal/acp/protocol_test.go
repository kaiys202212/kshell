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
