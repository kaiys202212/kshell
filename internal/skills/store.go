package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store 管理 ~/.kshell/skills 实体与 manifest。
type Store struct {
	Root string
}

// EntityDir 返回 id 对应实体绝对路径（owner/repo/skillId）。
func (s *Store) EntityDir(id string) (string, error) {
	owner, repo, skillID, err := splitID(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.Root, owner, repo, skillID), nil
}

// WriteEntity 将 files 原子写入实体目录，返回目录与 skill name。
func (s *Store) WriteEntity(id string, files []File) (entityDir, name string, err error) {
	entityDir, err = s.EntityDir(id)
	if err != nil {
		return "", "", err
	}
	var skillMD []byte
	for _, f := range files {
		p := filepath.ToSlash(f.Path)
		if p == "SKILL.md" || strings.EqualFold(filepath.Base(p), "SKILL.md") {
			skillMD = []byte(f.Contents)
			break
		}
	}
	if skillMD == nil {
		return "", "", errInvalidSkill
	}
	name, _, err = ParseSkillMD(skillMD)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return "", "", err
	}
	tmp, err := os.MkdirTemp(s.Root, ".tmp-*")
	if err != nil {
		return "", "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tmp)
		}
	}()

	for _, f := range files {
		rel := filepath.Clean(filepath.FromSlash(f.Path))
		if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		dst := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(dst, []byte(f.Contents), 0o644); err != nil {
			return "", "", err
		}
	}
	_ = os.RemoveAll(entityDir)
	if err := os.MkdirAll(filepath.Dir(entityDir), 0o755); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmp, entityDir); err != nil {
		if err := copyDir(tmp, entityDir); err != nil {
			return "", "", err
		}
		_ = os.RemoveAll(tmp)
	}
	ok = true
	return entityDir, name, nil
}

// ReadManifest 读取 manifest；不存在则返回空。
func (s *Store) ReadManifest() (Manifest, error) {
	path := filepath.Join(s.Root, "manifest.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{Version: 1, Skills: map[string]InstalledSkill{}}, nil
		}
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if m.Skills == nil {
		m.Skills = map[string]InstalledSkill{}
	}
	if m.Version == 0 {
		m.Version = 1
	}
	return m, nil
}

// SaveManifest 原子写入 manifest。
func (s *Store) SaveManifest(m Manifest) error {
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Skills == nil {
		m.Skills = map[string]InstalledSkill{}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.Root, "manifest.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// UpsertInstalled 合并一条已安装记录。
func (s *Store) UpsertInstalled(skill InstalledSkill) error {
	m, err := s.ReadManifest()
	if err != nil {
		return err
	}
	if skill.InstalledAt.IsZero() {
		skill.InstalledAt = time.Now().UTC()
	}
	m.Skills[skill.ID] = skill
	return s.SaveManifest(m)
}

// RemoveInstalled 从 manifest 删除；removeEntity 时删实体目录。
func (s *Store) RemoveInstalled(id string, removeEntity bool) error {
	m, err := s.ReadManifest()
	if err != nil {
		return err
	}
	rec, ok := m.Skills[id]
	delete(m.Skills, id)
	if err := s.SaveManifest(m); err != nil {
		return err
	}
	if removeEntity && ok {
		dir := rec.EntityPath
		if dir == "" {
			dir, _ = s.EntityDir(id)
		} else if !filepath.IsAbs(dir) {
			dir = filepath.Join(s.Root, dir)
		}
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode())
	})
}

// RelEntityPath 返回相对 Store.Root 的实体路径。
func RelEntityPath(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return rel
}

// EnsureRoot 校验 Store.Root 非空。
func (s *Store) EnsureRoot() error {
	if s == nil || s.Root == "" {
		return fmt.Errorf("%w|empty store root", errInvalidSkill)
	}
	return nil
}
