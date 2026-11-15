package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/mcparchive"
	"github.com/yangk/kshell/internal/mcpssh"
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
		if sh, err := StartSSHHub(a); err == nil && sh != nil {
			a.mu.Lock()
			a.sshHub = sh
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
				// 出站统一原始 ID 空间：SetOnInfo 携带 manager 侧新 ID，翻回链头，
				// 重建后前端按旧 ID upsert 才能命中同一镜像行（与 chat:update 同口径）。
				info = a.emitChatMeta(info)
				// 标题变化（首条消息改标题 / 扫描回填真实标题）时回填需求的会话名快照，
				// 供会话关闭或重启后需求列表仍能展示。无关联需求时幂等忽略。
				// 故意同步落盘：SetSessionTitle 幂等短路（同标题不写），正常是内存命中
				// 零写盘；异步化反而可能旧标题后到覆盖新标题（事件无序）。
				if info.Title != "" {
					if store := a.getRequirementStore(); store != nil {
						_, _ = store.SetSessionTitle(info.ID, info.Title)
					}
				}
				if info.Prompted {
					a.nudgeDiscovery()
				}
			})
			m.SetMCPFactory(a.chatMCPServers)
		}
	})
}

// emitChatMeta 把会话快照推给前端，ID 先翻回链头（与 SetOnInfo 同口径）。
// Manager.Open 成功本身不回调 onInfo：共享命令层（#en / 微信桥）创建或激活
// 会话时前端没有打开结果可登记，必须由打开路径主动推一次，否则 chats 镜像
// 缺行，AgentHub 快捷操作解析不到目标会话。返回翻回链头后的快照，供调用方
// 继续使用（如回填需求的会话标题）。
func (a *App) emitChatMeta(info chat.Info) chat.Info {
	info.ID = a.originalChatID(info.ID)
	a.Emit("chat:meta", info)
	return info
}

func (a *App) nudgeDiscovery() {
	for _, d := range discoveryNudgeDelays {
		delay := d
		time.AfterFunc(delay, func() { _, _ = a.ScanSessions() })
	}
}

func (a *App) prepareTermSpec(id string, info terminal.Info, spec terminal.Spec) terminal.Spec {
	ws := strings.TrimSpace(info.Workspace)
	if ws == "" {
		ws = strings.TrimSpace(spec.Dir)
	}
	archiveRef := ""
	if info.ToolID == "claude" {
		archiveRef = "terminal:" + id
	}
	servers, ok := a.mcpServerDefs(ws, archiveRef)
	if !ok {
		return spec
	}
	if info.ToolID != "claude" {
		servers = mcpServersWithoutArchive(servers)
		if len(servers) == 0 {
			return spec
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return spec
	}
	for i := range servers {
		servers[i].Command = exe
	}
	path := filepath.Join(a.mcpConfigDir(), "terminal-"+id+".json")
	if err := mcparchive.WriteServers(path, servers); err != nil {
		return spec
	}
	newArgs := mcpssh.InjectArgs(info.ToolID, path, spec.Args)
	if slices.Equal(newArgs, spec.Args) {
		// 工具不支持 --mcp-config 时与原先「跳过注入」一致。
		return spec
	}
	spec.Args = newArgs
	return spec
}

func mcpServersWithoutArchive(servers []mcparchive.ServerDef) []mcparchive.ServerDef {
	out := make([]mcparchive.ServerDef, 0, len(servers))
	for _, s := range servers {
		if s.Name == "kshell" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (a *App) chatMCPServers(id string) []acp.McpServerStdio {
	ws := a.chatWorkspace(id)
	servers, ok := a.mcpServerDefs(ws, "chat:"+id)
	if !ok {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	out := make([]acp.McpServerStdio, 0, len(servers))
	for _, s := range servers {
		// 不设 Type：claude-agent-acp 见 "type" 键即按 HTTP 形态处理并丢弃
		// command，导致 claude CLI session/new 即退（-32603）。缺省即 stdio。
		out = append(out, acp.McpServerStdio{
			Name:    s.Name,
			Command: exe,
			Args:    s.Args,
			Env:     []acp.EnvVariable{},
		})
	}
	return out
}

func (a *App) chatWorkspace(id string) string {
	m := a.chats()
	if m == nil {
		return ""
	}
	for _, c := range m.List() {
		if c.ID == id {
			return c.Workspace
		}
	}
	return ""
}

// mcpServerDefs 按工作区组装 archive / ssh MCP 定义（command 由调用方填可执行路径）。
func (a *App) mcpServerDefs(ws, archiveRef string) ([]mcparchive.ServerDef, bool) {
	a.mu.Lock()
	archiveHub := a.archiveHub
	sshHub := a.sshHub
	a.mu.Unlock()

	var servers []mcparchive.ServerDef
	if archiveHub != nil && archiveRef != "" {
		servers = append(servers, mcparchive.ServerDef{
			Name: "kshell",
			Args: []string{"mcp-archive", "--url", archiveHub.URL, "--token", archiveHub.Token, "--ref", archiveRef},
		})
	}
	if sshHub != nil && ws != "" && len(a.allowAgentConnections(ws)) > 0 {
		if tok, err := sshHub.IssueScopedToken(ws); err == nil && tok != "" {
			servers = append(servers, mcparchive.ServerDef{
				Name: "kshell-ssh",
				Args: []string{
					"mcp-ssh",
					"--hub-url", sshHub.URL,
					"--token", tok,
					"--workspace", ws,
				},
			})
		}
	}
	if len(servers) == 0 {
		return nil, false
	}
	return servers, true
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

// BatchArchiveSessions 批量归档，返回实际新增数；有变更只广播一次 archive:changed。
func (a *App) BatchArchiveSessions(ids []string) (int, error) {
	n, err := a.archives().AddMany(ids)
	if err != nil {
		return n, err
	}
	if n > 0 {
		a.Emit("archive:changed", map[string]any{})
	}
	return n, nil
}

// BatchRestoreSessions 批量恢复，返回实际移除数。
func (a *App) BatchRestoreSessions(ids []string) (int, error) {
	n, err := a.archives().RemoveMany(ids)
	if err != nil {
		return n, err
	}
	if n > 0 {
		a.Emit("archive:changed", map[string]any{})
	}
	return n, nil
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
