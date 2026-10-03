// Package acp 实现 Agent Client Protocol v1 的客户端侧：
// stdio 上换行分隔的 JSON-RPC 2.0 编解码与 agent 子进程交互。
package acp

import (
	"encoding/json"
	"fmt"
	"io"
)

const jsonrpcVersion = "2.0"

// RPCError 是 JSON-RPC 错误对象。
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("acp rpc error %d: %s", e.Code, e.Message) }

// idString 统一 id 形态：JSON 数字或字符串都归一为字符串，避免区分数字/字符串 id。
type idString string

func (s *idString) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = idString(str)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*s = idString(n)
	return nil
}

// message 是 JSON-RPC 请求/响应/通知的统一解包结构。
// 判定：Method!="" && ID!=nil → 反向请求；Method!="" && ID==nil → 通知；Method=="" && ID!=nil → 响应。
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *idString       `json:"id,omitempty"` // 保留原始字符串形态，比较无需关心数字/字符串
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// codec 在读取端用 json.Decoder（天然按 JSON 值流式切分，兼容分片读）；
// 写入端串行化 marshaled JSON + '\n'。写锁由调用方保证。
type codec struct {
	dec *json.Decoder
	w   io.Writer
}

func newCodec(r io.Reader, w io.Writer) *codec {
	return &codec{dec: json.NewDecoder(r), w: w}
}

func (c *codec) read() (message, error) {
	var m message
	if err := c.dec.Decode(&m); err != nil {
		return message{}, err
	}
	return m, nil
}

func (c *codec) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// requestEnvelope / responseEnvelope / notificationEnvelope 是写出的三种消息。
type requestEnvelope struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type responseEnvelope struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      string    `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

type notificationEnvelope struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
