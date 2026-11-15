package agentcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/yangk/kshell/internal/agentapi"
)

// 退出码约定（调用 agent 的机器信号）：
// 0 成功；1 一般错误；2 needs-confirm（CreateNL 歧义 / RouteDecide=ask）；
// 3 未就绪（桌面端未运行 / LLM 未配置）。
const (
	exitOK       = 0
	exitErr      = 1
	exitConfirm  = 2
	exitNotReady = 3
)

// cmdEnv 承载命令执行的输出与连接工厂（测试注入点）。
type cmdEnv struct {
	out, err io.Writer
	connect  func() (*Client, error)
}

// Main 是 CLI 入口：解析 agent 子命令并返回进程退出码。
func Main(args []string) int {
	return Run(args, os.Stdout, os.Stderr, Connect)
}

// Run 是 Main 的可注入形态（测试/嵌入用）：连接工厂与输出流均可替换。
func Run(args []string, stdout, stderr io.Writer, connect func() (*Client, error)) int {
	e := &cmdEnv{out: stdout, err: stderr, connect: connect}
	return e.run(args)
}

// run 分发子命令。
func (e *cmdEnv) run(args []string) int {
	if len(args) == 0 {
		e.usage()
		return exitErr
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list":
		return e.cmdList(rest, false)
	case "search":
		return e.cmdList(rest, true)
	case "info":
		return e.cmdInfo(rest)
	case "output":
		return e.cmdOutput(rest)
	case "watch":
		return e.cmdWatch(rest)
	case "send":
		return e.cmdSend(rest)
	case "stop":
		return e.cmdStop(rest)
	case "recall":
		return e.cmdRecall(rest)
	case "models":
		return e.cmdModels(rest)
	case "model":
		return e.cmdSetModel(rest)
	case "perms":
		return e.cmdPerms(rest)
	case "perm":
		return e.cmdGrant(rest)
	case "create":
		return e.cmdCreate(rest)
	case "open":
		return e.cmdOpen(rest)
	case "new":
		return e.cmdNew(rest)
	case "route":
		return e.cmdRoute(rest)
	case "help", "-h", "--help":
		e.usage()
		return exitOK
	default:
		fmt.Fprintf(e.err, "kshell agent: 未知命令 %q\n", cmd)
		e.usage()
		return exitErr
	}
}

// usage 输出命令清单。
func (e *cmdEnv) usage() {
	fmt.Fprint(e.out, `kshell agent —— 供外部 agent 操作 kshell 的命令行（硬依赖桌面端运行）

用法: kshell agent <命令> [参数]

会话:  list [--scope active|disk|archived|all] [--tool id] [--ws path] [--json]
       search <关键字> [--json]
       info <id>
输出:  output [--tail N] [--since TS] [--last] <id>   # 默认 NDJSON 逐条；--last 纯文本
       watch <id>                                     # NDJSON 事件流持续输出（Ctrl+C 断开）
操控:  send <id> <text>                     # text 为 - 时从 stdin 读
       stop <id> | recall <msgID>
       models <id> [--json] | model <id> <modelID>
       perms <id> [--json] | perm <id> <reqID> --allow|--deny
创建:  create --tool <id> --ws <路径> [--model m] | open <sessionID>
       new "<自然语言>" [--confirm '<plan JSON>' | --confirm-file <路径>]
路由:  route [--send] "<消息>"

退出码: 0 成功 | 1 错误 | 2 需要确认 | 3 未就绪（桌面端未运行/LLM 未配置）
`)
}

// connectOrFail 连接桌面端服务；失败输出提示并返回 false。
func (e *cmdEnv) connectOrFail() (*Client, int) {
	c, err := e.connect()
	if err != nil {
		fmt.Fprintf(e.err, "kshell agent: 无法连接桌面端（%v）——请先启动 kshell 桌面端\n", err)
		return nil, exitNotReady
	}
	return c, exitOK
}

