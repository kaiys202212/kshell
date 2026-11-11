// 工具「模型设置」弹窗：按工具定制平台模型同步（取代旧的全量注入 + 总开关）。
// - claude：4 类映射槽位（默认模型 / Sonnet / Opus / Haiku），各自选默认平台模型；
//   静态注入只有一份 env，模型只能来自默认平台。
// - opencode：按平台勾选模型（OpenCode 支持多 provider 并存），未勾选的平台不建 provider。
// - codebuddy：勾选默认平台模型（CLI 自定义条目按裸模型名透传，仅默认平台一份）。
// 未启用或未勾选任何模型 = 不注入（后端走「只清不建」清理既有注入物）。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  getToolSyncConfig,
  setToolSyncConfig,
  type ClaudeSlots,
  type ModelPlatformView,
  type ToolSyncInput,
} from '../lib/api';
import { backendError } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

interface Props {
  toolID: string;
  toolName: string;
  platforms: ModelPlatformView[];
  onClose: () => void;
}

const EMPTY_SLOTS: ClaudeSlots = { Model: '', Sonnet: '', Opus: '', Haiku: '' };

const CLAUDE_SLOT_FIELDS: { key: keyof ClaudeSlots; labelKey: string }[] = [
  { key: 'Model', labelKey: 'ui.toolsync.slot_model' },
  { key: 'Sonnet', labelKey: 'ui.toolsync.slot_sonnet' },
  { key: 'Opus', labelKey: 'ui.toolsync.slot_opus' },
  { key: 'Haiku', labelKey: 'ui.toolsync.slot_haiku' },
];

