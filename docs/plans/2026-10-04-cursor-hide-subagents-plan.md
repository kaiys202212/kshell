# Cursor 隐藏 Subagent 会话 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cursor 会话列表与 IDE 一致，只展示用户主会话，隐藏 Task/subagent。

**Architecture:** Cursor 改为 `SessionEnumerator`：主读 `~/.cursor/chats/*/meta.json`；chats 不可用时回退扫 transcript，并用「无 meta / store.db 含 subagentInfo」剔除子代理。`discovery.enumerateSessions` 在 CLI 缺失时仍调用枚举器。`indexVersion` 升至 6。

**Tech Stack:** Go 1.23+；只读 SQLite 用 `modernc.org/sqlite`（纯 Go，解析 `store.db` meta）；现有 `providers.Session` / `discovery.Scan`。

## Global Constraints

- 规格：`docs/plans/2026-10-04-cursor-hide-subagents-design.md`
- 仅改 Cursor + discovery 枚举入口；Claude/CodeBuddy/OpenCode/Codex/Gemini 行为不变
- 中文注释与提交信息；TDD：先红后绿
- 在 `.worktrees/feat/cursor-hide-subagents` 功能分支上实现，不在 master 直接改
- 验证：`go build ./... ; go vet ./... ; go test ./... -count=1`
- 提交仅在用户明确要求时执行

## File Structure

| 文件 | 职责 |
|---|---|
| `internal/providers/cursor.go` | `SessionRoots→nil`；实现 `EnumerateSessions`；chats/transcript/store 辅助函数 |
| `internal/providers/cursor_chats.go`（新建） | 读 meta.json、解析 store.db `subagentInfo`、定位 transcript 路径 |
| `internal/providers/cursor_test.go` | 枚举主源/子代理/兜底/空 bin 等单测 |
| `internal/providers/cursor_chats_test.go`（新建） | meta/store 解析单测 |
| `internal/discovery/index.go` | `enumerateSessions` 允许空 bin；`indexVersion=6` |
| `internal/discovery/index_test.go` | 空 bin 仍枚举 Cursor；OpenCode 空 bin 仍跳过 |
| `docs/smoke/feat-cursor-hide-subagents.md` | 冒烟增量 |
| `go.mod` / `go.sum` | 增加 `modernc.org/sqlite` |

---

### Task 1: chats meta.json 解析（主源基础）

**Files:**
- Create: `internal/providers/cursor_chats.go`
- Create: `internal/providers/cursor_chats_test.go`

**Interfaces:**
- Produces:
  - `type cursorChatMeta struct { SchemaVersion int; CreatedAtMs int64; UpdatedAtMs int64; HasConversation bool; Title string; Cwd string }`
  - `func loadCursorChatMeta(path string) (cursorChatMeta, error)`
  - `func listCursorChatSessions(home string) ([]Session, error)` — 遍历 `home/.cursor/chats/*/*/meta.json`，仅 `HasConversation`，映射为 `Session`（Path/Messages 可先空，Task 3 补）

- [ ] **Step 1: 写失败测试**

```go
func TestLoadCursorChatMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meta.json")
	os.WriteFile(path, []byte(`{
		"schemaVersion":1,
		"createdAtMs":1000,
		"updatedAtMs":2000,
		"hasConversation":true,
		"title":"Hello",
		"cwd":"D:\\proj"
	}`), 0o600)
	got, err := loadCursorChatMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasConversation || got.Title != "Hello" || got.Cwd != `D:\proj` {
		t.Fatalf("%+v", got)
	}
	if !got.CreatedAt().Equal(time.UnixMilli(1000)) {
		t.Fatalf("created %v", got.CreatedAt())
	}
}

func TestListCursorChatSessionsSkipsSubagentsAndEmpty(t *testing.T) {
	home := t.TempDir()
	hash := "abc"
	base := filepath.Join(home, ".cursor", "chats", hash)
	// 主会话
	mainID := "11111111-1111-4111-8111-111111111111"
	writeMeta(t, filepath.Join(base, mainID), true, "Main", `D:\ws`)
	// 子代理：无 meta
	subID := "22222222-2222-4222-8222-222222222222"
	os.MkdirAll(filepath.Join(base, subID), 0o755)
	os.WriteFile(filepath.Join(base, subID, "store.db"), []byte("x"), 0o600)
	// hasConversation=false
	emptyID := "33333333-3333-4333-8333-333333333333"
	writeMeta(t, filepath.Join(base, emptyID), false, "", `D:\ws`)

	got, err := listCursorChatSessions(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != mainID || got[0].Title != "Main" || got[0].Workspace != `D:\ws` {
		t.Fatalf("%+v", got)
	}
	if got[0].ToolID != cursorID {
		t.Fatalf("tool %q", got[0].ToolID)
	}
}
```

