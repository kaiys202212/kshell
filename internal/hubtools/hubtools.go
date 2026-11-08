// Package hubtools 实现 AgentHub 自主模型工具化：把用户对数据的操作意图
// 路由到本地工具调用，而不是让远端模型直接触碰工作区。后续在此包补充
// llm.go（OpenAI 兼容客户端）、tools.go（工具注册）、confirm.go（确认门禁）、
// router.go（意图路由）等。
//
// 本包不依赖 internal/hub / internal/desktop（避免低层包反向依赖上层），
// 配置由 desktop 层从 internal/config 映射为视图结构 Config 后传入。
package hubtools

import (
	"strings"

	"github.com/yangk/kshell/internal/config"
)

// Logf 是宿主注入的日志钩子（desktop 侧重定向到 ~/.kshell/logs/desktop.log），
// 用于观测工具化链路是否运行、降级原因与工具调用——此前全链路零日志，
// 掩盖了配置失配类静默回归。nil（默认，含测试）时静默。
var Logf func(format string, args ...any)

func logf(format string, args ...any) {
	if Logf != nil {
		Logf(format, args...)
	}
}

// Log 是 logf 的导出入口：desktop 桥层打点与包内同口径，Logf 未注入时静默。
func Log(format string, args ...any) { logf(format, args...) }

// Config 是自主模型链路的视图配置。hubtools 只关心这三个字段，
// 不直接持有整个 config.ModelConfig，便于测试与后续替换来源。
type Config struct {
	BaseURL string // OpenAI 兼容端点（含 /v1 风格）
	APIKey  string
	Model   string
}

// Ready 判断自主模型链路是否就绪：三者去空白后均非空。
// 就绪与否决定 AgentHub 输入是否启用意图路由，未就绪时走原有直连对话。
func Ready(c Config) bool {
	return strings.TrimSpace(c.BaseURL) != "" &&
		strings.TrimSpace(c.APIKey) != "" &&
		strings.TrimSpace(c.Model) != ""
}

// FromModelConfig 把 internal/config 的 ModelConfig 映射为视图 Config：
// 端点与密钥取默认平台（ActivePlatform：DefaultPlatform 命中，否则第一个），
// 模型取顶层 ActiveModel（与默认平台配对写入，见 chat 切模型逻辑）。
// ok 表示映射后链路就绪；即便不就绪也返回已映射的字段，方便上层展示缺了什么。
// 放在本包而非 desktop 层：internal/config 是叶子包，此处导入不产生循环依赖，
// 且映射规则与「就绪」定义强相关，放一起避免两处漂移。
// 注意：归一化后的配置顶层 legacy 字段恒为空（config normalize 迁入平台后清空），
// 端点必须从平台节点读取。
func FromModelConfig(cfg config.ModelConfig) (Config, bool) {
	plat, _ := cfg.ActivePlatform() // 无平台时 plat 为零值，BaseURL/APIKey 自然为空，Ready 兜底
	c := Config{
		BaseURL: strings.TrimSpace(plat.OpenAIBaseURL),
		APIKey:  strings.TrimSpace(plat.APIKey),
		Model:   strings.TrimSpace(cfg.ActiveModel),
	}
	return c, Ready(c)
}
