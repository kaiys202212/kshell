package skills

import "time"

// Summary 是 skills.sh 搜索结果条目。
type Summary struct {
	ID       string `json:"id"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	Installs int64  `json:"installs"`
}

// File 是 download API 返回的单个文件。
type File struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

// Detail 是预览用详情（可不落盘）。
type Detail struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	BodyPreview string `json:"bodyPreview"`
}

// TargetInfo 是可选安装目标（已检测且有已知 skills 根）。
type TargetInfo struct {
	ToolID         string `json:"toolId"`
	Root           string `json:"root"`
	DefaultChecked bool   `json:"defaultChecked"`
}

// TargetRecord 记录某 skill 在某一 tool 上的安装结果。
type TargetRecord struct {
	Mode string `json:"mode"` // link | copy
	Path string `json:"path"`
}

// InstalledSkill 是本地已安装条目。
type InstalledSkill struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Source      string                  `json:"source"`
	InstalledAt time.Time               `json:"installedAt"`
	EntityPath  string                  `json:"entityPath"`
	Targets     map[string]TargetRecord `json:"targets"`
}

// Manifest 持久化在 ~/.kshell/skills/manifest.json。
type Manifest struct {
	Version int                       `json:"version"`
	Skills  map[string]InstalledSkill `json:"skills"`
}

// InstallResult 是一次安装的汇总。
type InstallResult struct {
	ID      string                  `json:"id"`
	Name    string                  `json:"name"`
	Targets map[string]TargetRecord `json:"targets"`
	Errors  map[string]string       `json:"errors,omitempty"`
}
