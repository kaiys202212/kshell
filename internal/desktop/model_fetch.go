package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ModelsFetchView 是拉取模型列表的结果：Models 为双端合并去重升序；
// Errors 为失败端点的稳定 wire key（单端失败不影响另一端结果）。
type ModelsFetchView struct {
	Models []string `json:"Models"`
	Errors []string `json:"Errors"`
}

// fetchTimeout 拉取模型列表的单请求超时。
const fetchTimeout = 10 * time.Second

// FetchAvailableModels 用前端传入的表单端点与密钥并发拉取 OpenAI 兼容端点与
// Anthropic 端点的可用模型列表并合并去重（编辑未保存也能拉）。
// platformID 当前仅透传日志/未来扩展用，不参与拉取。
func (a *App) FetchAvailableModels(platformID, openaiURL, anthropicURL, apiKey string) (ModelsFetchView, error) {
	oaiURL := strings.TrimRight(strings.TrimSpace(openaiURL), "/")
	antURL := strings.TrimRight(strings.TrimSpace(anthropicURL), "/")
	if oaiURL == "" && antURL == "" {
		return ModelsFetchView{}, fmt.Errorf("err.model_fetch.no_endpoint")
	}
	if platformID != "" {
		log.Printf("[model_fetch] 平台 %q 拉取模型列表", platformID)
	}

	var (
		mu     sync.Mutex
		models []string
		errs   []string
		wg     sync.WaitGroup
		seen   = map[string]bool{}
	)
	collect := func(list []string) {
		mu.Lock()
		for _, id := range list {
			if id != "" && !seen[id] {
				seen[id] = true
				models = append(models, id)
			}
		}
		mu.Unlock()
	}
	fail := func(key string) {
		mu.Lock()
		errs = append(errs, key)
		mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	wg.Add(2)
	go func() {
		defer wg.Done()
		if oaiURL == "" {
			return
		}
		list, err := fetchOpenAIModels(ctx, oaiURL, apiKey)
		if err != nil {
			fail("err.model_fetch.openai_failed")
			return
		}
		collect(list)
	}()
	go func() {
		defer wg.Done()
		if antURL == "" {
			return
		}
		list, err := fetchAnthropicModels(ctx, antURL, apiKey)
		if err != nil {
			fail("err.model_fetch.anthropic_failed")
			return
		}
		collect(list)
	}()
	wg.Wait()

	sort.Strings(models)
	out := ModelsFetchView{Models: models, Errors: errs}
	if models == nil {
		out.Models = []string{}
	}
	// nil slice 会被 JSON 序列化成 null，前端读 length 会崩溃，统一兜底为空数组
	if errs == nil {
		out.Errors = []string{}
	}
	if len(errs) > 0 && len(models) == 0 {
		return out, fmt.Errorf("err.model_fetch.all_failed")
	}
	return out, nil
}

// fetchOpenAIModels 请求 OpenAI 兼容端点的 GET {base}/models。
func fetchOpenAIModels(ctx context.Context, base, apiKey string) ([]string, error) {
	return fetchModelList(ctx, base+"/models", func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	})
}

// fetchAnthropicModels 请求 Anthropic 端点的 GET {base}/v1/models。
func fetchAnthropicModels(ctx context.Context, base, apiKey string) ([]string, error) {
	return fetchModelList(ctx, base+"/v1/models", func(req *http.Request) {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	})
}

// fetchModelList 发起请求并解析 OpenAI 风格 {"data":[{"id":...}]} 响应。
func fetchModelList(ctx context.Context, url string, setHeaders func(*http.Request)) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}
