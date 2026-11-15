// 宿主同步卡片：释放最小 agent CLI + PATH 配置 + 按宿主安装 kshell 技能。
// 挂载即拉取同步状态与工具检测结果；宿主行 codebuddy 常显，其余宿主仅在
// 检测到已安装时出现——新宿主在 internal/skills 的 targetRel 加映射后自动跟随。
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  getHostSyncStatuses,
  getTools,
  onToolsUpdated,
  syncHosts,
} from '../lib/api';
import type { HostSyncResult, ToolInfo } from '../lib/api';
import { backendError, translateBackend } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';

export default function HostSyncCard() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [hostSync, setHostSync] = useState<HostSyncResult | null>(null);
  // 进行中的动作：'__all__' 或宿主 ID
  const [syncBusy, setSyncBusy] = useState<string | null>(null);
  const [tools, setTools] = useState<ToolInfo[]>([]);

  useEffect(() => {
    let cancelled = false;
    getHostSyncStatuses()
      .then((s) => {
        if (!cancelled) setHostSync(s);
      })
      .catch(() => {});
    const loadTools = () => {
      void getTools()
        .then((list) => {
          if (!cancelled) setTools(list);
        })
        .catch(() => {});
    };
    loadTools();
    const off = onToolsUpdated(loadTools);
    return () => {
      cancelled = true;
      off();
    };
  }, []);

  const handleHostSync = async (ids: string[]) => {
    setSyncBusy(ids.length > 1 ? '__all__' : (ids[0] ?? '__all__'));
    try {
      const res = await syncHosts(ids);
      // 单宿主同步的响应 Skills 只含该宿主：合并进现有状态，其余宿主行不消失
      //（release/PATH 是全局步骤，直接取响应最新值）。
      setHostSync((prev) => {
        if (!prev) return res;
        const fresh = new Map((res.skills ?? []).map((s) => [s.toolID, s]));
        return { ...res, skills: (prev.skills ?? []).map((s) => fresh.get(s.toolID) ?? s) };
      });
      notify(t('ui.settings.hostsync.done'), 'info');
    } catch (e) {
      notify(translateBackend(backendError(e)), 'error');
    } finally {
      setSyncBusy(null);
    }
  };

  const hostRows = (hostSync?.skills ?? [])
    .filter((s) => s.toolID === 'codebuddy' || tools.some((tl) => tl.ID === s.toolID && tl.Installed))
    .map((s) => {
      const tool = tools.find((tl) => tl.ID === s.toolID);
      return { id: s.toolID, name: tool?.Name || s.toolID };
    });

  return (
    <section className="mb-5 rounded border border-border bg-card p-3.5">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-medium">{t('ui.settings.hostsync.title')}</h2>
        <Button
          type="button"
          disabled={syncBusy !== null || hostRows.length === 0}
          onClick={() => {
            if (syncBusy !== null) return;
            void handleHostSync(hostRows.map((h) => h.id));
          }}
        >
          {syncBusy === '__all__' ? t('ui.settings.hostsync.syncing') : t('ui.settings.hostsync.all')}
        </Button>
      </div>
      <p className="mb-3 text-xs text-muted-foreground">{t('ui.settings.hostsync.desc')}</p>
      {hostSync ? (
        <div className="space-y-1 text-xs">
          <HostSyncRow label={t('ui.settings.hostsync.cli')} state={hostSync.binaryState} err={hostSync.binaryErr} path={hostSync.binaryPath} />
          <HostSyncRow label={t('ui.settings.hostsync.path')} state={hostSync.pathState} err={hostSync.pathErr} />
          {hostRows.map((h) => {
            const step = hostSync.skills?.find((s) => s.toolID === h.id);
            return (
              <div key={h.id} className="flex items-center gap-2">
                <span className="w-24 shrink-0 truncate" title={h.id}>{h.name}</span>
                <span className={step?.err ? 'text-destructive' : ''}>
                  {step?.err
                    ? translateBackend(step.err)
                    : t(`ui.settings.hostsync.state_${step?.state ?? 'absent'}`, { defaultValue: step?.state ?? 'absent' })}
                </span>
                <Button
                  variant="secondary"
                  type="button"
                  className="ml-auto"
                  disabled={syncBusy !== null}
                  onClick={() => {
                    if (syncBusy !== null) return;
                    void handleHostSync([h.id]);
                  }}
                >
                  {syncBusy === h.id ? t('ui.settings.hostsync.syncing') : t('ui.settings.hostsync.sync')}
                </Button>
              </div>
            );
          })}
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
    </section>
  );
}

// HostSyncRow 是全局步骤（CLI/PATH）的状态行：标签 + 状态文案（失败标红）+
// 可选路径。宿主行的状态内联渲染（带各自同步按钮，形态不同）。
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
