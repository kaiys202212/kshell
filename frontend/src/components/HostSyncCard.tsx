// 宿主同步卡片：主动同步仅支持 WorkBuddy（释放最小 agent CLI + PATH + 技能）；
// 其它宿主走自助安装——复制一句话指令发给该平台的 AI，它读 kshell 生成的
// ~/.kshell/host-install.md 自助完成安装（只写自己的 skills/kshell/ 子目录）。
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getHostSyncStatuses, syncHosts } from '../lib/api';
import type { HostSyncResult } from '../lib/api';
import { backendError, translateBackend } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';

// WorkBuddy 在宿主列表里的固定 toolID（skills.targetRel 的 codebuddy 落点）。
const WORKBUDDY_ID = 'codebuddy';

export default function HostSyncCard() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [hostSync, setHostSync] = useState<HostSyncResult | null>(null);
  const [syncBusy, setSyncBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;
    getHostSyncStatuses()
      .then((s) => {
        if (!cancelled) setHostSync(s);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  const handleHostSync = async () => {
    setSyncBusy(true);
    try {
      const res = await syncHosts([WORKBUDDY_ID]);
      setHostSync(res);
      notify(t('ui.settings.hostsync.done'), 'info');
    } catch (e) {
      notify(translateBackend(backendError(e)), 'error');
    } finally {
      setSyncBusy(false);
    }
  };

  const wbStep = hostSync?.skills?.find((s) => s.toolID === WORKBUDDY_ID);
  const instruction = t('ui.settings.hostsync.selfhelp_instruction');

  const copyInstruction = async () => {
    try {
      await navigator.clipboard.writeText(instruction);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      notify(translateBackend(t('ui.settings.hostsync.copy_failed')), 'error');
    }
  };

  return (
    <section className="mb-5 rounded border border-border bg-card p-3.5">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-medium">{t('ui.settings.hostsync.title')}</h2>
      </div>
      <p className="mb-3 text-xs text-muted-foreground">{t('ui.settings.hostsync.desc')}</p>
      {hostSync ? (
        <div className="space-y-1 text-xs">
          <HostSyncRow label={t('ui.settings.hostsync.cli')} state={hostSync.binaryState} err={hostSync.binaryErr} path={hostSync.binaryPath} />
          <HostSyncRow label={t('ui.settings.hostsync.path')} state={hostSync.pathState} err={hostSync.pathErr} />
          <div className="flex items-center gap-2">
            <span className="w-24 shrink-0 truncate">{t('ui.settings.hostsync.workbuddy')}</span>
            <span className={wbStep?.err ? 'text-destructive' : ''}>
              {wbStep?.err
                ? translateBackend(wbStep.err)
                : t(`ui.settings.hostsync.state_${wbStep?.state ?? 'absent'}`, { defaultValue: wbStep?.state ?? 'absent' })}
            </span>
            <Button
              type="button"
              className="ml-auto"
              disabled={syncBusy}
              onClick={() => {
                if (syncBusy) return;
                void handleHostSync();
              }}
            >
              {syncBusy ? t('ui.settings.hostsync.syncing') : t('ui.settings.hostsync.sync')}
            </Button>
          </div>
          {(hostSync.warnings ?? []).map((w) => (
            <p key={w} className="text-warning">
              {translateBackend(w)}
            </p>
          ))}
          {(hostSync.pathState === 'added' || hostSync.pathState === 'ready') && (
            <p className="text-muted-foreground">{t('ui.settings.hostsync.restart_note')}</p>
          )}
        </div>
      ) : null}

      <div className="mt-4 border-t border-border pt-3">
        <h3 className="mb-1 text-xs font-medium">{t('ui.settings.hostsync.selfhelp_title')}</h3>
        <p className="mb-2 text-xs text-muted-foreground">{t('ui.settings.hostsync.selfhelp_desc')}</p>
        <p className="mb-2 rounded border border-border bg-background p-2 font-mono text-xs leading-relaxed">
          {instruction}
        </p>
        <Button size="sm" variant="secondary" type="button" onClick={() => void copyInstruction()}>
          {copied ? t('ui.settings.hostsync.copied') : t('ui.settings.hostsync.copy')}
        </Button>
      </div>
    </section>
  );
}

// HostSyncRow 是全局步骤（CLI/PATH）的状态行：标签 + 状态文案（失败标红）+
// 可选路径。WorkBuddy 主行的状态内联渲染（带同步按钮，形态不同）。
function HostSyncRow({ label, state, err, path }: { label: string; state: string; err?: string; path?: string }) {
  const { t } = useTranslation();
  const failed = state === 'error' || !!err;
  return (
    <div className="flex items-center gap-2">
      <span className="w-24 shrink-0 text-muted-foreground">{label}</span>
      <span className={failed ? 'text-destructive' : ''}>
        {err ? translateBackend(err) : t(`ui.settings.hostsync.state_${state}`, { defaultValue: state })}
      </span>
      {path && <span className="truncate font-mono text-muted-foreground" title={path}>{path}</span>}
    </div>
  );
}
