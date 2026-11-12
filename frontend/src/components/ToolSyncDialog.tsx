// 工具「模型设置」弹窗：按工具定制平台模型同步（取代旧的全量注入 + 总开关）。
// - claude：先选平台（仅 Anthropic 端点），再为 4 类映射槽位选该平台模型；
//   静态注入只有一份 env，模型只能来自所选平台。
// - opencode：按平台勾选模型（OpenCode 支持多 provider 并存），未勾选的平台不建 provider。
// - codebuddy：先选平台（仅 OpenAI 端点），再勾选该平台模型（CLI 自定义条目按裸模型名透传）。
// - codex/gemini：没有静态注入，仅保存每工具平台（供启动端点解析），不显示启用开关。
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

// reconcileSlots 丢弃不属于指定平台模型列表的槽位值：旧平台被删/失去端点后
// 回落首个可用平台时，避免把不可见的旧模型名连同新平台端点写入工具配置。
function reconcileSlots(models: string[], slots: ClaudeSlots): ClaudeSlots {
  const has = new Set(models);
  return {
    Model: has.has(slots.Model) ? slots.Model : '',
    Sonnet: has.has(slots.Sonnet) ? slots.Sonnet : '',
    Opus: has.has(slots.Opus) ? slots.Opus : '',
    Haiku: has.has(slots.Haiku) ? slots.Haiku : '',
  };
}

