package skills

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

const bodyPreviewMax = 8 << 10

// Service 编排搜索、安装、卸载。
type Service struct {
	Home   string
	Client *Client
	Store  *Store
}

// NewService 用 home 装配默认 Client 与 Store。
func NewService(home string) *Service {
	return &Service{
		Home:   home,
		Client: &Client{},
		Store:  &Store{Root: filepath.Join(home, ".kshell", "skills")},
	}
}

// Search 代理 registry。
func (s *Service) Search(ctx context.Context, q string, limit int) ([]Summary, error) {
	return s.Client.Search(ctx, q, limit)
}

// Detail 下载并解析预览（不落盘）。
func (s *Service) Detail(ctx context.Context, id string) (Detail, error) {
	files, err := s.Client.Download(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	var skillMD []byte
	for _, f := range files {
		if filepath.ToSlash(f.Path) == "SKILL.md" || strings.EqualFold(filepath.Base(f.Path), "SKILL.md") {
			skillMD = []byte(f.Contents)
			break
		}
	}
	if skillMD == nil {
		return Detail{}, errInvalidSkill
	}
	name, desc, err := ParseSkillMD(skillMD)
	if err != nil {
		return Detail{}, err
	}
	owner, repo, _, _ := splitID(id)
	return Detail{
		ID:          id,
		Name:        name,
		Description: desc,
		Source:      owner + "/" + repo,
		BodyPreview: BodyPreview(skillMD, bodyPreviewMax),
	}, nil
}

// ListTargets 对已检测 toolID 返回可安装目标（默认勾选）。
func (s *Service) ListTargets(detected []string) []TargetInfo {
	out := make([]TargetInfo, 0, len(detected))
	seen := map[string]bool{}
	for _, id := range detected {
		if seen[id] {
			continue
		}
		root, ok := TargetRoot(id, s.Home)
		if !ok {
			continue
		}
		seen[id] = true
		out = append(out, TargetInfo{ToolID: id, Root: root, DefaultChecked: true})
	}
	return out
}

// Install 下载、写实体、安装到各 tool 目标。
// force 为 true 时覆盖各 agent 下已存在的同名非本实体目录。
func (s *Service) Install(ctx context.Context, id string, toolIDs []string, force bool) (InstallResult, error) {
	if len(toolIDs) == 0 {
		return InstallResult{}, errNoTargets
	}
	files, err := s.Client.Download(ctx, id)
	if err != nil {
		return InstallResult{}, err
	}
	// 重装前清掉旧目标，避免副本模式下路径冲突。
	if m, err := s.Store.ReadManifest(); err == nil {
		if old, ok := m.Skills[id]; ok {
			for _, t := range old.Targets {
				_ = RemoveTarget(t.Path)
			}
		}
	}
	entityDir, name, err := s.Store.WriteEntity(id, files)
	if err != nil {
		return InstallResult{}, err
	}
	owner, repo, _, _ := splitID(id)
	res := InstallResult{
		ID:      id,
		Name:    name,
		Targets: map[string]TargetRecord{},
		Errors:  map[string]string{},
	}
	for _, toolID := range toolIDs {
		root, ok := TargetRoot(toolID, s.Home)
		if !ok {
			res.Errors[toolID] = errInvalidID.Error()
			continue
		}
		target := filepath.Join(root, name)
		mode, err := InstallTarget(entityDir, target, force)
		if err != nil {
			res.Errors[toolID] = err.Error()
			continue
		}
		res.Targets[toolID] = TargetRecord{Mode: mode, Path: target}
	}
	if len(res.Targets) == 0 {
		_ = s.Store.RemoveInstalled(id, true)
		// 实体可能已写入但未进 manifest；直接删目录
		_ = RemoveTarget(entityDir)
		return res, errNoTargets
	}
	rec := InstalledSkill{
		ID:          id,
		Name:        name,
		Source:      owner + "/" + repo,
		InstalledAt: time.Now().UTC(),
		EntityPath:  RelEntityPath(s.Store.Root, entityDir),
		Targets:     res.Targets,
	}
	if err := s.Store.UpsertInstalled(rec); err != nil {
		return res, err
	}
	if len(res.Errors) == 0 {
		res.Errors = nil
	}
	return res, nil
}

// ListInstalled 返回 manifest 中的技能。
func (s *Service) ListInstalled() ([]InstalledSkill, error) {
	m, err := s.Store.ReadManifest()
	if err != nil {
		return nil, err
	}
	out := make([]InstalledSkill, 0, len(m.Skills))
	for _, v := range m.Skills {
		out = append(out, v)
	}
	return out, nil
}

// Uninstall 移除各目标并可选删除实体。
func (s *Service) Uninstall(id string, removeEntity bool) error {
	m, err := s.Store.ReadManifest()
	if err != nil {
		return err
	}
	rec, ok := m.Skills[id]
	if ok {
		for _, t := range rec.Targets {
			_ = RemoveTarget(t.Path)
		}
	}
	return s.Store.RemoveInstalled(id, removeEntity)
}
