package chat

import (
	"encoding/json"
	"reflect"
	"testing"
)

// parseElicitationFields 是前端表单渲染的唯一入口：这里守住各 JSON Schema
// 形态的拍平结果（含数字/布尔枚举），避免 agent 用非字符串 const 时整张表单调丢。
func TestParseElicitationFields(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		want   []ElicitationField
	}{
		{
			name:   "string enum -> select",
			schema: `{"type":"object","properties":{"pm":{"type":"string","enum":["bun","npm"]}},"required":["pm"]}`,
			want: []ElicitationField{{
				Key: "pm", Kind: "select", Required: true,
				Options: []ElicitationOption{{Value: "bun", Title: "bun"}, {Value: "npm", Title: "npm"}},
			}},
		},
		{
			name:   "string oneOf -> select with title fallback",
			schema: `{"type":"object","properties":{"x":{"oneOf":[{"const":"a","title":"A"},{"const":"b"}]}}}`,
			want: []ElicitationField{{
				Key: "x", Kind: "select",
				Options: []ElicitationOption{{Value: "a", Title: "A"}, {Value: "b", Title: "b"}},
			}},
		},
		{
			name:   "integer enum -> select with stringified values",
			schema: `{"type":"object","properties":{"n":{"type":"integer","enum":[1,2,3]}}}`,
			want: []ElicitationField{{
				Key: "n", Kind: "select",
				Options: []ElicitationOption{{Value: "1", Title: "1"}, {Value: "2", Title: "2"}, {Value: "3", Title: "3"}},
			}},
		},
		{
			name:   "boolean oneOf -> select with stringified consts",
			schema: `{"type":"object","properties":{"y":{"type":"boolean","oneOf":[{"const":true},{"const":false}]}}}`,
			want: []ElicitationField{{
				Key: "y", Kind: "select",
				Options: []ElicitationOption{{Value: "true", Title: "true"}, {Value: "false", Title: "false"}},
			}},
		},
		{
			name:   "array items enum -> multiselect",
			schema: `{"type":"object","properties":{"tags":{"type":"array","items":{"enum":["a","b"]}}}}`,
			want: []ElicitationField{{
				Key: "tags", Kind: "multiselect",
				Options: []ElicitationOption{{Value: "a", Title: "a"}, {Value: "b", Title: "b"}},
			}},
		},
		{
			name:   "default value preserved",
			schema: `{"type":"object","properties":{"n":{"type":"number","default":3},"s":{"type":"string","default":"hi"}}}`,
			want: []ElicitationField{
				{Key: "n", Kind: "number", Default: float64(3)},
				{Key: "s", Kind: "text", Default: "hi"},
			},
		},
		{
			name:   "empty oneOf const skipped",
			schema: `{"type":"object","properties":{"x":{"oneOf":[{"const":null},{"const":""},{"const":"v"}]}}}`,
			want: []ElicitationField{{
				Key: "x", Kind: "select",
				Options: []ElicitationOption{{Value: "v", Title: "v"}},
			}},
		},
		{
			name:   "unknown type degrades to text",
			schema: `{"type":"object","properties":{"weird":{"type":"weird"}}}`,
			want:   []ElicitationField{{Key: "weird", Kind: "text"}},
		},
		{
			name:   "malformed property degrades to text without dropping form",
			schema: `{"type":"object","properties":{"bad":{"type":123},"ok":{"type":"string"}}}`,
			want: []ElicitationField{
				{Key: "bad", Kind: "text"},
				{Key: "ok", Kind: "text"},
			},
		},
		{
			name:   "empty schema -> empty fields",
			schema: `{}`,
			want:   []ElicitationField{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseElicitationFields(json.RawMessage(tc.schema))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// 空 raw 与非法顶层 JSON 都返回空切片而非 nil，前端 JSON 恒为数组。
func TestParseElicitationFieldsEmptyRaw(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage("not json")} {
		got := parseElicitationFields(raw)
		if got == nil || len(got) != 0 {
			t.Fatalf("fields = %#v", got)
		}
	}
}
