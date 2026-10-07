// Skill 广场：搜索 skills.sh、预览、一键安装到已检测 agent。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  getSkillDetail,
  installSkill,
  listInstalledSkills,
  listSkillTargets,
  searchSkills,
  uninstallSkill,
  type InstalledSkillInfo,
  type SkillDetail,
  type SkillSummary,
  type SkillTargetInfo,
} from '../lib/api';
import { backendError } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

const RECOMMEND_QUERY = 'agent';

export function SkillPlaza() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [query, setQuery] = useState('');
  const [debounced, setDebounced] = useState('');
  const [recommend, setRecommend] = useState(true);
  const [results, setResults] = useState<SkillSummary[]>([]);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState('');
  const [installed, setInstalled] = useState<InstalledSkillInfo[]>([]);
  const [detail, setDetail] = useState<SkillDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [targets, setTargets] = useState<SkillTargetInfo[]>([]);
  const [checked, setChecked] = useState<Record<string, boolean>>({});
  const [installing, setInstalling] = useState(false);
  const [pendingID, setPendingID] = useState('');

  const refreshInstalled = async () => {
    try {
      setInstalled(await listInstalledSkills());
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  useEffect(() => {
    void refreshInstalled();
  }, []);

  useEffect(() => {
    const tmr = window.setTimeout(() => setDebounced(query.trim()), 300);
    return () => window.clearTimeout(tmr);
  }, [query]);

  useEffect(() => {
    let cancelled = false;
    const q = debounced.length >= 2 ? debounced : RECOMMEND_QUERY;
    setRecommend(debounced.length < 2);
    setSearching(true);
    setSearchError('');
    searchSkills(q, 20)
      .then((list) => {
        if (!cancelled) setResults(list);
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          setResults([]);
          setSearchError(backendError(e));
        }
      })
      .finally(() => {
        if (!cancelled) setSearching(false);
      });
    return () => {
      cancelled = true;
    };
  }, [debounced]);

  const openDetail = async (id: string) => {
    setDetailLoading(true);
    setPendingID(id);
    try {
      const [d, ts] = await Promise.all([getSkillDetail(id), listSkillTargets()]);
      setDetail(d);
      setTargets(ts);
      const next: Record<string, boolean> = {};
      for (const t of ts) next[t.ToolID] = t.DefaultChecked;
      setChecked(next);
    } catch (e: unknown) {
      notify(backendError(e), 'error');
      setDetail(null);
    } finally {
      setDetailLoading(false);
    }
  };

  const selectedToolIDs = useMemo(
    () => Object.entries(checked).filter(([, v]) => v).map(([k]) => k),
    [checked],
  );

  const handleInstall = async () => {
    if (!detail || installing) return;
    if (selectedToolIDs.length === 0) {
      notify(t('ui.settings.skills.need_target'), 'error');
      return;
    }
    setInstalling(true);
    try {
      const res = await installSkill(detail.ID, selectedToolIDs);
      const copyCount = Object.values(res.Targets || {}).filter((x) => x.Mode === 'copy').length;
      if (res.Errors && Object.keys(res.Errors).length > 0) {
        notify(t('ui.settings.skills.partial_fail'), 'error');
      } else if (copyCount > 0) {
        notify(t('ui.settings.skills.installed_copy'), 'info');
      } else {
        notify(t('ui.settings.skills.installed_ok'), 'info');
      }
      setDetail(null);
      await refreshInstalled();
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    } finally {
      setInstalling(false);
    }
  };

  const handleUninstall = async (id: string) => {
    try {
      await uninstallSkill(id, true);
      notify(t('ui.settings.skills.uninstalled'), 'info');
      await refreshInstalled();
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  return (
    <div className="space-y-5">
      <section className="rounded border border-border bg-card p-3.5">
        <h2 className="mb-3 text-sm font-medium">{t('ui.settings.skills.plaza_title')}</h2>
        <p className="mb-3 text-xs text-muted-foreground">{t('ui.settings.skills.plaza_desc')}</p>
        <input
          type="search"
          className="mb-3 w-full rounded border border-input bg-background px-2 py-1.5 text-sm"
          placeholder={t('ui.settings.skills.search_placeholder')}
          value={query}
          onChange={(e) => setQuery(e.currentTarget.value)}
          aria-label={t('ui.settings.skills.search_placeholder')}
        />
        {recommend && (
          <p className="mb-2 text-xs text-muted-foreground">{t('ui.settings.skills.recommend')}</p>
        )}
        {searchError && <p className="mb-2 text-xs text-destructive">{searchError}</p>}
        {searching && <p className="text-xs text-muted-foreground">{t('ui.settings.skills.searching')}</p>}
        <ul className="divide-y divide-border rounded border border-border">
          {results.map((s) => (
            <li key={s.ID} className="flex items-center gap-2 px-2.5 py-2 text-sm">
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium">{s.Name}</div>
                <div className="truncate text-xs text-muted-foreground">
                  {s.Source}
                  {s.Installs > 0 ? ` · ${s.Installs.toLocaleString()}` : ''}
                </div>
              </div>
              <Button
                size="sm"
                variant="secondary"
                disabled={detailLoading && pendingID === s.ID}
                onClick={() => void openDetail(s.ID)}
              >
                {t('ui.settings.skills.install')}
              </Button>
            </li>
          ))}
          {!searching && results.length === 0 && !searchError && (
            <li className="px-2.5 py-3 text-xs text-muted-foreground">{t('ui.settings.skills.empty')}</li>
          )}
        </ul>
      </section>

      <section className="rounded border border-border bg-card p-3.5">
        <h2 className="mb-3 text-sm font-medium">{t('ui.settings.skills.installed_title')}</h2>
        <ul className="divide-y divide-border rounded border border-border">
          {installed.map((s) => (
            <li key={s.ID} className="flex items-start gap-2 px-2.5 py-2 text-sm">
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium">{s.Name}</div>
                <div className="truncate text-xs text-muted-foreground">{s.Source}</div>
                <div className="mt-1 flex flex-wrap gap-1">
                  {Object.entries(s.Targets || {}).map(([tool, rec]) => (
                    <Badge key={tool} variant="muted">
                      {tool}
                      {rec.Mode === 'copy' ? ` · ${t('ui.settings.skills.copy_mode')}` : ''}
                    </Badge>
                  ))}
                </div>
              </div>
              <Button size="sm" variant="secondary" onClick={() => void handleUninstall(s.ID)}>
                {t('ui.settings.skills.uninstall')}
              </Button>
            </li>
          ))}
          {installed.length === 0 && (
            <li className="px-2.5 py-3 text-xs text-muted-foreground">
              {t('ui.settings.skills.installed_empty')}
            </li>
          )}
        </ul>
      </section>

      <Dialog
        open={!!detail}
        onOpenChange={(open) => {
          if (!open) setDetail(null);
        }}
        className="w-[28rem] max-w-[92vw]"
      >
        {detail && (
          <div className="space-y-3 text-sm">
            <DialogPrimitive.Title className="text-sm font-medium">{detail.Name}</DialogPrimitive.Title>
            <p className="text-muted-foreground">{detail.Description}</p>
            <p className="text-xs text-muted-foreground">{detail.Source}</p>
            <pre className="max-h-48 overflow-auto rounded border border-border bg-muted/40 p-2 text-xs whitespace-pre-wrap">
              {detail.BodyPreview || t('ui.settings.skills.no_preview')}
            </pre>
            <div>
              <p className="mb-1 text-xs font-medium">{t('ui.settings.skills.targets')}</p>
              {targets.length === 0 ? (
                <p className="text-xs text-muted-foreground">{t('ui.settings.skills.no_targets')}</p>
              ) : (
                <ul className="space-y-1">
                  {targets.map((tg) => (
                    <li key={tg.ToolID}>
                      <label className="flex items-center gap-2 text-xs">
                        <input
                          type="checkbox"
                          checked={!!checked[tg.ToolID]}
                          onChange={(e) =>
                            setChecked((prev) => ({ ...prev, [tg.ToolID]: e.currentTarget.checked }))
                          }
                        />
                        <span>{tg.ToolID}</span>
                        <span className="truncate text-muted-foreground">{tg.Root}</span>
                      </label>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <p className="text-xs text-muted-foreground">{t('ui.settings.skills.trust_hint')}</p>
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={() => setDetail(null)}>
                {t('ui.settings.skills.cancel')}
              </Button>
              <Button disabled={installing || targets.length === 0} onClick={() => void handleInstall()}>
                {installing ? t('ui.settings.skills.installing') : t('ui.settings.skills.confirm_install')}
              </Button>
            </div>
          </div>
        )}
      </Dialog>
    </div>
  );
}
