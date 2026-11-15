// Package agentcli 是 kshell agent CLI 的薄客户端与命令分发：经本地 HTTP
// （agentsrv）调用桌面端进程内的 agentapi。桌面端未运行时全部命令按
// ErrNotReachable → exit 3 提示。
package agentcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/agentapi"
)

// ErrNotReachable 是「桌面端未运行/服务不可达」哨兵：CLI 层映射 exit 3。
var ErrNotReachable = errors.New("agentcli: 桌面端服务不可达")

// httpTimeout 是普通请求的超时；watch 流不受此限制（长连接）。
const httpTimeout = 30 * time.Second

// Client 是 agentsrv 的 HTTP 客户端。
type Client struct {
	base  string
	token string
	hc    *http.Client
	// App 是 endpoint.json 里携带的桌面端应用版本（空 = 旧桌面端未写）。
	// 由 Connect 填充，供版本自检比对；测试可直接注入。
	App string
}

// NewClient 构造客户端。
func NewClient(baseURL, token string) *Client {
	return &Client{base: strings.TrimSuffix(baseURL, "/"), token: token, hc: &http.Client{Timeout: httpTimeout}}
}

// APIError 是服务端结构化错误。
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

// IsNotFound / IsLLMNotReady 供调用方按哨兵分支。
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == "not_found"
}

func IsLLMNotReady(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == "llm_not_ready"
}

// IsNotReachable 报告是否为服务不可达类错误。
func IsNotReachable(err error) bool { return errors.Is(err, ErrNotReachable) }

// do 发请求并解析响应：2xx 解码到 out（nil 不解析）；非 2xx 解码错误结构，
// not_found/llm_not_ready 包装回 agentapi 哨兵；网络层错误包 ErrNotReachable。
func (c *Client) do(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotReachable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return decodeAPIError(resp.StatusCode, data)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// sentinelErr 让错误同时满足 errors.Is(agentapi 哨兵) 与 errors.As(*APIError)；
// 显示文本只取服务端干净消息——服务端消息本身已含哨兵语义（如「会话不存在: <id>」），
// 再叠加哨兵 Error() 会输出「agentapi: 会话不存在: agentapi: 会话不存在: …」双层前缀。
type sentinelErr struct {
	sentinel error
	apiErr   *APIError
}

func (e *sentinelErr) Error() string { return e.apiErr.Message }

// Unwrap 返回两个分支：errors.Is 走哨兵，errors.As 走 APIError。
func (e *sentinelErr) Unwrap() []error { return []error{e.sentinel, e.apiErr} }

func decodeAPIError(status int, data []byte) error {
	var wrap struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := strings.TrimSpace(string(data))
	code := "internal"
	if json.Unmarshal(data, &wrap) == nil && wrap.Error.Code != "" {
		code = wrap.Error.Code
		msg = wrap.Error.Message
	}
	apiErr := &APIError{Status: status, Code: code, Message: msg}
	switch code {
	case "not_found":
		return &sentinelErr{sentinel: agentapi.ErrNotFound, apiErr: apiErr}
	case "llm_not_ready":
		return &sentinelErr{sentinel: agentapi.ErrLLMNotReady, apiErr: apiErr}
	}
	return apiErr
}

// get / post 是 do 的薄包装。
func (c *Client) get(path string, out any) error { return c.do(http.MethodGet, path, nil, out) }
func (c *Client) post(path string, body, out any) error {
	return c.do(http.MethodPost, path, body, out)
}

// —— 工具面方法（与 agentapi.API 一一对应）——

// List 按范围列会话（scope 空 = all）。
func (c *Client) List(scope string) ([]agentapi.Session, error) {
	if scope == "" {
		scope = "all"
	}
	var out []agentapi.Session
	err := c.get("/api/v1/sessions?scope="+scope, &out)
	return out, err
}

// Search 关键字搜索。
func (c *Client) Search(kw string) ([]agentapi.Session, error) {
	var out []agentapi.Session
	err := c.get("/api/v1/sessions/search?q="+url.QueryEscape(kw), &out)
	return out, err
}

// Info 会话详情；不存在返回 ok=false。
func (c *Client) Info(id string) (agentapi.Session, bool, error) {
	var wrap struct {
		Session agentapi.Session `json:"session"`
	}
	if err := c.get("/api/v1/sessions/"+url.QueryEscape(id), &wrap); err != nil {
		if IsNotFound(err) {
			return agentapi.Session{}, false, nil
		}
		return agentapi.Session{}, false, err
	}
	return wrap.Session, true, nil
}

// Messages 输出拉取。
func (c *Client) Messages(id string, tail int, since int64) ([]agentapi.Message, error) {
	var out []agentapi.Message
	err := c.get(fmt.Sprintf("/api/v1/sessions/%s/messages?tail=%d&since=%d", url.QueryEscape(id), tail, since), &out)
	return out, err
}

// LastFullOutput 最近一轮完整 agent 回复。
func (c *Client) LastFullOutput(id string) (agentapi.Message, error) {
	var out agentapi.Message
	err := c.get("/api/v1/sessions/"+url.QueryEscape(id)+"/output/last-full", &out)
	return out, err
}

// Watch 订阅事件流：服务端每行一个 Event JSON（首帧为 open 握手）。返回的
// 通道在连接断开/EOF 时关闭；cancel 主动断开。连接失败返回错误。
func (c *Client) Watch(id string) (<-chan agentapi.Event, func(), error) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/api/v1/sessions/"+url.QueryEscape(id)+"/events", nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	// 流式请求不设超时（hc.Timeout 会对整个响应体生效，会掐断长流）。
	hc := &http.Client{}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrNotReachable, err)
	}
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		return nil, nil, decodeAPIError(resp.StatusCode, data)
	}
	ch := make(chan agentapi.Event, 32)
	done := make(chan struct{})
	var closeOnce sync.Once
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for scanner.Scan() {
			var e agentapi.Event
			if json.Unmarshal(scanner.Bytes(), &e) != nil {
				continue // 容忍坏行
			}
			select {
			case ch <- e:
			case <-done:
				return
			}
		}
	}()
	// cancel 必须同时 close(done)：读协程可能阻塞在 ch<-e（消费方已停），仅关
	// body 解不了该阻塞，会泄漏协程。
	cancel := func() {
		closeOnce.Do(func() {
			close(done)
			_ = resp.Body.Close()
		})
	}
	return ch, cancel, nil
}

