package skills

import "time"

// Summary 是 skills.sh 搜索结果条目（Wails JSON 用导出字段名）。
type Summary struct {
	ID       string
	SkillID  string
	Name     string
	Source   string
	Installs int64
}

// File 是 download API 返回的单个文件。
type File struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

// Detail 是预览用详情（可不落盘）。
type Detail struct {
	ID          string
	Name        string
	Description string
	Source      string
	BodyPreview string
}

// TargetInfo 是可选安装目标（已检测且有已知 skills 根）。
type TargetInfo struct {
	ToolID         string
	Root           string
	DefaultChecked bool
}

// TargetRecord 记录某 skill 在某一 tool 上的安装结果。
type TargetRecord struct {
	Mode string // link | copy
	Path string
}

// InstalledSkill 是本地已安装条目。
type InstalledSkill struct {
	ID          string
	Name        string
	Source      string
	InstalledAt time.Time
	EntityPath  string
	Targets     map[string]TargetRecord
}

// Manifest 持久化在 ~/.kshell/skills/manifest.json。
type Manifest struct {
	Version int                       `json:"version"`
	Skills  map[string]InstalledSkill `json:"skills"`
}

// InstallResult 是一次安装的汇总。
type InstallResult struct {
	ID      string
	Name    string
	Targets map[string]TargetRecord
	Errors  map[string]string `json:"Errors,omitempty"`
}
