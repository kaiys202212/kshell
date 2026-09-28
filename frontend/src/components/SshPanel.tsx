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
import { execRemote, listConnections, openSSH } from '../lib/api';
import type { RemoteResult, SshConnection } from '../lib/api';
import { formatDuration, tailLines } from '../lib/format';
import { terminalTitle } from '../lib/title';
import { useAppStore } from '../state/store';

// 输出尾部行数：取 50 行——约两屏终端的量，足够看到命令关键结果又不撑爆右栏。
// Go 侧 Result 是完整输出，截多少只影响前端展示，与后端无耦合。
const OUTPUT_TAIL_LINES = 50;

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
      notify(e instanceof Error ? e.message : String(e));
    }
  };

  // 执行命令：非 0 退出码照常展示结果，只有调用本身失败才算错误
  const handleExec = async () => {
    const command = cmd.trim();
    if (!command || !selectedId || running) return;
    setRunning(true);
    setExecError('');
    setResult(null);
    try {
      const res = await execRemote(selectedId, command);
      setResult(res);
    } catch (e: unknown) {
      setExecError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  };

  if (error) {
    return <p className="ssh-error">{error}</p>;
  }
  if (conns === null) {
    return <p className="ssh-status">加载中……</p>;
  }

  const selected = conns.find((c) => c.ID === selectedId) ?? null;
  const stdoutTail = result ? tailLines(result.Stdout, OUTPUT_TAIL_LINES) : '';
  const stderrTail = result ? tailLines(result.Stderr, OUTPUT_TAIL_LINES) : '';

  return (
    <div className="ssh-panel">
      {conns.length === 0 ? (
        <p className="ssh-empty">没有可用的 SSH 连接</p>
      ) : (
        <ul className="ssh-list" aria-label="SSH 连接列表">
          {conns.map((c) => {
            const open = windowStatus[terminalTitle(c.Name)] === true;
            return (
              <li
                key={c.ID}
                className={
                  open
                    ? 'ssh-item ssh-item--open'
                    : c.ID === selectedId
                      ? 'ssh-item ssh-item--selected'
                      : 'ssh-item'
                }
              >
                <div className="ssh-row">
                  <button
                    className="ssh-name"
                    title={c.Host}
                    onClick={() => setSelectedId(c.ID)}
                  >
                    {c.Name}
                  </button>
                  {open && <span className="ssh-open-mark">✓</span>}
                  <button className="ssh-connect" onClick={() => void handleOpen(c)}>
                    连接
                  </button>
                </div>
                <div className="ssh-meta">
                  <span className="ssh-target">{display(c)}</span>
                  <span
                    className="ssh-source"
                    title={c.SourceFile || undefined}
                  >
                    {sourceLabel(c.Source)}
                  </span>
                  {c.Verified && <span className="ssh-verified">✓</span>}
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {selected && (
        <div className="ssh-exec">
          <div className="ssh-exec-row">
            <input
              className="ssh-cmd-input"
              aria-label="执行命令"
              placeholder={`在 ${selected.Name} 上执行命令`}
              value={cmd}
              onChange={(e) => setCmd(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void handleExec();
              }}
            />
            <button
              className="ssh-exec-btn"
              onClick={() => void handleExec()}
              disabled={running || !cmd.trim()}
            >
              执行
            </button>
          </div>
          {execError && <p className="ssh-exec-error">{execError}</p>}
          {result && (
            <>
              <p className="ssh-result-meta">
                退出码 {result.ExitCode} · 耗时 {formatDuration(result.Duration)}
              </p>
              <pre className="ssh-output" aria-label="命令输出">
                {stdoutTail || '（无输出）'}
              </pre>
              {stderrTail && (
                <pre className="ssh-output ssh-output--stderr" aria-label="错误输出">
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
