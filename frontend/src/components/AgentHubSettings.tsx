// 设置页 AgentHub 区块：启用开关即时翻转（关闭先确认并解绑微信、失败回读兜正）、
// 心跳阈值、直通关键词，其余设置走「保存」按钮统一落盘（dirty 才可点）。
// 微信扫码绑定区只在启用时渲染（关闭即自动解绑，没有可展示的绑定态）。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getAgentHubSettings, saveAgentHubSettings, setAgentHubEnabled } from '../lib/api';
import { backendError } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
import { WeixinSettings } from './WeixinSettings';

const inputClass = 'rounded border border-input bg-card px-2 py-1 text-sm';

export function AgentHubSettings() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [enabled, setEnabled] = useState(true);
  const [heartbeat, setHeartbeat] = useState(5);
  const [patterns, setPatterns] = useState('');
  const [dirty, setDirty] = useState(false);
  // 关闭确认框：关闭必须二次确认（会同时解绑微信）
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    getAgentHubSettings()
      .then((s) => {
        setEnabled(s.enabled);
        setHeartbeat(s.heartbeat_minutes);
        setPatterns((s.direct_patterns ?? []).join('\n'));
      })
      .catch(() => {});
  }, []);

  // 启用开关即时落盘；失败时回读后端真实值兜正（乐观翻转可能撤不回来）
  const applyEnabled = async (next: boolean) => {
    setEnabled(next);
    try {
      await setAgentHubEnabled(next);
    } catch (e: unknown) {
      notify(backendError(e), 'error');
      getAgentHubSettings()
        .then((s) => setEnabled(s.enabled))
        .catch(() => {});
    }
  };

  // 关闭走确认（会解绑微信）；开启无需确认，直接落盘。
  const toggle = () => {
    if (enabled) {
      setConfirmOpen(true);
      return;
    }
    void applyEnabled(true);
  };

  const confirmDisable = () => {
    setConfirmOpen(false);
    void applyEnabled(false);
  };

  const handleSave = async () => {
    const list = patterns
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean);
    // 钳制到 1..120 同步回 UI（非有限数回落 5，后端还会再做一次归一）
    const hb = Math.round(heartbeat);
    const clamped = Number.isFinite(hb) ? Math.max(1, Math.min(120, hb)) : 5;
    setHeartbeat(clamped);
    try {
      await saveAgentHubSettings(clamped, list);
      setDirty(false);
      notify(t('ui.settings.agenthub.saved'), 'info');
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  return (
    <>
      <section className="mb-5 rounded border border-border bg-card p-3.5">
        <h2 className="mb-3 text-sm font-medium">{t('ui.settings.agenthub.title')}</h2>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={enabled}
            onChange={toggle}
            aria-label={t('ui.settings.agenthub.enabled')}
          />
          {t('ui.settings.agenthub.enabled')}
        </label>
        <p className="mb-3 text-xs text-muted-foreground">{t('ui.settings.agenthub.enabled_hint')}</p>
        <label className="mb-3 flex items-center gap-2 text-sm">
          <span className="shrink-0">{t('ui.settings.agenthub.heartbeat')}</span>
          <input
            type="number"
            min={1}
            max={120}
            value={heartbeat}
            aria-label={t('ui.settings.agenthub.heartbeat')}
            className={`${inputClass} w-20`}
            onChange={(e) => {
              setHeartbeat(Number(e.currentTarget.value));
              setDirty(true);
            }}
          />
        </label>
        <label className="mb-3 block text-sm">
          {t('ui.settings.agenthub.patterns')}
          <textarea
            rows={4}
            value={patterns}
            aria-label={t('ui.settings.agenthub.patterns')}
            className={`mt-1 block w-full font-mono text-xs ${inputClass}`}
            onChange={(e) => {
              setPatterns(e.currentTarget.value);
              setDirty(true);
            }}
          />
        </label>
        <Button size="sm" disabled={!dirty} onClick={() => void handleSave()}>
          {t('ui.settings.agenthub.save')}
        </Button>
      </section>

      {enabled && <WeixinSettings />}

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen} className="w-96">
        <DialogPrimitive.Title className="mb-2 text-sm font-medium">
          {t('ui.settings.agenthub.disable_confirm_title')}
        </DialogPrimitive.Title>
        <p className="mb-4 text-sm text-muted-foreground">
          {t('ui.settings.agenthub.disable_confirm_desc')}
        </p>
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="secondary" onClick={() => setConfirmOpen(false)}>
            {t('ui.settings.agenthub.cancel')}
          </Button>
          <Button size="sm" variant="destructive" onClick={confirmDisable}>
            {t('ui.settings.agenthub.disable_confirm_ok')}
          </Button>
        </div>
      </Dialog>
    </>
  );
}
