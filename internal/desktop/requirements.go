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
func (a *App) ListRequirements() []requirements.Item {
	store := a.getRequirementStore()
	if store == nil {
		return []requirements.Item{}
	}
	return store.List()
}

// AddRequirement 新增一条需求（text 去空白非空；priority 非法回退 medium），
// 成功后 emit requirements:changed 让前端刷新列表。
func (a *App) AddRequirement(text, priority string, tags []string) (requirements.Item, error) {
	store := a.getRequirementStore()
	if store == nil {
		return requirements.Item{}, errReqStoreNotReady
	}
	it, err := store.Add(text, priority, tags)
	if err != nil {
		return requirements.Item{}, err
	}
	a.Emit("requirements:changed")
	return it, nil
}

// UpdateRequirement 修改需求（文本/优先级/标签整体替换）；id 不存在返回错误。
func (a *App) UpdateRequirement(id, text, priority string, tags []string) (requirements.Item, error) {
	store := a.getRequirementStore()
	if store == nil {
		return requirements.Item{}, errReqStoreNotReady
	}
	it, err := store.Update(id, text, priority, tags)
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

// MarkRequirementDispatched 标记需求已派发到某工作区并关联会话 ID；id 不存在返回错误。
func (a *App) MarkRequirementDispatched(id, workspace, chatID string) error {
	store := a.getRequirementStore()
	if store == nil {
		return errReqStoreNotReady
	}
	if err := store.MarkDispatched(id, workspace, chatID); err != nil {
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
		info, ok := m.Get(res.ChatID)
		if ok && info.Status != chat.StatusRunning && info.Status != chat.StatusStarting {
			if err := m.Close(res.ChatID); err == nil {
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
