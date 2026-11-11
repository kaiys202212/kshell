package hubtools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// routeTestSessions 是路由用例的会话快照：编号 1..3，覆盖 running/idle/gone。
func routeTestSessions() []SessionBrief {
	return []SessionBrief{
		{Num: 1, Tool: "claude", Title: "A", Status: "running"},
		{Num: 2, Tool: "codex", Title: "B", Status: "idle"},
		{Num: 3, Tool: "gemini", Title: "C", Status: "gone"},
	}
}

// routeServer 起一个只回固定内容的假 OpenAI 端点。
func routeServer(t *testing.T, content string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, mockChoice(content, nil))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// routeCfg 构造指向假端点的路由配置。
func routeCfg(baseURL string) Config {
	return Config{BaseURL: baseURL, APIKey: "sk-test", Model: "test-model"}
}

// TestRouteInputParse 覆盖路由决策的解析与校验：正常群发、越界/非法 action、
// 无 JSON、JSON 夹杂废话、ask、expand 默认与钳制、group 下限、single 目标数、
// 空 question、per_target 未知编号、单发超时降级。
func TestRouteInputParse(t *testing.T) {
	sessions := routeTestSessions()

	cases := []struct {
		name    string
		content string
		wantErr bool
		check   func(t *testing.T, d RouteDecision)
	}{
		{
			name:    "正常群发决策",
			content: `{"action":"group","targets":[1,3],"text":"修bug","per_target":{"1":"修A","3":"修B"}}`,
			check: func(t *testing.T, d RouteDecision) {
				if d.Action != "group" {
					t.Errorf("Action = %q", d.Action)
				}
				if !reflect.DeepEqual(d.Targets, []int{1, 3}) {
					t.Errorf("Targets = %v", d.Targets)
				}
				if d.Text != "修bug" {
					t.Errorf("Text = %q", d.Text)
				}
				if d.PerTargetText["1"] != "修A" || d.PerTargetText["3"] != "修B" {
					t.Errorf("PerTargetText = %v", d.PerTargetText)
				}
			},
		},
		{
			name:    "编号越界",
			content: `{"action":"single","targets":[9],"text":"hi"}`,
			wantErr: true,
		},
		{
			name:    "非法 action",
			content: `{"action":"dance","text":"hi"}`,
			wantErr: true,
		},
		{
			name:    "纯文本无 JSON",
			content: "我觉得应该发到群里",
			wantErr: true,
		},
		{
			name:    "JSON 夹杂前后废话",
			content: `好的，决策如下：{"action":"status"} 以上。`,
			check: func(t *testing.T, d RouteDecision) {
				if d.Action != "status" {
					t.Errorf("Action = %q", d.Action)
				}
			},
		},
		{
			name:    "ask 反问",
			content: `{"action":"ask","question":"改哪个?"}`,
			check: func(t *testing.T, d RouteDecision) {
				if d.Question != "改哪个?" {
					t.Errorf("Question = %q", d.Question)
				}
			},
		},
		{
			name:    "expand 默认条数补 5",
			content: `{"action":"expand","expand_num":2}`,
			check: func(t *testing.T, d RouteDecision) {
				if d.ExpandNum != 2 || d.ExpandCount != 5 {
					t.Errorf("ExpandNum=%d ExpandCount=%d", d.ExpandNum, d.ExpandCount)
				}
			},
		},
		{
			name:    "expand_count 钳 20",
			content: `{"action":"expand","expand_num":2,"expand_count":99}`,
			check: func(t *testing.T, d RouteDecision) {
				if d.ExpandCount != 20 {
					t.Errorf("ExpandCount = %d", d.ExpandCount)
				}
			},
		},
		{
			name:    "group 只有 1 个目标",
			content: `{"action":"group","targets":[1],"text":"hi"}`,
			wantErr: true,
		},
		{
			name:    "single 留空 targets 投当前",
			content: `{"action":"single","text":"继续"}`,
			check: func(t *testing.T, d RouteDecision) {
				if d.Action != "single" || len(d.Targets) != 0 {
					t.Errorf("Action=%q Targets=%v", d.Action, d.Targets)
				}
			},
		},
		{
			name:    "single 恰好 1 个合法目标",
			content: `{"action":"single","targets":[2],"text":"继续"}`,
			check: func(t *testing.T, d RouteDecision) {
				if !reflect.DeepEqual(d.Targets, []int{2}) {
					t.Errorf("Targets = %v", d.Targets)
				}
			},
		},
		{
			name:    "single 2 个目标非法",
			content: `{"action":"single","targets":[1,2],"text":"hi"}`,
			wantErr: true,
		},
		{
			name:    "ask 空 question",
			content: `{"action":"ask","question":"  "}`,
			wantErr: true,
		},
		{
			name:    "per_target 含快照外编号",
			content: `{"action":"group","targets":[1,2],"text":"hi","per_target":{"5":"x"}}`,
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := routeServer(t, c.content)
			d, err := RouteInput(context.Background(), routeCfg(srv), sessions, "hi")
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望校验/解析错误，得到决策 %+v", d)
				}
				return
			}
			if err != nil {
				t.Fatalf("RouteInput: %v", err)
			}
			if c.check != nil {
				c.check(t, d)
			}
		})
	}
}

// TestRouteInputTimeout 验证 2s 降级时限：假端点挂起 3s，调用必须在时限内
// 返回错误，调用方据此直投原文。
func TestRouteInputTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	start := time.Now()
	_, err := RouteInput(context.Background(), routeCfg(srv.URL), routeTestSessions(), "hi")
	if err == nil {
		t.Fatal("超时时应返回错误")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("超时降级未生效，耗时 %v", d)
	}
}

// TestRouteInputPrompt 验证提示词含会话快照、用户原文、JSON 契约字段名、
// 只输出一个 JSON 对象的约束，以及 group≥2 / 不确定时 ask 的指引。
func TestRouteInputPrompt(t *testing.T) {
	sessions := routeTestSessions()
	input := "帮我把登录页的 bug 修一下"
	p := BuildRoutePrompt(sessions, input)

	for _, want := range []string{
		"1 claude A running",
		"2 codex B idle",
		"3 gemini C gone",
		input,
		"只输出一个 JSON 对象",
		"至少 2",
		"ask",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("提示词缺少 %q:\n%s", want, p)
		}
	}
	for _, field := range []string{
		"action", "targets", "text", "per_target", "expand_num", "expand_count", "question",
	} {
		if !strings.Contains(p, field) {
			t.Errorf("契约缺少字段名 %q:\n%s", field, p)
		}
	}
}