export function ToolSyncDialog({ toolID, toolName, platforms, onClose }: Props) {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [loading, setLoading] = useState(true);
  const [enabled, setEnabled] = useState(false);
  const [claude, setClaude] = useState<ClaudeSlots>(EMPTY_SLOTS);
  // 静态注入目标各自选定的平台 ID（空 = 第一个可用平台）
  const [claudePlat, setClaudePlat] = useState('');
  const [codebuddyPlat, setCodebuddyPlat] = useState('');
  // codex/gemini 只保存每工具平台（无静态注入），空 = 跟随全局默认。
  // 悬空 ID 原样保留在 state，下拉显示「已失效」占位后允许重选。
  const [toolPlat, setToolPlat] = useState('');
  // 平台 ID → 勾选的模型集合（opencode 按平台；codebuddy 用 __codebuddy__ 键）
  const [checked, setChecked] = useState<Record<string, string[]>>({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    getToolSyncConfig(toolID)
      .then((v) => {
        if (cancelled) return;
        setEnabled(v.Enabled);
        // 生效平台 = 已保存平台（命中且端点可用）否则第一个可用平台；
        // 旧平台失效时按新平台模型列表归整，丢弃不可见的陈旧选择。
        const savedPlats = v.ToolPlatforms ?? {};
        const claudePlat =
          platforms.find((p) => p.ID === (savedPlats.claude ?? '') && !!p.AnthropicBaseURL.trim()) ??
          platforms.find((p) => !!p.AnthropicBaseURL.trim());
        setClaudePlat(claudePlat?.ID ?? '');
        setClaude(reconcileSlots(claudePlat?.Models ?? [], v.Claude ?? EMPTY_SLOTS));

        const codebuddyPlat =
          platforms.find((p) => p.ID === (savedPlats.codebuddy ?? '') && !!p.OpenAIBaseURL.trim()) ??
          platforms.find((p) => !!p.OpenAIBaseURL.trim());
        setCodebuddyPlat(codebuddyPlat?.ID ?? '');
        // codex/gemini 不做端点回落：悬空值保留，供下拉显示占位。
        setToolPlat(savedPlats[toolID] ?? '');

        const map: Record<string, string[]> = {};
        for (const [k, list] of Object.entries(v.OpenCode ?? {})) {
          map[k] = list ?? [];
        }
        if (toolID === 'codebuddy') {
          const allowed = new Set(codebuddyPlat?.Models ?? []);
          map.__codebuddy__ = (v.CodeBuddy ?? []).filter((m) => allowed.has(m));
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

  // workbuddy 平台是动态凭据特型（端点由进程内代理提供），不能作为静态注入
  // 目标的端点来源，统一排除出候选；codex/gemini 也只做每工具平台选择。
  const platformOnly = toolID === 'codex' || toolID === 'gemini';
  const staticPlats = platforms.filter((p) => p.Preset !== 'workbuddy');
  // 静态注入目标的平台候选：Claude 仅 Anthropic 端点，CodeBuddy 仅 OpenAI 端点。
  const anthropicPlats = staticPlats.filter((p) => !!p.AnthropicBaseURL.trim());
  const openaiPlats = staticPlats.filter((p) => !!p.OpenAIBaseURL.trim());
  const noEndpointPlats = staticPlats.filter((p) => !p.OpenAIBaseURL.trim());
  // 已保存平台有效则沿用，否则回落第一个可用平台。
  const claudeEffective = anthropicPlats.find((p) => p.ID === claudePlat) ?? anthropicPlats[0];
  const codebuddyEffective = openaiPlats.find((p) => p.ID === codebuddyPlat) ?? openaiPlats[0];
  const toolPlatDangling = !!toolPlat && !staticPlats.some((p) => p.ID === toolPlat);

  // 切 Claude 平台：新平台不含的槽位值清空，避免保存悬空模型名。
  const handleClaudePlatform = (id: string) => {
    setClaudePlat(id);
    const models = platforms.find((p) => p.ID === id)?.Models ?? [];
    setClaude((prev) => ({
      Model: models.includes(prev.Model) ? prev.Model : '',
      Sonnet: models.includes(prev.Sonnet) ? prev.Sonnet : '',
      Opus: models.includes(prev.Opus) ? prev.Opus : '',
      Haiku: models.includes(prev.Haiku) ? prev.Haiku : '',
    }));
  };

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
    // 平台选择统一走 ToolPlatforms 映射；悬空值原样提交，由后端 validPlatformID
    // 统一丢弃（与 claude/codebuddy 语义一致，不在前端静默改写）。
    const toolPlatforms: Record<string, string> = {};
    if (toolID === 'claude') toolPlatforms.claude = claudeEffective?.ID ?? '';
    if (toolID === 'codebuddy') toolPlatforms.codebuddy = codebuddyEffective?.ID ?? '';
    if (platformOnly) toolPlatforms[toolID] = toolPlat;

    const input: ToolSyncInput = {
      Enabled: enabled,
      Claude: claude,
      ToolPlatforms: toolPlatforms,
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

  const slotOptions = claudeEffective?.Models ?? [];
  const claudeInjectable = !!claudeEffective;
  const codebuddyInjectable = !!codebuddyEffective;

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
          {!platformOnly && (
            <>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                />
                {t('ui.toolsync.enable')}
              </label>
              <p className="text-xs text-muted-foreground">{t('ui.toolsync.hint')}</p>
            </>
          )}

          {platformOnly && (
            <div className="grid gap-2">
              <p className="text-xs text-muted-foreground">
                {t('ui.settings.tool_platform_hint')}
              </p>
              <label className="grid grid-cols-[5rem_1fr] items-center gap-2 text-xs">
                <span className="text-muted-foreground">{t('ui.settings.tool_platform')}</span>
                <select
                  className="rounded border border-input bg-card px-2 py-1 text-sm"
                  value={toolPlat}
                  onChange={(e) => setToolPlat(e.target.value)}
                >
                  <option value="">{t('ui.settings.tool_platform_unset')}</option>
                  {/* 悬空值：外部删除了平台，但映射里仍保留该 ID。显示占位而不是
                      空白，用户可直接改选；提交后后端按既有语义丢弃。 */}
                  {toolPlatDangling && (
                    <option value={toolPlat}>{t('ui.settings.tool_platform_invalid')}</option>
                  )}
                  {staticPlats.map((p) => (
                    <option key={p.ID} value={p.ID}>
                      {p.Name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          )}

          {toolID === 'claude' && (
            <div className="grid gap-2">
              {!claudeInjectable && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  {t('ui.toolsync.claude_no_endpoint')}
                </p>
              )}
              <label className="grid grid-cols-[5rem_1fr] items-center gap-2 text-xs">
                <span className="text-muted-foreground">{t('ui.toolsync.platform')}</span>
                <select
                  className="rounded border border-input bg-card px-2 py-1 text-sm"
                  value={claudeEffective?.ID ?? ''}
                  onChange={(e) => handleClaudePlatform(e.target.value)}
                >
                  {anthropicPlats.map((p) => (
                    <option key={p.ID} value={p.ID}>
                      {p.Name}
                    </option>
                  ))}
                </select>
              </label>
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
                  <p className="mb-1 text-xs font-medium">{p.Name}</p>
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
              <label className="grid grid-cols-[5rem_1fr] items-center gap-2 text-xs">
                <span className="text-muted-foreground">{t('ui.toolsync.platform')}</span>
                <select
                  className="rounded border border-input bg-card px-2 py-1 text-sm"
                  value={codebuddyEffective?.ID ?? ''}
                  onChange={(e) => {
                    setCodebuddyPlat(e.target.value);
                    // 勾选与平台绑定：切平台后清空，避免把旧平台模型写到新平台。
                    setChecked((prev) => ({ ...prev, __codebuddy__: [] }));
                  }}
                >
                  {openaiPlats.map((p) => (
                    <option key={p.ID} value={p.ID}>
                      {p.Name}
                    </option>
                  ))}
                </select>
              </label>
              {codebuddyEffective &&
                (codebuddyEffective.Models.length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t('ui.toolsync.models_empty')}</p>
                ) : (
                  <div className="grid gap-1 rounded border border-border p-2">
                    <p className="text-xs font-medium">{codebuddyEffective.Name}</p>
                    {codebuddyEffective.Models.map((m) => checkboxRow('__codebuddy__', m))}
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
