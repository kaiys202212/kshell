package workbuddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func catalogDoc(models []map[string]any, cliRoster []string) map[string]any {
	data := map[string]any{"models": models}
	if cliRoster != nil {
		agents := []map[string]any{{"id": "cli", "models": cliRoster}}
		data["agents"] = agents
	}
	return map[string]any{"code": 0, "msg": "ok", "data": data}
}

func TestParseCatalogRosterOrder(t *testing.T) {
	doc := catalogDoc([]map[string]any{
		{"id": "glm-5.3", "name": "GLM-5.3", "contextWindow": 1000000, "maxTokens": 48000},
		{"id": "hy3", "name": "Hy3", "contextWindow": 192000, "maxTokens": 64000},
		{"id": "auto", "name": "Auto", "contextWindow": 1},
		{"id": "hunyuan-image-alpha", "name": "Hunyuan Image", "contextWindow": 1, "tag": "text-to-image"},
	}, []string{"hy3", "glm-5.3"})
	raw, _ := json.Marshal(doc)
	models, err := ParseCatalog(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	// cli roster 决定顺序与集合；路由条目 auto 不入列。
	if len(models) != 2 {
		t.Fatalf("应只保留 cli roster 的 2 个模型: %+v", models)
	}
	if models[0].ID != "hy3" || models[1].ID != "glm-5.3" {
		t.Fatalf("roster 顺序不符: %v", models)
	}
	if models[1].ContextWindow != 1000000 || models[1].MaxTokens != 48000 {
		t.Fatalf("上下文窗口字段不符: %+v", models[1])
	}
}

func TestParseCatalogNoRoster(t *testing.T) {
	doc := catalogDoc([]map[string]any{
		{"id": "glm-5.3", "name": "GLM-5.3", "contextWindow": 1e6},
		{"id": "default-model", "name": "Auto"},
	}, nil)
	raw, _ := json.Marshal(doc)
	models, err := ParseCatalog(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(models) != 1 || models[0].ID != "glm-5.3" {
		t.Fatalf("无 roster 时应暴露全部非路由模型: %+v", models)
	}
}

func TestParseCatalogEmpty(t *testing.T) {
	raw, _ := json.Marshal(catalogDoc(nil, nil))
	if _, err := ParseCatalog(raw); err == nil {
		t.Fatal("空目录应当报错")
	}
}

func TestFetchModelsRequest(t *testing.T) {
	var gotPath, gotUA, gotAuth, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotUA = r.URL.Path, r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		gotOrigin = r.Header.Get("Origin")
		w.Header().Set("Content-Type", "application/json")
		raw, _ := json.Marshal(catalogDoc([]map[string]any{
			{"id": "hy3", "name": "Hy3", "contextWindow": 192000},
		}, nil))
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	hc := &http.Client{Transport: rewriteTransport{base: srv.URL}}
	models, err := FetchModels(context.Background(),
		Credential{AccessToken: "tok", UID: "u1", Domain: "www.codebuddy.cn"}, hc)
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(models) != 1 || models[0].ID != "hy3" {
		t.Fatalf("模型不符: %+v", models)
	}
	if gotPath != "/v3/config" {
		t.Fatalf("路径不符: %s", gotPath)
	}
	if gotUA != productUA {
		t.Fatalf("UA 应为 app 频道: %q", gotUA)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("鉴权头不符: %q", gotAuth)
	}
	if !strings.HasPrefix(gotOrigin, "https://") {
		t.Fatalf("Origin 缺失: %q", gotOrigin)
	}
}

// rewriteTransport 把请求改写到本地 httptest 服务，保持原 URL 的路径与头。
type rewriteTransport struct{ base string }

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := strings.TrimSuffix(t.base, "/v3/config")
	req.URL.Host = strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	req.URL.Scheme = "http"
	return http.DefaultTransport.RoundTrip(req)
}

func TestCatalogURLByRegion(t *testing.T) {
	cn := Credential{Domain: "www.codebuddy.cn"}
	if got := catalogBaseURL(cn); got != "https://www.workbuddy.cn" {
		t.Fatalf("CN 目录地址不符: %s", got)
	}
	gl := Credential{Domain: "www.workbuddy.ai"}
	if got := catalogBaseURL(gl); got != "https://www.workbuddy.ai" {
		t.Fatalf("global 目录地址不符: %s", got)
	}
}
