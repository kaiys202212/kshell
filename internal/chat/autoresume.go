package chat

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/acp"
)

// shouldAutoResume 判断一轮失败是否值得用「继续」自动重试。
// 只认明确的瞬时网络/限流特征；未知错误一律放弃，避免把确定性失败变成循环。
func shouldAutoResume(err error, stop, stderr string) bool {
	text := strings.ToLower(stderr)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}
		var rpc *acp.RPCError
		if errors.As(err, &rpc) {
			switch rpc.Code {
			case acp.ErrCodeAuthRequired, -32601, -32602:
				return false
			}
		}
		text = strings.ToLower(err.Error()) + "\n" + text
	} else {
		switch stop {
		case "", "end_turn", "cancelled", "max_tokens", "max_turn_requests":
			return false
		case "refusal":
			// 上游常把 429 映射成 refusal；只在 stderr 有瞬时特征、且不是配额等
			// 确定性失败时续跑。
			return !containsAny(text, deterministicHints) && containsAny(text, transientHints)
		default:
			return false
		}
	}
	if containsAny(text, deterministicHints) {
		return false
	}
	return containsAny(text, transientHints)
}

var transientHints = []string{
	"rate limit", "too many requests", "too frequent", "throttl",
	"http 429", "status 429", "status code 429",
	"http 5", "status 5", "service unavailable", "bad gateway", "gateway timeout",
	"overloaded", "temporarily unavailable",
	"timeout", "timed out", "deadline exceeded",
	"connection reset", "connection refused", "connection closed",
	"socket hang up", "unexpected eof", "stream error", "network error",
	"econnreset", "econnrefused", "etimedout", "enotfound",
}

var deterministicHints = []string{
	"authrequired", "unauthorized", "forbidden", "invalid",
	"not found", "unsupported", "quota", "billing", "insufficient",
	"context length", "too long", "maximum context",
	"permission denied", "does not exist", "canceled", "cancelled",
}

func containsAny(s string, hints []string) bool {
	for _, h := range hints {
		if strings.Contains(s, h) {
			return true
		}
	}
	return false
}

const autoResumeDefaultText = "Auto-resume: continue the unfinished task. Do not repeat completed steps; if everything is done, give the final answer."

// connStderrLen / connStderrSince 取 stderr 水位与增量。*acp.Agent 提供 O(1) 的
// StderrLen；其他实现退化为整段字符串长度（仅测试桩会走到）。
func connStderrLen(c Conn) int {
	if c == nil {
		return 0
	}
	if x, ok := c.(interface{ StderrLen() int }); ok {
		return x.StderrLen()
	}
	return len(c.Stderr())
}

func connStderrSince(c Conn, mark int) string {
	if c == nil {
		return ""
	}
	full := c.Stderr()
	if mark < 0 || mark > len(full) {
		return full
	}
	return full[mark:]
}

// connAlive 判断连接是否还能发下一轮；不支持 Alive 的实现按存活处理（测试桩）。
func connAlive(c Conn) bool {
	if c == nil {
		return false
	}
	if x, ok := c.(interface{ Alive() bool }); ok {
		return x.Alive()
	}
	return true
}

// cancelAutoResumeLocked 停止待执行的续跑并让已触发的定时器失效。调用方持锁。
func (s *session) cancelAutoResumeLocked() {
	if s.autoResumeTimer != nil {
		s.autoResumeTimer.Stop()
		s.autoResumeTimer = nil
	}
	s.autoResumeToken++
}

// scheduleAutoResumeLocked 在失败收尾后安排一次续跑；调用方持锁。
// turnToken 是本轮开始时的 token：用户 Cancel/关闭会递增 token，使本轮不再续跑。
func (m *Manager) scheduleAutoResumeLocked(s *session, turnToken int64, err error, stop, stderr string) {
	if !m.autoResume || s.exited || s.info.Status != StatusReady || s.promptInFlight ||
		len(s.queue) > 0 || len(s.pending) > 0 || len(s.pendingElicit) > 0 ||
		s.autoResumeToken != turnToken || !connAlive(s.conn) {
		return
	}
	if s.autoResumeCount >= 1 || !shouldAutoResume(err, stop, stderr) {
		return
	}
	text := autoResumeDefaultText
	if m.autoResumeText != nil {
		if v := strings.TrimSpace(m.autoResumeText()); v != "" {
			text = v
		}
	}
	s.autoResumeCount++
	s.cancelAutoResumeLocked()
	token := s.autoResumeToken
	turn := s.turnSeq
	delay := m.autoResumeDelay
	s.autoResumeTimer = time.AfterFunc(delay, func() { m.fireAutoResume(s.id, s, token, turn, text) })
}

// fireAutoResume 定时器触发：重新校验状态后投递自动续跑；任何一项不满足都静默放弃。
func (m *Manager) fireAutoResume(id string, s *session, token, turn int64, text string) {
	m.mu.Lock()
	if !m.autoResume || m.byID[id] != s || s.exited || s.info.Status != StatusReady || s.promptInFlight ||
		len(s.queue) > 0 || len(s.pending) > 0 || len(s.pendingElicit) > 0 ||
		s.autoResumeToken != token || s.turnSeq != turn || !connAlive(s.conn) {
		m.mu.Unlock()
		return
	}
	s.autoResumeTimer = nil
	m.mu.Unlock()
	_ = m.deliverTurn(id, s, queuedMsg{
		blocks:    []acp.ContentBlock{{Type: "text", Text: text}},
		auto:      true,
		autoToken: token,
		autoTurn:  turn,
	}, true)
}