// Send 发指令。
func (c *Client) Send(id, text string) error {
	return c.post("/api/v1/sessions/"+url.QueryEscape(id)+"/send", map[string]string{"text": text}, nil)
}

// Stop 中断当前轮次。
func (c *Client) Stop(id string) error {
	return c.post("/api/v1/sessions/"+url.QueryEscape(id)+"/stop", map[string]any{}, nil)
}

// Recall 撤回一条用户消息。
func (c *Client) Recall(msgID int64) error {
	return c.post("/api/v1/recall", map[string]int64{"msgID": msgID}, nil)
}

// Models 可选模型与当前模型。
func (c *Client) Models(id string) ([]agentapi.ModelOption, string, error) {
	var out struct {
		Models  []agentapi.ModelOption `json:"models"`
		Current string                 `json:"current"`
	}
	if err := c.get("/api/v1/sessions/"+url.QueryEscape(id)+"/models", &out); err != nil {
		return nil, "", err
	}
	return out.Models, out.Current, nil
}

// SetModel 切换模型。
func (c *Client) SetModel(id, model string) error {
	return c.post("/api/v1/sessions/"+url.QueryEscape(id)+"/model", map[string]string{"model": model}, nil)
}

// PendingPermissions 未处理权限请求。
func (c *Client) PendingPermissions(id string) ([]agentapi.Permission, error) {
	var out []agentapi.Permission
	err := c.get("/api/v1/sessions/"+url.QueryEscape(id)+"/permissions", &out)
	return out, err
}

// GrantPermission 处理权限请求。
func (c *Client) GrantPermission(id, reqID string, allow bool) error {
	return c.post("/api/v1/sessions/"+url.QueryEscape(id)+"/permissions/"+url.QueryEscape(reqID), map[string]bool{"allow": allow}, nil)
}

// Create 结构化新建会话。
func (c *Client) Create(tool, ws, model string) (agentapi.Session, error) {
	var out agentapi.Session
	err := c.post("/api/v1/sessions/create", map[string]string{"tool": tool, "ws": ws, "model": model}, &out)
	return out, err
}

// Open 激活磁盘会话。
func (c *Client) Open(sessionID string) (agentapi.Session, error) {
	var out agentapi.Session
	err := c.post("/api/v1/sessions/open", map[string]string{"sessionID": sessionID}, &out)
	return out, err
}

// CreateNL 自然语言创建（confirm 非 nil 时走确认路径，跳过 LLM）。
func (c *Client) CreateNL(ctx context.Context, text string, confirm *agentapi.CreatePlan) (agentapi.CreateResult, error) {
	var out agentapi.CreateResult
	err := c.post("/api/v1/sessions/create-nl", map[string]any{"text": text, "confirm": confirm}, &out)
	return out, err
}

// RouteDecide 投递决策。
func (c *Client) RouteDecide(ctx context.Context, text string) (agentapi.RoutePlan, error) {
	var out agentapi.RoutePlan
	err := c.post("/api/v1/route/decide", map[string]string{"text": text}, &out)
	return out, err
}

// RouteSend 按决策投递（decision + sessions 原样回传）。
func (c *Client) RouteSend(plan agentapi.RoutePlan) error {
	return c.post("/api/v1/route/send", map[string]any{"decision": plan.RouteDecision, "sessions": plan.Sessions}, nil)
}
