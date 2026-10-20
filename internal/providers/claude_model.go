package providers

import (
	"encoding/json"
	"strings"
)

// claudeLastModelTailLimit 扫会话尾部找上次助手模型的字节上限。
// 会话文件可达数十 MB；模型字段在近期 assistant 行，尾部足够且便宜。
const claudeLastModelTailLimit = 256 * 1024

// ClaudeLastAssistantModel 从 Claude 会话 JSONL 尾部取最近一次真实助手模型 ID。
// 跳过空串与 "<synthetic>"；读失败或找不到返回空串（调用方静默跳过）。
func ClaudeLastAssistantModel(path string) string {
	tail, err := ReadTail(path, claudeLastModelTailLimit)
	if err != nil || len(tail) == 0 {
		return ""
	}
	lines := strings.Split(string(tail), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Type != "assistant" {
			continue
		}
		m := strings.TrimSpace(rec.Message.Model)
		if m == "" || m == "<synthetic>" {
			continue
		}
		return m
	}
	return ""
}
