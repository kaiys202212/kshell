// 首次项目选择弹窗：等首个 scan:done 后判定；仅对新用户弹一次，默认全选，
// 确认后未勾选的工作区隐藏进回收站（可还原），关闭视为保留全部。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  dismissProjectSetup,
  getWorkspaces,
  hideProject,
  needsAgentSetup,
  needsProjectSetup,
  onScanDone,
} from '../lib/api';
import type { Workspace } from '../lib/api';
import { backendError } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

export default function ProjectSetupDialog() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Workspace[]>([]);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [confirming, setConfirming] = useState(false);
  // 判定只做一次：弹窗已打开或确认中时，后续 scan:done（设置页刷新等）不得重置勾选状态
  const decidedRef = useRef(false);

  useEffect(() => {
    let cancelled = false;
    // 首扫完成才有可信的工作区列表：等 scan:done 再判定，不在挂载时抢跑。
    const offScan = onScanDone(() => {
      if (decidedRef.current) return;
      void (async () => {
        try {
          // Agent 安装向导要弹时本次让位：等下次启动（届时向导已 dismissed）再引导。
          if (await needsAgentSetup()) return;
          if (!(await needsProjectSetup())) return;
          const list = await getWorkspaces();
          if (cancelled) return;
          decidedRef.current = true;
          if (list.length === 0) {
            // 空结果放弃引导：落盘标记永不再弹，但不要给用户看一个空列表弹窗。
            await dismissProjectSetup();
            return;
          }
          const sel: Record<string, boolean> = {};
          for (const ws of list) sel[ws.Path] = true; // 默认全选
          setItems(list);
          setSelected(sel);
          setOpen(true);
        } catch {
          // 判定链路任一环失败都静默跳过：这是引导性功能，不该打扰用户
        }
      })();
    });
    return () => {
      cancelled = true;
      offScan();
    };
  }, []);

  // 仅落盘标记并关闭；隐藏与否由调用路径决定
  const dismissAndClose = async () => {
    try {
      await dismissProjectSetup();
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
    setOpen(false);
  };

  const handleConfirm = async () => {
    if (confirming) return;
    setConfirming(true);
    // 未勾选的逐个隐藏进回收站；单项失败 notify 后继续，避免半途而废留下混合状态
    for (const ws of items) {
      if (selected[ws.Path]) continue;
      try {
        await hideProject(ws.Path);
      } catch (e: unknown) {
        notify(backendError(e), 'error');
      }
    }
    setConfirming(false);
    await dismissAndClose();
  };

  const selectedCount = items.filter((ws) => selected[ws.Path]).length;

  return (
    <Dialog open={open} onOpenChange={() => {}} dismissible={false} className="w-[32rem] max-w-[90vw]">
      <DialogPrimitive.Title className="text-sm font-medium">
        {t('ui.project_setup.title')}
      </DialogPrimitive.Title>
      <p className="mt-1 text-xs text-muted-foreground">{t('ui.project_setup.hint')}</p>
      <ul className="mt-3 max-h-64 divide-y divide-border overflow-auto rounded border border-border">
        {items.map((ws) => (
          <li key={ws.Path} className="px-2.5 py-1.5 text-xs">
            <label className="flex min-w-0 items-center gap-2">
              <input
                type="checkbox"
                checked={!!selected[ws.Path]}
                disabled={confirming}
                aria-label={ws.Path}
                onChange={(e) =>
                  setSelected((prev) => ({ ...prev, [ws.Path]: e.target.checked }))
                }
              />
              <span className="min-w-0 flex-1 truncate">
                <span className="font-medium">{ws.Name}</span>
                <span className="ml-2 text-muted-foreground">{ws.Path}</span>
              </span>
            </label>
          </li>
        ))}
      </ul>
      <p className="mt-2 text-xs text-muted-foreground">
        {t('ui.project_setup.count', { count: selectedCount, total: items.length })}
      </p>
      <div className="mt-3 flex justify-end gap-2">
        <Button type="button" variant="secondary" disabled={confirming} onClick={() => void dismissAndClose()}>
          {t('ui.project_setup.close')}
        </Button>
        <Button type="button" disabled={confirming} onClick={() => void handleConfirm()}>
          {t('ui.project_setup.confirm')}
        </Button>
      </div>
    </Dialog>
  );
}
