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

// FromModelConfig 把 internal/config 的 ModelConfig 映射为视图 Config，
// 取 OpenAI 兼容三元组并去空白。ok 表示映射后链路就绪；
// 即便不就绪也返回已映射的字段，方便上层展示缺了什么。
// 放在本包而非 desktop 层：internal/config 是叶子包，此处导入不产生循环依赖，
// 且映射规则与「就绪」定义强相关，放一起避免两处漂移。
func FromModelConfig(cfg config.ModelConfig) (Config, bool) {
	c := Config{
		BaseURL: strings.TrimSpace(cfg.OpenAIBaseURL),
		APIKey:  strings.TrimSpace(cfg.APIKey),
		Model:   strings.TrimSpace(cfg.ActiveModel),
	}
	return c, Ready(c)
}
