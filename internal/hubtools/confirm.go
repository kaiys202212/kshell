package hubtools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 哨兵错误：调用方用 errors.Is 判别确认状态机的失败形态。
var (
	ErrUnknownPending = errors.New("err.hubtools.unknown_pending")
	ErrPendingExpired = errors.New("err.hubtools.pending_expired")
)

// confirmTTL 是挂起写操作的有效期：超时未确认自动作废，避免陈旧操作被误放行。
const confirmTTL = 5 * time.Minute

// pendingOp 是一条挂起的写操作：确认时按 name+args 原样重放执行。
type pendingOp struct {
	name      string
	argsJSON  string
	summary   string
	createdAt time.Time
}

// Session 承载一轮 hubtools 对话的多轮状态：LLM 消息历史 + 待确认写操作。
// hub 消息处理与用户确认点击可能并发访问，内部用 mutex 保护。
type Session struct {
	cfg  Config
	reg  *Registry
	exec Executor // 可为 nil：不参与 LLM 循环，仅做工具挂起/确认

	mu       sync.Mutex
	pending  map[string]*pendingOp
	messages []Message
	now      func() time.Time // 测试可拨时钟
}

// NewSession 创建会话容器。exec 是注入 llm.go 工具循环的执行器形态，
// 传 nil 时 Session 仍可独立完成 Execute/Confirm 状态机职责。
func NewSession(cfg Config, reg *Registry, exec Executor) *Session {
	return &Session{
		cfg:     cfg,
		reg:     reg,
		exec:    exec,
		pending: make(map[string]*pendingOp),
		now:     time.Now,
	}
}

// newPendingID 生成 16 字节随机 hex，作挂起操作标识。
func newPendingID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败极罕见，退化为时间戳仍保唯一性
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Execute 处理一次工具调用请求（由 LLM 工具循环驱动时使用）：
// Mutating 工具不执行，生成 pendingID 存入挂起表并返回 *PendingConfirm
// （带 PendingID），上层拿去构造确认卡；只读工具直接执行返回结果文本。
func (s *Session) Execute(ctx context.Context, name, argsJSON string) (string, *PendingConfirm, error) {
	result, err := s.reg.Execute(ctx, name, argsJSON)
	if err == nil {
		return result, nil, nil
	}
	var pc *PendingConfirm
	if !errors.As(err, &pc) {
		return "", nil, err
	}
	// Mutating 挂起：登记进 pending 表等用户确认
	id := newPendingID()
	s.mu.Lock()
	s.pending[id] = &pendingOp{
		name:      pc.ToolName,
		argsJSON:  pc.ArgsJSON,
		summary:   pc.Summary,
		createdAt: s.now(),
	}
	s.mu.Unlock()
	pc.PendingID = id
	return "", pc, nil
}

// Confirm 确认或拒绝一个挂起操作。
// approved=true：真正执行工具（走 Registry 直接执行通道，绕过 Mutating 挂起）；
// approved=false：返回固定拒绝文本（喂回模型），不执行。
// pendingID 未知 → ErrUnknownPending；超 confirmTTL → ErrPendingExpired 并删除。
func (s *Session) Confirm(ctx context.Context, pendingID string, approved bool) (string, error) {
	s.mu.Lock()
	op, ok := s.pending[pendingID]
	if !ok {
		s.mu.Unlock()
		return "", ErrUnknownPending
	}
	if s.now().Sub(op.createdAt) > confirmTTL {
		delete(s.pending, pendingID)
		s.mu.Unlock()
		return "", ErrPendingExpired
	}
	// 先摘除再执行：同一 pendingID 无论成败都只允许消费一次
	delete(s.pending, pendingID)
	s.mu.Unlock()

	if !approved {
		return "用户拒绝了该操作，请勿重试，改用其他方式回应。", nil
	}
	argsJSON := op.argsJSON
	if op.name == "update_requirement" {
		merged, err := mergeUpdateArgs(s.reg.b, argsJSON)
		if err != nil {
			return "", fmt.Errorf("合并 update 参数: %w", err)
		}
		argsJSON = merged
	}
	return s.reg.ExecuteConfirmed(ctx, op.name, argsJSON)
}

// AppendMessage 追加一条 LLM 对话消息（多轮历史）。
func (s *Session) AppendMessage(m Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, m)
}

// History 返回消息历史拷贝，避免调用方并发修改内部切片。
func (s *Session) History() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// mergeUpdateArgs 钉死 update_requirement 的可选参数语义：
// 未提供的可选参数 = 保持当前值（先 GetRequirement 取现状回填），
// 显式空（空串 / 空数组）= 清空。tags 以「argsJSON 里是否存在 key」区分。
// 返回合并后的 argsJSON，供确认执行时原样重放。
func mergeUpdateArgs(b Bridge, argsJSON string) (string, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("参数不是合法 JSON: %w", err)
	}
	id, _ := args["id"].(string)
	if id == "" {
		return "", errors.New("update_requirement 缺少 id")
	}
	cur, err := b.GetRequirement(id)
	if err != nil {
		return "", err
	}
	if cur == nil {
		return "", errors.New("需求不存在: " + id)
	}
	if _, ok := args["project"]; !ok {
		args["project"] = cur.Project
	}
	if _, ok := args["priority"]; !ok {
		args["priority"] = cur.Priority
	}
	if _, ok := args["tags"]; !ok {
		if cur.Tags == nil {
			// 当前值为 nil 时回填空数组而非 null，避免下游解析歧义
			args["tags"] = []string{}
		} else {
			args["tags"] = cur.Tags
		}
	}
	// text 为必填字段，不回填；缺省时由下游校验报错
	out, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("序列化合并参数: %w", err)
	}
	return string(out), nil
}
