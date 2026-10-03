package appearance

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWatcherEmitsOnlyOnChange(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	w := NewWatcher(func(Theme) {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	w.interval = time.Millisecond

	seq := []Theme{ThemeDark, ThemeDark, ThemeLight, ThemeLight, ThemeLight}
	var i int
	w.probe = func() Theme {
		mu.Lock()
		defer mu.Unlock()
		v := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return v
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("onChange called %d times, want 1", n)
	}
}

func TestWatcherStopsOnCancel(t *testing.T) {
	w := NewWatcher(nil)
	w.interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
