// 工具「模型设置」弹窗：按工具定制平台模型同步（取代旧的全量注入 + 总开关）。
// - claude：先选平台（仅 Anthropic 端点），再为 4 类映射槽位选该平台模型；
//   静态注入只有一份 env，模型只能来自所选平台。
// - opencode：按平台勾选模型（OpenCode 支持多 provider 并存），未勾选的平台不建 provider。
// - codebuddy：先选平台（仅 OpenAI 端点），再勾选该平台模型（CLI 自定义条目按裸模型名透传）。
// - codex/gemini：没有静态注入，仅保存每工具平台（供启动端点解析），不显示启用开关。
// claude/opencode/codebuddy 未启用或未勾选任何模型 = 不注入（后端走「只清不建」
// 清理既有注入物）；codex/gemini 无注入物，保存平台选择即生效。
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
import { PLATFORM_ONLY_TARGETS } from '../lib/toolSync';
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
        const savedPlats = v.ToolPlatforms ?? {};
        // 保存值原样保留（空 = 未配置，渲染期回落第一个可用平台；悬空 = 已失效
        // 占位），不在挂载时把「未配置」固化成某个平台 ID。槽位值同样原样保留，
        // 由下方渲染期归整处理悬空/平台切换（平台列表异步到达也能自愈）。
        setClaudePlat(savedPlats.claude ?? '');
        setClaude(v.Claude ?? EMPTY_SLOTS);
        setCodebuddyPlat(savedPlats.codebuddy ?? '');
        // codex/gemini 同样保留原始悬空值，供下拉显示占位。
        setToolPlat(savedPlats[toolID] ?? '');

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

  // workbuddy 平台统一排除出「静态注入目标」候选：它是动态凭据特型，端点由
  // 进程内代理提供。claude/opencode/codebuddy 属于静态注入场景，天然不选它；
  // codex/gemini 是一期边界（走 Responses / Gemini 原生协议，本地代理不翻译），
  // 同样不受理 workbuddy 模型。UI「已失效」判定因此以 staticPlats 为基准。
  const platformOnly = PLATFORM_ONLY_TARGETS.has(toolID);
  const staticPlats = platforms.filter((p) => p.Preset !== 'workbuddy');
  // 静态注入目标的平台候选：Claude 仅 Anthropic 端点，CodeBuddy 仅 OpenAI 端点。
  const anthropicPlats = staticPlats.filter((p) => !!p.AnthropicBaseURL.trim());
  const openaiPlats = staticPlats.filter((p) => !!p.OpenAIBaseURL.trim());
  const noEndpointPlats = staticPlats.filter((p) => !p.OpenAIBaseURL.trim());
  // 悬空值：已保存 ID 不在候选列表。保留原值并用「已失效」占位，不回落第一个。
  const claudeDangling = !!claudePlat && !anthropicPlats.some((p) => p.ID === claudePlat);
  const codebuddyDangling = !!codebuddyPlat && !openaiPlats.some((p) => p.ID === codebuddyPlat);
  const toolPlatDangling = !!toolPlat && !staticPlats.some((p) => p.ID === toolPlat);
  // 生效平台：悬空时为空（只显示占位）；未配置时在渲染期回落第一个可用平台，
  // 恢复原有自愈语义（平台列表异步到达也能算出结果）。
  const claudeEffective = claudeDangling
    ? undefined
    : (anthropicPlats.find((p) => p.ID === claudePlat) ?? anthropicPlats[0]);
  const codebuddyEffective = codebuddyDangling
    ? undefined
    : (openaiPlats.find((p) => p.ID === codebuddyPlat) ?? openaiPlats[0]);
  const claudeSelectValue = claudeDangling ? claudePlat : (claudeEffective?.ID ?? '');
  const codebuddySelectValue = codebuddyDangling ? codebuddyPlat : (codebuddyEffective?.ID ?? '');

  // 平台列表异步到达/切换后，把槽位值归整到生效平台的模型列表，避免下拉出现
  // 不在选项里的陈旧模型名（挂载时平台列表可能尚未到达，只能在此归整）。
  // platforms 为空视为“尚未加载”：此时任何 ID 都像悬空值，直接归整会误清槽位。
  const claudeModelKey = (claudeEffective?.Models ?? []).join('\n');
  useEffect(() => {
    if (loading || platforms.length === 0) return;
    // 悬空平台没有生效模型列表，归整结果即清空槽位。
    setClaude((prev) => reconcileSlots(claudeEffective?.Models ?? [], prev));
    // claudeEffective 对象每次渲染新建，用 ID + 模型列表内容作稳定依赖。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading, platforms.length, claudeDangling, claudeEffective?.ID, claudeModelKey]);

  // CodeBuddy 勾选同理：平台列表到达/切换后只保留生效平台下的模型。
  const codebuddyModelKey = (codebuddyEffective?.Models ?? []).join('\n');
  useEffect(() => {
    if (loading || platforms.length === 0) return;
    const allowed = new Set(codebuddyEffective?.Models ?? []);
    setChecked((prev) => ({
      ...prev,
      __codebuddy__: (prev.__codebuddy__ ?? []).filter((m) => allowed.has(m)),
    }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading, platforms.length, codebuddyDangling, codebuddyEffective?.ID, codebuddyModelKey]);

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
    // 平台选择统一走 ToolPlatforms 映射：悬空值原样提交，由后端 validPlatformID
    // 统一丢弃（与 codex/gemini 语义一致，不在前端静默改写）；未配置时提交渲染期
    // 回落到的第一个可用平台，使「已生效」与保存值一致。
    const toolPlatforms: Record<string, string> = {};
    if (toolID === 'claude') {
      toolPlatforms.claude = claudeDangling ? claudePlat : (claudeEffective?.ID ?? '');
    }
    if (toolID === 'codebuddy') {
      toolPlatforms.codebuddy = codebuddyDangling ? codebuddyPlat : (codebuddyEffective?.ID ?? '');
    }
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
                {t('ui.toolsync.tool_platform_hint')}
              </p>
              <label className="grid grid-cols-[5rem_1fr] items-center gap-2 text-xs">
                <span className="text-muted-foreground">{t('ui.toolsync.platform')}</span>
                <select
                  className="rounded border border-input bg-card px-2 py-1 text-sm"
                  value={toolPlat}
                  onChange={(e) => setToolPlat(e.target.value)}
                >
                  <option value="">{t('ui.toolsync.tool_platform_unset')}</option>
                  {/* 悬空值：外部删除了平台，但映射里仍保留该 ID。显示占位而不是
                      空白，用户可直接改选；提交后后端按既有语义丢弃。 */}
                  {toolPlatDangling && (
                    <option value={toolPlat}>{t('ui.toolsync.tool_platform_invalid')}</option>
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
                  value={claudeSelectValue}
                  onChange={(e) => handleClaudePlatform(e.target.value)}
                >
                  {/* 静态注入没有「跟随全局默认」语义：未保存时渲染期即回落到
                      第一个可用平台，因此不提供空选项，避免误以为可取消选择。 */}
                  {claudeDangling && (
                    <option value={claudePlat}>{t('ui.toolsync.tool_platform_invalid')}</option>
                  )}
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
                  value={codebuddySelectValue}
                  onChange={(e) => {
                    setCodebuddyPlat(e.target.value);
                    // 勾选与平台绑定：切平台后清空，避免把旧平台模型写到新平台。
                    setChecked((prev) => ({ ...prev, __codebuddy__: [] }));
                  }}
                >
                  {/* 同 claude：未保存时渲染期回落第一个可用平台，无空选项。 */}
                  {codebuddyDangling && (
                    <option value={codebuddyPlat}>{t('ui.toolsync.tool_platform_invalid')}</option>
                  )}
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
