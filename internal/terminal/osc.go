package terminal

import (
	"strings"
)

// 本文件实现 Gemini OSC 通知扫描器：Gemini CLI 没有 hooks 机制，
// 任务完成 / 等待确认提示通过终端转义序列发出：
//   - OSC 9 ; <text>          以 BEL (\x07) 或 ST (ESC \) 终止
//   - OSC 777 ; notify ; <title> ; <body>   同终止符（rxvt 桌面通知约定）
//
// 注意 OSC 9;4 是 Windows Terminal 的进度条约定（9;4;state;progress），
// 不代表「等待用户」，必须排除。
//
// 透传性说明：Windows ConPTY 可能吞掉或改写 OSC 序列，这是运行时行为，
// 无法可靠自动化验证——真实透传需人工冒烟确认（冒烟清单 T9 条目）。
// 本扫描器只保证纯解析层正确：字节怎么到，就怎么按块识别。

const (
	// oscMaxPending 是单个未终止序列的缓冲上限：终端输出里出现无终止符的
	// ESC ] 时不能无限攒字节，超限直接丢弃缓冲，防止内存被撑爆。
	oscMaxPending = 4 * 1024
)

// oscScannerState 是扫描器的有限状态机状态。
type oscScannerState int

const (
	oscGround   oscScannerState = iota // 普通字节流
	oscSawESC                          // 普通流中刚看到 ESC（等下一个字节判断是否 OSC 引导）
	oscInBody                          // 已进入 OSC 体（buf 从 ESC ] 起收集）
	oscBodyESC                         // OSC 体中刚看到 ESC（等 \ 结算 ST）
)

// oscHit 是一次 OSC 通知命中的解析结果。
type oscHit struct {
	Summary string // OSC 9 的 text / OSC 777 的 title（title 为空时退回 body）
	Raw     string // 完整原始序列（含 ESC ] 引导与终止符），供调试与透传
}

// oscScanner 是流式增量扫描器：数据按 chunk 到达，序列可能跨 chunk 边界。
// 只观察不消费——返回命中列表，原始字节流由调用方原样继续搬运（不截断不改写）。
// 非并发安全：仅在读协程单 goroutine 内使用。
type oscScanner struct {
	state oscScannerState
	buf   []byte // OSC 体的收集缓冲（含 ESC ] 引导，结算后清空）
}

func newOSCScanner() *oscScanner { return &oscScanner{} }

// reset 丢弃未决缓冲回到普通流状态（结算命中或超限丢弃时调用）。
func (sc *oscScanner) reset() {
	sc.state = oscGround
	sc.buf = sc.buf[:0]
}

// Feed 喂入一个输出 chunk，返回其中识别到的通知命中（可能为空）。
// 输入切片只读，不会被改写。
func (sc *oscScanner) Feed(chunk []byte) []oscHit {
	var hits []oscHit
	for _, b := range chunk {
		switch sc.state {
		case oscGround:
			if b == 0x1b {
				sc.state = oscSawESC
			}
		case oscSawESC:
			switch b {
			case ']':
				sc.state = oscInBody
				sc.buf = append(sc.buf[:0], 0x1b, ']')
			case 0x1b:
				// 连续 ESC：保持等待引导状态
			default:
				sc.state = oscGround
			}
		case oscInBody:
			sc.buf = append(sc.buf, b)
			switch {
			case b == 0x07: // BEL 终止
				if h, ok := settleOSC(sc.buf, 1); ok {
					hits = append(hits, h)
				}
				sc.reset()
			case b == 0x1b:
				sc.state = oscBodyESC
			case len(sc.buf) > oscMaxPending:
				sc.reset() // 无终止符超限：丢弃缓冲防 OOM，继续观察后续字节
			}
		case oscBodyESC:
			sc.buf = append(sc.buf, b)
			if b == '\\' { // ST 终止（buf 里已有 ESC，本字节是 \）
				if h, ok := settleOSC(sc.buf, 2); ok {
					hits = append(hits, h)
				}
				sc.reset()
			} else {
				// ESC 不是终止引导：并回体内容继续收集
				sc.state = oscInBody
				if len(sc.buf) > oscMaxPending {
					sc.reset()
				}
			}
		}
	}
	return hits
}

// settleOSC 结算一个完整序列：buf 形如 ESC ] body + 终止符（termLen 字节）。
// 命中 OSC 9（非 9;4）或 OSC 777;notify 时返回通知摘要与原文。
func settleOSC(buf []byte, termLen int) (oscHit, bool) {
	if len(buf) < 2+termLen {
		return oscHit{}, false
	}
	body := string(buf[2 : len(buf)-termLen])
	switch {
	case strings.HasPrefix(body, "9;"):
		if body == "9;4" || strings.HasPrefix(body, "9;4;") {
			return oscHit{}, false // Windows Terminal 进度条，不是通知
		}
		text := body[2:]
		if text == "" {
			return oscHit{}, false
		}
		return oscHit{Summary: text, Raw: string(buf)}, true
	case strings.HasPrefix(body, "777;notify;"):
		// 777;notify;<title>;<body>：正文里可能还有分号，title 取第 3 段
		parts := strings.SplitN(body, ";", 4)
		title := parts[2]
		if len(parts) < 4 || title == "" {
			return oscHit{}, false
		}
		return oscHit{Summary: title, Raw: string(buf)}, true
	default:
		return oscHit{}, false
	}
}
