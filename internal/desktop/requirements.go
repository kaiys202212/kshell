package desktop

import (
	"errors"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/requirements"
)

// errReqStoreNotReady 是 store 未装配时的统一错误（正常启动流程总会装配，
// 只有测试注入缺省才会出现）。
var errReqStoreNotReady = errors.New("requirements store not ready")

// getRequirementStore 取需求列表 store；未装配（未就绪/测试）返回 nil，
// 绑定方法据此返回空值而不是 panic。
func (a *App) getRequirementStore() *requirements.Store {
	return a.snapshot().RequirementsStore
}

// ListRequirements 返回全部需求条目（最新在前）。store 未装配返回空切片。
// store.List 内部先尝试存量迁移（DispatchedTo → Project 回填，resolver 未就绪时
// 自动推迟），保证前端拿到的列表已完成回填。
// 返回前做两层会话绑定维护（仅内存覆盖，不落盘——落盘由 SetOnInfo hook 的
// SetSessionTitle / 惰性 SetSessionID 负责）：
//  1. ChatID 直配活跃会话时补标题、并把磁盘会话 ID 惰性落盘（旧数据无 sessionId）；
//  2. ChatID 失效（桌面端重启后 chat ID 含 bootID 不再有效）时按条目 SessionID
//     反查活跃会话恢复 ChatID/SessionTitle，需求卡片的状态点与跳转随之恢复。
func (a *App) ListRequirements() []requirements.Item {
	store := a.getRequirementStore()
	if store == nil {
		return []requirements.Item{}
	}
	items := store.List()
	m := a.chats()
	if m == nil {
		return items
	}
	// SessionID → 活跃会话索引：ChatID 失效时按磁盘会话 ID 恢复绑定。
	bySession := make(map[string]chat.Info)
	for _, c := range m.List() {
		if c.SessionID != "" {
			bySession[c.SessionID] = c
		}
	}
	for i := range items {
		it := &items[i]
		if it.ChatID != "" {
			// 落盘 ChatID 是前端空间（原始 ID）；重建后 manager 里只有最新 ID，先解析再反查。
			resolved, ok := a.resolveChatID(it.ChatID)
			if ok {
				info, ok := m.Get(resolved)
				if !ok {
					continue
				}
				if it.SessionTitle == "" && info.Title != "" {
					it.SessionTitle = info.Title
				}
				// 存量条目无 SessionID：借活跃会话补录落盘，重启后才能按它恢复。
				if it.SessionID == "" && info.SessionID != "" {
					_, _ = store.SetSessionID(it.ChatID, info.SessionID)
					it.SessionID = info.SessionID
				}
				continue
			}
		}
		// ChatID 直配失败：按 SessionID 反查活跃会话恢复绑定（含 ChatID 覆盖）。
		if it.SessionID == "" {
			continue
		}
		info, ok := bySession[it.SessionID]
		if !ok {
			continue
		}
		it.ChatID = a.originalChatID(info.ID)
		if it.SessionTitle == "" && info.Title != "" {
			it.SessionTitle = info.Title
		}
	}
	return items
}

// AddRequirement 新增一条需求（project/text 去空白非空；priority 非法回退 medium），
// 成功后 emit requirements:changed 让前端刷新列表。
func (a *App) AddRequirement(project, text, priority string, tags []string) (requirements.Item, error) {
	store := a.getRequirementStore()
	if store == nil {
		return requirements.Item{}, errReqStoreNotReady
	}
	it, err := store.Add(project, text, priority, tags)
	if err != nil {
		return requirements.Item{}, err
	}
	a.Emit("requirements:changed")
	return it, nil
}

// UpdateRequirement 修改需求（项目/文本/优先级/标签整体替换）；id 不存在返回错误。
func (a *App) UpdateRequirement(id, project, text, priority string, tags []string) (requirements.Item, error) {
	store := a.getRequirementStore()
	if store == nil {
		return requirements.Item{}, errReqStoreNotReady
	}
	it, err := store.Update(id, project, text, priority, tags)
	if err != nil {
		return requirements.Item{}, err
	}
	a.Emit("requirements:changed")
	return it, nil
}