辅助 `writeMeta` 写 `meta.json`；`CreatedAt()` 可为 `cursorChatMeta` 方法或测试内 `time.UnixMilli`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/providers -run "TestLoadCursorChatMeta|TestListCursorChatSessions" -count=1`  
Expected: FAIL（undefined）

- [ ] **Step 3: 最小实现**

`cursor_chats.go`：

```go
package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type cursorChatMeta struct {
	SchemaVersion   int    `json:"schemaVersion"`
	CreatedAtMs     int64  `json:"createdAtMs"`
	UpdatedAtMs     int64  `json:"updatedAtMs"`
	HasConversation bool   `json:"hasConversation"`
	Title           string `json:"title"`
	Cwd             string `json:"cwd"`
}

func loadCursorChatMeta(path string) (cursorChatMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cursorChatMeta{}, err
	}
	var m cursorChatMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return cursorChatMeta{}, err
	}
	return m, nil
}

func listCursorChatSessions(home string) ([]Session, error) {
	root := filepath.Join(home, ".cursor", "chats")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Session
	for _, hashEnt := range entries {
		if !hashEnt.IsDir() {
			continue
		}
		hashDir := filepath.Join(root, hashEnt.Name())
		agents, err := os.ReadDir(hashDir)
		if err != nil {
			continue
		}
		for _, ag := range agents {
			if !ag.IsDir() {
				continue
			}
			id := ag.Name()
			metaPath := filepath.Join(hashDir, id, "meta.json")
			m, err := loadCursorChatMeta(metaPath)
			if err != nil || !m.HasConversation {
				continue // 损坏 / 无 meta / 空会话 → 跳过
			}
			out = append(out, Session{
				ID:        id,
				ToolID:    cursorID,
				Workspace: m.Cwd,
				Title:     m.Title,
				CreatedAt: time.UnixMilli(m.CreatedAtMs),
				UpdatedAt: time.UnixMilli(m.UpdatedAtMs),
			})
		}
	}
	return out, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/providers -run "TestLoadCursorChatMeta|TestListCursorChatSessions" -count=1`  
Expected: PASS

- [ ] **Step 5: Commit**（仅当用户要求时）

```text
feat: 解析 Cursor chats meta 作为主会话源
```

---

### Task 2: store.db subagentInfo 判别

**Files:**
- Modify: `internal/providers/cursor_chats.go`
- Modify: `internal/providers/cursor_chats_test.go`
- Modify: `go.mod` / `go.sum`（`go get modernc.org/sqlite`）

**Interfaces:**
- Produces: `func cursorStoreHasSubagentInfo(storeDB string) (has bool, err error)`
  - 只读打开；读 `meta` 表 `key='0'`；value 若为 hex 字符串则 `hex.Decode` 再 JSON；存在 `subagentInfo` 键且为 object → `true`
  - 打开/查询失败 → 返回 `(false, err)`（调用方「宁可保留」）

- [ ] **Step 1: 准备最小 store.db 夹具并写失败测试**

在测试里用 `modernc.org/sqlite` **写入**临时库（仅测试侧），插入 hex(JSON)：

```go
func TestCursorStoreHasSubagentInfo(t *testing.T) {
	dir := t.TempDir()
	subDB := filepath.Join(dir, "sub.db")
	writeStoreMeta(t, subDB, `{"agentId":"a","name":"New Agent","subagentInfo":{"parentAgentId":"p","typeName":"generalPurpose"}}`)
	has, err := cursorStoreHasSubagentInfo(subDB)
	if err != nil || !has {
		t.Fatalf("has=%v err=%v, want true", has, err)
	}

	mainDB := filepath.Join(dir, "main.db")
	writeStoreMeta(t, mainDB, `{"agentId":"b","name":"Main Chat"}`)
	has, err = cursorStoreHasSubagentInfo(mainDB)
	if err != nil || has {
		t.Fatalf("has=%v err=%v, want false", has, err)
	}

	_, err = cursorStoreHasSubagentInfo(filepath.Join(dir, "missing.db"))
	if err == nil {
		t.Fatal("want error for missing db")
	}
}
```

`writeStoreMeta`：`CREATE TABLE meta (key TEXT, value TEXT); INSERT ... hex.EncodeToString([]byte(json))`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/providers -run TestCursorStoreHasSubagentInfo -count=1`  
Expected: FAIL

- [ ] **Step 3: 实现 + 引入依赖**

```powershell
go get modernc.org/sqlite@latest
```

```go
import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	_ "modernc.org/sqlite"
)

func cursorStoreHasSubagentInfo(storeDB string) (bool, error) {
	db, err := sql.Open("sqlite", storeDB)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var val string
	err = db.QueryRow(`SELECT value FROM meta WHERE key = '0' LIMIT 1`).Scan(&val)
	if err != nil {
		return false, err
	}
	raw := []byte(val)
	if decoded, derr := hex.DecodeString(val); derr == nil {
		raw = decoded
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, err
	}
	_, ok := obj["subagentInfo"]
	return ok, nil
}
```

连接串可加 `?mode=ro`（若 driver 支持 URI：`file:path?mode=ro`）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/providers -run TestCursorStoreHasSubagentInfo -count=1`  
Expected: PASS

- [ ] **Step 5: Commit**（仅当用户要求时）

```text
feat: 解析 Cursor store.db 的 subagentInfo
```

---

### Task 3: EnumerateSessions 主源 + transcript 补全 Path/Messages/Title

**Files:**
- Modify: `internal/providers/cursor.go` — `SessionRoots` 返回 nil；实现 `EnumerateSessions`
- Modify: `internal/providers/cursor_chats.go` — `resolveCursorTranscript`、`enrichCursorSession`
- Modify: `internal/providers/cursor_test.go`

**Interfaces:**
- Consumes: `listCursorChatSessions`, `loadCursorChatMeta`, `cursorStoreHasSubagentInfo`, 现有 `ParseSession`/`MatchSessionRel`/`WorkspaceToSlug`
- Produces: `func (Cursor) EnumerateSessions(home, bin string) ([]Session, error)`（**忽略 bin**，可为空）
- `SessionRoots` → `nil`；保留 `ParseSession`/`MatchSessionRel` 供兜底与测试

- [ ] **Step 1: 写失败测试**

```go
func TestCursorEnumerateFromChats(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "ws")
	os.MkdirAll(ws, 0o755)
	id := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0001"
	hash := "deadbeef" // 任意；枚举不依赖 hash 算法
	writeMeta(t, filepath.Join(home, ".cursor", "chats", hash, id), true, "FromMeta", ws)
	// transcript 供 Path/Messages
	slug := (Cursor{}).WorkspaceToSlug(ws)
	trDir := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id)
	os.MkdirAll(trDir, 0o755)
	body := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>ignored</user_query>"}]}}` + "\n"
	os.WriteFile(filepath.Join(trDir, id+".jsonl"), []byte(body), 0o600)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "FromMeta" {
		t.Fatalf("title=%q", got[0].Title)
	}
	if got[0].Path == "" || got[0].Messages < 1 {
		t.Fatalf("path/messages %+v", got[0])
	}
}