// fail 按错误类别输出与定退出码。
func (e *cmdEnv) fail(err error) int {
	switch {
	case IsNotReachable(err):
		fmt.Fprintf(e.err, "kshell agent: 桌面端服务不可达（%v）——请先启动 kshell 桌面端\n", err)
		return exitNotReady
	case errors.Is(err, agentapi.ErrLLMNotReady):
		fmt.Fprintf(e.err, "kshell agent: LLM 未配置（%v）——请在设置页配置自主模型\n", err)
		return exitNotReady
	default:
		fmt.Fprintf(e.err, "kshell agent: %v\n", err)
		return exitErr
	}
}

// printJSON 缩进序列化到 stdout。
func (e *cmdEnv) printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(e.out, string(b))
}

// writeNDJSON 把切片逐行 JSON 输出。
func (e *cmdEnv) writeNDJSON[T any](items []T) {
	enc := json.NewEncoder(e.out)
	for _, it := range items {
		_ = enc.Encode(it)
	}
}

// cmdList / cmdSearch 共用：列表输出（--json 数组，否则人读表格）。
func (e *cmdEnv) cmdList(args []string, search bool) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(e.err)
	scope := fs.String("scope", "all", "会话范围 active|disk|archived|all")
	tool := fs.String("tool", "", "按工具 ID 过滤")
	ws := fs.String("ws", "", "按工作区路径过滤")
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	var sessions []agentapi.Session
	var err error
	if search {
		sessions, err = c.Search(strings.Join(fs.Args(), " "))
	} else {
		sessions, err = c.List(*scope)
	}
	if err != nil {
		return e.fail(err)
	}
	if *tool != "" {
		sessions = filterSessions(sessions, func(s agentapi.Session) bool { return s.Tool == *tool })
	}
	if *ws != "" {
		sessions = filterSessions(sessions, func(s agentapi.Session) bool { return s.Workspace == *ws })
	}
	if *asJSON || search && !*asJSON {
		// search 默认也 JSON（关键字场景多是机器调用）。
		e.printJSON(sessions)
		return exitOK
	}
	fmt.Fprintf(e.out, "%-9s %-10s %s\n", "状态", "工具", "标题 [工作区]")
	for _, s := range sessions {
		id := s.ID
		if id == "" {
			id = s.SessionID
		}
		fmt.Fprintf(e.out, "%-9s %-10s %s [%s] (%s)\n", s.Status, s.Tool, s.Title, s.Workspace, id)
	}
	return exitOK
}

func filterSessions(sessions []agentapi.Session, keep func(agentapi.Session) bool) []agentapi.Session {
	out := sessions[:0]
	for _, s := range sessions {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

// cmdInfo 会话详情 JSON。
func (e *cmdEnv) cmdInfo(args []string) int {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.err, "用法: kshell agent info <id>")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	s, ok, err := c.Info(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if !ok {
		fmt.Fprintf(e.err, "kshell agent: 会话不存在: %s\n", fs.Arg(0))
		return exitErr
	}
	e.printJSON(s)
	return exitOK
}

// cmdOutput 输出拉取：--last 纯文本，否则 NDJSON。
func (e *cmdEnv) cmdOutput(args []string) int {
	fs := flag.NewFlagSet("output", flag.ContinueOnError)
	fs.SetOutput(e.err)
	tail := fs.Int("tail", 0, "尾部条数")
	since := fs.Int64("since", 0, "Ts 水位（增量）")
	last := fs.Bool("last", false, "最近一轮完整回复")
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.err, "用法: kshell agent output [--tail N] [--since TS] [--last] <id>（flag 需在 id 之前）")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	id := fs.Arg(0)
	if *last {
		m, err := c.LastFullOutput(id)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.out, m.Text)
		return exitOK
	}
	msgs, err := c.Messages(id, *tail, *since)
	if err != nil {
		return e.fail(err)
	}
	e.writeNDJSON(msgs)
	return exitOK
}

