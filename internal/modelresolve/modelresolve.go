// Package modelresolve 把「工具 + 显式模型 + 会话模型 + 配置」翻译成
// launch.ModelOptions，消灭各入口自建 Resolver 的漂移。桌面端已接入，
// TUI 待 Task 7 迁移。平台解析优先级：显式模型 ID → 会话模型反查 →
// 每工具平台（ToolPlatforms）→ 全局激活平台。
package modelresolve

import (
	"strings"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/workbuddy"
)

// Proxier 是 workbuddy 本地代理的最小接口：modelresolve 命中 workbuddy
// 平台时惰性调用 Ensure（不选 workbuddy 模型就不起代理），由实现方提供
// 代理端点与临时 token。实现须保证接口值本身非 nil：把 nil 指针装进接口
// 仍是非 nil 接口（typed-nil 陷阱），此时调用 Ensure 会 panic。
type Proxier interface {
	Ensure() (base, apiKey string, err error)
}

// Query 是一次解析的输入。
type Query struct {
	ToolID       string
	ModelID      string // 显式模型 ID，可带 kshell:<platID>: 前缀
	SessionModel string // 会话真实模型裸名（恢复路径）；空时依次回落每工具平台与全局激活平台
}

// SplitKshellModelID 解析 "kshell:<platID>:<model>" 前缀 ID（自 desktop/chat.go 迁入）。
// platID 不含冒号，model 可含冒号：去 "kshell:" 头后按第一个冒号切分。
// 非前缀或 platID 为空返回 ok=false。
func SplitKshellModelID(id string) (platID, model string, ok bool) {
	rest, found := strings.CutPrefix(id, "kshell:")
	if !found {
		return "", "", false
	}
	i := strings.Index(rest, ":")
	if i <= 0 {
		return "", "", false
	}
	model = rest[i+1:]
	if model == "" {
		// 畸形 ID（如 "kshell:plat:"）：空模型名不应下发给 SetModel，按透传容错。
		return "", "", false
	}
	return rest[:i], model, true
}

// ModelIDLooseEqual 字面相等或 '.'/'-' 归一后相等（自 desktop/restore_model.go 原样迁入）。
// Claude/网关常把配置里的 glm-5.3 写进 JSONL 成 glm-5-3。仅做归一，不改大小写语义：
// desktop 调用方依赖原比较行为。
func ModelIDLooseEqual(a, b string) bool {
	if a == b {
		return true
	}
	return strings.ReplaceAll(a, ".", "-") == strings.ReplaceAll(b, ".", "-")
}

// looseModelEqual 是会话模型反查专用的宽松比较：在 ModelIDLooseEqual 的
// 点号/连字符归一之上再忽略大小写（部分工具回报的模型名与配置大小写不一致）。
// 仅用于 platformByModel，不改变导出函数的原语义。
func looseModelEqual(a, b string) bool {
	return strings.EqualFold(strings.ReplaceAll(a, ".", "-"), strings.ReplaceAll(b, ".", "-"))
}

// platformByID 按平台 ID 查找；查不到返回 false。
func platformByID(cfg config.ModelConfig, id string) (config.ModelPlatform, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return config.ModelPlatform{}, false
	}
	for _, p := range cfg.Platforms {
		if p.ID == id {
			return p, true
		}
	}
	return config.ModelPlatform{}, false
}

// platformByModel 按模型名反查平台：先全平台精确匹配，再宽松匹配。
// 重名（多个平台都有该模型）时优先 ToolPlatforms[toolID] 选定的平台，
// 否则取配置顺序中的第一个。workbuddy 特型平台不排除（模型经本地代理可用）。
func platformByModel(cfg config.ModelConfig, model, toolID string) (config.ModelPlatform, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return config.ModelPlatform{}, false
	}
	preferred := cfg.ToolSync.ToolPlatformID(toolID)
	pick := func(equals func(string) bool) (config.ModelPlatform, bool) {
		var first *config.ModelPlatform
		for i := range cfg.Platforms {
			p := &cfg.Platforms[i]
			hit := false
			for _, m := range p.Models {
				if equals(strings.TrimSpace(m)) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
			if preferred != "" && p.ID == preferred {
				return *p, true
			}
			if first == nil {
				first = p
			}
		}
		if first != nil {
			return *first, true
		}
		return config.ModelPlatform{}, false
	}
	if p, ok := pick(func(candidate string) bool { return candidate == model }); ok {
		return p, true
	}
	return pick(func(candidate string) bool { return looseModelEqual(candidate, model) })
}

