package clisync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yangk/kshell/internal/agentcli"
)

// skillMDTemplate 生成 WorkBuddy 的 kshell 技能文档。命令面正文直接复用
// agentcli.UsageText()——单一事实源，CLI 命令变更时文档同步演进；
// 版本行写进正文，应用升级后内容哈希变化自然触发重写。
const skillMDTemplate = `---
name: kshell
description: Manage kshell agent sessions - list/search/open/create agent coding sessions, send instructions, read or watch output, stop sessions, switch models, grant permissions, and route messages. Use whenever the user asks to operate, inspect, or manage kshell or its agent sessions.
---

# kshell agent CLI

通过 ` + "`kshell agent`" + ` 命令行管理 kshell 中的 agent 会话。kshell 桌面端必须在
本机运行（CLI 是薄客户端，经 ` + "`~/.kshell/agentapi/endpoint.json`" + ` 自动发现连接与
鉴权，无需任何配置）。桌面端未运行时命令返回退出码 3。

## 命令一览

` + "```" + `
%s
` + "```" + `

## 常用工作流

1. ` + "`kshell agent list`" + `（或 ` + "`--json`" + `）找到目标会话 id；
2. ` + "`kshell agent output --last <id>`" + ` 看最近一轮回复，或
   ` + "`kshell agent watch <id>`" + ` 持续订阅事件流（NDJSON）；
3. ` + "`kshell agent send <id> <text>`" + ` 下发指令（text 为 ` + "`-`" + ` 时从 stdin 读）；
4. 会话出现权限请求时用 ` + "`kshell agent perms <id>`" + ` 查看，
   ` + "`kshell agent perm <id> <reqID> --allow|--deny`" + ` 处置；
5. 新建会话用 ` + "`kshell agent create --tool <id> --ws <路径>`" + `，或
   ` + "`kshell agent new \"<自然语言>\"`" + `（退出码 2 表示需要带 --confirm 二次确认）。

## 退出码语义

- 0 成功；1 一般错误；
- 2 需要确认（new 的歧义计划 / route 的 ask 决策）——把 JSON 里的计划呈给用户
  确认后，用 ` + "--confirm" + ` 参数重试；
- 3 未就绪（桌面端未运行 / LLM 未配置）——提示用户先启动 kshell 桌面端。

---

> 本文件由 kshell %s 同步生成；在 kshell 设置页重新同步即可更新。
`

// installSkill 把 SKILL.md 写入 ~/.codebuddy/skills/kshell/。内容与版本绑定
//（版本行参与哈希），升级后重同步即更新；内容相同跳过。
func installSkill(home, ver string) (string, error) {
	dir := skillDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stateError, err
	}
	target := filepath.Join(dir, "SKILL.md")
	content := buildSkillMD(ver)
	if cur, err := os.ReadFile(target); err == nil && string(cur) == string(content) {
		return stateCurrent, nil
	}
	// 临时文件 + rename：技能扫描/Status 读到半截文件的窗口最小化。
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return stateError, err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return stateError, err
	}
	return skillInstalled, nil
}

// buildSkillMD 组装 SKILL.md 内容。Version 为空（异常路径）时用 "unknown" 占位，
// 保证 frontmatter 与版本行始终完整。
func buildSkillMD(ver string) []byte {
	if ver == "" {
		ver = "unknown"
	}
	return []byte(fmt.Sprintf(skillMDTemplate, agentcli.UsageText(), ver))
}
