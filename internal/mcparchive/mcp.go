// Package mcparchive 提供两件事：
//  1. 桌面进程上的本机 HTTP，接收「建议归档」；
//  2. 一个最小 stdio MCP 服务（kshell mcp-archive），把 suggest_archive 工具转发给上面的 HTTP。
//
// agent 在任务完成时调用该工具，kshell 再问用户是否归档，而不是由工具直接改名单。
package mcparchive

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// PrependConfig 给支持 --mcp-config 的 CLI 前置配置路径。未知工具原样返回，避免启动失败。
func PrependConfig(toolID, configPath string, args []string) []string {
	if toolID != "claude" || configPath == "" {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "--mcp-config", configPath)
	return append(out, args...)
}

// WriteConfig 写出 Claude Code 认识的 mcpServers JSON。
func WriteConfig(path, command string, args []string) error {
	if args == nil {
		args = []string{}
	}
	payload := map[string]any{
		"mcpServers": map[string]any{
			"kshell": map[string]any{
				"command": command,
				"args":    args,
			},
		},
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func dirOf(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i <= 0 {
		return "."
	}
	return path[:i]
}

// Hub 是仅监听 127.0.0.1 的建议入口。Token 防止本机其它进程误调。
type Hub struct {
	URL   string
	Token string
	srv   *http.Server
}

// StartHub 在随机端口监听。on 在请求校验通过后调用，参数是 ref 与 summary。
func StartHub(on func(ref, summary string)) (*Hub, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(buf)
	mux := http.NewServeMux()
	mux.HandleFunc("/suggest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			Ref     string `json:"ref"`
			Summary string `json:"summary"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if on != nil {
			on(body.Ref, body.Summary)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	h := &Hub{URL: "http://" + ln.Addr().String(), Token: token, srv: &http.Server{Handler: mux}}
	go func() { _ = h.srv.Serve(ln) }()
	return h, nil
}

// Close 停止监听。
func (h *Hub) Close() {
	if h != nil && h.srv != nil {
		_ = h.srv.Close()
	}
}

type rpcReq struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Handle 处理一条 MCP JSON-RPC 消息。通知（无 id）返回 ok=false，调用方不回包。
func Handle(msg []byte, suggest func(summary string) error) ([]byte, bool) {
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
			"serverInfo":      map[string]any{"name": "kshell", "version": "0.1.0"},
		}, nil), true
	case "tools/list":
		return rpcResult(req.ID, map[string]any{"tools": []any{archiveTool()}}, nil), true
	case "tools/call":
		var p struct {
			Name      string `json:"name"`
			Arguments struct {
				Summary string `json:"summary"`
			} `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.Name != "suggest_archive" {
			return rpcResult(req.ID, nil, fmt.Errorf("unknown tool %s", p.Name)), true
		}
		if suggest != nil {
			if err := suggest(p.Arguments.Summary); err != nil {
				return rpcResult(req.ID, map[string]any{
					"content": []any{map[string]string{"type": "text", "text": err.Error()}},
					"isError": true,
				}, nil), true
			}
		}
		return rpcResult(req.ID, map[string]any{
			"content": []any{map[string]string{"type": "text", "text": "已询问用户是否归档"}},
		}, nil), true
	default:
		return rpcResult(req.ID, nil, fmt.Errorf("unknown method %s", req.Method)), true
	}
}

func archiveTool() map[string]any {
	return map[string]any{
		"name":        "suggest_archive",
		"description": "仅当用户交给你的任务已经全部完成时调用。kshell 会询问用户是否把当前会话归档。任务还在进行、或只是阶段性进展时不要调用。",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]string{
					"type":        "string",
					"description": "一句话说明完成了什么",
				},
			},
			"required": []string{"summary"},
		},
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

// ServeIO 按 LSP 风格的 Content-Length 帧读写 stdio，直到输入结束。
func ServeIO(r io.Reader, w io.Writer, suggest func(summary string) error) error {
	br := bufio.NewReader(r)
	for {
		msg, err := readFrame(br)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		resp, ok := Handle(msg, suggest)
		if !ok {
			continue
		}
		if err := writeFrame(w, resp); err != nil {
			return err
		}
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	var length int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			if err != nil {
				return nil, err
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("bad content-length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeFrame(w io.Writer, body []byte) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(body))
	buf.Write(body)
	_, err := w.Write(buf.Bytes())
	return err
}

// Main 是 `kshell mcp-archive` 子命令：把工具调用 POST 回桌面进程。
func Main(args []string) error {
	fs := flag.NewFlagSet("mcp-archive", flag.ContinueOnError)
	url := fs.String("url", "", "desktop suggest url")
	token := fs.String("token", "", "bearer token")
	ref := fs.String("ref", "", "terminal:<id> or chat:<id>")
	if err := fs.Parse(args); err != nil {
		return err
	}
	suggest := func(summary string) error {
		body, err := json.Marshal(map[string]string{"ref": *ref, "summary": summary})
		if err != nil {
			return err
		}
		endpoint := strings.TrimRight(*url, "/") + "/suggest"
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+*token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("suggest status %d", res.StatusCode)
		}
		return nil
	}
	return ServeIO(os.Stdin, os.Stdout, suggest)
}
