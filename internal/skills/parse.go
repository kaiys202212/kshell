package skills

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSkillMD 解析 SKILL.md，返回 name 与 description。
func ParseSkillMD(content []byte) (name, description string, err error) {
	text := string(content)
	text = strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(strings.TrimSpace(text), "---") {
		return "", "", errInvalidSkill
	}
	rest := strings.TrimSpace(text)
	rest = strings.TrimPrefix(rest, "---")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", errInvalidSkill
	}
	fm := rest[:end]
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return "", "", errInvalidSkill
	}
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Description = strings.TrimSpace(meta.Description)
	if meta.Name == "" || meta.Description == "" {
		return "", "", errInvalidSkill
	}
	return meta.Name, meta.Description, nil
}

// BodyPreview 取 frontmatter 之后的正文预览，最多 max 字节。
func BodyPreview(content []byte, max int) string {
	text := string(content)
	text = strings.TrimPrefix(text, "\ufeff")
	rest := strings.TrimSpace(text)
	if !strings.HasPrefix(rest, "---") {
		if max > 0 && len(rest) > max {
			return rest[:max]
		}
		return rest
	}
	rest = strings.TrimPrefix(rest, "---")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	body := strings.TrimSpace(rest[end+len("\n---"):])
	if max > 0 && len(body) > max {
		return body[:max]
	}
	return body
}