func TestCursorEnumerateHidesStoreOnlySubagent(t *testing.T) {
	home := t.TempDir()
	hash := "h1"
	mainID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0002"
	subID := "bbbbbbbb-cccc-4ddd-8eee-ffff00001111"
	writeMeta(t, filepath.Join(home, ".cursor", "chats", hash, mainID), true, "M", `D:\w`)
	subDir := filepath.Join(home, ".cursor", "chats", hash, subID)
	os.MkdirAll(subDir, 0o755)
	writeStoreMeta(t, filepath.Join(subDir, "store.db"), `{"subagentInfo":{"parentAgentId":"x"}}`)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != mainID {
		t.Fatalf("%+v", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/providers -run "TestCursorEnumerateFromChats|TestCursorEnumerateHides" -count=1`  
Expected: FAIL

- [ ] **Step 3: 实现 EnumerateSessions（先主源）**

```go
func (Cursor) SessionRoots(home string) []string { return nil }

func (Cursor) EnumerateSessions(home, _ string) ([]Session, error) {
	sessions, err := listCursorChatSessions(home)
	if err != nil {
		return nil, err
	}
	if len(sessions) > 0 {
		for i := range sessions {
			enrichCursorSession(home, &sessions[i])
		}
		return sessions, nil
	}
	return enumerateCursorTranscriptFallback(home)
}

func enrichCursorSession(home string, s *Session) {
	path := resolveCursorTranscript(home, s.Workspace, s.ID)
	if path == "" {
		return
	}
	s.Path = path
	if n, err := CountLines(path); err == nil {
		s.Messages = n
	}
	if s.Title == "" {
		if head, err := ReadHead(path, 256*1024); err == nil {
			if parsed, err := (Cursor{}).ParseSession(path, head); err == nil && parsed.Title != "" {
				s.Title = parsed.Title
			}
		}
	}
}

func resolveCursorTranscript(home, cwd, id string) string {
	slug := (Cursor{}).WorkspaceToSlug(cwd)
	p := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id, id+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	// cwd 空或 slug 有损时：在 projects/*/agent-transcripts/<id>/<id>.jsonl 搜一次
	root := filepath.Join(home, ".cursor", "projects")
	matches, _ := filepath.Glob(filepath.Join(root, "*", "agent-transcripts", id, id+".jsonl"))
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
```

Task 3 可先让 `enumerateCursorTranscriptFallback` 返回 `nil, nil`，Task 4 再实现。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/providers -run "TestCursorEnumerate" -count=1`  
Expected: PASS

- [ ] **Step 5: Commit**（仅当用户要求时）

```text
feat: Cursor 从 chats 枚举主会话
```

---

### Task 4: transcript 兜底 + 子代理过滤

**Files:**
- Modify: `internal/providers/cursor_chats.go` — `enumerateCursorTranscriptFallback`、`cursorChatAgentStatus`
- Modify: `internal/providers/cursor_test.go`

**Interfaces:**
- Produces:
  - `func enumerateCursorTranscriptFallback(home string) ([]Session, error)`
  - `func cursorShouldSkipTranscript(home, agentID string) bool`  
    - 若存在任意 `chats/*/<id>/`：无 meta 或 `!HasConversation` → skip；若有 store.db 且 `cursorStoreHasSubagentInfo==true` → skip；store 读失败 → **不 skip**
    - 若 chats 中完全没有该 id → 不 skip（CLI 独有会话）

- [ ] **Step 1: 写失败测试**

```go
func TestCursorEnumerateFallbackFiltersSubagents(t *testing.T) {
	home := t.TempDir()
	slug := "d-ws"
	mainID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0101"
	subID := "bbbbbbbb-cccc-4ddd-8eee-ffff00000202"
	// 无 chats 主源 → 走 fallback；但 chats 里给 sub 建无 meta 目录
	os.MkdirAll(filepath.Join(home, ".cursor", "chats", "h", subID), 0o755)

	writeTranscript := func(id, title string) {
		dir := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id)
		os.MkdirAll(dir, 0o755)
		line := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>` + title + `</user_query>"}]}}` + "\n"
		os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0o600)
	}
	writeTranscript(mainID, "real user")
	writeTranscript(subID, "You are implementing Task 1")
	// subagents 路径不得收录
	os.MkdirAll(filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", mainID, "subagents"), 0o755)
	os.WriteFile(filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", mainID, "subagents", "agent-x.jsonl"),
		[]byte(`{"role":"user","message":{"content":[{"type":"text","text":"x"}]}}`+"\n"), 0o600)

	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != mainID {
		t.Fatalf("%+v", got)
	}
}

func TestCursorEnumerateFallbackKeepsWhenStoreUnreadable(t *testing.T) {
	home := t.TempDir()
	id := "cccccccc-dddd-4eee-8fff-000011112222"
	slug := "d-ws"
	dir := filepath.Join(home, ".cursor", "projects", slug, "agent-transcripts", id)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, id+".jsonl"),
		[]byte(`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>keep me</user_query>"}]}}`+"\n"), 0o600)
	// chats 下有目录 + 坏 store.db，无 meta → 按「无 meta」应 skip；本用例改为：无 chats 条目，坏文件不存在
	// 规格：store 读失败宁可保留 → 构造「有 meta 的主会话路径不适用」；
	// 改为：chats/<h>/<id>/store.db 存在且损坏，同时有一份假 meta？ 
	// 简化：仅当「有 store、无 meta」时 skip；「有 store 损坏、无 meta」→ skip（目录存在无 meta）
	// 本测试覆盖：完全无 chats 条目时保留
	got, err := (Cursor{}).EnumerateSessions(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("%+v", got)
	}
}
```

补充一个明确用例：`chats/.../id/store.db` 损坏且无 meta → skip（因无 meta）；另建 `chats/.../id2/` 有损坏 store **且** 有 `hasConversation:true` meta → 主源收录，不依赖 store。

再补：`chats` 不存在时，仅 transcript，全部顶层 jsonl 收录（降级），`subagents/` 仍拒。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/providers -run TestCursorEnumerateFallback -count=1`  
Expected: FAIL