// DeleteRequirement 删除需求；幂等，id 不存在也返回 nil。
func (a *App) DeleteRequirement(id string) error {
	store := a.getRequirementStore()
	if store == nil {
		return errReqStoreNotReady
	}
	if err := store.Delete(id); err != nil {
		return err
	}
	a.Emit("requirements:changed")
	return nil
}

// MarkRequirementDispatched 标记需求已派发到某工作区，并关联会话 ID 与会话标题
// 快照（新建会话为占位标题、恢复会话为磁盘标题；随后标题变化经 SetOnInfo 回填）；
// sessionID 是磁盘会话 ID（跨进程稳定，重启后据此恢复绑定）；
// id 不存在返回错误。
func (a *App) MarkRequirementDispatched(id, workspace, chatID, title, sessionID string) error {
	store := a.getRequirementStore()
	if store == nil {
		return errReqStoreNotReady
	}
	if err := store.MarkDispatched(id, workspace, chatID, title, sessionID); err != nil {
		return err
	}
	a.Emit("requirements:changed")
	return nil
}

// ArchiveResult 是需求归档结果：Released 表示是否释放了关联会话，
// ChatID 为该会话 ID（未关联或已消失时为空串）。
type ArchiveResult struct {
	Released bool   `json:"released"`
	ChatID   string `json:"chatId,omitempty"`
}

// ArchiveRequirement 归档已完成（done）的需求，并视会话状态释放其关联聊天：
// 空闲会话直接关闭腾出资源；执行中（running/starting）的会话保持不动，
// Released=false 供前端提示「已归档但会话执行中未释放」。
func (a *App) ArchiveRequirement(id string) (ArchiveResult, error) {
	store := a.getRequirementStore()
	if store == nil {
		return ArchiveResult{}, errReqStoreNotReady
	}
	// 先校验状态再归档：store.Archive 只做终态守卫，这里补「未完成不可归档」的
	// 面向用户文案，避免把 store 层内部措辞直接透给前端。
	found := false
	for _, it := range store.List() {
		if it.ID == id {
			found = true
			if it.Status != requirements.StatusDone {
				return ArchiveResult{}, errors.New("仅已完成的需求可归档")
			}
			break
		}
	}
	if !found {
		return ArchiveResult{}, errors.New("需求不存在: " + id)
	}
	if err := store.Archive(id); err != nil {
		return ArchiveResult{}, err
	}

	// 释放关联会话：仅空闲（非 running/starting）时关闭；Close 幂等且会从
	// manager 摘除，比 Cancel（只取消当前轮次、不关进程）更符合「释放」语义。
	res := ArchiveResult{}
	for _, it := range store.List() {
		if it.ID != id {
			continue
		}
		res.ChatID = it.ChatID
		break
	}
	if m := a.chats(); m != nil && res.ChatID != "" {
		// 落盘 ChatID 是前端空间（原始 ID），manager 只认最新 ID，先解析再判状态/关闭。
		resolved, _ := a.resolveChatID(res.ChatID)
		info, ok := m.Get(resolved)
		if ok && info.Status != chat.StatusRunning && info.Status != chat.StatusStarting {
			if err := m.Close(resolved); err == nil {
				res.Released = true
			}
		}
	}
	a.Emit("requirements:changed")
	return res, nil
}

// SetRequirementStatus 推进需求状态机（pending→active、active→done），
// 非法流转由 store 拒绝；成功后推送 requirements:changed 让前端刷新。
func (a *App) SetRequirementStatus(id, status string) error {
	store := a.getRequirementStore()
	if store == nil {
		return errReqStoreNotReady
	}
	if err := store.SetStatus(id, status); err != nil {
		return err
	}
	a.Emit("requirements:changed")
	return nil
}
