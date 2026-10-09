// SSH 面板（工作区页签右栏「SSH」页签）：连接列表 + 新建/编辑 + 命令执行。
// 双击列表行 → 回调 onOpenRemote（父组件在预览区开内嵌 SSH 终端）。
// 「编辑」打开表单；「新建」同表单空值。私钥只填路径。
// 命令执行走 ExecRemote（非交互）：非 0 退出码不是异常，结果里带 ExitCode。
import { useEffect, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import {
  deleteConnection,
  execRemote,
  listConnections,
  pickFile,
  upsertConnection,
} from '../lib/api';
import type { RemoteResult, SshConnection } from '../lib/api';
import { backendError } from '../lib/errors';
import { formatDuration, tailLines } from '../lib/format';
import { LIST_ROW_ACTIVE } from '../lib/ui';
import { cn } from '../lib/cn';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
import { EmptyState } from './ui/empty-state';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const OUTPUT_TAIL_LINES = 50;
const HISTORY_LIMIT = 20;
const HISTORY_CHIPS = 5;

const SOURCE_KEYS = ['sshconfig', 'env', 'spring', 'deploy', 'docs', 'manual'];

function sourceLabel(source: string, t: TFunction): string {
  return SOURCE_KEYS.includes(source) ? t(`ui.ssh.source_${source}`) : source;
}

function display(c: SshConnection): string {
  let target = c.User ? `${c.User}@${c.Host}` : c.Host;
  if (c.Port > 0 && c.Port !== 22) target += `:${c.Port}`;
  return target;
}

function wsBaseName(ws: string): string {
  const n = ws.replace(/[\\/]+$/, '');
  const i = Math.max(n.lastIndexOf('/'), n.lastIndexOf('\\'));
  return i >= 0 ? n.slice(i + 1) : n;
}

type FormState = {
  ID: string;
  Name: string;
  Host: string;
  User: string;
  Port: string;
  IdentityFile: string;
  Password: string;
};

const emptyForm = (): FormState => ({
  ID: '',
  Name: '',
  Host: '',
  User: '',
  Port: '22',
  IdentityFile: '',
  Password: '',
});

function formFromConn(c: SshConnection): FormState {
  return {
    ID: c.ID,
    Name: c.Name,
    Host: c.Host,
    User: c.User,
    Port: String(c.Port > 0 ? c.Port : 22),
    IdentityFile: c.IdentityFile,
    Password: c.Password ?? '',
  };
}

export default function SshPanel({
  wsPath,
  onOpenRemote,
}: {
  wsPath: string;
  onOpenRemote?: (c: SshConnection) => void;
}) {
  const { t } = useTranslation();
  const [conns, setConns] = useState<SshConnection[] | null>(null);
  const [error, setError] = useState('');
  const [selectedId, setSelectedId] = useState('');
  const [cmd, setCmd] = useState('');
  const [history, setHistory] = useState<string[]>([]);
  const [historyIdx, setHistoryIdx] = useState(-1);
  const [draft, setDraft] = useState('');
  const [result, setResult] = useState<RemoteResult | null>(null);
  const [execError, setExecError] = useState('');
  const [running, setRunning] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [formError, setFormError] = useState('');
  const [saving, setSaving] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const notify = useAppStore((s) => s.notify);
  const listScope = showAll ? '' : wsPath;

  useEffect(() => {
    let cancelled = false;
    setConns(null);
    setError('');
    listConnections(listScope)
      .then((list) => {
        if (cancelled) return;
        setConns(list);
        setSelectedId((cur) => (list.some((c) => c.ID === cur) ? cur : list[0]?.ID || ''));
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(backendError(e));
      });
    return () => {
      cancelled = true;
    };
  }, [listScope]);

  const openEdit = (c: SshConnection) => {
    setForm(formFromConn(c));
    setFormError('');
    setFormOpen(true);
  };

  const openNew = () => {
    setForm({ ...emptyForm(), Name: '' });
    setFormError('');
    setFormOpen(true);
  };

  const handleSaveOnce = async () => {
    const host = form.Host.trim();
    if (!host) {
      setFormError(t('ui.ssh.host_required'));
      return;
    }
    const port = Number(form.Port) || 22;
    setSaving(true);
    setFormError('');
    try {
      const prev = form.ID ? conns?.find((c) => c.ID === form.ID) : undefined;
      const saved = await upsertConnection({
        ID: form.ID,
        Name: form.Name.trim(),
        Host: host,
        User: form.User.trim(),
        Port: port,
        IdentityFile: form.IdentityFile.trim(),
        Password: form.Password,
        Workspace: prev?.Workspace ?? wsPath,
        Source: prev?.Source ?? '',
        SourceFile: prev?.SourceFile ?? '',
        Verified: prev?.Verified ?? false,
      });
      setFormOpen(false);
      const list = await listConnections(listScope);
      setConns(list);
      setSelectedId(saved.ID);
    } catch (e: unknown) {
      setFormError(backendError(e));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!form.ID) return;
    setSaving(true);
    setFormError('');
    try {
      await deleteConnection(form.ID);
      setFormOpen(false);
      const list = await listConnections(listScope);
      setConns(list);
      setSelectedId((cur) => (cur === form.ID ? list[0]?.ID || '' : cur));
    } catch (e: unknown) {
      setFormError(backendError(e));
    } finally {
      setSaving(false);
    }
  };

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
      setExecError(backendError(e));
    } finally {
      setRunning(false);
    }
  };

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
      <div className="flex flex-col gap-2" aria-label={t('ui.ssh.loading_aria')}>
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
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">{t('ui.ssh.double_click_hint')}</span>
        <div className="flex shrink-0 items-center gap-1">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => setShowAll((v) => !v)}
          >
            {showAll ? t('ui.ssh.view_current') : t('ui.ssh.view_all')}
          </Button>
          <Button size="sm" variant="secondary" onClick={openNew}>
            {t('ui.ssh.new')}
          </Button>
        </div>
      </div>

      {conns.length === 0 ? (
        <EmptyState title={t('ui.ssh.empty')} />
      ) : (
        <ul aria-label={t('ui.ssh.list_aria')} className="m-0 flex list-none flex-col gap-1.5 p-0">
          {conns.map((c) => (
            <li
              key={c.ID}
              className={cn(
                'rounded border border-border bg-card px-2.5 py-1.5 transition-colors',
                c.ID === selectedId && LIST_ROW_ACTIVE,
              )}
              onDoubleClick={() => {
                setSelectedId(c.ID);
                if (onOpenRemote) onOpenRemote(c);
                else notify(t('ui.ssh.no_open_callback'), 'error');
              }}
            >
              <div className="flex min-w-0 items-center gap-1.5">
                <button
                  className="min-w-0 truncate text-left text-sm font-medium"
                  title={c.Host}
                  onClick={() => setSelectedId(c.ID)}
                >
                  {c.Name}
                </button>
                <Button
                  size="sm"
                  variant="secondary"
                  className="ml-auto shrink-0"
                  onClick={() => openEdit(c)}
                >
                  {t('ui.ssh.edit')}
                </Button>
              </div>
              <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                <span className="truncate">{display(c)}</span>
                {showAll && c.Workspace && (
                  <span
                    className="shrink-0 truncate font-mono text-[10px] text-muted-foreground"
                    title={c.Workspace}
                  >
                    {t('ui.ssh.workspace_label', { name: wsBaseName(c.Workspace) })}
                  </span>
                )}
                <span
                  className="shrink-0 rounded-sm border border-border px-1.5 py-px font-mono text-[10px]"
                  title={c.SourceFile || undefined}
                >
                  {sourceLabel(c.Source, t)}
                </span>
                {c.Verified && <span className="shrink-0 text-success">✓</span>}
              </div>
            </li>
          ))}
        </ul>
      )}

      {selected && (
        <div className="flex flex-col gap-2">
          <div className="flex gap-2">
            <Input
              size="sm"
              className="min-w-0 flex-1"
              aria-label={t('ui.ssh.exec_aria')}
              placeholder={t('ui.ssh.exec_placeholder', { name: selected.Name })}
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
              {t('ui.ssh.exec')}
            </Button>
          </div>
          {history.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5" aria-label={t('ui.ssh.history_aria')}>
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
                aria-label={t('ui.ssh.clear_history_aria')}
                onClick={() => {
                  setHistory([]);
                  setHistoryIdx(-1);
                }}
              >
                {t('ui.ssh.clear')}
              </button>
            </div>
          )}
          {execError && <p className="text-xs text-destructive">{execError}</p>}
          {result && (
            <>
              <p className="text-xs text-muted-foreground">
                {t('ui.ssh.exit_summary', {
                  code: result.ExitCode,
                  duration: formatDuration(result.Duration),
                })}
              </p>
              <pre
                className="max-h-80 overflow-auto rounded bg-muted p-2.5 font-mono text-xs leading-[1.5] whitespace-pre-wrap"
                aria-label={t('ui.ssh.stdout_aria')}
              >
                {stdoutTail || t('ui.ssh.no_output')}
              </pre>
              {stderrTail && (
                <pre
                  className="max-h-80 overflow-auto rounded bg-muted p-2.5 font-mono text-xs leading-[1.5] whitespace-pre-wrap text-destructive/90"
                  aria-label={t('ui.ssh.stderr_aria')}
                >
                  {stderrTail}
                </pre>
              )}
            </>
          )}
        </div>
      )}

      <Dialog open={formOpen} onOpenChange={setFormOpen} className="w-[min(92vw,22rem)]">
        <DialogPrimitive.Title className="mb-2 text-sm font-medium">
          {form.ID ? t('ui.ssh.edit_title') : t('ui.ssh.new_title')}
        </DialogPrimitive.Title>
        <div className="flex flex-col gap-2">
          <Input
            size="sm"
            aria-label={t('ui.ssh.name_aria')}
            placeholder={t('ui.ssh.name_placeholder')}
            value={form.Name}
            onChange={(e) => setForm((f) => ({ ...f, Name: e.target.value }))}
          />
          <Input
            size="sm"
            aria-label={t('ui.ssh.host_aria')}
            placeholder={t('ui.ssh.host_placeholder')}
            value={form.Host}
            onChange={(e) => setForm((f) => ({ ...f, Host: e.target.value }))}
          />
          <Input
            size="sm"
            aria-label={t('ui.ssh.user_aria')}
            placeholder={t('ui.ssh.user_placeholder')}
            value={form.User}
            onChange={(e) => setForm((f) => ({ ...f, User: e.target.value }))}
          />
          <Input
            size="sm"
            aria-label={t('ui.ssh.port_aria')}
            placeholder={t('ui.ssh.port_placeholder')}
            value={form.Port}
            onChange={(e) => setForm((f) => ({ ...f, Port: e.target.value }))}
          />
          <div className="flex gap-1">
            <Input
              size="sm"
              className="min-w-0 flex-1"
              aria-label={t('ui.ssh.key_aria')}
              placeholder={t('ui.ssh.key_placeholder')}
              value={form.IdentityFile}
              onChange={(e) => setForm((f) => ({ ...f, IdentityFile: e.target.value }))}
            />
            <Button
              size="sm"
              variant="secondary"
              type="button"
              className="shrink-0 self-center"
              onClick={() => {
                void pickFile(t('ui.ssh.pick_key_title')).then((p) => {
                  if (p) setForm((f) => ({ ...f, IdentityFile: p }));
                });
              }}
            >
              {t('ui.ssh.browse_key')}
            </Button>
          </div>
          <Input
            size="sm"
            type="password"
            aria-label={t('ui.ssh.password_aria')}
            placeholder={t('ui.ssh.password_placeholder')}
            value={form.Password}
            onChange={(e) => setForm((f) => ({ ...f, Password: e.target.value }))}
            autoComplete="new-password"
          />
          <p className="text-xs text-muted-foreground">{t('ui.ssh.password_plaintext_hint')}</p>
          {formError && <p className="text-xs text-destructive">{formError}</p>}
          <div className="mt-1 flex justify-end gap-2">
            {form.ID && (
              <Button size="sm" variant="secondary" disabled={saving} onClick={() => void handleDelete()}>
                {t('ui.ssh.delete')}
              </Button>
            )}
            <Button size="sm" variant="secondary" onClick={() => setFormOpen(false)}>
              {t('ui.ssh.cancel')}
            </Button>
            <Button size="sm" disabled={saving} onClick={() => void handleSaveOnce()}>
              {t('ui.ssh.save')}
            </Button>
          </div>
        </div>
      </Dialog>
    </div>
  );
}
