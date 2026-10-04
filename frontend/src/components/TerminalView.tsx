// 中心区内嵌终端：xterm.js + FitAddon 的 React 包装。
// 约定（与设计文档第 5 节对应）：
// - 输出不经本组件订阅：App 全局订阅一次 terminal:data，再按 id 投递到 lib/terminalRegistry；
//   本组件只在挂载时登记、卸载时注销。
// - 组件常挂载、由父级用 hidden 切换可见性：非激活时不做任何销毁，xterm 缓冲与历史都保留。
// - 键盘输入 → writeTerminal(base64)；FitAddon 改变行列 → resizeTerminal（否则 PTY 仍按旧尺寸折行）。
import { useEffect, useRef, useState } from 'react';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import type { TerminalInfo } from '../lib/api';
import { resizeTerminal, writeTerminal } from '../lib/api';
import { terminalTheme } from '../lib/appearance';
import { encodeTerminalInput } from '../lib/base64';
import { DRAG_MIME, quotePathForShell } from '../lib/dragPath';
import { useAppStore } from '../state/store';
import { registerTerminal, unregisterTerminal } from '../lib/terminalRegistry';
import { computeImeAnchor } from '../lib/imeAnchor';

interface Props {
  term: TerminalInfo; // store 里的镜像（Status/ExitCode 决定退出提示）
  active: boolean; // 是否为当前可见页签
}

// 退出提示：只在状态首次变为 exited 时写一次，避免 store 每次 upsert 都往终端里塞一行
const EXITED_HINT = '\r\n\x1b[90m[会话已退出]\x1b[0m\r\n';

// 最小可用行列：窄于此尺寸的 resize 一律不推给 PTY。
// 根因（真机实测）：页签被 hidden 时宿主宽高为 0，FitAddon 会把 0 钳成 cols=2/rows=1，
// 这个退化尺寸一旦推给伪终端，opencode 这类 TUI 会直接崩溃退出（实测退出码 3）。
const MIN_COLS = 20;
const MIN_ROWS = 5;