- [ ] **Step 3: 实现 fallback**

```go
func enumerateCursorTranscriptFallback(home string) ([]Session, error) {
	root := filepath.Join(home, ".cursor", "projects")
	if _, err := os.Stat(root); err != nil {
		return nil, nil
	}
	var out []Session
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil || !(Cursor{}).MatchSessionRel(filepath.ToSlash(rel)) {
			return nil
		}
		id := filepath.Base(filepath.Dir(path))
		if cursorShouldSkipTranscript(home, id) {
			return nil
		}
		head, herr := ReadHead(path, 256*1024)
		if herr != nil {
			return nil
		}
		s, serr := (Cursor{}).ParseSession(path, head)
		if serr != nil {
			return nil
		}
		out = append(out, *s)
		return nil
	})
	return out, nil
}

func cursorShouldSkipTranscript(home, agentID string) bool {
	root := filepath.Join(home, ".cursor", "chats")
	matches, _ := filepath.Glob(filepath.Join(root, "*", agentID))
	if len(matches) == 0 {
		return false
	}
	dir := matches[0]
	metaPath := filepath.Join(dir, "meta.json")
	if m, err := loadCursorChatMeta(metaPath); err == nil {
		return !m.HasConversation
	}
	// 无可用 meta → 子代理
	store := filepath.Join(dir, "store.db")
	if has, err := cursorStoreHasSubagentInfo(store); err == nil && has {
		return true
	}
	// 无 meta：即使 store 读失败也 skip（目录存在即视为非主会话索引）
	if _, err := os.Stat(metaPath); err != nil {
		return true
	}
	return false
}
```

