package desktop

import (
	"context"
	"time"

	"github.com/yangk/kshell/internal/skills"
)

// SkillSummaryView 供前端列表渲染。
type SkillSummaryView = skills.Summary

// SkillDetailView 供前端详情预览。
type SkillDetailView = skills.Detail

// SkillTargetView 供安装前勾选。
type SkillTargetView = skills.TargetInfo

// InstalledSkillView 供已安装列表。
type InstalledSkillView = skills.InstalledSkill

// InstallSkillView 安装结果。
type InstallSkillView = skills.InstallResult

func (a *App) skillsService() *skills.Service {
	home := a.snapshot().Home
	if home == "" {
		home = a.opts.Home
	}
	return skills.NewService(home)
}

// SearchSkills 搜索 skills.sh；limit≤0 时用默认值。
func (a *App) SearchSkills(query string, limit int) ([]SkillSummaryView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.skillsService().Search(ctx, query, limit)
}

// GetSkillDetail 拉取 skill 预览（不落盘）。
func (a *App) GetSkillDetail(id string) (SkillDetailView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.skillsService().Detail(ctx, id)
}

// ListSkillTargets 返回已安装工具中可安装 skill 的目标（默认勾选）。
func (a *App) ListSkillTargets() []SkillTargetView {
	a.ensureToolsReady()
	a.mu.Lock()
	tools := a.tools
	a.mu.Unlock()
	ids := make([]string, 0, len(tools))
	for _, t := range tools {
		if t.Installed && !t.Broken {
			ids = append(ids, t.ID)
		}
	}
	return a.skillsService().ListTargets(ids)
}

// InstallSkill 安装到指定 tool 目标。
func (a *App) InstallSkill(id string, toolIDs []string) (InstallSkillView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return a.skillsService().Install(ctx, id, toolIDs)
}

// ListInstalledSkills 列出本地已安装 skill。
func (a *App) ListInstalledSkills() ([]InstalledSkillView, error) {
	return a.skillsService().ListInstalled()
}

// UninstallSkill 卸载；removeEntity 为 true 时删除统一实体目录。
func (a *App) UninstallSkill(id string, removeEntity bool) error {
	return a.skillsService().Uninstall(id, removeEntity)
}
