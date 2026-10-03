package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/yangk/kshell/internal/acp"
)

var (
	errNoBackend = errors.New("chat: 后端未装配")
	errNotFound  = errors.New("chat: 会话不存在或已关闭")
	errRunning   = errors.New("chat: 上一轮尚未结束")
)

// maxHistory 限制每会话保留的时间线事件数，防止长会话内存无限增长。
const maxHistory = 500

type session struct {
	id        string
	key       string
	info      Info
	conn      Conn
	sessionID string
	handler   *sessionHandler

	ctx    context.Context
	cancel context.CancelFunc

	seq     int64
	history []Update
	tools   map[string]*ToolCall

	autoSeq  int    // 合成 messageId 的轮次计数（分片缺 messageId 时用）
	lastType string // 上一事件类型；用于判断连续同类分片

	pending map[string]chan permissionResult
	exited  bool
}

type permissionResult struct {
	outcome string
	option  string
}

type sessionHandler struct {
	m  *Manager
	id string
}

type Manager struct {
	backend      Backend
	onUpdate     func(id string, u Update)
	onPermission func(id string, r PermissionRequest)
	onExit       func(id string, code int, errMsg string)

	mu      sync.Mutex
	byKey   map[string]*session
	byID    map[string]*session
	order   []*session
	nextSeq int
}

func NewManager(b Backend,
	onUpdate func(id string, u Update),
	onPermission func(id string, r PermissionRequest),
	onExit func(id string, code int, errMsg string)) *Manager {
	return &Manager{
		backend: b, onUpdate: onUpdate, onPermission: onPermission, onExit: onExit,
		byKey: make(map[string]*session), byID: make(map[string]*session),
	}
}

