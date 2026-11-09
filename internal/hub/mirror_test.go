package hub

import "testing"

func TestMirrorPolicyEnterMatchDiscard(t *testing.T) {
	p := NewMirrorPolicy()
	if p.Active() {
		t.Fatal("初始应为非聚合")
	}
	p.Enter([]SessionRef{
		{ChatID: "c1", SessionID: "s1", Title: "A"},
		{ChatID: "c2", SessionID: "s2", Title: "B"},
	})
	if !p.Active() {
		t.Fatal("Enter 两个目标后应为聚合")
	}
	if p.Count() != 2 {
		t.Fatalf("Count = %d, want 2", p.Count())
	}
	// SessionID / ChatID / termKey 派生三种维度命中等价。
	if !p.Match("c1", "", "") || !p.Match("", "", "s2") || !p.Match("", "session:s1", "") {
		t.Fatal("Match 应命中已登记目标")
	}
	if p.Match("c9", "", "") || p.Match("", "session:s9", "") || p.Match("", "", "s9") {
		t.Fatal("未登记目标不得命中")
	}
	p.EnterSinceTs(12345)
	if got := p.SinceTs(); got != 12345 {
		t.Fatalf("SinceTs = %d, want 12345", got)
	}
	p.Discard()
	if p.Active() || p.Match("c1", "", "") {
		t.Fatal("Discard 后应清空")
	}
	// Enter 单目标 = 回到非聚合(单会话不属于聚合语义)。
	p.Enter([]SessionRef{
		{ChatID: "c1", SessionID: "s1"},
		{ChatID: "c2", SessionID: "s2"},
	})
	if !p.Active() {
		t.Fatal("两目标 Enter 后应为聚合")
	}
	p.Enter([]SessionRef{{ChatID: "c1", SessionID: "s1"}})
	if p.Active() {
		t.Fatal("单目标 Enter 应回到非聚合")
	}
}

func TestMirrorPolicyReEnterReplaces(t *testing.T) {
	p := NewMirrorPolicy()
	p.Enter([]SessionRef{
		{ChatID: "c1", SessionID: "s1"},
		{ChatID: "c2", SessionID: "s2"},
	})
	p.Enter([]SessionRef{
		{ChatID: "c3", SessionID: "s3"},
		{ChatID: "c4", SessionID: "s4"},
	})
	if p.Match("c1", "", "") {
		t.Fatal("重复 Enter 应替换旧目标集")
	}
	if !p.Match("c3", "", "") {
		t.Fatal("新目标集应生效")
	}
}

func TestMirrorPolicyDuplicateKeyFolds(t *testing.T) {
	p := NewMirrorPolicy()
	p.Enter([]SessionRef{
		{ChatID: "c1", SessionID: "s1"},
		{ChatID: "c2", SessionID: "s1"},
	})
	if p.Count() != 1 {
		t.Fatalf("同 SessionID 重复引用应折叠, want 1, got %d", p.Count())
	}
}

func TestMirrorPolicyTermKeyNonSessionPrefix(t *testing.T) {
	p := NewMirrorPolicy()
	p.Enter([]SessionRef{
		{ChatID: "c1", SessionID: "s1"},
		{ChatID: "c2", SessionID: "s2"},
	})
	if p.Match("", "new:1", "") {
		t.Fatal("termKey 非 session: 前缀不得命中")
	}
}

func TestMirrorPolicyMissingIdentifiers(t *testing.T) {
	p := NewMirrorPolicy()
	p.Enter([]SessionRef{
		{ChatID: "c1", SessionID: "s1"},
		{ChatID: "x", SessionID: "s0"},
	})
	if p.Count() != 2 {
		t.Fatalf("Count = %d, want 2", p.Count())
	}
	// ChatID 与 SessionID 皆空的引用不得入库(空 key 会误命中全部空归因消息);
	// 重复 Enter 替换旧集,仅有效 key({ChatID:"c2"})入库。
	p.Enter([]SessionRef{
		{ChatID: "", SessionID: ""},
		{ChatID: "c2", SessionID: ""},
	})
	if p.Count() != 1 {
		t.Fatalf("仅含有效 key 的引用入库, want 1, got %d", p.Count())
	}
	if p.Active() {
		t.Fatal("单有效目标不构成聚合")
	}
}