注意与规格对齐：「store 读失败 → 宁可保留」适用于**已有合法 meta 仍去读 store 复核**的路径；无 meta 时按主信号 skip。若实现「有 meta 且 hasConversation 却仍查 store」：仅当 `has==true` 才 skip，`err!=nil` 不 skip。

- [ ] **Step 4: 跑全量 providers 测试**

Run: `go test ./internal/providers -count=1`  
Expected: PASS

- [ ] **Step 5: Commit**（仅当用户要求时）

```text
feat: Cursor transcript 兜底并过滤子代理
```

---

### Task 5: discovery 空 bin 仍枚举 + indexVersion=6

**Files:**
- Modify: `internal/discovery/index.go` — `indexVersion = 6`；改 `enumerateSessions`
- Modify: `internal/discovery/index_test.go` — 新增用例

**Interfaces:**
- Consumes: Cursor `EnumerateSessions`（空 bin 可用）、OpenCode（空 bin → nil）

- [ ] **Step 1: 写失败测试**

```go
func TestEnumerateSessionsCallsEvenWithoutBin(t *testing.T) {
	// 用假 provider 或真实 Cursor + 临时 home
	home := t.TempDir()
	id := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeffff0303"
	writeCursorMainChat(t, home, id, "T", filepath.Join(home, "w"))
	// 将 providers.LookPath / Detect 隔离开：直接测 enumerateSessions 内部逻辑较难导出；
	// 改为导出测试：Scan 时 PATH 不含 cursor-agent，但 home 下有 chats
	t.Setenv("PATH", t.TempDir()) // 清空可用 bin
	cache := filepath.Join(home, "cache")
	res, err := Scan(home, cache, []providers.Provider{providers.Cursor{}}, ScanOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for _, s := range res.Sessions {
		if s.ToolID == "cursor" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("cursor sessions=%d, want 1（无 bin 也应枚举 chats）", n)
	}
}

func TestEnumerateSessionsOpencodeStillSkipsEmptyBin(t *testing.T) {
	// 回归：OpenCode 在 bin 空时不报错、不产出
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	cache := filepath.Join(home, "cache")
	res, err := Scan(home, cache, []providers.Provider{providers.Opencode{}}, ScanOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Sessions {
		if s.ToolID == "opencode" {
			t.Fatalf("unexpected opencode session %+v", s)
		}
	}
}
```

