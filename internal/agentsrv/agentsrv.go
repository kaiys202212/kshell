// Package agentsrv 把 agentapi 暴露为本机 HTTP 服务：桌面端启动时常驻，
// 127.0.0.1 临时端口 + 随机 Bearer token；连接发现文件
// ~/.kshell/agentapi/endpoint.json（{url,token,pid,version}）供 CLI 薄客户端
// 读取。仅本机可信调用方（外部 agent CLI），v1 变更类操作直通不做确认门禁。
package agentsrv

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/yangk/kshell/internal/agentapi"
)

// endpointVersion 是发现文件结构版本。
const endpointVersion = 1

// watchBuf 是服务端订阅事件流的缓冲（客户端侧另有缓冲；慢客户端丢帧优先，
// 绝不阻塞聊天主路径）。
const watchBuf = 64

// Options 是服务启动参数。
type Options struct {
	API          *agentapi.API
	EndpointPath string // 发现文件路径（目录不存在自动创建）
}

// Server 是常驻服务句柄。
type Server struct {
	url      string
	token    string
	srv      *http.Server
	epPath   string
	closeOnce sync.Once
}

// URL 返回服务根地址（http://127.0.0.1:<port>）。
func (s *Server) URL() string { return s.url }

// Token 返回 Bearer token。
func (s *Server) Token() string { return s.token }

// Close 停止服务并删除发现文件（幂等）。
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		_ = s.srv.Close()
		if s.epPath != "" {
			_ = os.Remove(s.epPath)
		}
	})
}

// Start 启动服务并写发现文件。
func Start(opts Options) (*Server, error) {
	if opts.API == nil {
		return nil, errors.New("agentsrv: API 不能为空")
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{
		url:    "http://" + ln.Addr().String(),
		token:  hex.EncodeToString(token),
		epPath: opts.EndpointPath,
	}
	s.srv = &http.Server{Handler: s.auth(s.mux(opts.API))}
	go func() {
		// Serve 在 Close 时返回 ErrServerClosed，属正常退出。
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("agentsrv: 服务异常退出: %v", err)
		}
	}()
	if err := s.writeEndpoint(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// writeEndpoint 原子性要求不高（每次启动重写），直接 MkdirAll + WriteFile 0600。
func (s *Server) writeEndpoint() error {
	if s.epPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.epPath), 0o700); err != nil {
		return err
	}
	ep, _ := json.Marshal(map[string]any{
		"url":     s.url,
		"token":   s.token,
		"pid":     os.Getpid(),
		"version": endpointVersion,
	})
	return os.WriteFile(s.epPath, ep, 0o600)
}

// auth 是 Bearer 校验中间件（token 比较用常量时间，防时序侧信道——虽仅本机）。
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		given := strings.TrimPrefix(authHeader, "Bearer ")
		if !strings.HasPrefix(authHeader, "Bearer ") ||
			subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mux 构建全部路由。路径带会话 id，手工解析（数量少，不引第三方路由）。
