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

// mcpProtocolVersion 是客户端未声明 protocolVersion 时的兜底版本。
// 正常情况下应回显客户端请求的版本：cursor-agent 1.0.0 发 2025-11-25，若服务端
// 坚持回旧版本，它会进入长退避重试（实测 session/new 156–202s，越过 kshell 的
// 握手超时上限并降级为终端）。
const mcpProtocolVersion = "2024-11-05"

// mcpSupportedVersions 是 kshell MCP 服务真正实现过的协议版本。
// 只回显这个集合里的值：对未知版本回落到兜底常量，避免向客户端谎称支持它
// 可能并不兼容的协议。本服务只用到 initialize / tools/list / tools/call，
// 语义在这些版本间一致，无需随规范演进频繁扩表。
var mcpSupportedVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}

// echoProtocolVersion 在客户端版本属于已知集合时原样回显，否则回落兜底常量。
func echoProtocolVersion(requested string) string {
	v := strings.TrimSpace(requested)
	for _, known := range mcpSupportedVersions {
		if v == known {
			return v
		}
	}
	return mcpProtocolVersion
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
		// 回显客户端声明的协议版本（限已知集合，见 echoProtocolVersion）；
		// 缺失或未知时才用兜底常量。部分客户端（cursor-agent）对回落的旧版本
		// 会长时间退避重试，因此已知的新版本必须原样回显。
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := echoProtocolVersion(p.ProtocolVersion)
		return rpcResult(req.ID, map[string]any{
			"protocolVersion": version,
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
