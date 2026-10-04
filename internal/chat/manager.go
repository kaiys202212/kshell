package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/discovery"
)

// attachCreatedGrace 打开新建聊天后，仍可能绑上稍早落盘的新会话（agent 写盘略早于 Open）。
const attachCreatedGrace = 2 * time.Minute

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

	openedAt time.Time
	knownIDs map[string]bool // 打开时扫描结果里已有的磁盘会话，禁止绑到本新建聊天
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
	autoAllow    func() bool // bypass：自动选 allow 类 option，不弹窗

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

// SetAutoAllowPermission 设置权限自动放行回调（nil=不自动放行）。
func (m *Manager) SetAutoAllowPermission(fn func() bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoAllow = fn
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
		id:       fmt.Sprintf("c%d", m.nextSeq),
		key:      key,
		info:     info,
		pending:  make(map[string]chan permissionResult),
		tools:    make(map[string]*ToolCall),
		openedAt: time.Now(),
		knownIDs: map[string]bool{},
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.handler = &sessionHandler{m: m, id: s.id}
	s.info.ID = s.id
	s.info.Status = StatusStarting
	s.knownIDs = map[string]bool{}
	for _, sid := range info.KnownSessionIDs {
		if sid != "" {
			s.knownIDs[sid] = true
		}
	}
	s.info.KnownSessionIDs = nil
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
	// Info.SessionID 统一表示「绑定的磁盘会话 ID」：恢复型聊天传入的就是磁盘 ID，保持不变；
	// 新建聊天协议层拿到的是 ACP 内部 ID（与磁盘会话 ID 不同体系），Info 留空等扫描回填。
	if info.SessionID == "" && info.Kind == KindSession {
		s.info.SessionID = sessionID
	}
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
	// 客户端主动发出的用户消息要立刻进时间线：ACP 实时轮次不回显用户消息
	//（只有 session/load 重放才发 user_message_chunk），不本地补一条界面会一直是空的。
	s.seq++
	userUpdate := Update{Seq: s.seq, Type: "user", MessageID: fmt.Sprintf("user:%d", s.seq), Text: text}
	s.history = append(s.history, userUpdate)
	if len(s.history) > maxHistory {
		s.history = append([]Update(nil), s.history[len(s.history)-maxHistory:]...)
	}
	sid := s.sessionID
	conn := s.conn
	ctx := s.ctx
	m.mu.Unlock()

	if m.onUpdate != nil {
		m.onUpdate(id, userUpdate)
	}

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

// Close 结束聊天并从 List 摘除。幂等。
// 必须摘除：前端关闭页签后会在 scan:done 时用 ListChats 整表重建镜像，
// 若只标 exited 仍留在 List，已关页签会被「复活」。自然退出仍保留在 List
//（供前端显示退出态），直到用户点关闭才走本方法。
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
	// 先关连接再摘除：watchExit 看到 exited 后直接返回，不会误推 onExit
	var closeErr error
	if conn != nil {
		closeErr = conn.Close()
	}
	m.remove(s)
	return closeErr
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

// RememberKnownIDs 记下打开本聊天时已存在的磁盘会话 ID，扫描回填时不得把它们绑上来。
func (m *Manager) RememberKnownIDs(id string, ids []string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return
	}
	known := make(map[string]bool, len(ids))
	for _, sid := range ids {
		if sid != "" {
			known[sid] = true
		}
	}
	s.knownIDs = known
}

// AttachSession 把扫描发现的磁盘会话绑定到匹配的新建聊天上：
// 新建（KindNew）聊天启动时磁盘上还没有会话记录，Info.SessionID 为空、标题是占位文案；
// 扫描发现新会话后由 desktop 层调用本方法回填，页签标题与前端「恢复/切换」判断都依赖它。
// 只绑未退出、未绑定（Info.SessionID 为空）的新建聊天；工作区路径归一化后比较，
// toolID 与聊天 ToolID 不一致时不绑（ToolID 为空表示由 launch 选首选，允许绑定）。
// 打开时已知的会话 ID、以及 CreatedAt 明显早于打开时刻的旧对话一律跳过，避免页签套用左侧列表标题。
// 绑定 SessionID 后，仅当 messages>0 且 title 非空才改页签标题（用户已发言）。
// 只改 Info：协议层继续用 agent 返回的内部 sessionID，Prompt/Cancel 不受影响。
func (m *Manager) AttachSession(sessionID, workspace, toolID, title string, messages int, createdAt time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := discovery.NormalizePath(workspace)
	for _, s := range m.order {
		info := &s.info
		if s.exited || info.Kind != KindNew || info.SessionID != "" {
			continue
		}
		if s.knownIDs[sessionID] {
			continue
		}
		if !createdAt.IsZero() && createdAt.Before(s.openedAt.Add(-attachCreatedGrace)) {
			continue
		}
		if discovery.NormalizePath(info.Workspace) != ws {
			continue
		}
		if info.ToolID != "" && toolID != "" && info.ToolID != toolID {
			continue
		}
		info.SessionID = sessionID
		if messages > 0 && title != "" {
			info.Title = title
		}
		return true
	}
	return false
}

// UpdateSessionTitle 按磁盘会话 ID 更新已绑定聊天的标题。
// 必须已有用户消息（messages>0）且标题非空；用于扫描后把后来落盘的真实标题刷到页签。
func (m *Manager) UpdateSessionTitle(sessionID, title string, messages int) bool {
	if sessionID == "" || title == "" || messages <= 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.order {
		if s.info.SessionID != sessionID || s.info.Title == title {
			continue
		}
		s.info.Title = title
		return true
	}
	return false
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
	if optID, ok := pickAllowOption(p.Options); ok {
		m.mu.Lock()
		fn := m.autoAllow
		m.mu.Unlock()
		if fn != nil && fn() {
			return acp.PermissionOutcome{Outcome: "selected", OptionID: optID}, nil
		}
	}

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

// pickAllowOption 按 kind 优先选 allow_once → allow_always → allow（含子串匹配）。
func pickAllowOption(opts []acp.PermissionOption) (string, bool) {
	priority := []string{"allow_once", "allow_always", "allow"}
	for _, kind := range priority {
		for _, o := range opts {
			if o.Kind == kind || o.OptionID == kind {
				return o.OptionID, true
			}
		}
	}
	for _, o := range opts {
		k := strings.ToLower(o.Kind + " " + o.OptionID)
		if strings.Contains(k, "allow") && !strings.Contains(k, "deny") {
			return o.OptionID, true
		}
	}
	return "", false
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
