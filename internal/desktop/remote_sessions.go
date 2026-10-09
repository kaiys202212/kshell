package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// remoteSessions 按工作区 Ref 缓存最近一次远端扫描结果。
type remoteSessionsCache struct {
	byRef map[string][]providers.Session
}

func (a *App) remoteCacheRoot() string {
	o := a.snapshot()
	if o.Layout.Cache != "" {
		return o.Layout.Cache
	}
	if o.CachePath != "" {
		return filepath.Dir(o.CachePath)
	}
	return ""
}

// ScanRemoteSessions 扫描 ssh 工作区远端会话；Session.Workspace 改写为 ssh Ref，
// 以便前端按 tab.id 过滤。非 ssh Ref 返回 err.remote.sessions_not_ssh。
func (a *App) ScanRemoteSessions(wsRef string) ([]providers.Session, error) {
	wsRef = strings.TrimSpace(wsRef)
	kind, connID, remotePath, err := discovery.ParseWorkspaceRef(wsRef)
	if err != nil {
		return nil, err
	}
	if kind != discovery.KindSSH {
		return nil, errors.New("err.remote.sessions_not_ssh")
	}
	c, ok := a.connByID(connID)
	if !ok {
		return nil, errConnNotFound
	}
	o := a.snapshot()
	ps := o.Providers
	if len(ps) == 0 {
		return nil, errNotReady
	}
	ref := discovery.FormatSSHRef(connID, remotePath)
	cacheDir := discovery.RemoteCacheDir(a.remoteCacheRoot(), connID)
	cachePath := ""
	if cacheDir != "" {
		cachePath = filepath.Join(cacheDir, "index.json")
	}

	ctx, cancel := context.WithTimeout(a.sshCtx(), 60*time.Second)
	defer cancel()

	raw, err := discovery.ScanRemoteSessions(
		ctx,
		c,
		discovery.ScanOptions{
			Roots:    o.Config.ScanRoots,
			MaxDepth: o.Config.MaxDepth,
			Exclude:  o.Config.Exclude,
		},
		ps,
		remotePath,
		a.remoteRunner(),
		cachePath,
	)
	if err != nil {
		return nil, err
	}

	out := make([]providers.Session, len(raw))
	for i, s := range raw {
		s.Workspace = ref
		out[i] = s
	}

	a.mu.Lock()
	if a.remoteSessions.byRef == nil {
		a.remoteSessions.byRef = map[string][]providers.Session{}
	}
	a.remoteSessions.byRef[ref] = out
	a.mu.Unlock()

	return out, nil
}

// GetRemoteSessions 返回该 ssh 工作区最近一次 ScanRemoteSessions 的缓存；
// 未扫过返回空切片（不触发扫描）。非 ssh Ref 返回空切片。
func (a *App) GetRemoteSessions(wsRef string) []providers.Session {
	kind, connID, remotePath, err := discovery.ParseWorkspaceRef(strings.TrimSpace(wsRef))
	if err != nil || kind != discovery.KindSSH {
		return []providers.Session{}
	}
	ref := discovery.FormatSSHRef(connID, remotePath)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.remoteSessions.byRef == nil {
		return []providers.Session{}
	}
	list := a.remoteSessions.byRef[ref]
	if list == nil {
		return []providers.Session{}
	}
	return list
}
