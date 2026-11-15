package agentapi

import "fmt"

// control.go 是操控原语的薄包装：统一做寻址键归一（resolve）与「未激活守卫」，
// 执行转发给 Backend。错误语义：未知/未激活会话 ErrNotFound，其余原样透传
// 底层错误。

// resolveActive 在 resolve 之上要求会话已激活（有活跃 chat ID）：操控类原语
// 全部作用于活跃会话，磁盘会话须先 Open。
func (a *API) resolveActive(id string) (Session, error) {
	s, err := a.resolve(id)
	if err != nil {
		return s, err
	}
	if s.ID == "" {
		return s, fmt.Errorf("%w: 会话未激活（先 Open）: %s", ErrNotFound, s.SessionID)
	}
	return s, nil
}

// Send 向指定会话发送一轮用户输入。
func (a *API) Send(id, text string) error {
	s, err := a.resolveActive(id)
	if err != nil {
		return err
	}
	return a.b.Send(s.ID, text)
}

// Stop 中断指定会话当前轮次。
func (a *API) Stop(id string) error {
	s, err := a.resolveActive(id)
	if err != nil {
		return err
	}
	return a.b.Stop(s.ID)
}

// Recall 按 hub 消息 ID 撤回一条用户消息。
func (a *API) Recall(msgID int64) error {
	return a.b.Recall(msgID)
}

// Models 返回会话可选模型与当前模型。
func (a *API) Models(id string) ([]ModelOption, string, error) {
	s, err := a.resolveActive(id)
	if err != nil {
		return nil, "", err
	}
	return a.b.ListModels(s.ID)
}

// SetModel 运行时切换会话模型。
func (a *API) SetModel(id, model string) error {
	s, err := a.resolveActive(id)
	if err != nil {
		return err
	}
	return a.b.SetModel(s.ID, model)
}

// PendingPermissions 返回该会话记录的未处理权限请求快照。
func (a *API) PendingPermissions(id string) []Permission {
	s, err := a.resolveActive(id)
	if err != nil {
		return nil
	}
	return a.b.Feed().PendingPermissions(s.ID)
}

// GrantPermission 处理权限请求；成功后从 pending 快照移除，
// 让 PendingPermissions 反映处理后的状态。
func (a *API) GrantPermission(id, reqID string, allow bool) error {
	s, err := a.resolveActive(id)
	if err != nil {
		return err
	}
	if err := a.b.GrantPermission(s.ID, reqID, allow); err != nil {
		return err
	}
	a.b.Feed().ClearPermission(s.ID, reqID)
	return nil
}

// Watch 订阅某会话的事件流（update/permission/exit），返回只读通道与取消
// 函数。投递非阻塞（缓冲满丢帧），兜底拉取用 Messages/LastFullOutput。
// 未知或未激活会话返回已关闭通道与空操作 cancel。
func (a *API) Watch(id string, buf int) (<-chan Event, func()) {
	s, err := a.resolveActive(id)
	if err != nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	return a.b.Feed().Subscribe(s.ID, buf)
}
