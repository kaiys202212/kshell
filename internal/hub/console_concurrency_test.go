package hub

import (
	"strings"
	"sync"
	"testing"
)

// TestConsoleConcurrentAccess 并发冒烟测试：微信轮询 goroutine 与桌面端装配
// goroutine 可能并发调 Handle / OpenPermissionMenu / SetCurrent。
// go test -race 下此前会报数据竞争（perm/menu/current/lastList/hasCurrent 无锁）。
func TestConsoleConcurrentAccess(t *testing.T) {
	fe := &fakeExecutor{
		listSessions:   []SessionRef{wsRef(0, "c1", "", "", "claude", "A", false)},
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "", "", "claude", "A", true),
	}
	c := NewConsole(fe)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Handle("#?")
				c.Handle("#l")
				c.Handle("hello")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.OpenPermissionMenu("chat1", "req1", "desc")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.SetCurrent(wsRef(0, "c1", "", "", "claude", "A", false))
			}
		}()
	}
	wg.Wait()

	// 并发结束后状态机应仍可用。
	c.SetCurrent(fe.currentSession)
	out := join(c.Handle("#l"))
	if !strings.Contains(out, "[") {
		t.Fatalf("并发后状态机应仍可用: %q", out)
	}
}
