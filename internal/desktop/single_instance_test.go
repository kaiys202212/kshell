package desktop

import (
	"context"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestOnSecondInstanceLaunchShowsWindow(t *testing.T) {
	calls := 0
	orig := windowShow
	windowShow = func(context.Context) { calls++ }
	t.Cleanup(func() { windowShow = orig })

	app := NewAppWith(Options{})
	app.ctx = context.Background()
	app.OnSecondInstanceLaunch(options.SecondInstanceData{})
	if calls != 1 {
		t.Fatalf("WindowShow 调用 %d 次, want 1", calls)
	}
}

func TestOnSecondInstanceLaunchNoopWithoutCtx(t *testing.T) {
	calls := 0
	orig := windowShow
	windowShow = func(context.Context) { calls++ }
	t.Cleanup(func() { windowShow = orig })

	app := NewAppWith(Options{})
	app.OnSecondInstanceLaunch(options.SecondInstanceData{})
	if calls != 0 {
		t.Fatalf("ctx 未就绪不应 Show, got %d", calls)
	}
}
