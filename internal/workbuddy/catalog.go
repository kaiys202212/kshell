package workbuddy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ModelView 是目录中的一个原生聊天模型。
type ModelView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int    `json:"contextWindow"`
	MaxTokens     int    `json:"maxTokens"`
}

// productUA 是 WorkBuddy app 频道的产品 UA：目录接口按 UA 区分客户端频道，
// app 频道的答案最全（CLI 频道是其子集且无价格）。版本后缀惰性，频道即身份。
const productUA = "WorkBuddy/5.8.3 WorkBuddy/5.8.3 CLI/2.161.0"

// routerModelIDs 是网关放在清单里的路由条目（自动选择器），不是真实模型。
var routerModelIDs = map[string]bool{"auto": true, "default-model": true}

// catalogBaseURL 返回凭据所属区的目录基址；两个区的入口域名不同但答案同构。
func catalogBaseURL(cred Credential) string {
	if cred.Region() == "global" {
		return "https://www.workbuddy.ai"
	}
	return "https://www.workbuddy.cn"
}

// originBase 返回 Origin/Referer 用的账单基址。
func originBase(cred Credential) string {
	if cred.Region() == "global" {
		return catalogBaseURL(cred)
	}
	return "https://www.codebuddy.cn"
}

// ParseCatalog 解析 /v3/config 信封：取 data.models，剔除路由条目；
// 若 data.agents 带 cli roster，则按 roster 的声明顺序与集合输出（媒体模型
// 由此自然排除）；无可用 roster 时暴露全部非路由模型，绝不因 roster 缺席而清空。
func ParseCatalog(data []byte) ([]ModelView, error) {
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Models []struct {
				ID            string          `json:"id"`
				Name          string          `json:"name"`
				ContextWindow json.RawMessage `json:"contextWindow"`
				MaxTokens     json.RawMessage `json:"maxTokens"`
			} `json:"models"`
			Agents []struct {
				ID     string   `json:"id"`
				Models []string `json:"models"`
			} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("err.workbuddy.bad_catalog|%v", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("err.workbuddy.catalog_code|%d", envelope.Code)
	}
	byID := make(map[string]ModelView)
	var order []string
	for _, m := range envelope.Data.Models {
		if m.ID == "" || routerModelIDs[m.ID] {
			continue
		}
		if _, dup := byID[m.ID]; dup {
			continue
		}
		v := ModelView{ID: m.ID, Name: m.Name}
		v.ContextWindow, v.MaxTokens = intOf(m.ContextWindow), intOf(m.MaxTokens)
		byID[m.ID] = v
		order = append(order, m.ID)
	}
	var roster []string
	for _, a := range envelope.Data.Agents {
		if a.ID == "cli" && len(a.Models) > 0 {
			roster = a.Models
			break
		}
	}
	out := make([]ModelView, 0, len(order))
	if len(roster) > 0 {
		seen := map[string]bool{}
		for _, id := range roster {
			if v, ok := byID[id]; ok && !seen[id] {
				seen[id] = true
				out = append(out, v)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
		// roster 与 models 完全对不上时按无 roster 处理，避免目录清空。
	}
	for _, id := range order {
		out = append(out, byID[id])
	}
	if len(out) == 0 {
		return nil, errors.New("err.workbuddy.empty_catalog")
	}
	return out, nil
}

func intOf(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return int(n)
	}
	// 部分字段是对象形状：取 default/max/input/output 里第一个可解析数。
	var obj map[string]json.Number
	if json.Unmarshal(raw, &obj) == nil {
		for _, k := range []string{"default", "max", "input", "output"} {
			if v, ok := obj[k]; ok {
				if n, err := v.Int64(); err == nil {
					return int(n)
				}
			}
		}
	}
	return 0
}

// FetchModels 拉取凭据所属区的原生模型目录。
func FetchModels(ctx context.Context, cred Credential, hc *http.Client) ([]ModelView, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogBaseURL(cred)+"/v3/config", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", productUA)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", originBase(cred))
	req.Header.Set("Referer", originBase(cred)+"/")
	if cred.UID != "" {
		req.Header.Set("X-User-Id", cred.UID)
	}
	if cred.Domain != "" {
		req.Header.Set("X-Domain", cred.Domain)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("err.workbuddy.catalog_unreachable|%v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("err.workbuddy.catalog_unreachable|%v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("err.workbuddy.catalog_http|%d", resp.StatusCode)
	}
	return ParseCatalog(body)
}

// chatGatewayBase 返回聊天网关基址（与目录基址不同域：CN 走 copilot.tencent.com）。
func chatGatewayBase(cred Credential) string {
	if cred.Region() == "global" {
		return "https://www.workbuddy.ai"
	}
	return "https://copilot.tencent.com"
}
