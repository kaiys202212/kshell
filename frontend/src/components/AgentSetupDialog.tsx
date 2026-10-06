// 桌面端首次启动：扫描内置 Agent 并支持勾选后串行一键安装。可跳过，只自动弹一次。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useMemo, useState } from 'react';
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
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

export default function AgentSetupDialog() {
  const notify = useAppStore((s) => s.notify);
  const [open, setOpen] = useState(false);
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [recipes, setRecipes] = useState<Record<string, InstallRecipeView>>({});
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [installing, setInstalling] = useState(false);
  const [activeId, setActiveId] = useState('');
  const [log, setLog] = useState('');

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

  const closeAfterDismiss = async () => {
    try {
      await dismissAgentSetup();
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
    setOpen(false);
  };

  const handleInstall = async () => {
    if (installing || selectedIds.length === 0) return;
    setInstalling(true);
    setLog('');
    const offLog = onToolInstallLog((p) => {
      setActiveId(p.toolID);
      setLog((prev) => (prev ? `${prev}\n${p.text}` : p.text));
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
            notify(result.error || `${id} 安装失败`, 'error');
          }
        } catch (e: unknown) {
          offDone();
          notify(e instanceof Error ? e.message : String(e), 'error');
        }
      }
      await closeAfterDismiss();
    } finally {
      offLog();
      setInstalling(false);
      setActiveId('');
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v && !installing) void closeAfterDismiss();
      }}
      className="w-[28rem] max-w-[90vw]"
    >
      <DialogPrimitive.Title className="text-sm font-medium">
        检测本机 Agent 工具
      </DialogPrimitive.Title>
      <p className="mt-1 text-xs text-muted-foreground">
        勾选未安装的工具后可一键安装。也可跳过，之后在设置中安装。
      </p>
      <ul className="mt-3 max-h-64 divide-y divide-border overflow-auto rounded border border-border">
        {tools.map((t) => {
          const canInstall = !t.BinPath && !!recipes[t.ID];
          return (
            <li key={t.ID} className="flex items-center justify-between gap-2 px-2.5 py-1.5 text-xs">
              {canInstall ? (
                <label className="flex min-w-0 items-center gap-2">
                  <input
                    type="checkbox"
                    checked={!!selected[t.ID]}
                    disabled={installing}
                    aria-label={t.Name}
                    onChange={(e) =>
                      setSelected((prev) => ({ ...prev, [t.ID]: e.target.checked }))
                    }
                  />
                  <span className="font-medium">{t.Name}</span>
                  <span className="text-muted-foreground">未安装</span>
                </label>
              ) : (
                <div className="flex min-w-0 items-center gap-2">
                  <span className="font-medium">{t.Name}</span>
                  {t.BinPath ? (
                    <span className="text-muted-foreground">已安装</span>
                  ) : (
                    <span className="text-muted-foreground">未安装（需手动）</span>
                  )}
                </div>
              )}
            </li>
          );
        })}
      </ul>
      {installing && (
        <pre className="mt-2 max-h-28 overflow-auto rounded bg-muted p-2 font-mono text-[11px] text-muted-foreground">
          {activeId ? `正在安装 ${activeId}…\n` : ''}
          {log}
        </pre>
      )}
      <div className="mt-3 flex justify-end gap-2">
        <Button type="button" variant="secondary" disabled={installing} onClick={() => void closeAfterDismiss()}>
          跳过
        </Button>
        <Button
          type="button"
          disabled={installing || selectedIds.length === 0}
          onClick={() => void handleInstall()}
        >
          {installing ? '安装中…' : '一键安装'}
        </Button>
      </div>
    </Dialog>
  );
}