// Open 起进程并完成 initialize + session/new|load；sessionID 非空则 load。
func (m *Manager) Open(key string, info Info, spec Spec, sessionID string) (Info, error) {
	if m.backend == nil {
		return Info{}, errNoBackend
	}
	if key == "" {
		return Info{}, errors.New("chat: key 不能为空")
	}

	// 已退出的旧会话：先整体摘除再重建，避免 byID/order 泄漏。
	m.mu.Lock()
	old, ok := m.byKey[key]
	if ok && !old.exited {
		out := old.info
		m.mu.Unlock()
		return out, nil
	}
	m.mu.Unlock()
	if ok {
		m.remove(old)
		if old.cancel != nil {
			old.cancel()
		}
		if old.conn != nil {
			go old.conn.Close()
		}
	}

	m.mu.Lock()
	m.nextSeq++
	s := &session{
		id:      fmt.Sprintf("c%d", m.nextSeq),
		key:     key,
		info:    info,
		pending: make(map[string]chan permissionResult),
		tools:   make(map[string]*ToolCall),
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.handler = &sessionHandler{m: m, id: s.id}
	s.info.ID = s.id
	s.info.Status = StatusStarting
	m.byKey[key] = s
	m.byID[s.id] = s
	m.order = append(m.order, s)
	m.mu.Unlock()

	conn, err := m.backend.Start(spec, s.handler)
	if err != nil {
		m.fail(s.id, 0, err.Error())
		return Info{}, err
	}

	// conn/sessionID 在锁内发布，避免 watchExit 等 goroutine 读取到未同步的值。
	m.mu.Lock()
	if cur := m.byID[s.id]; cur != s {
		m.mu.Unlock()
		_ = conn.Close()
		return Info{}, errNotFound
	}
	s.conn = conn
	m.mu.Unlock()

	init, err := conn.Initialize(s.ctx)
	if err != nil {
		_ = conn.Close()
		m.fail(s.id, 0, err.Error())
		return Info{}, err
	}
	// 协议版本不符时拒绝继续，避免按错误语义解读消息。
	if init.ProtocolVersion != 1 {
		msg := fmt.Sprintf("acp: 不支持的协议版本 %d", init.ProtocolVersion)
		_ = conn.Close()
		m.fail(s.id, 0, msg)
		return Info{}, errors.New(msg)
	}

	if sessionID != "" {
		// load 前先确认对方声明了该能力，否则会拿到无意义的 RPC 错误。
		if !init.AgentCapabilities.LoadSession {
			msg := "acp: 该 agent 不支持 session/load"
			_ = conn.Close()
			m.fail(s.id, 0, msg)
			return Info{}, errors.New(msg)
		}
		if err := conn.LoadSession(s.ctx, sessionID, info.Workspace); err != nil {
			_ = conn.Close()
			m.fail(s.id, 0, err.Error())
			return Info{}, err
		}
	} else {
		sid, err := conn.NewSession(s.ctx, info.Workspace)
		if err != nil {
			_ = conn.Close()
			m.fail(s.id, 0, err.Error())
			return Info{}, err
		}
		sessionID = sid
	}

	m.mu.Lock()
	s.sessionID = sessionID
	s.info.SessionID = sessionID
	s.info.Status = StatusReady
	out := s.info
	m.mu.Unlock()

	go m.watchExit(s)
	return out, nil
}

// remove 从三张表中摘除会话并取消其挂起权限；连接由调用方在锁外关闭。
func (m *Manager) remove(s *session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byKey, s.key)
	delete(m.byID, s.id)
	for i, cur := range m.order {
		if cur == s {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	cancelPending(pending)
}

// cancelPending 非阻塞地让所有等待中的权限请求以 cancelled 结束。
func cancelPending(pending map[string]chan permissionResult) {
	for _, ch := range pending {
		select {
		case ch <- permissionResult{outcome: "cancelled"}:
		default:
		}
	}
}

// watchExit 等进程退出：若未主动关闭则标记退出、取消 ctx 与挂起权限，并回调 onExit。
func (m *Manager) watchExit(s *session) {
	code, err := s.conn.Wait()
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	m.mu.Lock()
	if s.exited {
		m.mu.Unlock()
		return
	}
	s.exited = true
	s.info.Status = StatusExited
	s.info.ExitCode = code
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	m.mu.Unlock()

	// 进程已消失，等待中的权限请求不可能再被回答，全部以 cancelled 收尾。
	cancelPending(pending)
	if s.cancel != nil {
		s.cancel()
	}
	if m.onExit != nil {
		m.onExit(s.id, code, errMsg)
	}
}

func (m *Manager) fail(id string, code int, msg string) {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return
	}
	s.exited = true
	s.info.Status = StatusExited
	s.info.ExitCode = code
	s.info.Error = msg
	m.mu.Unlock()

	// 握手/启动失败不留下幽灵会话。这里不回调 onExit：失败会话的 id 从未返回给
	// 前端（Open 直接报错），推 chat:exit 前端也无从对应，属无效事件。
	m.remove(s)
	if s.cancel != nil {
		s.cancel()
	}
}

func (m *Manager) Prompt(id, text string) error {
	m.mu.Lock()
	s := m.byID[id]
	// 仅握手完成的 ready 会话可发起新轮次，避免握手期调用 conn.Prompt("")。
	if s == nil || s.exited {
		m.mu.Unlock()
		return errNotFound
	}
	if s.info.Status != StatusReady {
		m.mu.Unlock()
		return errRunning
	}
	s.info.Status = StatusRunning
	sid := s.sessionID
	conn := s.conn
	ctx := s.ctx
	m.mu.Unlock()

	go func() {
		stop, err := conn.Prompt(ctx, sid, text)
		m.mu.Lock()
		if cur := m.byID[id]; cur != s || s.exited {
			m.mu.Unlock()
			return
		}
		s.info.Status = StatusReady
		s.seq++
		u := Update{Seq: s.seq, Type: "turn_done", StopReason: stop}
		if err != nil {
			u.Type = "error"
			u.Text = err.Error()
			s.info.Error = err.Error()
		}
		s.history = append(s.history, u)
		if len(s.history) > maxHistory {
			s.history = append([]Update(nil), s.history[len(s.history)-maxHistory:]...)
		}
		m.mu.Unlock()
		if m.onUpdate != nil {
			m.onUpdate(id, u)
		}
	}()
	return nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	sid := s.sessionID
	conn := s.conn
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	m.mu.Unlock()

	cancelPending(pending)
	if sid != "" && conn != nil {
		_ = conn.Cancel(sid)
	}
	return nil
}

func (m *Manager) RespondPermission(id, requestID, optionID string) error {
	return m.resolvePermission(id, requestID, permissionResult{outcome: "selected", option: optionID})
}

func (m *Manager) CancelPermission(id, requestID string) error {
	return m.resolvePermission(id, requestID, permissionResult{outcome: "cancelled"})
}

func (m *Manager) resolvePermission(id, requestID string, r permissionResult) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	ch := s.pending[requestID]
	delete(s.pending, requestID)
	m.mu.Unlock()
	if ch == nil {
		return errNotFound
	}
	ch <- r
	return nil
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return nil
	}
	s.exited = true
	s.info.Status = StatusExited
	if s.cancel != nil {
		s.cancel()
	}
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	conn := s.conn
	m.mu.Unlock()

	cancelPending(pending)
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*session, len(m.order))
	copy(all, m.order)
	m.mu.Unlock()
	for _, s := range all {
		_ = m.Close(s.id)
	}
}

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.order))
	for _, s := range m.order {
		out = append(out, s.info)
	}
	return out
}