// platformSource 记录平台解析的命中来源。Options 据此判断会话模型名能否
// 安全下发：只有「平台正是由该会话模型反查命中」时才注入，否则查不到配置的
// 原生模型名会带着回落平台端点发出去（必失败）。
type platformSource int

const (
	sourceNone platformSource = iota
	sourceModelID
	sourceSessionModel
	sourceToolPlatform
	sourceActivePlatform
)

// resolvePlatform 是 ResolvePlatform 的实现，额外返回命中来源。
func resolvePlatform(cfg config.ModelConfig, q Query) (config.ModelPlatform, platformSource, bool) {
	q.ModelID = strings.TrimSpace(q.ModelID)
	q.SessionModel = strings.TrimSpace(q.SessionModel)
	// 1. 显式模型 ID 的平台前缀（kshell:<platID>:<model>）。
	if platID, _, ok := SplitKshellModelID(q.ModelID); ok {
		if p, found := platformByID(cfg, platID); found {
			return p, sourceModelID, true
		}
	}
	// 2. 会话真实模型反查平台（唯一命中；重名时每工具平台优先）。
	if p, found := platformByModel(cfg, q.SessionModel, q.ToolID); found {
		return p, sourceSessionModel, true
	}
	// 3. 每工具平台映射。必须走 ToolPlatformID：已知工具空/悬空回落旧字段、
	// 未知工具返回空，勿直读 map 绕过迁移契约。
	if p, found := platformByID(cfg, cfg.ToolSync.ToolPlatformID(q.ToolID)); found {
		return p, sourceToolPlatform, true
	}
	// 4. 全局激活平台（ActivePlatformID 命中否则第一个）。
	if p, found := cfg.ActivePlatform(); found {
		return p, sourceActivePlatform, true
	}
	return config.ModelPlatform{}, sourceNone, false
}

// ResolvePlatform 按优先级解析平台；任一环悬空继续兜底，全空不命中。
// 契约：本函数不看 cfg.Enabled（总开关由 Options 负责），纯平台选择可单独复用。
func ResolvePlatform(cfg config.ModelConfig, q Query) (config.ModelPlatform, bool) {
	p, _, ok := resolvePlatform(cfg, q)
	return p, ok
}

// Options 产出 launch.ModelOptions；Resolver 闭包在调用时才解析平台。
// workbuddy preset 平台：端点/密钥改由代理提供（px==nil 时返回 false）。
func Options(cfg config.ModelConfig, q Query, px Proxier) launch.ModelOptions {
	if !cfg.Enabled {
		return launch.ModelOptions{}
	}
	// 统一在入口 trim 一次：同一份 ModelID 既用于切前缀也用作模型名，避免
	// 空白前缀 ID 整串下发（与迁移前 desktop「先 TrimSpace 再切分」语义一致）。
	q.ModelID = strings.TrimSpace(q.ModelID)
	q.SessionModel = strings.TrimSpace(q.SessionModel)
	return launch.ModelOptions{Resolver: func(toolID string) (providers.ModelConfig, bool) {
		// 每次调用都从外层 q 复制一份：调用方未显式给工具 ID 时，用 launch
		// 传入的真实 toolID 兜底每工具平台；不污染后续调用。
		rq := q
		if rq.ToolID == "" {
			rq.ToolID = strings.TrimSpace(toolID)
		}
		p, src, ok := resolvePlatform(cfg, rq)
		if !ok {
			return providers.ModelConfig{}, false
		}
		// 模型名：显式模型 ID 优先（前缀 ID 取裸名）。会话模型只在「平台正是
		// 由它反查命中」时才下发；查不到配置的会话模型（如 agent 原生 provider
		// ID）只参与平台选择，不能凭空注入成 ANTHROPIC_MODEL。
		model := rq.ModelID
		if _, bare, ok := SplitKshellModelID(model); ok {
			model = bare
		}
		if model == "" && src == sourceSessionModel {
			model = rq.SessionModel
		}
		if p.Preset == workbuddy.PresetID {
			if px == nil {
				return providers.ModelConfig{}, false
			}
			base, token, err := px.Ensure()
			if err != nil {
				return providers.ModelConfig{}, false
			}
			return providers.ModelConfig{
				OpenAIBaseURL:    base,
				AnthropicBaseURL: base,
				APIKey:           token,
				Model:            model,
			}, true
		}
		return providers.ModelConfig{
			OpenAIBaseURL:    p.OpenAIBaseURL,
			AnthropicBaseURL: p.AnthropicBaseURL,
			APIKey:           p.APIKey,
			Model:            model,
		}, true
	}}
}