// cmdWatch 订阅事件流：跳过 open 握手帧，其余逐行输出。
func (e *cmdEnv) cmdWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.err, "用法: kshell agent watch <id>")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	ch, cancel, err := c.Watch(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	defer cancel()
	enc := json.NewEncoder(e.out)
	for ev := range ch {
		if ev.Kind == agentapi.EventOpen {
			continue
		}
		if enc.Encode(ev) != nil {
			return exitOK // stdout 断开（管道关闭）即退出
		}
	}
	return exitOK
}

// cmdSend 发指令：text 为 - 时读 stdin 全量。
func (e *cmdEnv) cmdSend(args []string) int {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() < 2 {
		fmt.Fprintln(e.err, "用法: kshell agent send <id> <text>（text 为 - 时从 stdin 读）")
		return exitErr
	}
	text := strings.Join(fs.Args()[1:], " ")
	if text == "-" {
		b, err := io.ReadAll(bufio.NewReader(os.Stdin))
		if err != nil {
			fmt.Fprintf(e.err, "kshell agent: 读 stdin: %v\n", err)
			return exitErr
		}
		text = strings.TrimRight(string(b), "\r\n")
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	if err := c.Send(fs.Arg(0), text); err != nil {
		return e.fail(err)
	}
	return exitOK
}

// cmdStop / cmdRecall / cmdSetModel / cmdGrant：单参数操控。
func (e *cmdEnv) cmdStop(args []string) int  { return e.oneArg(args, "stop", func(c *Client, id string) error { return c.Stop(id) }) }
func (e *cmdEnv) cmdRecall(args []string) int {
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.err, "用法: kshell agent recall <msgID>")
		return exitErr
	}
	msgID, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		fmt.Fprintf(e.err, "kshell agent: msgID 应为整数: %v\n", err)
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	if err := c.Recall(msgID); err != nil {
		return e.fail(err)
	}
	return exitOK
}

