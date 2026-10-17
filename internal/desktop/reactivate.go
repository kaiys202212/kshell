package desktop

import (
	"fmt"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
)

// chatGoneErr 复用 chat 包的 err.chat.gone 语义：未知 ID（或已关闭页签）不可投递。
// desktop 层不直接引用 chat 包内部错误变量，按 wire key 文本独立构造，保证
// chat 侧改错误文案不影响此处（两处都译作「会话不存在或已关闭」）。
var chatGoneErr = fmt.Errorf("err.chat.gone")

// ensureChatAlive 保证 chat 可投递：已退出则按原 key 重建（有 SessionID 则
// resume+LoadSession，否则按原 Workspace+ToolID 新起），无感继续对话。
// 返回投递应使用的会话 ID：未重建时与入参相同；重建后是新 ID
// （Manager.Open 摘除旧会话重新编号），调用方必须改用返回值投递。
func (a *App) ensureChatAlive(chatID string) (string, error) {
	m := a.chats()
	if m == nil {
		return chatID, errNotReady
	}
	info, ok := m.Get(chatID)
	if !ok {
		// 旧 ID 在重建后已不在册（Manager.Open 换新 ID）：查别名表，命中说明
		// 本会话已被重建过，直接用新 ID 走快速路径。
		if mapped, loaded := a.resolveChatAlias(chatID); loaded {
			return a.ensureChatAlive(mapped)
		}
		return chatID, chatGoneErr
	}
	if info.Status != chat.StatusExited {
		return chatID, nil // 快速路径：starting/ready/running 都可直接投递（忙时 manager 自行入队）
	}
	return a.rebuildChat(m, info)
}

// rebuildChat 按原 key 重建已退出会话：恢复型（KindSession）尽量 resume，
// 拿不到磁盘会话记录时降级为新起；新建型按原 Workspace+ToolID 新起。
func (a *App) rebuildChat(m *chat.Manager, info chat.Info) (string, error) {
	o := a.snapshot()
	key := info.Key
	// KindSession：优先按原会话 resume，保住上下文。
	if info.Kind == chat.KindSession && info.SessionID != "" {
		if s, tools, ok := a.sessionByIDReady(info.SessionID); ok {
			if l, err := launch.ForSessionACP(o.Providers, tools, s, a.modelOptions(), a.permissionOptions()); err == nil {
				out, err := openChat(m, key, chat.Info{
					Kind:      chat.KindSession,
					SessionID: s.ID,
					Workspace: s.Workspace,
					Title:     s.Title,
					ToolID:    s.ToolID,
				}, l, s.ID)
				if err != nil {
					return "", fmt.Errorf("err.chat.not_ready|%w", err)
				}
				a.rememberChatAlias(info.ID, out.ID)
				return out.ID, nil
			}
		}
		// discovery 缓存里查不到该会话（如缓存被清）→ 降级为按工作区新起，
		// 宁可丢上下文也不让用户对着「会话已退出」干瞪眼。
	}
	// KindNew 或降级：按原 Workspace+ToolID 新起（与 openWorkspaceACP 同构）。
	ws, tools, ok := a.workspaceByIDReady(info.Workspace)
	if !ok {
		// 扫描结果里没有该工作区：用 Info 里的路径合成一个，路径仍是最可靠的锚点
		ws = discovery.Workspace{Path: info.Workspace}
	}
	l, err := launch.ForWorkspaceACP(o.Providers, tools, ws, info.ToolID, a.modelOptions(), a.permissionOptions())
	if err != nil {
		return "", fmt.Errorf("err.chat.not_ready|%w", err)
	}
	// 新起不写欢迎词：这不是「第一次打开」，重复写会让 AgentHub 每次重建都多一条
	out, err := openChat(m, key, chat.Info{
		Kind:      chat.KindNew,
		Workspace: info.Workspace,
		Title:     info.Title,
		ToolID:    info.ToolID,
	}, l, "")
	if err != nil {
		return "", fmt.Errorf("err.chat.not_ready|%w", err)
	}
	a.rememberChatAlias(info.ID, out.ID)
	return out.ID, nil
}

// resolveChatAlias 把重建前的旧会话 ID 映射到重建后的新 ID：前端页签持有旧 ID，
// 第二条消息开始靠这张表继续无感投递。loaded=false 表示无记录（真不存在）。
func (a *App) resolveChatAlias(oldID string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatAliases == nil {
		return "", false
	}
	id, ok := a.chatAliases[oldID]
	return id, ok
}

// rememberChatAlias 记录一次重建的新旧 ID 映射（链式重建自然覆盖到最新）。
func (a *App) rememberChatAlias(oldID, newID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatAliases == nil {
		a.chatAliases = make(map[string]string)
	}
	a.chatAliases[oldID] = newID
}
