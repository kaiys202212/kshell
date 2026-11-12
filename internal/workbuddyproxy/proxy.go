// Package workbuddyproxy 进程内本地代理：把 Anthropic/OpenAI 两种协议的请求
// 经 workbuddy.ChatOnce（动态凭据）转发到 WorkBuddy 网关。agent 是 kshell 子
// 进程，主进程退出则子进程与代理一同终结，代理生命周期天然对齐，无需常驻。
package workbuddyproxy

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/workbuddy"
)

const (
	// maxBodyBytes 限制单次请求体上限，防止本地端口被滥用打满内存。
	maxBodyBytes = 32 << 20
	// credentialUnavailableMessage 是凭据不可用时对外的稳定文案：
	// 完整错误可能含本机路径/账号细节，只丢弃不回传。
	credentialUnavailableMessage = "err.workbuddy.credential_unavailable"
)

// chatOnceFunc 与 workbuddy.ChatOnce 同签名，抽成类型便于测试注入。
type chatOnceFunc func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error)

var (
	// 生产实现：每请求实时加载凭据并调用网关；测试可替换注入点。
	chatOnce       chatOnceFunc = workbuddy.ChatOnce
	loadCredential              = workbuddy.LoadCredential
)

// Server 是进程内单例代理实例；由调用方（desktop/TUI）持有并负责 Close。
type Server struct {
	mu      sync.Mutex
	srv     *http.Server
	ln      net.Listener
	base    string
	token   string
	started bool
}

// New 创建未启动实例；进程内单例由调用方持有。
func New() *Server {
	return &Server{}
}

// Ensure 幂等启动：首次起 listener（127.0.0.1:0）并生成随机 token；
// 后续调用返回同一 base/token。满足 modelresolve.Proxier。
func (s *Server) Ensure() (base, apiKey string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return s.base, s.token, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", err
	}
	token, err := randomToken()
	if err != nil {
		_ = ln.Close()
		return "", "", err
	}
	srv := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.srv = srv
	s.ln = ln
	s.base = "http://" + ln.Addr().String()
	s.token = token
	s.started = true
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("workbuddyproxy: serve exited: %v", err)
		}
		// listener 自然故障时重置状态，允许下次 Ensure 重启；
		// 若已被 Close/重新 Ensure 替换则不动当前实例。
		s.mu.Lock()
		if s.srv == srv {
			s.srv, s.ln = nil, nil
			s.base, s.token = "", ""
			s.started = false
		}
		s.mu.Unlock()
	}()
	return s.base, s.token, nil
}

// Close 关闭 listener 与 server（测试与 App 关闭时调用；幂等）。
func (s *Server) Close() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	srv, ln := s.srv, s.ln
	s.srv, s.ln = nil, nil
	s.base, s.token = "", ""
	s.started = false
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err != nil {
		// Shutdown 超时：放弃在途请求，强制断连以释放端口。
		_ = srv.Close()
	}
	_ = ln.Close()
	return err
}

// routes 注册代理路由；鉴权中间件对路径无关，Task 4 的 /v1/messages 可直接复用。
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.withAuth(s.handleChatCompletions))
	mux.HandleFunc("POST /v1/messages", s.withAuth(s.handleMessages))
	return mux
}

// withAuth 校验 Authorization: Bearer <token> 或 x-api-key: <token>。
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("x-api-key")
		if token == "" {
			token = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		}
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.currentToken())) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid api key", "authentication_error")
			return
		}
		next(w, r)
	}
}

// currentToken 加锁读取：Close 会清空 token，避免与在途请求竞态。
func (s *Server) currentToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// handleChatCompletions 做 OpenAI 协议直通：body 原样交给 ChatOnce，
// 聚合后的非流式 OpenAI 响应原样写回。
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large", "invalid_request_error")
			return
		}
		writeError(w, http.StatusBadRequest, "read request body: "+err.Error(), "invalid_request_error")
		return
	}
	cred, err := loadCredential()
	if err != nil {
		// 凭据缺失/解密失败对调用方等同鉴权失败；完整错误可能含本机路径，
		// 只丢弃不回传，仅返回稳定文案。
		writeError(w, http.StatusUnauthorized, credentialUnavailableMessage, "authentication_error")
		return
	}
	out, err := chatOnce(r.Context(), cred, body, nil)
	if err != nil {
		status, typ := http.StatusBadGateway, "workbuddy_error"
		if strings.Contains(err.Error(), "token_expired") {
			status, typ = http.StatusUnauthorized, "authentication_error"
		}
		writeError(w, status, err.Error(), typ)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// handleMessages 接 Anthropic Messages 协议：请求翻译成 OpenAI 形状后复用
// ChatOnce，响应再翻译回 Anthropic；stream:true 时整段合成 SSE 事件流。
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAnthropicError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large")
			return
		}
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "read request body: "+err.Error())
		return
	}

	// 先取原始请求的 model/stream：翻译后的 OpenAI body 不再保留 stream。
	var meta struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.Unmarshal(body, &meta)

	openaiBody, err := anthropicToOpenAI(body)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	cred, err := loadCredential()
	if err != nil {
		// 与 /v1/chat/completions 一致：凭据问题对外等同鉴权失败，
		// 完整错误可能含本机路径，只丢弃不回传。
		writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", credentialUnavailableMessage)
		return
	}
	out, err := chatOnce(r.Context(), cred, openaiBody, nil)
	if err != nil {
		if strings.Contains(err.Error(), "token_expired") {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", err.Error())
		} else {
			writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		}
		return
	}

	if meta.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, synthAnthropicSSE(out, meta.Model))
		return
	}
	payload, err := json.Marshal(openAIToAnthropic(out, meta.Model))
	if err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

// writeAnthropicError 输出 Anthropic 错误信封（注意与 OpenAI 的 writeError 形状不同），
// Anthropic SDK 依赖 type:"error" 与 error.type 做错误分类。
func writeAnthropicError(w http.ResponseWriter, status int, typ, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": typ, "message": message},
	})
}

// writeError 统一输出 OpenAI 错误信封，客户端可按 error.message 展示。
func writeError(w http.ResponseWriter, status int, message, typ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": message, "type": typ},
	})
}

// randomToken 生成 32 位 hex 随机 token（16 字节熵），仅存活于进程与本机回环。
func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
