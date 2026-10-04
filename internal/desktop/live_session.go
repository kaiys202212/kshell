package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/mcparchive"
	"github.com/yangk/kshell/internal/sessionarchive"
	"github.com/yangk/kshell/internal/terminal"
)

// discoveryNudgeDelays 是用户提交首条消息后的重扫间隔。
// 打开时的 3/8/15s 经常赶在用户发言之前，会话文件要等这一轮才落盘。
var discoveryNudgeDelays = []time.Duration{
	200 * time.Millisecond,
	1500 * time.Millisecond,
	4 * time.Second,
	10 * time.Second,
}

// ensureSessionHooks 把「首条消息改标题」和 MCP 注入接到终端/聊天管理器上。只做一次。
func (a *App) ensureSessionHooks() {
	a.hooksOnce.Do(func() {
		h, err := mcparchive.StartHub(func(ref, summary string) {
			a.Emit("archive:suggest", map[string]any{"ref": ref, "summary": summary})
		})
		if err == nil {
			a.mu.Lock()
			a.archiveHub = h
			a.mu.Unlock()
		}
		if m := a.terminals(); m != nil {
			m.SetOnInfo(func(info terminal.Info) {
				a.Emit("terminal:meta", info)
				if info.Prompted {
					a.nudgeDiscovery()
				}
			})
			m.SetPrepareSpec(a.prepareTermSpec)
		}
		if m := a.chats(); m != nil {
			m.SetOnInfo(func(info chat.Info) {
				a.Emit("chat:meta", info)
				if info.Prompted {
					a.nudgeDiscovery()
				}
			})
			m.SetMCPFactory(a.chatMCPServers)
		}
	})
}

func (a *App) nudgeDiscovery() {
	for _, d := range discoveryNudgeDelays {
		delay := d
		time.AfterFunc(delay, func() { _, _ = a.ScanSessions() })
	}
}

func (a *App) prepareTermSpec(id string, info terminal.Info, spec terminal.Spec) terminal.Spec {
	if info.ToolID != "claude" || a.archiveHub == nil {
		return spec
	}
	exe, err := os.Executable()
	if err != nil {
		return spec
	}
	path := filepath.Join(a.mcpConfigDir(), "terminal-"+id+".json")
	args := []string{"mcp-archive", "--url", a.archiveHub.URL, "--token", a.archiveHub.Token, "--ref", "terminal:" + id}
	if err := mcparchive.WriteConfig(path, exe, args); err != nil {
		return spec
	}
	spec.Args = mcparchive.PrependConfig("claude", path, spec.Args)
	return spec
}

func (a *App) chatMCPServers(id string) []acp.McpServerStdio {
	if a.archiveHub == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	return []acp.McpServerStdio{{
		Type:    "stdio",
		Name:    "kshell",
		Command: exe,
		Args:    []string{"mcp-archive", "--url", a.archiveHub.URL, "--token", a.archiveHub.Token, "--ref", "chat:" + id},
		Env:     []acp.EnvVariable{},
	}}
}

func (a *App) mcpConfigDir() string {
	if a.opts.Layout.Root != "" {
		return filepath.Join(a.opts.Layout.Root, "mcp")
	}
	if a.opts.Home != "" {
		return filepath.Join(a.opts.Home, ".kshell", "mcp")
	}
	return filepath.Join(os.TempDir(), "kshell-mcp")
}

func (a *App) archives() *sessionarchive.Store {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.archiveStore != nil {
		return a.archiveStore
	}
	path := a.opts.Layout.Archived
	if path == "" {
		root := a.opts.Layout.Root
		if root == "" && a.opts.Home != "" {
			root = filepath.Join(a.opts.Home, ".kshell")
		}
		if root == "" {
			root = os.TempDir()
		}
		path = filepath.Join(root, "archived.json")
	}
	st, err := sessionarchive.Open(path)
	if err != nil {
		st, _ = sessionarchive.Open(filepath.Join(os.TempDir(), "kshell-archived.json"))
	}
	a.archiveStore = st
	return st
}

// ArchivedIDs 返回已归档的磁盘会话 ID。
func (a *App) ArchivedIDs() []string {
	st := a.archives()
	if st == nil {
		return []string{}
	}
	ids := st.IDs()
	if ids == nil {
		return []string{}
	}
	return ids
}

// ArchiveSession 按磁盘会话 ID 归档。
func (a *App) ArchiveSession(id string) error {
	if id == "" {
		return errSessionNotFound
	}
	if err := a.archives().Add(id); err != nil {
		return err
	}
	a.Emit("archive:changed", map[string]any{})
	return nil
}

// RestoreSession 把会话从归档名单移回开发列表。
func (a *App) RestoreSession(id string) error {
	if err := a.archives().Remove(id); err != nil {
		return err
	}
	a.Emit("archive:changed", map[string]any{})
	return nil
}

// ConfirmArchive 用户确认归档。ref 形如 terminal:t1 / chat:c1。
// 磁盘会话还没绑上时先记下，等扫描回填后再写入名单。
func (a *App) ConfirmArchive(ref string) error {
	if id := a.sessionIDForRef(ref); id != "" {
		return a.ArchiveSession(id)
	}
	a.mu.Lock()
	if a.pendingArchive == nil {
		a.pendingArchive = map[string]bool{}
	}
	a.pendingArchive[ref] = true
	a.mu.Unlock()
	return nil
}

func (a *App) sessionIDForRef(ref string) string {
	kind, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return ""
	}
	switch kind {
	case "terminal":
		m := a.terminals()
		if m == nil {
			return ""
		}
		for _, t := range m.List() {
			if t.ID == id {
				return t.SessionID
			}
		}
	case "chat":
		m := a.chats()
		if m == nil {
			return ""
		}
		for _, c := range m.List() {
			if c.ID == id {
				return c.SessionID
			}
		}
	}
	return ""
}

func (a *App) flushPendingArchive() {
	a.mu.Lock()
	if len(a.pendingArchive) == 0 {
		a.mu.Unlock()
		return
	}
	refs := make([]string, 0, len(a.pendingArchive))
	for ref := range a.pendingArchive {
		refs = append(refs, ref)
	}
	a.mu.Unlock()

	var done []string
	for _, ref := range refs {
		id := a.sessionIDForRef(ref)
		if id == "" {
			continue
		}
		if err := a.archives().Add(id); err != nil {
			continue
		}
		done = append(done, ref)
	}
	if len(done) == 0 {
		return
	}
	a.mu.Lock()
	for _, ref := range done {
		delete(a.pendingArchive, ref)
	}
	a.mu.Unlock()
	a.Emit("archive:changed", map[string]any{})
}