export function ToolSyncDialog({ toolID, toolName, platforms, onClose }: Props) {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [loading, setLoading] = useState(true);
  const [enabled, setEnabled] = useState(false);
  const [claude, setClaude] = useState<ClaudeSlots>(EMPTY_SLOTS);
  // 平台 ID → 勾选的模型集合（opencode 按平台；codebuddy 只用默认平台键）
  const [checked, setChecked] = useState<Record<string, string[]>>({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    getToolSyncConfig(toolID)
      .then((v) => {
        if (cancelled) return;
        setEnabled(v.Enabled);
        setClaude(v.Claude ?? EMPTY_SLOTS);
        const map: Record<string, string[]> = {};
        for (const [k, list] of Object.entries(v.OpenCode ?? {})) {
          map[k] = list ?? [];
        }
        if (toolID === 'codebuddy') {
          map.__codebuddy__ = v.CodeBuddy ?? [];
        }
        setChecked(map);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        notify(backendError(e), 'error');
        onClose();
      });
    return () => {
      cancelled = true;
    };
    // onClose 经父组件内联箭头传入，仅挂载时取一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toolID]);

  // 默认平台：徽标优先，缺省回落第一个（与设置页默认模型口径一致）
  const defPlat = platforms.find((p) => p.IsDefault) ?? platforms[0];

  const toggleModel = (group: string, model: string) => {
    setChecked((prev) => {
      const list = prev[group] ?? [];
      const next = list.includes(model) ? list.filter((m) => m !== model) : [...list, model];
      return { ...prev, [group]: next };
    });
  };

  const handleSave = async () => {
    if (saving) return;
    setSaving(true);
    const input: ToolSyncInput = {
      Enabled: enabled,
      Claude: claude,
      OpenCode: Object.fromEntries(
        Object.entries(checked)
          .filter(([k, list]) => k !== '__codebuddy__' && list.length > 0)
          .map(([k, list]) => [k, list]),
      ),
      CodeBuddy: checked['__codebuddy__'] ?? [],
    };
    try {
      await setToolSyncConfig(toolID, input);
      notify(t('ui.toolsync.saved'), 'success');
      onClose();
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    } finally {
      setSaving(false);
    }
  };

  const slotOptions = defPlat?.Models ?? [];
  const claudeInjectable = !!defPlat && !!defPlat.AnthropicBaseURL.trim();
  const codebuddyInjectable = !!defPlat && !!defPlat.OpenAIBaseURL.trim();
  // opencode 可勾选平台：有 OpenAI 端点的平台；无端点平台（如 WorkBuddy）单独提示
  const openaiPlats = platforms.filter((p) => !!p.OpenAIBaseURL.trim());
  const noEndpointPlats = platforms.filter((p) => !p.OpenAIBaseURL.trim());

  const checkboxRow = (group: string, model: string) => (
    <label key={model} className="flex items-center gap-2 text-xs">
      <input
        type="checkbox"
        checked={(checked[group] ?? []).includes(model)}
        onChange={() => toggleModel(group, model)}
      />
      <span className="truncate">{model}</span>
    </label>
  );

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }} className="w-[30rem] max-w-[90vw]">
      <DialogPrimitive.Title className="text-sm font-medium">
        {t('ui.toolsync.title', { name: toolName })}
      </DialogPrimitive.Title>
      {loading ? (
        <p className="mt-3 text-xs text-muted-foreground">{t('ui.toolsync.loading')}</p>
      ) : (
        <div className="mt-2 grid gap-3">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
            />
            {t('ui.toolsync.enable')}
          </label>
          <p className="text-xs text-muted-foreground">{t('ui.toolsync.hint')}</p>

          {toolID === 'claude' && (
            <div className="grid gap-2">
              {!claudeInjectable && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  {t('ui.toolsync.claude_no_endpoint')}
                </p>
              )}
              {CLAUDE_SLOT_FIELDS.map(({ key, labelKey }) => (
                <label key={key} className="grid grid-cols-[5rem_1fr] items-center gap-2 text-xs">
                  <span className="text-muted-foreground">{t(labelKey)}</span>
                  <select
                    className="rounded border border-input bg-card px-2 py-1 text-sm"
                    value={claude[key]}
                    onChange={(e) => setClaude({ ...claude, [key]: e.target.value })}
                  >
                    <option value="">{t('ui.toolsync.slot_unset')}</option>
                    {slotOptions.map((m) => (
                      <option key={m} value={m}>
                        {m}
                      </option>
                    ))}
                  </select>
                </label>
              ))}
            </div>
          )}

          {toolID === 'opencode' && (
            <div className="grid gap-2">
              {openaiPlats.map((p) => (
                <div key={p.ID} className="rounded border border-border p-2">
                  <p className="mb-1 text-xs font-medium">
                    {p.Name}
                    {p.ID === defPlat?.ID && (
                      <span className="ml-1 text-muted-foreground">{t('ui.toolsync.default_tag')}</span>
                    )}
                  </p>
                  {p.Models.length === 0 ? (
                    <p className="text-xs text-muted-foreground">{t('ui.toolsync.models_empty')}</p>
                  ) : (
                    <div className="grid gap-1">{p.Models.map((m) => checkboxRow(p.ID, m))}</div>
                  )}
                </div>
              ))}
              {noEndpointPlats.map((p) => (
                <p key={p.ID} className="text-xs text-muted-foreground">
                  {t('ui.toolsync.platform_no_endpoint', { 0: p.Name })}
                </p>
              ))}
            </div>
          )}

          {toolID === 'codebuddy' && (
            <div className="grid gap-2">
              {!codebuddyInjectable && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  {t('ui.toolsync.codebuddy_no_endpoint')}
                </p>
              )}
              {defPlat &&
                (defPlat.Models.length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t('ui.toolsync.models_empty')}</p>
                ) : (
                  <div className="grid gap-1 rounded border border-border p-2">
                    <p className="text-xs font-medium">
                      {defPlat.Name}
                      <span className="ml-1 text-muted-foreground">{t('ui.toolsync.default_tag')}</span>
                    </p>
                    {defPlat.Models.map((m) => checkboxRow('__codebuddy__', m))}
                  </div>
                ))}
            </div>
          )}
        </div>
      )}
      <div className="mt-3 flex items-center justify-end gap-2">
        <Button type="button" variant="secondary" onClick={onClose}>
          {t('ui.toolsync.cancel')}
        </Button>
        <Button type="button" disabled={loading || saving} onClick={() => void handleSave()}>
          {t('ui.toolsync.save')}
        </Button>
      </div>
    </Dialog>
  );
}