func (s *Server) mux(api *agentapi.API) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sessions/search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, nonNil(api.Search(r.URL.Query().Get("q"))))
	})
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		s.handleList(w, r, api)
	})
	mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		s.handleSession(w, r, api)
	})
	mux.HandleFunc("/api/v1/recall", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MsgID int64 `json:"msgID"`
		}
		if !readBody(w, r, &body) {
			return
		}
		writeErrOrOK(w, api.Recall(body.MsgID))
	})
	mux.HandleFunc("/api/v1/sessions/create", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tool  string `json:"tool"`
			Ws    string `json:"ws"`
			Model string `json:"model"`
		}
		if !readBody(w, r, &body) {
			return
		}
		s, err := api.Create(body.Tool, body.Ws, body.Model)
		writeErrOrObj(w, s, err)
	})
	mux.HandleFunc("/api/v1/sessions/open", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"sessionID"`
		}
		if !readBody(w, r, &body) {
			return
		}
		s, err := api.Open(body.SessionID)
		writeErrOrObj(w, s, err)
	})
	mux.HandleFunc("/api/v1/sessions/create-nl", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text    string              `json:"text"`
			Confirm *agentapi.CreatePlan `json:"confirm,omitempty"`
		}
		if !readBody(w, r, &body) {
			return
		}
		res, err := api.CreateNL(r.Context(), body.Text, agentapi.NLCreate{Confirm: body.Confirm})
		writeErrOrObj(w, res, err)
	})
	mux.HandleFunc("/api/v1/route/decide", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if !readBody(w, r, &body) {
			return
		}
		plan, err := api.RouteDecide(r.Context(), body.Text)
		writeErrOrObj(w, plan, err)
	})
	mux.HandleFunc("/api/v1/route/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Decision json.RawMessage   `json:"decision"`
			Sessions []agentapi.Session `json:"sessions"`
		}
		if !readBody(w, r, &body) {
			return
		}
		var dec agentapi.RoutePlan
		if err := json.Unmarshal(body.Decision, &dec.RouteDecision); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "decision JSON 非法: "+err.Error())
			return
		}
		dec.Sessions = body.Sessions
		writeErrOrOK(w, api.RouteSend(dec))
	})
	return mux
}

// nonNil 把 nil 切片归一成空数组：JSON 契约里列表恒为数组，null 增加客户端分支。
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// handleList 处理 GET /api/v1/sessions?scope=。
func (s *Server) handleList(w http.ResponseWriter, r *http.Request, api *agentapi.API) {
	scope := agentapi.Scope(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = agentapi.ScopeAll
	}
	sessions, err := api.List(scope)
	writeErrOrObj(w, nonNil(sessions), err)
}

// handleSession 处理 /api/v1/sessions/{id}[...] 全部子路径。
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request, api *agentapi.API) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	id, sub, _ := strings.Cut(rest, "/")
	if id == "" {
		writeErr(w, http.StatusNotFound, "not_found", "缺少会话 id")
		return
	}
	switch sub {
	case "":
		if s, ok := api.Info(id); ok {
			writeJSON(w, map[string]agentapi.Session{"session": s})
		} else {
			writeErr(w, http.StatusNotFound, "not_found", "会话不存在: "+id)
		}
	case "messages":
		q := r.URL.Query()
		query := agentapi.MessageQuery{Since: parseI64(q.Get("since")), Tail: int(parseI64(q.Get("tail")))}
		msgs, err := api.Messages(id, query)
		writeErrOrObj(w, nonNil(msgs), err)
	case "output/last-full":
		msg, err := api.LastFullOutput(id)
		writeErrOrObj(w, msg, err)
	case "events":
		s.handleEvents(w, r, api, id)
	case "models", "model":
		if r.Method == http.MethodPost {
			var body struct {
				Model string `json:"model"`
			}
			if !readBody(w, r, &body) {
				return
			}
			writeErrOrOK(w, api.SetModel(id, body.Model))
			return
		}
		models, cur, err := api.Models(id)
		if err != nil {
			writeAPIErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"models": models, "current": cur})
	case "stop":
		writeErrOrOK(w, api.Stop(id))
	case "send":
		var body struct {
			Text string `json:"text"`
		}
		if !readBody(w, r, &body) {
			return
		}
		writeErrOrOK(w, api.Send(id, body.Text))
	case "permissions":
		writeJSON(w, nonNil(api.PendingPermissions(id)))
	default:
		if reqID, ok := strings.CutPrefix(sub, "permissions/"); ok && r.Method == http.MethodPost {
			var body struct {
				Allow bool `json:"allow"`
			}
			if !readBody(w, r, &body) {
				return
			}
			writeErrOrOK(w, api.GrantPermission(id, reqID, body.Allow))
			return
		}
		writeErr(w, http.StatusNotFound, "not_found", "未知子路径: "+sub)
	}
}

// handleEvents 以 NDJSON 行式流推送会话事件：订阅前先校验会话存在（否则
// 404，避免头已写出才知道找不到）；handler 退出（客户端断开）时取消订阅。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request, api *agentapi.API, id string) {
	if _, ok := api.Info(id); !ok {
		writeErr(w, http.StatusNotFound, "not_found", "会话不存在: "+id)
		return
	}
	ch, cancel := api.Watch(id, watchBuf)
	defer cancel()
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	// 立即推送握手帧并 flush：客户端 http 响应要等到首字节才返回，
	// 没有这帧「先订阅后取事件」的客户端会与服务端互相等待。
	_ = enc.Encode(agentapi.Event{SessionID: id, Kind: agentapi.EventOpen})
	flusher.Flush()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return // 订阅被取消（服务关闭等）
			}
			if err := enc.Encode(e); err != nil {
				return // 客户端断开
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// parseI64 解析 query 整数，非法返回 0。
func parseI64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// readBody 解码 JSON 请求体（限长 8MB，防恶意超大payload），非法时已写 400。
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "请求体 JSON 非法: "+err.Error())
		return false
	}
	return true
}

// writeJSON 序列化响应（nil 序列化为 null——调用方按形状处理）。
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeErrOrOK：nil 错误写 {"ok":true}，否则按错误映射写。
func writeErrOrOK(w http.ResponseWriter, err error) {
	if err != nil {
		writeAPIErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// writeErrOrObj：nil 错误写对象，否则按错误映射写。
func writeErrOrObj(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeAPIErr(w, err)
		return
	}
	writeJSON(w, v)
}

// 错误响应结构：{"error":{"code","message"}}。
type errBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeAPIErr 把 agentapi 哨兵错误映射为 HTTP 状态码与错误码。
func writeAPIErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentapi.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, agentapi.ErrLLMNotReady):
		writeErr(w, http.StatusServiceUnavailable, "llm_not_ready", err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]errBody{"error": {Code: code, Message: msg}})
}
