package desktop

import (
	"log"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/agentsync"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
)

// agentSyncDebounce 是模型同步的防抖窗口：设置页连续切换默认平台/默认模型时，
// 窗口内的多次请求合并成窗口结束后的一次同步。
const agentSyncDebounce = 400 * time.Millisecond

// modelSyncScheduler 把高频的配置变更触发合并成一次同步：防抖窗口内的重复
// 请求只保留最后一次；同步执行中的请求记为 pending，本次跑完立即补跑一次
//（最多积压一次，不无限排队）。
type modelSyncScheduler struct {
	mu      sync.Mutex
	timer   *time.Timer
	running bool
	pending bool
	delay   time.Duration
	syncFn  func()
}

func newModelSyncScheduler(delay time.Duration, syncFn func()) *modelSyncScheduler {
	return &modelSyncScheduler{delay: delay, syncFn: syncFn}
}

// schedule 请求一次同步（并发安全）。
func (s *modelSyncScheduler) schedule() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.pending = true
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.delay, s.fire)
}

func (s *modelSyncScheduler) fire() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	// running/pending 复位放 defer：syncFn panic 不能让调度器永久卡死
	//（running 永真后所有 schedule 只累积 pending，本会话同步失效）。
	defer func() {
		s.mu.Lock()
		s.running = false
		again := s.pending
		s.pending = false
		if again {
			s.timer = time.AfterFunc(0, s.fire)
		}
		s.mu.Unlock()
	}()

	s.syncFn()
}

// agentSyncMu 收口本进程全部 agentsync 落盘调用：调度器触发的 SyncAll 与
// SetToolSyncConfig/SetModelConfig 的立即 SyncTool 都是对同一批文件
//（~/.claude/settings.json、models.json、opencode.json）的读-改-写，
// writeAtomic 只防半截文件不防交错 RMW 丢条目，必须串行化。
var agentSyncMu sync.Mutex

func syncAllSerialized(home string, in agentsync.SyncInput, installed []string) error {
	agentSyncMu.Lock()
	defer agentSyncMu.Unlock()
	return agentsync.SyncAll(home, in, installed)
}

func syncToolSerialized(home, toolID string, in agentsync.SyncInput) error {
	agentSyncMu.Lock()
	defer agentSyncMu.Unlock()
	return agentsync.SyncTool(home, toolID, in)
}

// scheduleAgentSync 请求一次防抖合并的模型同步（惰性创建调度器，测试注入的
// App 也安全）。启动首轮同步与「只清不建」清理不走此路径——它们必须即时。
func (a *App) scheduleAgentSync() {
	a.mu.Lock()
	if a.agentSyncSched == nil {
		a.agentSyncSched = newModelSyncScheduler(agentSyncDebounce, a.syncAgentModels)
	}
	s := a.agentSyncSched
	a.mu.Unlock()
	s.schedule()
}

// syncAgentModels 把当前模型平台写入已安装（或已有配置）的 OpenCode/CodeBuddy/Claude。
// 失败只记日志语义：调用方不因同步失败回滚配置保存；但错误必须留痕，
// 否则目标文件被外部改坏（如 BOM）导致的静默冻结无从排查。
func (a *App) syncAgentModels() {
	o := a.snapshot()
	if err := syncAllSerialized(o.Home, agentsync.InputFromModel(o.Config.Model), installedToolIDs(a.toolsSnapshot())); err != nil {
		log.Printf("agentsync: 模型平台同步失败: %v", err)
	}
}

// syncAgentModelsAfterSave 处理模型配置保存后的落盘：通常按新配置同步；但若
// 本次保存把最后一点 kshell 托管痕迹也删掉了（删光平台并禁用），必须先用旧
// 平台做一次「只清不建」的同步，否则历史注入物会永久残留、指向已删端点。
// 不能用「永久 managed 标记」代替：已回退用户每次启动都会被清掉 ccswitch 配置。
func (a *App) syncAgentModelsAfterSave(prev config.ModelConfig) {
	o := a.snapshot()
	next := agentsync.InputFromModel(o.Config.Model)
	if !next.Managed() && agentsync.InputFromModel(prev).Managed() {
		cleanup := agentsync.SyncInput{Enabled: false, Platforms: prev.Platforms}
		if err := syncAllSerialized(o.Home, cleanup, installedToolIDs(a.toolsSnapshot())); err != nil {
			log.Printf("agentsync: 模型平台清理同步失败: %v", err)
		}
		return
	}
	if err := syncAllSerialized(o.Home, next, installedToolIDs(a.toolsSnapshot())); err != nil {
		log.Printf("agentsync: 模型平台同步失败: %v", err)
	}
}

// syncAgentModelTool 安装成功后只同步刚装上的工具。
func (a *App) syncAgentModelTool(toolID string) {
	o := a.snapshot()
	if err := syncToolSerialized(o.Home, toolID, agentsync.InputFromModel(o.Config.Model)); err != nil {
		log.Printf("agentsync: %s 同步失败: %v", toolID, err)
	}
}

func installedToolIDs(tools []discovery.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if t.Installed {
			out = append(out, t.ID)
		}
	}
	return out
}