func (e *cmdEnv) cmdSetModel(args []string) int {
	fs := flag.NewFlagSet("model", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(e.err, "用法: kshell agent model <id> <modelID>")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	if err := c.SetModel(fs.Arg(0), fs.Arg(1)); err != nil {
		return e.fail(err)
	}
	return exitOK
}

func (e *cmdEnv) cmdGrant(args []string) int {
	fs := flag.NewFlagSet("perm", flag.ContinueOnError)
	fs.SetOutput(e.err)
	allow := fs.Bool("allow", false, "单次允许（默认拒绝）")
	deny := fs.Bool("deny", false, "单次拒绝")
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 2 || (*allow && *deny) {
		fmt.Fprintln(e.err, "用法: kshell agent perm <id> <reqID> --allow|--deny")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	if err := c.GrantPermission(fs.Arg(0), fs.Arg(1), *allow); err != nil {
		return e.fail(err)
	}
	return exitOK
}

// oneArg 是单参数操控命令的公共形态。
func (e *cmdEnv) oneArg(args []string, name string, fn func(*Client, string) error) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(e.err, "用法: kshell agent %s <id>\n", name)
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	if err := fn(c, fs.Arg(0)); err != nil {
		return e.fail(err)
	}
	return exitOK
}

// cmdModels 模型列表（--json 或表格）。
func (e *cmdEnv) cmdModels(args []string) int {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.SetOutput(e.err)
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.err, "用法: kshell agent models <id> [--json]")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	models, cur, err := c.Models(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if *asJSON {
		e.printJSON(map[string]any{"models": models, "current": cur})
		return exitOK
	}
	fmt.Fprintf(e.out, "当前: %s\n", cur)
	for _, m := range models {
		label := m.Name
		if label == "" {
			label = m.ID
		}
		fmt.Fprintf(e.out, "  %s (%s)\n", label, m.ID)
	}
	return exitOK
}

// cmdPerms 权限请求列表。
func (e *cmdEnv) cmdPerms(args []string) int {
	fs := flag.NewFlagSet("perms", flag.ContinueOnError)
	fs.SetOutput(e.err)
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.err, "用法: kshell agent perms <id> [--json]")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	perms, err := c.PendingPermissions(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if *asJSON {
		e.printJSON(perms)
		return exitOK
	}
	for _, p := range perms {
		fmt.Fprintf(e.out, "%s  %s\n", p.RequestID, p.Title)
	}
	return exitOK
}

// cmdCreate 结构化创建。
func (e *cmdEnv) cmdCreate(args []string) int {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(e.err)
	tool := fs.String("tool", "", "工具 ID（必填）")
	ws := fs.String("ws", "", "工作区路径（必填）")
	model := fs.String("model", "", "模型 ID（可空）")
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if *tool == "" || *ws == "" {
		fmt.Fprintln(e.err, "用法: kshell agent create --tool <id> --ws <路径> [--model m]")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	s, err := c.Create(*tool, *ws, *model)
	if err != nil {
		return e.fail(err)
	}
	e.printJSON(s)
	return exitOK
}

// cmdOpen 激活磁盘会话。
func (e *cmdEnv) cmdOpen(args []string) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.err, "用法: kshell agent open <sessionID>")
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	s, err := c.Open(fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	e.printJSON(s)
	return exitOK
}

// cmdNew 自然语言创建：NeedsConfirm → exit 2；--confirm 走确认路径。
func (e *cmdEnv) cmdNew(args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(e.err)
	confirm := fs.String("confirm", "", "已确认的 plan JSON（内联）")
	confirmFile := fs.String("confirm-file", "", "已确认的 plan JSON 文件路径")
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 && *confirm == "" && *confirmFile == "" {
		fmt.Fprintln(e.err, `用法: kshell agent new "<自然语言>" [--confirm '<plan JSON>']`)
		return exitErr
	}
	text := ""
	if fs.NArg() > 0 {
		text = fs.Arg(0)
	}
	var plan *agentapi.CreatePlan
	if *confirmFile != "" {
		b, err := os.ReadFile(*confirmFile)
		if err != nil {
			fmt.Fprintf(e.err, "kshell agent: 读 confirm 文件: %v\n", err)
			return exitErr
		}
		*confirm = string(b)
	}
	if *confirm != "" {
		plan = &agentapi.CreatePlan{}
		if err := json.Unmarshal([]byte(*confirm), plan); err != nil {
			fmt.Fprintf(e.err, "kshell agent: confirm JSON 非法: %v\n", err)
			return exitErr
		}
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	res, err := c.CreateNL(context.Background(), text, plan)
	if err != nil {
		return e.fail(err)
	}
	e.printJSON(res)
	if res.NeedsConfirm != nil {
		return exitConfirm
	}
	return exitOK
}

// cmdRoute 投递决策：--send 时对 single/group 直接投递；ask → exit 2。
func (e *cmdEnv) cmdRoute(args []string) int {
	fs := flag.NewFlagSet("route", flag.ContinueOnError)
	fs.SetOutput(e.err)
	send := fs.Bool("send", false, "single/group 决策直接投递")
	if err := fs.Parse(args); err != nil {
		return exitErr
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.err, `用法: kshell agent route [--send] "<消息>"（flag 需在消息之前）`)
		return exitErr
	}
	c, code := e.connectOrFail()
	if c == nil {
		return code
	}
	plan, err := c.RouteDecide(context.Background(), fs.Arg(0))
	if err != nil {
		return e.fail(err)
	}
	if *send {
		switch plan.Action {
		case "single", "group":
			if err := c.RouteSend(plan); err != nil {
				return e.fail(err)
			}
		default:
			fmt.Fprintf(e.err, "kshell agent: action %q 无投递语义，未投递\n", plan.Action)
			e.printJSON(plan)
			return exitErr
		}
	}
	e.printJSON(plan)
	if plan.Action == "ask" {
		return exitConfirm
	}
	return exitOK
}
