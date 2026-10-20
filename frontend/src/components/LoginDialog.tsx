// 登录弹窗：为指定工具打开官方交互式登录命令的内嵌终端（后端 key 固定 login:<toolID>，
// 同工具全局唯一，重复打开复用/原地重启）。与 TerminalView 同约定：
// - 输出不经本组件订阅：App 全局订阅一次 terminal:data，按 id 投递到 terminalRegistry；
//   本组件只在拿到终端 Info 后登记自己的 xterm，卸载时注销。
// - 登录态轮询 CheckToolAuth（2s 一次，10 分钟上限）：探测到 in → 成功提示 +
//   触发后台重扫（工具表实时刷新）+ 自动关闭。
// - 关闭弹窗刻意不杀登录终端进程：登录流程可能仍在进行，进程由后端按 key 复用管理。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { Terminal } from '@xterm/xterm';
import { checkToolAuth, openLoginTerminal, resizeTerminal, scanSessions, writeTerminal } from '../lib/api';
import type { TerminalInfo } from '../lib/api';
import { terminalTheme } from '../lib/appearance';
import { encodeTerminalInput } from '../lib/base64';
import { handleHref } from '../lib/openHref';
import { backendError } from '../lib/errors';
import { useAppStore } from '../state/store';
import { registerTerminal, unregisterTerminal } from '../lib/terminalRegistry';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

interface Props {
  toolID: string;
  toolName: string;
  onClose: () => void;
}

// 登录态轮询间隔与总上限（10 分钟）：上限到了停止轮询，弹窗留给用户手动关闭，
// 不自动判失败——慢网络/多步 OAuth 可能只是更慢。
const POLL_INTERVAL_MS = 2000;
const POLL_CAP_MS = 10 * 60 * 1000;

// 与 TerminalView 相同的退化尺寸守卫：宿主尚未布局完成时 fit 会算出极小行列，
// 推给 PTY 会把登录 TUI 折行错乱。
const MIN_COLS = 20;
const MIN_ROWS = 5;

export default function LoginDialog({ toolID, toolName, onClose }: Props) {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const resolved = useAppStore((s) => s.appearance.resolved);
  const [info, setInfo] = useState<TerminalInfo | null>(null);
  // 打开登录终端失败（含 err.login.unsupported 不支持登录）：模板包一层后展示
  // 翻译后的原因（wire key 经 backendError 直译，非 wire key 原样透出）
  const [openError, setOpenError] = useState('');
  const [polling, setPolling] = useState(true);
  const hostRef = useRef<HTMLDivElement | null>(null);
  // onClose 经 ref 透传：父组件传的是内联箭头函数，直接进依赖会让父每次重渲染
  // 都拆掉轮询定时器重建，轮询节奏失去 2s 间隔的意义
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  });

  // 挂载即开登录终端；key 固定 login:<toolID>，运行中重复打开由后端复用同一会话
  useEffect(() => {
    let cancelled = false;
    openLoginTerminal(toolID, 80, 24)
      .then((opened) => {
        if (!cancelled) setInfo(opened);
      })
      .catch((e: unknown) => {
        // 打开失败（含不支持登录）即停轮询：探测入口已不可用，继续轮询只是空转
        if (!cancelled) {
          setOpenError(t('ui.auth.login_open_failed', { err: backendError(e) }));
          setPolling(false);
        }
      });
    return () => {
      cancelled = true;
    };
    // t 仅用于构造错误模板；语言切换不值得重开终端（后端按 key 幂等复用）
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toolID]);

  // xterm 实例：拿到终端 Info（即拿到 ID）后才建，数据经 terminalRegistry 由全局总线投递
  useEffect(() => {
    const host = hostRef.current;
    const termId = info?.ID;
    if (!host || !termId) return;

    const instance = new Terminal({
      convertEol: false,
      cursorBlink: true,
      fontFamily: 'Consolas, "Cascadia Mono", monospace',
      fontSize: useAppStore.getState().appearance.fontSize ?? 13,
      scrollback: 5000,
      theme: terminalTheme(resolved),
    });
    const fitAddon = new FitAddon();
    instance.loadAddon(fitAddon);
    instance.loadAddon(
      new WebLinksAddon((ev, uri) => {
        ev.preventDefault();
        handleHref(uri);
      }),
    );
    instance.open(host);
    const dataSub = instance.onData((data) => {
      void writeTerminal(termId, encodeTerminalInput(data));
    });
    const resizeSub = instance.onResize(({ cols, rows }) => {
      if (cols < MIN_COLS || rows < MIN_ROWS) return;
      void resizeTerminal(termId, cols, rows);
    });
    const observer =
      typeof ResizeObserver === 'function'
        ? new ResizeObserver(() => {
            requestAnimationFrame(() => fitAddon.fit());
          })
        : null;
    observer?.observe(host);

    registerTerminal(termId, {
      write: (bytes) => instance.write(bytes),
      focus: () => instance.focus(),
      fit: () => fitAddon.fit(),
    });
    // 首帧布局完成后再 fit + 聚焦：登录流程（设备码/OAuth 选择）需要键盘可达
    const raf = requestAnimationFrame(() => {
      fitAddon.fit();
      instance.focus();
    });

    return () => {
      cancelAnimationFrame(raf);
      observer?.disconnect();
      dataSub.dispose();
      resizeSub.dispose();
      unregisterTerminal(termId);
      instance.dispose();
    };
    // resolved 只在创建时取初值；明暗热更对短生命周期弹窗不值得重建终端
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [info?.ID]);

  // 登录态轮询：in → 停轮询 + 提示 + 重扫工具表（后端实探会翻转 Auth 并广播 tools:updated）+ 关闭
  useEffect(() => {
    if (!polling) return;
    let stopped = false;
    const check = () => {
      checkToolAuth(toolID)
        .then((status) => {
          if (stopped || status !== 'in') return;
          stopped = true;
          setPolling(false);
          notify(t('ui.auth.login_success', { name: toolName }), 'success');
          void scanSessions(); // 重扫会实探登录态并 Emit tools:updated，各处徽标随之刷新
          onCloseRef.current();
        })
        .catch(() => {
          // 单次探测失败（后端忙等）不中断轮询，下一次再试
        });
    };
    const timer = window.setInterval(check, POLL_INTERVAL_MS);
    const cap = window.setTimeout(() => {
      if (!stopped) setPolling(false);
    }, POLL_CAP_MS);
    check(); // 立即探一次：复用已登录终端的场景能当场发现
    return () => {
      stopped = true;
      window.clearInterval(timer);
      window.clearTimeout(cap);
    };
  }, [polling, toolID, toolName, notify, t]);

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }} className="w-[36rem] max-w-[90vw]">
      <DialogPrimitive.Title className="text-sm font-medium">
        {t('ui.auth.login_title', { name: toolName })}
      </DialogPrimitive.Title>
      <p className="mt-1 text-xs text-muted-foreground">{t('ui.auth.login_hint')}</p>
      {openError ? (
        <p role="alert" className="mt-3 text-xs text-destructive">{openError}</p>
      ) : info ? (
        <div ref={hostRef} className="xterm-host mt-3 h-72 w-full overflow-hidden rounded border border-border bg-card" />
      ) : (
        <p className="mt-3 text-xs text-muted-foreground">{t('ui.auth.login_waiting')}</p>
      )}
      <div className="mt-3 flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">
          {polling && !openError ? t('ui.auth.login_waiting') : ''}
        </span>
        <Button type="button" variant="secondary" onClick={onClose}>
          {t('ui.auth.close')}
        </Button>
      </div>
    </Dialog>
  );
}