export default function TerminalView({ term, active }: Props) {
  const termId = term.ID;
  const resolved = useAppStore((s) => s.appearance.resolved);
  const hostRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const exitedHintRef = useRef(false);
  const [dropHint, setDropHint] = useState(false);
  // 渲染期同步（不是 useEffect）：ResizeObserver 回调在 DOM 变更后的布局阶段触发，
  // 必须保证「变隐藏」那一刻回调读到的已经是 false，否则仍会 fit 出退化尺寸。
  const activeRef = useRef(active);
  activeRef.current = active;

  // 依赖只取 termId：store 里 term 的其余字段（Status/ExitCode/Cols）变化不应重建终端实例
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    const instance = new Terminal({
      convertEol: false,
      cursorBlink: true,
      fontFamily: 'Consolas, "Cascadia Mono", monospace',
      fontSize: 13,
      scrollback: 5000,
      // agent TUI（claude code / codebuddy 等 ink 系 CLI）在主缓冲区用 ED2 全屏重绘。
      // 默认语义下 ED2 只擦除视口，重绘与用户滚动交错会让视口与缓冲失位：
      // 滚动条已到底但画面停在别处，TUI 的输入行「再也滚不回来」，改窗口大小才复位。
      // 开启后 ED2 把旧屏内容滚入滚动缓冲（与 Windows Terminal 行为一致），
      // live 区恒在缓冲底部，阅读位置稳定、滚到底必见输入行（xterm 6.0 新选项，实测有效）。
      scrollOnEraseInDisplay: true,
      theme: terminalTheme(resolved),
    });
    const fitAddon = new FitAddon();
    instance.loadAddon(fitAddon);
    instance.open(host);
    termRef.current = instance;
    fitRef.current = fitAddon;

    const dataSub = instance.onData((data) => {
      void writeTerminal(termId, encodeTerminalInput(data));
    });
    const resizeSub = instance.onResize(({ cols, rows }) => {
      // 退化尺寸守卫（见 MIN_COLS 说明）：隐藏态算出的 2x1 绝不能推给 PTY
      if (cols < MIN_COLS || rows < MIN_ROWS) return;
      void resizeTerminal(termId, cols, rows);
    });

    registerTerminal(termId, {
      write: (bytes) => instance.write(bytes),
      focus: () => instance.focus(),
      fit: () => fitAddon.fit(),
    });

    // IME 锚点修复（对应 xtermjs#5734/#5759，上游修复要 7.0 才发布）：
    // xterm 把隐藏 textarea 锚在 buffer 光标处，Windows IME 候选窗跟随其屏幕位置；
    // agent TUI 等待输入时常把光标 park 在行尾，候选窗贴屏幕右缘，会把窗口挤动。
    // compositionstart 时把 textarea 拉回视口内（横向钳制到 60% 宽度处）。
    const textarea = instance.textarea;
    const onCompositionStart = () => {
      const screen = host.querySelector<HTMLElement>('.xterm-screen');
      const buf = instance.buffer.active;
      if (!textarea || !screen) return;
      const anchor = computeImeAnchor({
        cols: instance.cols,
        rows: instance.rows,
        cursorX: buf.cursorX,
        cursorY: buf.cursorY,
        viewportWidth: screen.clientWidth,
        viewportHeight: screen.clientHeight,
      });
      if (!anchor) return;
      textarea.style.left = `${anchor.left}px`;
      textarea.style.top = `${anchor.top}px`;
    };
    textarea?.addEventListener('compositionstart', onCompositionStart);

    // 容器尺寸变化 → 下一帧再 fit：同一帧里可能还有布局变动（三栏拖动、页签切换）。
    // 非激活页签（被 hidden）时宿主尺寸为 0，此时 fit 会算出 2x1 的退化尺寸，直接跳过。
    const observer =
      typeof ResizeObserver === 'function'
        ? new ResizeObserver(() => {
            requestAnimationFrame(() => {
              if (!activeRef.current) return;
              fitAddon.fit();
            });
          })
        : null;
    observer?.observe(host);

    return () => {
      observer?.disconnect();
      textarea?.removeEventListener('compositionstart', onCompositionStart);
      dataSub.dispose();
      resizeSub.dispose();
      unregisterTerminal(termId);
      instance.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, [termId]);

  // 变为可见页签时才适配尺寸并取焦点（放进 rAF：等父级 hidden→visible 的布局完成）
  useEffect(() => {
    if (!active) return;
    const raf = requestAnimationFrame(() => {
      fitRef.current?.fit();
      termRef.current?.focus();
    });
    return () => cancelAnimationFrame(raf);
  }, [active]);

  // 明暗变化时热更新已存在终端实例的配色
  useEffect(() => {
    if (termRef.current) {
      termRef.current.options.theme = terminalTheme(resolved);
    }
  }, [resolved]);

  // 退出提示只写一次；Status 若从 exited 回到 running（重新打开）不重置，由上层重新挂载负责
  useEffect(() => {
    if (term.Status !== 'exited' || exitedHintRef.current) return;
    exitedHintRef.current = true;
    termRef.current?.write(EXITED_HINT);
  }, [term.Status]);

  return (
    <div
      className="relative flex h-full min-h-0 flex-col"
      data-drop-zone={`terminal:${termId}`}
      onDragOver={(e) => {
        if (!e.dataTransfer.types.includes(DRAG_MIME) || term.Status === 'exited') return;
        e.preventDefault();
        setDropHint(true);
      }}
      onDragLeave={() => setDropHint(false)}
      onDrop={(e) => {
        setDropHint(false);
        if (!e.dataTransfer.types.includes(DRAG_MIME) || term.Status === 'exited') return;
        e.preventDefault();
        const p = e.dataTransfer.getData(DRAG_MIME);
        if (!p) return;
        void writeTerminal(termId, encodeTerminalInput(quotePathForShell(p)));
        termRef.current?.focus();
      }}
    >
      {term.Status === 'exited' && (
        <div className="shrink-0 bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          会话已退出（退出码 {term.ExitCode}）
        </div>
      )}
      <div ref={hostRef} className="xterm-host h-full w-full overflow-hidden bg-card" />
      {dropHint && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded border-2 border-dashed border-primary bg-primary/10 text-sm">
          松开插入文件路径
        </div>
      )}
    </div>
  );
}
