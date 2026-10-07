// 桌面端首次启动：扫描内置 Agent 并支持勾选后串行一键安装。可跳过，只自动弹一次。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  dismissAgentSetup,
  getToolInstallRecipe,
  getTools,
  installBuiltinTool,
  needsAgentSetup,
  onToolInstallDone,
  onToolInstallLog,
} from '../lib/api';
import type { InstallRecipeView, ToolInfo } from '../lib/api';
import { backendError, translateBackend } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

export default function AgentSetupDialog() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [open, setOpen] = useState(false);
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [recipes, setRecipes] = useState<Record<string, InstallRecipeView>>({});
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [installing, setInstalling] = useState(false);
  const [activeId, setActiveId] = useState('');
  const [log, setLog] = useState('');
  const installingRef = useRef(false);

  useEffect(() => {
    let cancelled = false;
    needsAgentSetup()
      .then(async (need) => {
        if (cancelled || !need) return;
        const list = await getTools();
        const pairs = await Promise.all(
          list.map((t) =>
            getToolInstallRecipe(t.ID)
              .then((r) => [t.ID, r] as const)
              .catch(() => null),
          ),
        );
        const rec: Record<string, InstallRecipeView> = {};
        for (const p of pairs) {
          if (p) rec[p[0]] = p[1];
        }
        const sel: Record<string, boolean> = {};
        for (const t of list) {
          if (!t.BinPath && rec[t.ID]) sel[t.ID] = true;
        }
        if (cancelled) return;
        setTools(list);
        setRecipes(rec);
        setSelected(sel);
        setOpen(true);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  const installable = useMemo(
    () => tools.filter((t) => !t.BinPath && recipes[t.ID]),
    [tools, recipes],
  );
  const selectedIds = installable.filter((t) => selected[t.ID]).map((t) => t.ID);
  const activeName = tools.find((t) => t.ID === activeId)?.Name ?? activeId;

  const closeAfterDismiss = async () => {
    if (installingRef.current) return;
    try {
      await dismissAgentSetup();
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
    setOpen(false);
  };

  const handleInstall = async () => {
    if (installingRef.current || selectedIds.length === 0) return;
    installingRef.current = true;
    setInstalling(true);
    setLog('');
    const offLog = onToolInstallLog((p) => {
      setActiveId(p.toolID);
      const text = translateBackend(p.text);
      setLog((prev) => (prev ? `${prev}\n${text}` : text));
    });
    try {
      for (const id of selectedIds) {
        setActiveId(id);
        let offDone = () => {};
        const done = new Promise<{ ok: boolean; error?: string }>((resolve) => {
          offDone = onToolInstallDone((p) => {
            if (p.toolID !== id || p.action !== 'install') return;
            offDone();
            resolve(p);
          });
        });
        try {
          await installBuiltinTool(id);
          const result = await done;
          if (!result.ok) {
            notify(
              result.error
                ? translateBackend(result.error)
                : t('ui.agent_setup.tool_install_failed', { id }),
              'error',
            );
          }
        } catch (e: unknown) {
          offDone();
          notify(backendError(e), 'error');
        }
      }
    } finally {
      offLog();
      installingRef.current = false;
      setInstalling(false);
      setActiveId('');
    }
    await closeAfterDismiss();
  };

  return (
    <Dialog open={open} onOpenChange={() => {}} dismissible={false} className="w-[28rem] max-w-[90vw]">
      <DialogPrimitive.Title className="text-sm font-medium">
        {t('ui.agent_setup.title')}
      </DialogPrimitive.Title>
      <p className="mt-1 text-xs text-muted-foreground">
        {t('ui.agent_setup.hint')}
      </p>
      <ul className="mt-3 max-h-64 divide-y divide-border overflow-auto rounded border border-border">
        {tools.map((tool) => {
          const canInstall = !tool.BinPath && !!recipes[tool.ID];
          return (
            <li key={tool.ID} className="flex items-center justify-between gap-2 px-2.5 py-1.5 text-xs">
              {canInstall ? (
                <label className="flex min-w-0 items-center gap-2">
                  <input
                    type="checkbox"
                    checked={!!selected[tool.ID]}
                    disabled={installing}
                    aria-label={tool.Name}
                    onChange={(e) =>
                      setSelected((prev) => ({ ...prev, [tool.ID]: e.target.checked }))
                    }
                  />
                  <span className="font-medium">{tool.Name}</span>
                  <span className="text-muted-foreground">{t('ui.agent_setup.not_installed')}</span>
                </label>
              ) : (
                <div className="flex min-w-0 items-center gap-2">
                  <span className="font-medium">{tool.Name}</span>
                  {tool.BinPath ? (
                    <span className="text-muted-foreground">{t('ui.agent_setup.installed')}</span>
                  ) : (
                    <span className="text-muted-foreground">{t('ui.agent_setup.not_installed_manual')}</span>
                  )}
                </div>
              )}
            </li>
          );
        })}
      </ul>
      {installing && (
        <pre className="mt-2 max-h-28 overflow-auto rounded bg-muted p-2 font-mono text-[11px] text-muted-foreground">
          {activeName ? t('ui.agent_setup.installing_n', { name: activeName }) : ''}
          {log}
        </pre>
      )}
      <div className="mt-3 flex justify-end gap-2">
        <Button type="button" variant="secondary" disabled={installing} onClick={() => void closeAfterDismiss()}>
          {t('ui.agent_setup.skip')}
        </Button>
        <Button
          type="button"
          disabled={installing || selectedIds.length === 0}
          onClick={() => void handleInstall()}
        >
          {installing ? t('ui.agent_setup.installing') : t('ui.agent_setup.install_all')}
        </Button>
      </div>
    </Dialog>
  );
}
