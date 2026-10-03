// SSH 面板（工作区页签右栏「SSH」页签）：连接列表 + 命令执行输出尾部展示。
// 连接列表来自 ListConnections（当前工作区绑定连接 + 全局连接），每行标注来源
// （中文标注，SourceFile 作 title 提示）与连通性验证状态（✓ = Verified）。
// 「连接」弹 OpenSSH 终端窗口（BatchMode 恒定，绝不卡密码提示）：已打开的连接
// 重复点击仍走 OpenSSH，Go 侧幂等转聚焦；前端按窗口标题（terminalTitle 形态）
// 维护 open 状态，与 SessionList 同一套 windowStatus 机制。
// 命令执行走 ExecRemote（非交互）：非 0 退出码不是异常，结果里带 ExitCode；
// Go Result 已是完整输出，前端只展示尾部（stdout/stderr 各取 OUTPUT_TAIL_LINES 行）。
// 已知限制：windowStatus 以 terminalTitle(连接名) 为键，与 SessionList 共用一张表，
// 不同来源的同名连接（或同名会话）会命中同一键、状态互相污染；连接名与工作区/会话名
// 冲突概率极低，暂不做键空间隔离。
import { useEffect, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { execRemote, listConnections, openSSH } from '../lib/api';
import type { RemoteResult, SshConnection } from '../lib/api';
import { formatDuration, tailLines } from '../lib/format';
import { terminalTitle } from '../lib/title';
import { LIST_ROW_ACTIVE } from '../lib/ui';
import { cn } from '../lib/cn';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

// 输出尾部行数：取 50 行——约两屏终端的量，足够看到命令关键结果又不撑爆右栏。
// Go 侧 Result 是完整输出，截多少只影响前端展示，与后端无耦合。
const OUTPUT_TAIL_LINES = 50;

// 命令历史上限：头部插入、去重后保留最近 20 条（纯前端记忆，不落盘）
const HISTORY_LIMIT = 20;

// 历史 chip 展示条数：右栏空间有限，只露出最近 5 条
const HISTORY_CHIPS = 5;

// 连接来源的中文标注（Source 取值见 internal/remote/scanners/*，Go 侧只产生
// sshconfig / env / spring / deploy / docs 五种；未知值回退展示原始串）
const SOURCE_LABELS: Record<string, string> = {
  sshconfig: 'ssh 配置',
  env: '环境变量',
  spring: 'Spring 配置',
  deploy: '部署脚本',
  docs: '文档',
};

function sourceLabel(source: string): string {
  return SOURCE_LABELS[source] ?? source;
}

// 目标串与 Go 侧 Connection.Display 对齐：user@host（端口非 22 才显示）
function display(c: SshConnection): string {
  let target = c.User ? `${c.User}@${c.Host}` : c.Host;
  if (c.Port > 0 && c.Port !== 22) target += `:${c.Port}`;
  return target;
}

export default function SshPanel({ wsPath }: { wsPath: string }) {
  // null 哨兵表示「列表尚未加载完」，与 FileTree 同款模式：避免加载瞬间闪错误/空态
  const [conns, setConns] = useState<SshConnection[] | null>(null);
  const [error, setError] = useState('');
  const [selectedId, setSelectedId] = useState('');
  const [cmd, setCmd] = useState('');
  // 命令历史（最近在前）：historyIdx 为历史浏览游标，-1 表示非浏览态；
  // 进入浏览态时用 draft 记下当前草稿，ArrowDown 退到头时恢复
  const [history, setHistory] = useState<string[]>([]);
  const [historyIdx, setHistoryIdx] = useState(-1);
  const [draft, setDraft] = useState('');
  const [result, setResult] = useState<RemoteResult | null>(null);
  const [execError, setExecError] = useState('');
  const [running, setRunning] = useState(false);
  const windowStatus = useAppStore((s) => s.windowStatus);
  const setWindowStatus = useAppStore((s) => s.setWindowStatus);
  const notify = useAppStore((s) => s.notify);

  useEffect(() => {
    let cancelled = false;
    listConnections(wsPath)
      .then((list) => {
        if (cancelled) return;
        setConns(list);
        // 默认选中第一个连接，命令执行不需要再手动选择
        setSelectedId((cur) => cur || list[0]?.ID || '');
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [wsPath]);

  // 打开/聚焦连接：open 状态下复用 OpenSSH（Go 侧幂等转聚焦），失败不置 open。
  // 操作失败走轻量提示（notify）而非 setError：面板级 error 只留给列表加载失败，
  // 不让单次「连接」失败炸掉整个面板。
  const handleOpen = async (c: SshConnection) => {
    try {
      await openSSH(c.ID);
      setWindowStatus(terminalTitle(c.Name), true);
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };

  // 执行命令：非 0 退出码照常展示结果，只有调用本身失败才算错误。
  // 真正发起执行后命令入历史（头部插入、去重、最多 HISTORY_LIMIT 条）。
  const handleExec = async () => {
    const command = cmd.trim();
    if (!command || !selectedId || running) return;
    setRunning(true);
    setExecError('');
    setResult(null);
    setHistory((h) => [command, ...h.filter((c) => c !== command)].slice(0, HISTORY_LIMIT));
    setHistoryIdx(-1);
    try {
      const res = await execRemote(selectedId, command);
      setResult(res);
    } catch (e: unknown) {
      setExecError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  };

  // ↑/↓ 在历史中回填：↑ 取 min(idx+1, len-1)（到头停在最早一条），
  // ↓ 逐条退回，减到 -1（非浏览态）时恢复进入浏览态前的草稿
  const handleHistoryKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowUp' && history.length > 0) {
      e.preventDefault();
      if (historyIdx === -1) setDraft(cmd);
      const next = historyIdx === -1 ? 0 : Math.min(historyIdx + 1, history.length - 1);
      setHistoryIdx(next);
      setCmd(history[next]);
    } else if (e.key === 'ArrowDown' && historyIdx !== -1) {
      e.preventDefault();
      const prev = historyIdx - 1;
      setHistoryIdx(prev);
      setCmd(prev === -1 ? draft : history[prev]);
    }
  };

  if (error) {
    return <p className="text-sm text-destructive">{error}</p>;
  }
  if (conns === null) {
    return (
      <div className="flex flex-col gap-2" aria-label="SSH 连接列表加载中">
        <Skeleton className="h-12 rounded border border-border" />
        <Skeleton className="h-12 rounded border border-border" />
        <Skeleton className="h-12 rounded border border-border" />
      </div>
    );
  }

  const selected = conns.find((c) => c.ID === selectedId) ?? null;
  const stdoutTail = result ? tailLines(result.Stdout, OUTPUT_TAIL_LINES) : '';
  const stderrTail = result ? tailLines(result.Stderr, OUTPUT_TAIL_LINES) : '';

  return (
    <div className="flex flex-col gap-2.5 text-sm">
      {conns.length === 0 ? (
        <EmptyState title="没有可用的 SSH 连接" />
      ) : (
        <ul aria-label="SSH 连接列表" className="m-0 flex list-none flex-col gap-1.5 p-0">
          {conns.map((c) => {
            const open = windowStatus[terminalTitle(c.Name)] === true;
            return (
              <li
                key={c.ID}
                className={cn(
                  'rounded border border-border bg-card px-2.5 py-1.5 transition-colors',
                  open ? LIST_ROW_ACTIVE : c.ID === selectedId && 'bg-muted/60',
                )}
              >
                <div className="flex min-w-0 items-center gap-1.5">
                  <button
                    className="min-w-0 truncate text-left text-sm font-medium"
                    title={c.Host}
                    onClick={() => setSelectedId(c.ID)}
                  >
                    {c.Name}
                  </button>
                  {open && <span className="shrink-0 text-xs text-primary">✓</span>}
                  <Button
                    size="sm"
                    variant="secondary"
                    className="ml-auto shrink-0"
                    onClick={() => void handleOpen(c)}
                  >
                    连接
                  </Button>
                </div>
                <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                  <span className="truncate">{display(c)}</span>
                  <span
                    className="shrink-0 rounded-sm border border-border px-1.5 py-px font-mono text-[10px]"
                    title={c.SourceFile || undefined}
                  >
                    {sourceLabel(c.Source)}
                  </span>
                  {c.Verified && <span className="shrink-0 text-success">✓</span>}
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {selected && (
        <div className="flex flex-col gap-2">
          <div className="flex gap-2">
            <Input
              size="sm"
              className="min-w-0 flex-1"
              aria-label="执行命令"
              placeholder={`在 ${selected.Name} 上执行命令`}
              value={cmd}
              onChange={(e) => setCmd(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  void handleExec();
                  return;
                }
                handleHistoryKey(e);
              }}
            />
            <Button
              size="sm"
              className="shrink-0 self-center"
              onClick={() => void handleExec()}
              disabled={running || !cmd.trim()}
            >
              执行
            </Button>
          </div>
          {/* 最近命令 chip：点击回填到输入框，「清空」一键清空历史 */}
          {history.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5" aria-label="命令历史">
              {history.slice(0, HISTORY_CHIPS).map((c) => (
                <button
                  key={c}
                  className="max-w-40 truncate rounded-sm border border-border bg-secondary px-2 py-0.5 text-xs text-secondary-foreground transition-colors hover:bg-muted"
                  title={c}
                  onClick={() => {
                    setCmd(c);
                    setHistoryIdx(-1);
                  }}
                >
                  {c}
                </button>
              ))}
              <button
                className="text-xs text-muted-foreground transition-colors hover:text-foreground"
                aria-label="清空命令历史"
                onClick={() => {
                  setHistory([]);
                  setHistoryIdx(-1);
                }}
              >
                清空
              </button>
            </div>
          )}
          {execError && <p className="text-xs text-destructive">{execError}</p>}
          {result && (
            <>
              <p className="text-xs text-muted-foreground">
                退出码 {result.ExitCode} · 耗时 {formatDuration(result.Duration)}
              </p>
              <pre
                className="max-h-80 overflow-auto rounded bg-muted p-2.5 font-mono text-xs leading-[1.5] whitespace-pre-wrap"
                aria-label="命令输出"
              >
                {stdoutTail || '（无输出）'}
              </pre>
              {stderrTail && (
                <pre
                  className="max-h-80 overflow-auto rounded bg-muted p-2.5 font-mono text-xs leading-[1.5] whitespace-pre-wrap text-destructive/90"
                  aria-label="错误输出"
                >
                  {stderrTail}
                </pre>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
