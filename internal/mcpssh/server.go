package mcpssh

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type rpcReq struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Handle 处理一条 MCP JSON-RPC 消息。通知（无 id）返回 ok=false，调用方不回包。
func Handle(msg []byte, tools *Tools) ([]byte, bool) {
	var req rpcReq
	if err := json.Unmarshal(msg, &req); err != nil {
		return rpcResult(nil, nil, err), true
	}
	if len(req.ID) == 0 {
		return nil, false
	}
	switch req.Method {
	case "initialize":
		return rpcResult(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "kshell-mcp-ssh", "version": "0.1.0"},
		}, nil), true
	case "tools/list":
		defs := tools.ToolDefs()
		out := make([]any, len(defs))
		for i, d := range defs {
			out[i] = d
		}
		return rpcResult(req.ID, map[string]any{"tools": out}, nil), true
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcResult(req.ID, nil, err), true
		}
		text, isError, err := tools.Call(context.Background(), p.Name, p.Arguments)
		if err != nil && !isError {
			return rpcResult(req.ID, nil, err), true
		}
		if isError {
			msg := err.Error()
			if err == nil {
				msg = "tool error"
			}
			return rpcResult(req.ID, map[string]any{
				"content": []any{map[string]string{"type": "text", "text": msg}},
				"isError": true,
			}, nil), true
		}
		return rpcResult(req.ID, map[string]any{
			"content": []any{map[string]string{"type": "text", "text": text}},
		}, nil), true
	default:
		return rpcResult(req.ID, nil, fmt.Errorf("unknown method %s", req.Method)), true
	}
}

func rpcResult(id json.RawMessage, result any, err error) []byte {
	env := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id)}
	if err != nil {
		env["error"] = map[string]any{"code": -32000, "message": err.Error()}
	} else {
		env["result"] = result
	}
	b, _ := json.Marshal(env)
	return b
}

// ServeIO 按 MCP stdio 标准（换行分隔 JSON）读写，直到输入结束。
// 此前误用 LSP Content-Length 帧：MCP 客户端发换行 JSON initialize 得不到
// 应答，干等到启动超时（~30s）。与 mcparchive.ServeIO 同构。
func ServeIO(r io.Reader, w io.Writer, tools *Tools) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024) // 超长消息不爆默认 64KB 上限
	bw := bufio.NewWriter(w)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		resp, ok := Handle([]byte(line), tools)
		if !ok {
			continue
		}
		if _, err := bw.Write(resp); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
		// 逐消息 Flush：stdio 长连接下 stdin 不会关闭，攒缓冲客户端永远收不到。
		if err := bw.Flush(); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return bw.Flush()
}