func (m *Manager) History(id string) []Update {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return nil
	}
	out := make([]Update, len(s.history))
	copy(out, s.history)
	return out
}

func (h *sessionHandler) SessionUpdate(sessionID string, raw json.RawMessage) {
	h.m.onSessionUpdate(h.id, sessionID, raw)
}

func (h *sessionHandler) RequestPermission(ctx context.Context, sessionID, requestID string, p acp.RequestPermissionParams) (acp.PermissionOutcome, error) {
	return h.m.awaitPermission(ctx, h.id, sessionID, requestID, p)
}

// acpUpdate 按 ACP 规范的小写字段解码 session/update（前端模型是 PascalCase，故单独解码再转换）。
type acpUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	MessageID     string          `json:"messageId"`
	Content       json.RawMessage `json:"content"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	RawInput      json.RawMessage `json:"rawInput"`
	RawOutput     json.RawMessage `json:"rawOutput"`
	Entries       []acpPlanEntry  `json:"entries"`
}

type acpPlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

// normalizeUpdate 把 ACP session/update 转成前端时间线事件；不支持的变体返回 ok=false。
func normalizeUpdate(raw json.RawMessage) (Update, bool) {
	var u acpUpdate
	if err := json.Unmarshal(raw, &u); err != nil {
		return Update{}, false
	}
	switch u.SessionUpdate {
	case "user_message_chunk":
		return Update{Type: "user", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "agent_message_chunk":
		return Update{Type: "assistant", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "agent_thought_chunk":
		return Update{Type: "thought", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "tool_call", "tool_call_update":
		if u.ToolCallID == "" {
			return Update{}, false
		}
		var probe struct {
			Content []json.RawMessage `json:"content"`
		}
		var content []json.RawMessage
		if err := json.Unmarshal(raw, &probe); err == nil {
			content = probe.Content
		}
		tc := ToolCall{
			ToolCallID: u.ToolCallID, Name: u.Name, Title: u.Title,
			Kind: u.Kind, Status: u.Status, Content: content,
			RawInput: u.RawInput, RawOutput: u.RawOutput,
		}
		return Update{Type: "tool", ToolCallID: tc.ToolCallID, Tool: &tc}, true
	case "plan":
		plan := make([]PlanEntry, 0, len(u.Entries))
		for _, e := range u.Entries {
			plan = append(plan, PlanEntry{Content: e.Content, Priority: e.Priority, Status: e.Status})
		}
		return Update{Type: "plan", Plan: plan}, true
	default:
		return Update{}, false
	}
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var block struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &block); err == nil && block.Text != "" {
		return block.Text
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func (m *Manager) onSessionUpdate(id, sessionID string, raw json.RawMessage) {
	u, ok := normalizeUpdate(raw)
	if !ok {
		return
	}
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return
	}
	// tool_call_update 常是稀疏增量，需与已有快照合并后再发前端。
	if u.Type == "tool" && u.Tool != nil {
		merged := mergeToolCall(s.tools[u.ToolCallID], u.Tool)
		s.tools[u.ToolCallID] = merged
		u.Tool = merged
		u.ToolCallID = merged.ToolCallID
	}
	// 文本类分片常缺 messageId：按连续同类分片合成 key，供前端归并同一消息。
	if u.Type == "user" || u.Type == "assistant" || u.Type == "thought" {
		if u.MessageID == "" {
			if s.lastType != u.Type {
				s.autoSeq++
			}
			u.MessageID = fmt.Sprintf("auto:%s:%d", u.Type, s.autoSeq)
		}
		s.lastType = u.Type
	} else {
		s.lastType = "" // 工具/计划/结束事件打断连续分片
	}
	s.seq++
	u.Seq = s.seq
	s.history = append(s.history, u)
	if len(s.history) > maxHistory {
		s.history = append([]Update(nil), s.history[len(s.history)-maxHistory:]...)
	}
	m.mu.Unlock()
	if m.onUpdate != nil {
		m.onUpdate(id, u)
	}
}

// mergeToolCall 保留 prev 的非空字段，用 next 的非空字段覆盖；Content 为空时沿用 prev。
func mergeToolCall(prev, next *ToolCall) *ToolCall {
	if prev == nil {
		return next
	}
	out := *prev
	if next.Name != "" {
		out.Name = next.Name
	}
	if next.Title != "" {
		out.Title = next.Title
	}
	if next.Kind != "" {
		out.Kind = next.Kind
	}
	if next.Status != "" {
		out.Status = next.Status
	}
	if len(next.Content) > 0 {
		out.Content = next.Content
	}
	if len(next.RawInput) > 0 {
		out.RawInput = next.RawInput
	}
	if len(next.RawOutput) > 0 {
		out.RawOutput = next.RawOutput
	}
	return &out
}

func (m *Manager) awaitPermission(ctx context.Context, id, sessionID, requestID string, p acp.RequestPermissionParams) (acp.PermissionOutcome, error) {
	ch := make(chan permissionResult, 1)
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return acp.PermissionOutcome{Outcome: "cancelled"}, nil
	}
	s.pending[requestID] = ch
	m.mu.Unlock()

	req := PermissionRequest{RequestID: requestID, SessionID: sessionID, ToolCall: toToolCall(p.ToolCall)}
	for _, o := range p.Options {
		req.Options = append(req.Options, PermissionOption{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}
	if m.onPermission != nil {
		m.onPermission(id, req)
	}

	select {
	case r := <-ch:
		if r.outcome == "cancelled" {
			return acp.PermissionOutcome{Outcome: "cancelled"}, nil
		}
		return acp.PermissionOutcome{Outcome: "selected", OptionID: r.option}, nil
	case <-ctx.Done():
		return acp.PermissionOutcome{Outcome: "cancelled"}, nil
	}
}

func toToolCall(t acp.ToolCall) ToolCall {
	return ToolCall{
		ToolCallID: t.ToolCallID,
		Name:       t.Name,
		Title:      t.Title,
		Kind:       t.Kind,
		Status:     t.Status,
		Content:    t.Content,
		RawInput:   t.RawInput,
		RawOutput:  t.RawOutput,
	}
}
