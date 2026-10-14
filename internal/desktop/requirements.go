package desktop

import (
	"errors"

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

// MarkRequirementDispatched 标记需求已派发到某工作区；id 不存在返回错误。
func (a *App) MarkRequirementDispatched(id, workspace string) error {
	store := a.getRequirementStore()
	if store == nil {
		return errReqStoreNotReady
	}
	if err := store.MarkDispatched(id, workspace); err != nil {
		return err
	}
	a.Emit("requirements:changed")
	return nil
}
