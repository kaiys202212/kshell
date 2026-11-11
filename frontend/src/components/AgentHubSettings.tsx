// 设置页 AgentHub 区块：启用开关即时翻转（失败回读兜正）、心跳阈值、直通关键词，
// 其余设置走「保存」按钮统一落盘（dirty 才可点）。
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getAgentHubSettings, saveAgentHubSettings, setAgentHubEnabled } from '../lib/api';
import { backendError } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';

const inputClass = 'rounded border border-input bg-card px-2 py-1 text-sm';

export function AgentHubSettings() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [enabled, setEnabled] = useState(true);
  const [heartbeat, setHeartbeat] = useState(5);
  const [patterns, setPatterns] = useState('');
  const [dirty, setDirty] = useState(false);

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
  const toggle = async () => {
    const next = !enabled;
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

  const handleSave = async () => {
    const list = patterns
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean);
    try {
      await saveAgentHubSettings(heartbeat, list);
      setDirty(false);
      notify(t('ui.settings.agenthub.saved'), 'info');
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  return (
    <section className="mb-5 rounded border border-border bg-card p-3.5">
      <h2 className="mb-3 text-sm font-medium">{t('ui.settings.agenthub.title')}</h2>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={enabled}
          onChange={() => void toggle()}
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
  );
}
