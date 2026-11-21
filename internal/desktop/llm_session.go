package desktop

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// llm-session.json：应用级稳定 LLM 会话 ID。
// opencode GO 网关要求所有请求带 x-opencode-session（稳定会话 ID，用于路由
// 优化与提示缓存），缺失直接 400（MissingSessionID）。Claude/Codex 等 ACP
// CLI 自带原生会话头不受影响，只有 kshell 自家调用（Agent Hub 管理者循环、
// Git 提交信息生成等 hubtools 链路）需要补。App 级单一稳定 ID 粒度粗于官方
// 建议的「每会话」，但满足「稳定」要求且避免把会话概念穿透进 hubtools。
const llmSessionStateFile = "llm-session.json"

// llmSessionState 是落盘形态。
type llmSessionState struct {
	SessionID string `json:"sessionId"`
}

// llmSessionID 返回应用级稳定 LLM 会话 ID：首次调用生成 UUIDv4 并原子落盘
// state 目录，之后跨重启沿用；Layout.State 未装配时退化为进程内临时 ID
//（不落盘）。缓存不随后续 Layout 装配刷新：生产时序 Layout 装配先于任何
// hubtools 调用，临时 ID 仅测试零值 App 可达。缓存 + mu 保护，并发首次
// 调用只生成一次。
func (a *App) llmSessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.llmSessionIDCache != "" {
		return a.llmSessionIDCache
	}
	id := a.loadLLMSessionID()
	if id == "" {
		id = newUUIDv4()
		a.saveLLMSessionID(id)
	}
	a.llmSessionIDCache = id
	return id
}

// llmSessionStatePath 返回 state 文件完整路径；Layout.State 未装配返回空串
//（与 session_model_state.go 同口径：调用方据空串跳过落盘）。
func (a *App) llmSessionStatePath() string {
	root := a.opts.Layout.State
	if root == "" {
		return ""
	}
	return filepath.Join(root, llmSessionStateFile)
}

// loadLLMSessionID 读落盘 ID；缺失/损坏返回空串（调用方重新生成并覆盖）。
// 持 a.mu 调用。
func (a *App) loadLLMSessionID() string {
	path := a.llmSessionStatePath()
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var st llmSessionState
	if json.Unmarshal(b, &st) != nil {
		return ""
	}
	return strings.TrimSpace(st.SessionID)
}

// saveLLMSessionID 原子落盘 ID；失败静默——缓存里已有值，本进程内功能不受
// 影响，下次启动重新生成（网关侧仅路由归属漂移，不影响可用性）。持 a.mu 调用。
func (a *App) saveLLMSessionID(id string) {
	path := a.llmSessionStatePath()
	if path == "" {
		return
	}
	raw, err := json.MarshalIndent(llmSessionState{SessionID: id}, "", "  ")
	if err != nil {
		return
	}
	writeStateJSONAtomic(path, append(raw, '\n'))
}

// newUUIDv4 用 crypto/rand 拼标准 UUIDv4 字符串（8-4-4-4-12），不引第三方依赖。
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败极罕见（系统熵源不可用）：回退进程唯一字符串，
		// 保证 ID 非空且跨安装不同值（网关只要求稳定字符串，不校验版本位）。
		return fmt.Sprintf("kshell-llm-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hex[v>>4], hex[v&0x0f])
	}
	return string(out)
}