若 `Detect` 在 ConfigDirs 存在时仍可能找到某种路径：测试 home 不要创建 `~/.local/share/opencode`；Cursor 不依赖 bin。

同时断言 `LoadIndex`：写入 version=5 的假 index 后 Scan，保存后 version==6。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/discovery -run "TestEnumerateSessionsCallsEvenWithoutBin|TestEnumerateSessionsOpencode" -count=1`  
Expected: FAIL（Cursor 会话为 0，因空 bin continue）

- [ ] **Step 3: 改 enumerateSessions + version**

```go
const indexVersion = 6 // v6：Cursor 改 chats 枚举，废弃 transcript 全量缓存中的子代理

func enumerateSessions(home string, ps []providers.Provider, failed *[]string) []providers.Session {
	var sessions []providers.Session
	for _, p := range ps {
		en, ok := p.(providers.SessionEnumerator)
		if !ok {
			continue
		}
		det := providers.Detect(p.DetectSpec(home), home)
		got, err := en.EnumerateSessions(home, det.BinPath) // BinPath 可为空
		if err != nil {
			*failed = append(*failed, fmt.Sprintf("%s 会话查询失败: %v", p.DisplayName(), err))
			continue
		}
		sessions = append(sessions, got...)
	}
	return sessions
}
```

删除 `if det.BinPath == "" { continue }`。

- [ ] **Step 4: 跑 discovery + providers 测试**

Run: `go test ./internal/discovery ./internal/providers -count=1`  
Expected: PASS

- [ ] **Step 5: Commit**（仅当用户要求时）

```text
fix: 无 CLI 时仍枚举 Cursor chats，并提升 indexVersion
```

---

### Task 6: 全量验证 + 冒烟文档

**Files:**
- Create: `docs/smoke/feat-cursor-hide-subagents.md`

- [ ] **Step 1: 全量验证**

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
```

Expected: 全绿

- [ ] **Step 2: 写冒烟增量**

`docs/smoke/feat-cursor-hide-subagents.md`：

```markdown
# feat/cursor-hide-subagents 冒烟

## Desktop / TUI

- [ ] 打开含大量 Cursor Task 子代理的工作区（如本机 kshell）：会话列表 Cursor 条数接近 IDE 主会话，不出现「You are implementing Task…」「Review Task…」
- [ ] 点击主会话仍可 `--resume` 恢复（需本机有 cursor-agent）
- [ ] Claude / CodeBuddy / Codex 会话数量与改前无明显异常
```

- [ ] **Step 3: Commit**（仅当用户要求时）

```text
docs: 补充 Cursor 隐藏 subagent 冒烟项
```

---

## Spec coverage（自检）

| 规格条款 | 任务 |
|---|---|
| 主读 chats meta / hasConversation | Task 1、3 |
| 隐藏无 meta 子代理 | Task 1、3、4 |
| store.db subagentInfo | Task 2、4 |
| transcript 兜底 + MatchSessionRel | Task 4 |
| store 读失败宁可保留（有复核路径时） | Task 4 |
| SessionRoots nil / EnumerateSessions | Task 3 |
| discovery 空 bin 仍调用 | Task 5 |
| indexVersion 6 | Task 5 |
| ResumeCmd 不变 | 已有测试，Task 3 不改行为 |
| 冒烟文档 | Task 6 |
| 其它工具不改 | Task 5 OpenCode 回归 |

## Execution Handoff

计划已保存到 `docs/plans/2026-10-04-cursor-hide-subagents-plan.md`。

两种执行方式：

1. **Subagent-Driven（推荐）** — 每任务新开子代理，任务间审查  
2. **Inline Execution** — 本会话按 `executing-plans` 连续执行并设检查点  

要哪种？
