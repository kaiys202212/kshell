package hub

import "strings"

// DefaultDirectPatterns 返回逃生门白名单内置默认集：# 命令族、@ 指名、
// continue/explain 语境词（config.AgentHub.DirectPatterns 空/nil 时使用）。
func DefaultDirectPatterns() []string {
	return []string{"#", "@", "continue", "explain"}
}

// IsDirectPass 判定输入是否命中逃生门白名单（命中=不经智能路由，走既有
// 确定性路径）。patterns 每项为前缀匹配；首词项（无空白分隔的单词）按
// "input 首词 == pattern（大小写不敏感）"判定，兼容 "Continue please"。
// # 与全角 ＃ 都视为命令前缀放行（命令合法性由 Console.Handle 自行判定）。
//
// 单词项按结尾字符分两种口径：ASCII 单词按整词匹配，避免 "continue" 吞掉
// "continuation"；非 ASCII 项（中文等无空格词边界）按首词前缀匹配，兼容
// 用户自定义 "部署" 命中 "部署一下 prod"。
func IsDirectPass(input string, patterns []string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}
	if strings.HasPrefix(input, "#") || strings.HasPrefix(input, "＃") {
		return true
	}
	if len(patterns) == 0 {
		patterns = DefaultDirectPatterns()
	}
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false
	}
	first := fields[0]
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.ContainsAny(p, " \t") {
			// 多词 pattern：整体前缀匹配（大小写不敏感）
			if strings.HasPrefix(strings.ToLower(input), strings.ToLower(p)) {
				return true
			}
			continue
		}
		if p == "#" || p == "@" {
			if strings.HasPrefix(input, p) {
				return true
			}
			continue
		}
		if endsWithASCIILetter(p) {
			if strings.EqualFold(first, p) {
				return true
			}
			continue
		}
		if strings.HasPrefix(strings.ToLower(first), strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// endsWithASCIILetter 报告 pattern 是否以 ASCII 字母/数字结尾：是则视为
// 有空格词边界的英文单词，只做整词匹配；否则（中文等）允许前缀匹配。
func endsWithASCIILetter(p string) bool {
	rs := []rune(p)
	if len(rs) == 0 {
		return false
	}
	r := rs[len(rs)-1]
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}
