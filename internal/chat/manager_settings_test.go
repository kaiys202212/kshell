package chat

import (
	"testing"

	"github.com/yangk/kshell/internal/acp"
)

// settingsMetaConn 记录 SetSettingsMeta 收到的载荷；直接实现 Conn 全集
// （内嵌 fakeConn 会撞字段，这里只重定向 Start 产物即可——改用包装 Backend）。
type settingsMetaConn struct {
	fakeConn
	got string
}

func (c *settingsMetaConn) SetSettingsMeta(raw string) { c.got = raw }

// Open 必须把 factory 产出的 settings 载荷交给连接（claude ACP 的
// _meta.claudeCode.options.settings 注入通路）；factory 为 nil 时零调用。
func TestOpenDeliversSettingsMeta(t *testing.T) {
	b := newTestManagerWithConn(t, `{"claudeCode":{"options":{"settings":"{\"env\":{}}"}}}`)
	if b.got != `{"claudeCode":{"options":{"settings":"{\"env\":{}}"}}}` {
		t.Fatalf("SetSettingsMeta got %q", b.got)
	}
}

func TestOpenNilFactoryNoSettings(t *testing.T) {
	b := newTestManagerWithConn(t, "")
	if b.got != "" {
		t.Fatalf("nil factory 不应调用 SetSettingsMeta, got %q", b.got)
	}
}

// newTestManagerWithConn 建一个 manager，factory 返回 want（want 为空串表示不注册 factory）。
func newTestManagerWithConn(t *testing.T, want string) *settingsMetaConn {
	t.Helper()
	conn := &settingsMetaConn{}
	conn.newStart = acp.SessionStart{SessionID: "sess-new"}
	b := &recordingBackend{inner: &fakeBackend{conn: &conn.fakeConn}, meta: conn}
	m := NewManager(b, nil, nil, nil)
	if want != "" {
		m.SetSettingsMetaFactory(func(string) string { return want })
	}
	if _, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, ""); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return conn
}

// recordingBackend 在 fakeBackend 产物外再包一层记录 SetSettingsMeta 的壳。
type recordingBackend struct {
	inner *fakeBackend
	meta  *settingsMetaConn
}

func (b *recordingBackend) Start(spec Spec, h acp.Handler) (Conn, error) {
	c, err := b.inner.Start(spec, h)
	if err != nil {
		return nil, err
	}
	return &metaRecordingConn{Conn: c, rec: b.meta}, nil
}

type metaRecordingConn struct {
	Conn
	rec *settingsMetaConn
}

func (c *metaRecordingConn) SetSettingsMeta(raw string) { c.rec.got = raw }
