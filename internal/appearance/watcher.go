package appearance

import (
	"context"
	"time"
)

// Watcher 周期性探测系统明暗，仅在变化时回调。字段非导出，测试可直接改。
type Watcher struct {
	interval time.Duration
	probe    func() Theme
	onChange func(Theme)
}

// NewWatcher 创建监听器，onChange 可为 nil。
func NewWatcher(onChange func(Theme)) *Watcher {
	return &Watcher{
		interval: 3 * time.Second,
		probe:    func() Theme { return detectOSTheme() },
		onChange: onChange,
	}
}

// Run 阻塞轮询直到 ctx 取消。首次探测作为基线，不回调。
func (w *Watcher) Run(ctx context.Context) {
	last := w.probe()
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if cur := w.probe(); cur != last {
				last = cur
				if w.onChange != nil {
					w.onChange(cur)
				}
			}
		}
	}
}
