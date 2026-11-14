package agentapi

import "fmt"

// MessageQuery 是输出拉取参数：Since 为 Ts 水位（只取 Ts>Since 的增量），
// Tail 为尾部条数（两者同时给定时先过滤后截尾）。
type MessageQuery struct {
	Since int64
	Tail  int
}

// Messages 拉取某会话的消息（时间正序）。活跃 chat 走 hub store；
// 磁盘未激活会话无归因消息，返回空切片而非错误。
func (a *API) Messages(id string, q MessageQuery) ([]Message, error) {
	s, err := a.resolve(id)
	if err != nil {
		return nil, err
	}
	if s.ID == "" {
		return []Message{}, nil
	}
	msgs := a.b.SessionMessages(s.ID)
	// Since 过滤后 Tail 截尾，保持时间正序。
	if q.Since > 0 {
		filtered := msgs[:0]
		for _, m := range msgs {
			if m.Ts > q.Since {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if q.Tail > 0 && len(msgs) > q.Tail {
		msgs = msgs[len(msgs)-q.Tail:]
	}
	return msgs, nil
}

// LastFullOutput 返回该会话最近一轮的完整 agent 回复（最近一条 agent 可见
// 消息全文）。该会话没有任何 agent 回复时返回 ErrNotFound。
func (a *API) LastFullOutput(id string) (Message, error) {
	s, err := a.resolve(id)
	if err != nil {
		return Message{}, err
	}
	msgs := a.b.SessionMessages(s.ID)
	var last *Message
	for i := range msgs {
		if msgs[i].Kind == "agent" {
			last = &msgs[i]
		}
	}
	if last == nil {
		return Message{}, fmt.Errorf("%w: 会话无 agent 回复 %s", ErrNotFound, id)
	}
	return *last, nil
}
