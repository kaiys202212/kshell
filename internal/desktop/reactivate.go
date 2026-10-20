package desktop

import (
	"errors"
	"fmt"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/launch"
)

// chatGoneErr 复用 chat 包的 err.chat.gone 语义：未知 ID（或已关闭页签）不可投递。
// desktop 层不直接引用 chat 包内部错误变量，按 wire key 文本独立构造，保证
// chat 侧改错误文案不影响此处（两处都译作「会话不存在或已关闭」）。
var chatGoneErr = errors.New("err.chat.gone")

// ensureChatAlive 保证 chat 可投递：已退出则按原 key 重建（有 SessionID 则
// resume+LoadSession，否则按原 Workspace+ToolID 新起），无感继续对话。
// 返回投递应使用的会话 ID：未重建时与入参相同；重建后是新 ID
//（Manager.Open 摘除旧会话重新编号），调用方必须改用返回值投递。
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
					return "", fmt.Errorf("err.chat.rebuild_failed|%w", err)
				}
				a.rememberChatAlias(info.ID, out.ID)
				// 按会话 JSONL 尾部上次真实模型恢复（与 OpenSessionACP 同口径）。
				a.restoreSessionModel(out.ID, s.ToolID, s.Path)
				// resume 会重放历史分片（manager 侧 fresh 起步、Seq 从头计数），
				// 前端旧 ID 的 Seq 门控已抬高，必须通知重置并重拉 ChatHistory，
				// 否则重放与后续新事件全被门控丢弃（时间线冻结）。Rebuilt 只在
				// 此处为 true；重放基准换血也是前端唯一可信的重建信号。
				// 注意返回值必须是新 ID（manager 只认它，调用方靠它投递），
				// chat:meta 里才翻回旧 ID（前端索引空间）。
				meta := out
				meta.ID = info.ID
				meta.Rebuilt = true
				a.Emit("chat:meta", meta)
				return out.ID, nil
			}
		}
		// discovery 缓存里查不到该会话（如缓存被清）→ 降级为按工作区新起，
		// 宁可丢上下文也不让用户对着「会话已退出」干瞪眼。
	}
	// KindNew 或降级：按原 Workspace+ToolID 新起（与 openWorkspaceACP 同构）。
	// 工作区路径为空时无法锚定重建（launch 合成空 cwd 必然失败）：
	// 直接报错并携带底层原因，前端经 err.chat.rebuild_failed 透出细节。
	if info.Workspace == "" {
		return "", fmt.Errorf("err.chat.rebuild_failed|workspace empty")
	}
	ws, tools, ok := a.workspaceByIDReady(info.Workspace)
	if !ok {
		// 扫描结果里没有该工作区：用 Info 里的路径合成一个，路径仍是最可靠的锚点
		ws = discovery.Workspace{Path: info.Workspace}
	}
	l, err := launch.ForWorkspaceACP(o.Providers, tools, ws, info.ToolID, a.modelOptions(), a.permissionOptions())
	if err != nil {
		// 未登录是用户可自助修复的前置状态，不是重建链路故障：返回裸 wire key，
		// 前端 translateBackend 直译并引导去登录，不当「重建失败」复合错误展示。
		if errors.Is(err, launch.ErrNotLoggedIn) {
			return "", err
		}
		return "", fmt.Errorf("err.chat.rebuild_failed|%w", err)
	}
	// 新起不写欢迎词：这不是「第一次打开」，重复写会让 AgentHub 每次重建都多一条
	out, err := openChat(m, key, chat.Info{
		Kind:      chat.KindNew,
		Workspace: info.Workspace,
		Title:     info.Title,
		ToolID:    info.ToolID,
	}, l, "")
	if err != nil {
		return "", fmt.Errorf("err.chat.rebuild_failed|%w", err)
	}
	a.rememberChatAlias(info.ID, out.ID)
	return out.ID, nil
}

// resolveChatID 把前端持有的会话 ID 沿别名链解析到 manager 里的最新 ID：
// 重建换 ID 后前端页签仍持旧 ID，所有接受 chatID 的入口都先过这里再操作。
// 链尾在 manager 在册（或入参本就无别名）时原样返回；整个链都不在册时
// 返回链尾与 false（已是最新仍查不到，调用方按 err.chat.gone 报）。
func (a *App) resolveChatID(id string) (string, bool) {
	m := a.chats()
	if m == nil {
		return id, false
	}
	cur := id
	for {
		if _, ok := m.Get(cur); ok {
			return cur, true
		}
		next, ok := a.resolveChatAlias(cur)
		if !ok {
			return cur, false
		}
		cur = next
	}
}

// originalChatID 把 manager 侧的新会话 ID 翻回前端持有的最原始 ID（别名链头）：
// 出站事件统一用它，前端按旧 ID 索引的状态才收得到重建后的事件。
// 新会话（从未重建过）链头即自身，零开销。
func (a *App) originalChatID(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.originalChatIDLocked(id)
}

// originalChatIDLocked 是 originalChatID 的锁内实现；调用方必须已持 a.mu。
func (a *App) originalChatIDLocked(id string) string {
	// 反向查找：命中「某旧 ID → id」的键即为链上更早一环，走到没有前驱为止。
	// 别名链由重建顺序自然形成（old→new→newest），环由 rememberChatAlias 的
	// 旧 ID 单调递增性质排除（新 ID 永不作为旧 ID 复用）。
	cur := id
	for prev, ok := a.chatAliasPrevLocked(cur); ok; prev, ok = a.chatAliasPrevLocked(cur) {
		cur = prev
	}
	return cur
}

// chatAliasPrevLocked 反向查一步：谁指向 id。chatAliases 是 旧→新 单向表，
// 反查需线性扫描（条目数为重建次数，量级极小，不值得维护第二张反向表）。
func (a *App) chatAliasPrevLocked(id string) (string, bool) {
	if a.chatAliases == nil {
		return "", false
	}
	for old, newID := range a.chatAliases {
		if newID == id {
			return old, true
		}
	}
	return "", false
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
// 新 ID 不会反过来成为旧 ID（Manager.Open 单调递增编号），所以链不会成环。
func (a *App) rememberChatAlias(oldID, newID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatAliases == nil {
		a.chatAliases = make(map[string]string)
	}
	a.chatAliases[oldID] = newID
}

// forgetChatChain 清理别名表中指向被关会话链的全部条目：CloseChat 成功后调用。
// 别名只服务「重建后继续投递」，页签关掉意味着前端不再持有该链的任何 ID，
// 残留会把已关会话误判成存活。先收集要删的键再统一删：边遍历边 delete 虽然在
// Go map 语义上安全，但删掉的条目不再参与后续 originalChatIDLocked 回溯，
// 链式重建（旧→新→更新）时后段会因前段被删而回溯断链、漏删。
func (a *App) forgetChatChain(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	original := a.originalChatIDLocked(id)
	// 链上所有键都指向同一链头（originalChatIDLocked 沿值回溯），用值域归因
	// 即可整链清除：k 是旧 ID、v 是下一环，同链则 v 回溯回 original。
	var doomed []string
	for k, v := range a.chatAliases {
		if original == k || a.originalChatIDLocked(v) == original {
			doomed = append(doomed, k)
		}
	}
	for _, k := range doomed {
		delete(a.chatAliases, k)
	}
}
