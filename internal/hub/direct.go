package hub

import "strings"

// DefaultDirectPatterns 返回逃生门白名单内置默认集：# 命令族、@ 指名、
// continue 语境词（config.AgentHub.DirectPatterns 空/nil 时使用）。explain
// 不再内置：该类自由文本交管理者循环处理，用户仍可用 DirectPatterns 配置加回。
func DefaultDirectPatterns() []string {
	return []string{"#", "@", "continue"}
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
	// #/＃ 无条件放行：不可经 direct_patterns 配置关闭——# 命令必须由
	// Console.Handle 解析（是否合法命令由 Handle 自行判定）。
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
			// "#" 分支仅为与 @ 对称：input 以 #/＃ 开头已在上方无条件放行。
			if strings.HasPrefix(input, p) {
				return true
			}
			continue
		}
		if isASCIIWordTail(p) {
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

// isASCIIWordTail 报告 pattern 是否以 ASCII 词尾字符结尾（字母/数字/-/_）：
// 是则视为有空格词边界的英文单词，只做整词匹配；否则（中文等）允许前缀匹配。
func isASCIIWordTail(p string) bool {
	rs := []rune(p)
	if len(rs) == 0 {
		return false
	}
	r := rs[len(rs)-1]
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}

// continueConfirms 是「继续类短确认」捷径的白名单：去空白后整词命中即视为
// 用户对上一轮反问/等待会话的继续确认（ASCII 项统一小写，匹配用 EqualFold）。
var continueConfirms = []string{"继续", "继续吧", "continue", "好", "好的", "行", "ok"}

// IsContinueConfirm 判定输入是否为「继续类短确认」：去首尾空白后与白名单
// 整词匹配（大小写不敏感）。供桌面 Hub 捷径直投使用——用户对等待中的会话
// 说「继续」时无需进 LLM 循环，直接投递原文。子串/带后缀变体（如「继续
// 一下」）不命中：确认语义必须明确，模糊输入交循环或待选处理。
func IsContinueConfirm(input string) bool {
	s := strings.TrimSpace(input)
	if s == "" {
		return false
	}
	for _, p := range continueConfirms {
		if strings.EqualFold(s, p) {
			return true
		}
	}
	return false
}
