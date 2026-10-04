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
import { readClipboardPaste, resizeTerminal, writeTerminal } from '../lib/api';
import { terminalTheme } from '../lib/appearance';
import { encodeTerminalInput } from '../lib/base64';
import { composeTerminalPaste, isPasteKey } from '../lib/clipboardPaste';
import { DRAG_MIME, quotePathForShell } from '../lib/dragPath';
import { useAppStore } from '../state/store';
import { registerTerminal, unregisterTerminal } from '../lib/terminalRegistry';
import {
  computeImeClampStyles,
  imeOverflowLockStyles,
  pickVisualCaret,
  shouldResetScrollLeft,
  type CellHint,
} from '../lib/imeAnchor';

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
  const statusRef = useRef(term.Status);
  statusRef.current = term.Status;

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
    // WebView2 经常不给 xterm 隐藏 textarea 派发 paste，Ctrl+V 就被吃掉；
    // 拦截粘贴键改走原生剪贴板：文本直写 PTY，位图落盘后写路径（cursor-agent 认路径）。
    // xterm 自己也在 textarea / .xterm 上听 paste（先注册、冒泡阶段），若只在其后 preventDefault，
    // 浏览器一旦真的派发 paste，就会「xterm 贴一次 + 我们再贴一次」。捕获阶段 stopImmediatePropagation。
    let pasteLock = false;
    const injectClipboard = async () => {
      if (statusRef.current === 'exited') return;
      if (pasteLock) return;
      pasteLock = true;
      try {
        const clip = await readClipboardPaste();
        const text = composeTerminalPaste(clip);
        if (!text) return;
        instance.paste(text);
      } catch {
        // 剪贴板被占用时静默跳过，避免打断正在输入
      } finally {
        window.setTimeout(() => {
          pasteLock = false;
        }, 200);
      }
    };
    instance.attachCustomKeyEventHandler((ev) => {
      if (!isPasteKey(ev)) return true;
      if (ev.type === 'keydown') void injectClipboard();
      return false;
    });
    const onPaste = (e: ClipboardEvent) => {
      e.preventDefault();
      e.stopImmediatePropagation();
      void injectClipboard();
    };
    host.addEventListener('paste', onPaste, true);
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

    // IME 组合全程钳制（第三轮：视觉 caret + 横向溢出锁）：
    // xterm CompositionHelper 会把 .composition-view / textarea 重钉到 buffer 光标；
    // agent TUI 常把硬件光标 park 行尾。优先扫视口反色单元作锚点，找不到再 60% 钳制；
    // 组合期锁 overflow-x，并用 MutationObserver + rAF 对抗 xterm 同帧重定位。
    const textarea = instance.textarea;
    let composing = false;
    let imeRaf = 0;
    let applyingImeClamp = false;
    let imeObserver: MutationObserver | null = null;
    let observedCompositionView: HTMLElement | null = null;

    // xterm IBufferCell.isInverse() → number（0/非 0），语义等同 CSI 7 m 反色
    const collectInverseHints = (): CellHint[] => {
      const hints: CellHint[] = [];
      const buf = instance.buffer.active;
      for (let y = 0; y < instance.rows; y++) {
        // getLine 用缓冲绝对行号；hints 的 y 保持视口相对，与 cursorY / pickVisualCaret 一致
        const line = buf.getLine(buf.viewportY + y);
        if (!line) continue;
        for (let x = 0; x < instance.cols; x++) {
          const cell = line.getCell(x);
          if (cell && typeof cell.isInverse === 'function' && cell.isInverse()) {
            hints.push({ x, y, inverse: true });
          }
        }
      }
      return hints;
    };

    const imeOverflowTargets = (): HTMLElement[] => {
      const nodes = [
        host.querySelector<HTMLElement>('.xterm'),
        host.querySelector<HTMLElement>('.xterm-viewport'),
        host.querySelector<HTMLElement>('.xterm-screen'),
      ];
      return nodes.filter((n): n is HTMLElement => !!n);
    };

    const lockImeOverflow = () => {
      const { overflowX } = imeOverflowLockStyles();
      for (const el of imeOverflowTargets()) {
        if (el.dataset.kshellImePrevOverflowX === undefined) {
          el.dataset.kshellImePrevOverflowX = el.style.overflowX;
        }
        el.style.overflowX = overflowX;
      }
    };

    const restoreImeOverflow = () => {
      for (const el of imeOverflowTargets()) {
        if (el.dataset.kshellImePrevOverflowX === undefined) continue;
        el.style.overflowX = el.dataset.kshellImePrevOverflowX;
        delete el.dataset.kshellImePrevOverflowX;
      }
    };

    const clearImeInlineClamp = (compositionView: HTMLElement | null) => {
      if (textarea) {
        textarea.style.left = '';
        textarea.style.top = '';
      }
      if (compositionView) {
        compositionView.style.left = '';
        compositionView.style.top = '';
        compositionView.style.maxWidth = '';
        compositionView.style.overflow = '';
      }
    };

    const observeImeStyleTargets = (compositionView: HTMLElement | null) => {
      if (!imeObserver || !composing) return;
      if (textarea) {
        imeObserver.observe(textarea, { attributes: true, attributeFilter: ['style'] });
      }
      if (compositionView && compositionView !== observedCompositionView) {
        imeObserver.observe(compositionView, { attributes: true, attributeFilter: ['style'] });
        observedCompositionView = compositionView;
      }
    };

    const stopImeObserver = () => {
      imeObserver?.disconnect();
      imeObserver = null;
      observedCompositionView = null;
    };

    // 仅在值变化时写 style，避免自写入触发 MutationObserver 循环
    const setStyleIfChanged = (el: HTMLElement, prop: 'left' | 'top' | 'maxWidth' | 'overflow', value: string) => {
      if (el.style[prop] !== value) el.style[prop] = value;
    };

    const applyImeClamp = () => {
      const screen = host.querySelector<HTMLElement>('.xterm-screen');
      const viewport = host.querySelector<HTMLElement>('.xterm-viewport');
      const compositionView = host.querySelector<HTMLElement>('.composition-view');
      const buf = instance.buffer.active;
      if (!textarea || !screen) return;

      const caret = pickVisualCaret(
        collectInverseHints(),
        buf.cursorX,
        buf.cursorY,
        instance.cols,
        instance.rows,
      );
      const styles = computeImeClampStyles({
        cols: instance.cols,
        rows: instance.rows,
        cursorX: caret.cursorX,
        cursorY: caret.cursorY,
        viewportWidth: screen.clientWidth,
        viewportHeight: screen.clientHeight,
      });
      if (!styles) return;

      applyingImeClamp = true;
      try {
        const left = `${styles.left}px`;
        const top = `${styles.top}px`;
        setStyleIfChanged(textarea, 'left', left);
        setStyleIfChanged(textarea, 'top', top);
        if (compositionView) {
          setStyleIfChanged(compositionView, 'left', left);
          setStyleIfChanged(compositionView, 'top', top);
          setStyleIfChanged(compositionView, 'maxWidth', `${styles.maxWidth}px`);
          setStyleIfChanged(compositionView, 'overflow', 'hidden');
        }
        lockImeOverflow();
        if (viewport && shouldResetScrollLeft(viewport.scrollLeft)) {
          viewport.scrollLeft = 0;
        }
        observeImeStyleTargets(compositionView);
      } finally {
        // MO 回调是微任务；同步清 flag 会让自写入再次 schedule → 循环。
        // 延后到下一微任务，确保 MO 仍看到 applyingImeClamp=true。
        queueMicrotask(() => {
          applyingImeClamp = false;
        });
      }
    };
    // 同帧内 xterm updateCompositionElements 可能后跑；再排一帧压过
    const scheduleImeClampRaf = () => {
      if (imeRaf) cancelAnimationFrame(imeRaf);
      imeRaf = requestAnimationFrame(() => {
        imeRaf = 0;
        if (composing) applyImeClamp();
      });
    };
    const scheduleImeClamp = () => {
      applyImeClamp();
      scheduleImeClampRaf();
    };
    const startImeObserver = () => {
      stopImeObserver();
      // 组合期 MO 只排 rAF，避免同步 apply → 写 style → MO 重入
      imeObserver = new MutationObserver(() => {
        if (composing && !applyingImeClamp) scheduleImeClampRaf();
      });
      observeImeStyleTargets(host.querySelector<HTMLElement>('.composition-view'));
    };
    const onCompositionStart = () => {
      composing = true;
      startImeObserver();
      scheduleImeClamp();
    };
    const onCompositionUpdate = () => {
      scheduleImeClamp();
    };
    const onCompositionEnd = () => {
      composing = false;
      if (imeRaf) {
        cancelAnimationFrame(imeRaf);
        imeRaf = 0;
      }
      stopImeObserver();
      restoreImeOverflow();
      clearImeInlineClamp(host.querySelector<HTMLElement>('.composition-view'));
    };
    textarea?.addEventListener('compositionstart', onCompositionStart);
    textarea?.addEventListener('compositionupdate', onCompositionUpdate);
    textarea?.addEventListener('compositionend', onCompositionEnd);
    const renderSub = instance.onRender(() => {
      if (composing) scheduleImeClamp();
    });

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
      composing = false;
      if (imeRaf) cancelAnimationFrame(imeRaf);
      stopImeObserver();
      restoreImeOverflow();
      clearImeInlineClamp(host.querySelector<HTMLElement>('.composition-view'));
      textarea?.removeEventListener('compositionstart', onCompositionStart);
      textarea?.removeEventListener('compositionupdate', onCompositionUpdate);
      textarea?.removeEventListener('compositionend', onCompositionEnd);
      host.removeEventListener('paste', onPaste, true);
      renderSub.dispose();
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
