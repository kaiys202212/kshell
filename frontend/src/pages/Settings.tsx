// 设置页：左导航分区（通用 / 模型 / 工具）+ 右侧内容。
// 通用含外观/语言/关闭/会话模式/权限/关于（检查更新、反馈问题）；模型含预设与双协议 Base URL；工具含检测与自定义表单。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  getAppearance,
  getCloseBehavior,
  getModelConfig,
  getPermissionMode,
  getSessionMode,
  getToolInstallJob,
  getToolInstallRecipe,
  getTools,
  installBuiltinTool,
  listModelPresets,
  formatProvidersYAML,
  loadProvidersYAML,
  parseProvidersYAML,
  pickDirectory,
  pickFile,
  onScanDone,
  onToolsUpdated,
  onToolInstallDone,
  onToolInstallLog,
  applyUpdate,
  checkForUpdate,
  getAppVersion,
  saveProvidersYAML,
  scanSessions,
  setAppearanceMode,
  setAppearanceFontSize,
  setCloseBehavior,
  setLanguage,
  setModelConfig,
  setPermissionMode,
  setSessionMode,
  uninstallBuiltinTool,
} from '../lib/api';
import type { InstallRecipeView, ModelPreset, ToolInfo, ToolInstallJobView, UpdateInfo } from '../lib/api';
import { cn } from '../lib/cn';
import { translateBackend } from '../lib/errors';
import { applyUiFontSize, clampUiFontSize } from '../lib/appearance';
import { getLanguageOptions } from '../i18n';
import { openExternal } from '../lib/openHref';
import { GITHUB_ISSUES_NEW_URL } from '../lib/projectLinks';
import { useAppStore } from '../state/store';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';
import { Dialog } from '../components/ui/dialog';
import { ProvidersEditor } from '../components/ProvidersEditor';
import { normalizeSpec, withoutBuiltinSpecs, type CustomProviderSpec } from '../lib/providersForm';

const MODEL_AGENTS = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex CLI' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'opencode', label: 'OpenCode' },
];

// 语言行内置三选项；system（跟随系统）不是语言码，不在 getLanguageOptions 里
const BUILTIN_LANGUAGE_CODES = ['en', 'zh-CN', 'system'];

// 后端错误串统一过 translateBackend：注册 key 翻成文案，非注册串原样返回
const backendError = (e: unknown) => translateBackend(e instanceof Error ? e.message : String(e));

type Section = 'general' | 'model' | 'tools';

const SECTIONS: { id: Section; labelKey: string }[] = [
  { id: 'general', labelKey: 'ui.settings.nav.general' },
  { id: 'model', labelKey: 'ui.settings.nav.model' },
  { id: 'tools', labelKey: 'ui.settings.nav.tools' },
];

export default function Settings() {
  const [section, setSection] = useState<Section>('general');
  const [tools, setTools] = useState<ToolInfo[] | null>(null);
  const [toolsError, setToolsError] = useState('');
  const [yaml, setYaml] = useState<string | null>(null);
  const [yamlError, setYamlError] = useState('');
  const [saved, setSaved] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);
  const [editorMode, setEditorMode] = useState<'form' | 'source'>('form');
  const [specs, setSpecs] = useState<CustomProviderSpec[]>([]);
  // 工具检测「重新扫描」进行中；tools:updated（DetectAll 完成才推）到达即恢复
  const [rescanning, setRescanning] = useState(false);
  const [appearance, setAppearanceLocal] = useState<string>('dark');
  const [fontSize, setFontSizeLocal] = useState(13);
  const [closeBehavior, setCloseBehaviorLocal] = useState<string>('tray');
  const [sessionMode, setSessionModeLocal] = useState('tui');
  const [permissionMode, setPermissionModeLocal] = useState('default');
  const [presets, setPresets] = useState<ModelPreset[]>([]);
  const [modelEnabled, setModelEnabled] = useState(false);
  const [modelPreset, setModelPreset] = useState('custom');
  const [modelOpenAIURL, setModelOpenAIURL] = useState('');
  const [modelAnthropicURL, setModelAnthropicURL] = useState('');
  const [modelApiKey, setModelApiKey] = useState('');
  const [modelApiKeySet, setModelApiKeySet] = useState(false);
  const [modelClearKey, setModelClearKey] = useState(false);
  const [modelAgents, setModelAgents] = useState<Record<string, string>>({});
  const [modelDirtyHint, setModelDirtyHint] = useState(false);
  const [modelSaving, setModelSaving] = useState(false);
  const [recipes, setRecipes] = useState<Record<string, InstallRecipeView>>({});
  const [job, setJob] = useState<ToolInstallJobView | null>(null);
  const [activeId, setActiveId] = useState('');
  const [installLog, setInstallLog] = useState('');
  const [pendingId, setPendingId] = useState('');
  const [uninstallTarget, setUninstallTarget] = useState<ToolInfo | null>(null);
  const [purgeConfig, setPurgeConfig] = useState(false);
  const [appVersion, setAppVersion] = useState('');
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [updateError, setUpdateError] = useState('');
  const notify = useAppStore((s) => s.notify);
  const notifySound = useAppStore((s) => s.notifySound);
  const setNotifySound = useAppStore((s) => s.setNotifySound);
  const { t } = useTranslation();
  // 语言选中态：store 为准（main.tsx 启动接线 + language:changed 事件写入），
  // 本地 state 只做点击后的即时反馈。依赖整个对象：事件携带新对象身份，
  // 即使 configured 同值确认也能回同步一次。
  const languageInfo = useAppStore((s) => s.language);
  const [languageLocal, setLanguageLocal] = useState(languageInfo.configured);

  useEffect(() => {
    setLanguageLocal(languageInfo.configured);
  }, [languageInfo]);

  const loadRecipes = async (list: ToolInfo[]) => {
    const pairs = await Promise.all(
      list.map((tool) =>
        getToolInstallRecipe(tool.ID)
          .then((r) => [tool.ID, r] as const)
          .catch(() => null),
      ),
    );
    const rec: Record<string, InstallRecipeView> = {};
    for (const p of pairs) {
      if (p) rec[p[0]] = p[1];
    }
    return rec;
  };

  useEffect(() => {
    let cancelled = false;
    getTools()
      .then(async (list) => {
        if (cancelled) return;
        setTools(list);
        const rec = await loadRecipes(list);
        if (!cancelled) setRecipes(rec);
      })
      .catch((e: unknown) => {
        if (!cancelled) setToolsError(backendError(e));
      });
    getToolInstallJob()
      .then((j) => {
        if (cancelled) return;
        setJob(j);
        if (j.Running) {
          setActiveId(j.ToolID);
          setInstallLog(j.Log);
        }
      })
      .catch(() => {});
    loadProvidersYAML()
      .then(async (content) => {
        if (cancelled) return;
        setYaml(content);
        try {
          const list = await parseProvidersYAML(content);
          if (!cancelled) setSpecs(withoutBuiltinSpecs(list));
        } catch (e: unknown) {
          if (!cancelled) setYamlError(backendError(e));
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) setYamlError(backendError(e));
      });
    getAppearance()
      .then((info) => {
        if (!cancelled) {
          setAppearanceLocal(info.mode);
          setFontSizeLocal(clampUiFontSize(info.fontSize));
        }
      })
      .catch(() => {});
    getAppVersion()
      .then((v) => {
        if (!cancelled) setAppVersion(v);
      })
      .catch(() => {});
    getCloseBehavior()
      .then((mode) => {
        if (!cancelled) setCloseBehaviorLocal(mode);
      })
      .catch(() => {});
    getSessionMode()
      .then((mode) => {
        if (!cancelled) setSessionModeLocal(mode);
      })
      .catch(() => {});
    getPermissionMode()
      .then((mode) => {
        if (!cancelled) setPermissionModeLocal(mode);
      })
      .catch(() => {});
    listModelPresets()
      .then((list) => {
        if (!cancelled) setPresets(list);
      })
      .catch(() => {});
    getModelConfig()
      .then((v) => {
        if (cancelled) return;
        setModelEnabled(v.Enabled);
        setModelPreset(v.Preset || 'custom');
        setModelOpenAIURL(v.OpenAIBaseURL);
        setModelAnthropicURL(v.AnthropicBaseURL);
        setModelApiKeySet(v.APIKeySet);
        setModelAgents(v.Agents ?? {});
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  const previewFontSize = (n: number) => {
    const px = clampUiFontSize(n);
    setFontSizeLocal(px);
    applyUiFontSize(px);
    const cur = useAppStore.getState().appearance;
    useAppStore.getState().setAppearance({ ...cur, fontSize: px });
  };

  const handleFontSizeCommit = async (n: number) => {
    const px = clampUiFontSize(n);
    previewFontSize(px);
    try {
      await setAppearanceFontSize(px);
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  useEffect(() => {
    // DetectAll 结束会推 tools:updated（早于会话扫描）；scan:done 再补一次。
    const refresh = () => {
      getTools()
        .then(async (list) => {
          setTools(list);
          setRecipes(await loadRecipes(list));
        })
        .catch((e: unknown) => {
          setToolsError(backendError(e));
        });
    };
    const offScan = onScanDone(() => {
      setRescanning(false);
      refresh();
    });
    const offTools = onToolsUpdated(() => {
      setRescanning(false);
      refresh();
    });
    return () => {
      offScan();
      offTools();
    };
  }, []);

  useEffect(() => {
    const offLog = onToolInstallLog((p) => {
      setActiveId(p.toolID);
      setInstallLog((prev) => (prev ? `${prev}\n${p.text}` : p.text));
      // 自动修复是后端后台启动的，设置页收不到单独的启动事件；
      // 若不刷新 job，Running/Trigger 仍是挂载时的旧值，
      // 「正在自动修复…」提示与行按钮忙态永远不会出现。
      getToolInstallJob()
        .then((j) => setJob(j))
        .catch(() => {});
    });
    const offDone = onToolInstallDone((p) => {
      setPendingId('');
      setJob((prev) => (prev ? { ...prev, Running: false } : prev));
      // 拉取最终 job（errText/trigger），与本地乐观更新互补
      getToolInstallJob()
        .then((j) => setJob(j))
        .catch(() => {});
      if (p.ok === false) {
        notify(p.error ? translateBackend(p.error) : t('ui.settings.tools.action_failed'), 'error');
      }
      getTools()
        .then(async (list) => {
          setTools(list);
          setRecipes(await loadRecipes(list));
        })
        .catch((e: unknown) => {
          setToolsError(backendError(e));
        });
    });
    return () => {
      offLog();
      offDone();
    };
  }, [notify, t]);


  const handleAppearance = async (mode: string) => {
    if (mode === appearance) return;
    try {
      await setAppearanceMode(mode);
      setAppearanceLocal(mode);
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  const handleLanguage = async (code: string) => {
    if (code === languageLocal) return;
    // 即时反馈选中态；持久化成功后 Go 广播 language:changed 由 main.tsx 同步 store，
    // 这里不直接写 store（避免双写）。失败回滚：读 getState() 的新鲜值，避免 render 闭包过期。
    setLanguageLocal(code);
    try {
      await setLanguage(code);
    } catch (e: unknown) {
      setLanguageLocal(useAppStore.getState().language.configured);
      notify(backendError(e), 'error');
    }
  };

  const handleCloseBehavior = async (mode: string) => {
    if (mode === closeBehavior) return;
    try {
      await setCloseBehavior(mode);
      setCloseBehaviorLocal(mode);
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  const handleSessionMode = async (mode: string) => {
    if (mode === sessionMode) return;
    try {
      await setSessionMode(mode);
      setSessionModeLocal(mode);
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  const handlePermissionMode = async (mode: string) => {
    if (mode === permissionMode) return;
    try {
      await setPermissionMode(mode);
      setPermissionModeLocal(mode);
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  const applyPreset = (id: string, forceModels: boolean) => {
    setModelPreset(id);
    setModelDirtyHint(false);
    if (id === 'custom') return;
    const p = presets.find((x) => x.ID === id);
    if (!p) return;
    setModelOpenAIURL(p.OpenAIBaseURL);
    setModelAnthropicURL(p.AnthropicBaseURL);
    setModelAgents((prev) => {
      const next = { ...prev };
      for (const a of MODEL_AGENTS) {
        if (forceModels || !next[a.id]) next[a.id] = p.RecommendedModel;
      }
      return next;
    });
  };

  const handleSave = async () => {
    if (yaml === null || saving) return;
    setSaving(true);
    setSaveError('');
    setSaved(false);
    try {
      let content = yaml;
      if (editorMode === 'form') {
        content = await formatProvidersYAML(specs);
        setYaml(content);
      }
      await saveProvidersYAML(content);
      setSaved(true);
    } catch (e: unknown) {
      setSaveError(backendError(e));
    } finally {
      setSaving(false);
    }
  };

  const handleEditorMode = async (m: 'form' | 'source') => {
    if (m === editorMode) return;
    try {
      if (m === 'source') {
        const content = await formatProvidersYAML(specs);
        setYaml(content);
      } else if (yaml !== null) {
        const list = await parseProvidersYAML(yaml);
        setSpecs(withoutBuiltinSpecs(list));
      }
      setEditorMode(m);
      setYamlError('');
    } catch (e: unknown) {
      setYamlError(backendError(e));
    }
  };

  const handleSaveModel = async () => {
    if (modelSaving) return;
    setModelSaving(true);
    try {
      await setModelConfig({
        Enabled: modelEnabled,
        Preset: modelPreset,
        OpenAIBaseURL: modelOpenAIURL.trim(),
        AnthropicBaseURL: modelAnthropicURL.trim(),
        APIKey: modelApiKey,
        ClearAPIKey: modelClearKey,
        Agents: modelAgents,
      });
      if (modelApiKey) setModelApiKeySet(true);
      if (modelClearKey) setModelApiKeySet(false);
      setModelApiKey('');
      setModelClearKey(false);
      setModelDirtyHint(false);
      notify(t('ui.settings.model.saved'), 'info');
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    } finally {
      setModelSaving(false);
    }
  };

  const busyAny = !!pendingId || !!job?.Running;

  const cmdPreview = (tool: ToolInfo, recipe: InstallRecipeView) => {
    if (!tool.BinPath) return recipe.InstallCmd;
    return recipe.UninstallCmd || t('ui.settings.tools.cmd_delete', { 0: tool.BinPath });
  };

  const handleInstall = async (id: string) => {
    if (busyAny) return;
    setPendingId(id);
    setActiveId(id);
    setInstallLog('');
    try {
      await installBuiltinTool(id);
    } catch (e: unknown) {
      setPendingId('');
      notify(backendError(e), 'error');
    }
  };

  const openUninstall = (tool: ToolInfo) => {
    if (busyAny) return;
    setPurgeConfig(false);
    setUninstallTarget(tool);
  };

  const handleConfirmUninstall = async () => {
    if (!uninstallTarget) return;
    const id = uninstallTarget.ID;
    const purge = purgeConfig;
    setUninstallTarget(null);
    setPurgeConfig(false);
    setPendingId(id);
    setActiveId(id);
    setInstallLog('');
    try {
      await uninstallBuiltinTool(id, purge);
    } catch (e: unknown) {
      setPendingId('');
      notify(backendError(e), 'error');
    }
  };

  const inputClass = 'rounded border border-input bg-card px-2 py-1 text-sm';
  const uninstallRecipe = uninstallTarget ? recipes[uninstallTarget.ID] : undefined;

  // 语言选项：内置三项 + 外部语言包（label 用包内 $name，缺省回退语言码）；
  // 配置值不在任何选项里（如外部包被删）时兜底追加该 code，保证选中态可见。
  const languageOptions: { code: string; label: string }[] = [
    { code: 'en', label: t('ui.settings.language.en') },
    { code: 'zh-CN', label: t('ui.settings.language.zh_cn') },
    { code: 'system', label: t('ui.settings.language.system') },
    ...getLanguageOptions()
      .filter((o) => !BUILTIN_LANGUAGE_CODES.includes(o.code))
      .map((o) => ({ code: o.code, label: o.name ?? o.code })),
  ];
  if (!languageOptions.some((o) => o.code === languageLocal)) {
    languageOptions.push({ code: languageLocal, label: languageLocal });
  }

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <nav
        className="flex w-36 shrink-0 flex-col gap-0.5 border-r border-border bg-card p-2"
        aria-label={t('ui.settings.nav.aria')}
      >
        {SECTIONS.map((s) => (
          <button
            key={s.id}
            type="button"
            className={cn(
              'rounded px-2.5 py-1.5 text-left text-sm transition-colors',
              section === s.id ? 'bg-primary/10 font-medium text-primary' : 'hover:bg-muted',
            )}
            aria-current={section === s.id ? 'page' : undefined}
            onClick={() => setSection(s.id)}
          >
            {t(s.labelKey)}
          </button>
        ))}
      </nav>

      <div className="flex-1 overflow-y-auto p-6">
        <div className="max-w-2xl">
          <h1 className="mb-4 text-lg font-semibold">{t('ui.settings.title')}</h1>

          {section === 'general' && (
            <>
              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">{t('ui.settings.appearance.title')}</h2>
                <div className="flex gap-2">
                  {[
                    { value: 'system', labelKey: 'ui.settings.appearance.system' },
                    { value: 'light', labelKey: 'ui.settings.appearance.light' },
                    { value: 'dark', labelKey: 'ui.settings.appearance.dark' },
                  ].map((opt) => (
                    <Button
                      key={opt.value}
                      variant={appearance === opt.value ? 'default' : 'secondary'}
                      aria-pressed={appearance === opt.value}
                      onClick={() => void handleAppearance(opt.value)}
                    >
                      {t(opt.labelKey)}
                    </Button>
                  ))}
                </div>
                <label className="mt-3 flex items-center gap-3 text-sm">
                  <span className="shrink-0">{t('ui.settings.appearance.font_size')}</span>
                  <input
                    type="range"
                    min={10}
                    max={20}
                    step={1}
                    value={fontSize}
                    aria-label={t('ui.settings.appearance.font_size')}
                    className="min-w-0 flex-1 accent-primary"
                    onChange={(e) => previewFontSize(Number(e.currentTarget.value))}
                    onPointerUp={(e) => void handleFontSizeCommit(Number(e.currentTarget.value))}
                    onKeyUp={(e) => void handleFontSizeCommit(Number(e.currentTarget.value))}
                  />
                  <span className="w-10 shrink-0 tabular-nums text-muted-foreground">{fontSize}px</span>
                </label>
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">{t('ui.settings.language.title')}</h2>
                <div className="flex gap-2">
                  {languageOptions.map((opt) => (
                    <Button
                      key={opt.code}
                      variant={languageLocal === opt.code ? 'default' : 'secondary'}
                      aria-pressed={languageLocal === opt.code}
                      onClick={() => void handleLanguage(opt.code)}
                    >
                      {opt.label}
                    </Button>
                  ))}
                </div>
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">{t('ui.settings.close_behavior.title')}</h2>
                <div className="flex gap-2">
                  {[
                    { value: 'tray', labelKey: 'ui.settings.close_behavior.tray' },
                    { value: 'exit', labelKey: 'ui.settings.close_behavior.exit' },
                  ].map((opt) => (
                    <Button
                      key={opt.value}
                      variant={closeBehavior === opt.value ? 'default' : 'secondary'}
                      aria-pressed={closeBehavior === opt.value}
                      onClick={() => void handleCloseBehavior(opt.value)}
                    >
                      {t(opt.labelKey)}
                    </Button>
                  ))}
                </div>
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">{t('ui.settings.notify.title')}</h2>
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={notifySound}
                    onChange={(e) => setNotifySound(e.currentTarget.checked)}
                    aria-label={t('ui.settings.notify.sound')}
                  />
                  {t('ui.settings.notify.sound')}
                </label>
                <p className="mt-1 text-xs text-muted-foreground">
                  {t('ui.settings.notify.sound_desc')}
                </p>
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">{t('ui.settings.session_mode.title')}</h2>
                <p className="mb-2 text-xs text-muted-foreground">
                  {t('ui.settings.session_mode.desc')}
                </p>
                <div className="flex gap-2">
                  {[
                    { value: 'tui', labelKey: 'ui.settings.session_mode.tui' },
                    { value: 'acp', labelKey: 'ui.settings.session_mode.acp' },
                  ].map((opt) => (
                    <Button
                      key={opt.value}
                      variant={sessionMode === opt.value ? 'default' : 'secondary'}
                      aria-pressed={sessionMode === opt.value}
                      onClick={() => void handleSessionMode(opt.value)}
                    >
                      {t(opt.labelKey)}
                    </Button>
                  ))}
                </div>
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">{t('ui.settings.permission_mode.title')}</h2>
                <div className="flex gap-2">
                  {[
                    { value: 'default', labelKey: 'ui.settings.permission_mode.default' },
                    { value: 'bypass', labelKey: 'ui.settings.permission_mode.bypass' },
                  ].map((opt) => (
                    <Button
                      key={opt.value}
                      variant={permissionMode === opt.value ? 'default' : 'secondary'}
                      aria-pressed={permissionMode === opt.value}
                      onClick={() => void handlePermissionMode(opt.value)}
                    >
                      {t(opt.labelKey)}
                    </Button>
                  ))}
                </div>
                {permissionMode === 'bypass' && (
                  <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
                    {t('ui.settings.permission_mode.bypass_warning')}
                  </p>
                )}
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">{t('ui.settings.about.title')}</h2>
                <p className="mb-2 text-sm">
                  {t('ui.settings.about.current_version')}{' '}
                  <span className="tabular-nums text-muted-foreground">{appVersion || '…'}</span>
                </p>
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="secondary"
                    disabled={updateBusy}
                    onClick={() => {
                      void (async () => {
                        setUpdateBusy(true);
                        setUpdateError('');
                        try {
                          const info = await checkForUpdate();
                          setUpdateInfo(info);
                        } catch (e: unknown) {
                          setUpdateError(backendError(e));
                        } finally {
                          setUpdateBusy(false);
                        }
                      })();
                    }}
                  >
                    {updateBusy ? t('ui.settings.about.checking') : t('ui.settings.about.check_update')}
                  </Button>
                  <Button variant="secondary" onClick={() => openExternal(GITHUB_ISSUES_NEW_URL)}>
                    {t('ui.settings.about.feedback')}
                  </Button>
                  {updateInfo?.Available && (
                    <Button
                      disabled={updateBusy}
                      onClick={() => {
                        void (async () => {
                          setUpdateBusy(true);
                          setUpdateError('');
                          try {
                            await applyUpdate();
                          } catch (e: unknown) {
                            setUpdateError(backendError(e));
                            setUpdateBusy(false);
                          }
                        })();
                      }}
                    >
                      {t('ui.settings.about.upgrade_now')}
                    </Button>
                  )}
                </div>
                {updateError && <p className="mt-2 text-xs text-destructive">{updateError}</p>}
                {updateInfo?.Skipped && (
                  <p className="mt-2 text-xs text-muted-foreground">
                    {translateBackend(updateInfo.Reason)}
                  </p>
                )}
                {updateInfo && !updateInfo.Available && !updateInfo.Skipped && (
                  <p className="mt-2 text-xs text-muted-foreground">
                    {updateInfo.Reason
                      ? translateBackend(updateInfo.Reason)
                      : t('ui.settings.about.up_to_date')}
                  </p>
                )}
                {updateInfo?.Available && (
                  <div className="mt-2 text-xs text-muted-foreground">
                    <p>
                      {t('ui.settings.about.new_version', { 0: updateInfo.Latest })}
                      {updateInfo.Source
                        ? t('ui.settings.about.source', { 0: updateInfo.Source })
                        : ''}
                    </p>
                    {updateInfo.Notes && (
                      <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap rounded bg-muted p-2">
                        {updateInfo.Notes}
                      </pre>
                    )}
                  </div>
                )}
              </section>
            </>
          )}

          {section === 'model' && (
            <section className="mb-5 rounded border border-border bg-card p-3.5">
              <h2 className="mb-3 text-sm font-medium">{t('ui.settings.model.title')}</h2>
              <label className="mb-2 flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={modelEnabled}
                  onChange={(e) => setModelEnabled(e.target.checked)}
                />
                {t('ui.settings.model.enable')}
              </label>
              <div className="grid gap-2">
                <label className="text-xs text-muted-foreground" htmlFor="model-preset">
                  {t('ui.settings.model.preset')}
                </label>
                <select
                  id="model-preset"
                  aria-label={t('ui.settings.model.preset')}
                  className={inputClass}
                  value={modelPreset}
                  onChange={(e) => applyPreset(e.target.value, false)}
                >
                  {presets.map((p) => (
                    <option key={p.ID} value={p.ID}>
                      {p.Name}
                      {p.Note ? ` — ${p.Note}` : ''}
                    </option>
                  ))}
                </select>
                {modelDirtyHint && (
                  <p className="text-xs text-muted-foreground">{t('ui.settings.model.dirty')}</p>
                )}
                <label className="text-xs text-muted-foreground" htmlFor="model-openai-url">
                  OpenAI Base URL
                </label>
                <input
                  id="model-openai-url"
                  aria-label="OpenAI Base URL"
                  className={inputClass}
                  placeholder="https://..."
                  value={modelOpenAIURL}
                  onChange={(e) => {
                    setModelOpenAIURL(e.target.value);
                    setModelDirtyHint(true);
                  }}
                />
                <label className="text-xs text-muted-foreground" htmlFor="model-anthropic-url">
                  Anthropic Base URL
                </label>
                <input
                  id="model-anthropic-url"
                  aria-label="Anthropic Base URL"
                  className={inputClass}
                  placeholder="https://..."
                  value={modelAnthropicURL}
                  onChange={(e) => {
                    setModelAnthropicURL(e.target.value);
                    setModelDirtyHint(true);
                  }}
                />
                <label className="text-xs text-muted-foreground" htmlFor="model-api-key">
                  {t('ui.settings.model.api_key')}
                </label>
                <input
                  id="model-api-key"
                  aria-label={t('ui.settings.model.api_key')}
                  type="password"
                  className={inputClass}
                  placeholder={
                    modelApiKeySet
                      ? t('ui.settings.model.api_key_set')
                      : t('ui.settings.model.api_key_unset')
                  }
                  value={modelApiKey}
                  onChange={(e) => setModelApiKey(e.target.value)}
                />
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <input
                    type="checkbox"
                    checked={modelClearKey}
                    onChange={(e) => setModelClearKey(e.target.checked)}
                    aria-label={t('ui.settings.model.clear_key')}
                  />
                  {t('ui.settings.model.clear_key')}
                </label>
                {MODEL_AGENTS.map((a) => (
                  <div key={a.id} className="grid gap-1">
                    <label className="text-xs text-muted-foreground" htmlFor={`model-${a.id}`}>
                      {t('ui.settings.model.agent_model', { 0: a.label })}
                    </label>
                    <input
                      id={`model-${a.id}`}
                      aria-label={t('ui.settings.model.agent_model', { 0: a.label })}
                      className={inputClass}
                      value={modelAgents[a.id] ?? ''}
                      onChange={(e) => {
                        setModelAgents((prev) => ({ ...prev, [a.id]: e.target.value }));
                        setModelDirtyHint(true);
                      }}
                    />
                  </div>
                ))}
                <div className="flex flex-wrap gap-2">
                  <Button onClick={() => void handleSaveModel()} disabled={modelSaving}>
                    {t('ui.settings.model.save')}
                  </Button>
                  <Button
                    variant="secondary"
                    type="button"
                    disabled={modelPreset === 'custom'}
                    onClick={() => applyPreset(modelPreset, true)}
                  >
                    {t('ui.settings.model.apply_recommended')}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">{t('ui.settings.model.desc')}</p>
              </div>
            </section>
          )}

          {section === 'tools' && (
            <>
              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <div className="mb-3 flex items-center justify-between gap-2">
                  <h2 className="text-sm font-medium">{t('ui.settings.tools.title')}</h2>
                  <Button
                    variant="secondary"
                    type="button"
                    disabled={rescanning}
                    onClick={() => {
                      if (rescanning) return;
                      setRescanning(true);
                      void scanSessions();
                    }}
                  >
                    {rescanning ? t('ui.settings.tools.rescanning') : t('ui.settings.tools.rescan')}
                  </Button>
                </div>
                {toolsError && <p className="text-sm text-destructive">{toolsError}</p>}
                {job?.Running && job.Trigger === 'auto' && (
                  <p className="text-xs text-warning">
                    {t('ui.settings.tools.auto_repairing', { 0: job.ToolID })}
                  </p>
                )}
                {tools === null && !toolsError && (
                  <p className="text-sm text-muted-foreground">{t('ui.settings.tools.loading')}</p>
                )}
                {tools !== null && tools.length === 0 && (
                  <p className="text-sm text-muted-foreground">{t('ui.settings.tools.empty')}</p>
                )}
                {tools !== null && tools.length > 0 && (
                  <ul className="divide-y divide-border rounded border border-border">
                    {tools.map((tool) => {
                      const recipe = recipes[tool.ID];
                      const rowBusy =
                        pendingId === tool.ID || (!!job?.Running && job.ToolID === tool.ID);
                      const hasBin = !!tool.BinPath;
                      const isBroken = !!tool.Broken && !hasBin;
                      return (
                        <li
                          key={tool.ID}
                          className={cn(
                            'flex flex-col gap-1 px-2.5 py-1.5 text-xs transition-colors hover:bg-muted',
                            !tool.Installed && 'opacity-50',
                          )}
                          title={
                            isBroken
                              ? t('ui.settings.tools.title_broken')
                              : tool.Source === 'config-dir'
                                ? t('ui.settings.tools.title_config_only')
                                : tool.BinPath
                          }
                        >
                          <div className="flex items-center justify-between gap-2">
                            <div className="flex min-w-0 items-center gap-2">
                              <span className="font-medium">{tool.Name}</span>
                              {isBroken ? (
                                <Badge variant="destructive">{t('ui.settings.tools.broken')}</Badge>
                              ) : (
                                tool.Source === 'config-dir' && (
                                  <Badge variant="warning">{t('ui.settings.tools.unverified')}</Badge>
                                )
                              )}
                              {tool.Installed && !isBroken ? (
                                tool.Version && (
                                  <span className="text-xs text-muted-foreground">{tool.Version}</span>
                                )
                              ) : (
                                <span className="text-xs text-muted-foreground">
                                  {isBroken
                                    ? t('ui.settings.tools.missing_bin')
                                    : t('ui.settings.tools.not_installed')}
                                </span>
                              )}
                            </div>
                            {recipe && (
                              <Button
                                size="sm"
                                variant={hasBin ? 'secondary' : 'default'}
                                disabled={busyAny}
                                onClick={() =>
                                  hasBin ? openUninstall(tool) : void handleInstall(tool.ID)
                                }
                              >
                                {rowBusy
                                  ? hasBin
                                    ? t('ui.settings.tools.uninstalling')
                                    : isBroken
                                      ? t('ui.settings.tools.repairing')
                                      : t('ui.settings.tools.installing')
                                  : hasBin
                                    ? t('ui.settings.tools.uninstall')
                                    : isBroken
                                      ? t('ui.settings.tools.repair')
                                      : t('ui.settings.tools.install')}
                              </Button>
                            )}
                          </div>
                          {recipe && (
                            <p className="font-mono text-[11px] text-muted-foreground">
                              {cmdPreview(tool, recipe)}
                            </p>
                          )}
                          {activeId === tool.ID && installLog && (
                            <pre className="max-h-32 overflow-auto whitespace-pre-wrap rounded bg-muted/60 p-1.5 font-mono text-[11px]">
                              {installLog}
                            </pre>
                          )}
                        </li>
                      );
                    })}
                  </ul>
                )}
                {uninstallTarget && uninstallRecipe && (
                  <Dialog
                    open
                    onOpenChange={(o) => {
                      if (!o) {
                        setUninstallTarget(null);
                        setPurgeConfig(false);
                      }
                    }}
                    className="w-80"
                  >
                    <DialogPrimitive.Title className="mb-2 text-sm font-medium">
                      {t('ui.settings.tools.uninstall_title', { 0: uninstallTarget.Name })}
                    </DialogPrimitive.Title>
                    <p className="mb-3 font-mono text-[11px] text-muted-foreground">
                      {cmdPreview(uninstallTarget, uninstallRecipe)}
                    </p>
                    {uninstallRecipe.CanPurge && (
                      <div className="mb-3">
                        <label className="flex items-center gap-2 text-xs">
                          <input
                            type="checkbox"
                            checked={purgeConfig}
                            onChange={(e) => setPurgeConfig(e.target.checked)}
                            aria-label={t('ui.settings.tools.purge_config')}
                          />
                          {t('ui.settings.tools.purge_config')}
                        </label>
                        {purgeConfig && (
                          <div className="mt-2 text-xs text-muted-foreground">
                            <ul className="mb-1 list-disc pl-4">
                              {uninstallRecipe.PurgeDirs.map((d) => (
                                <li key={d}>{d}</li>
                              ))}
                            </ul>
                            <p>{t('ui.settings.tools.purge_note')}</p>
                          </div>
                        )}
                      </div>
                    )}
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setUninstallTarget(null);
                          setPurgeConfig(false);
                        }}
                      >
                        {t('ui.settings.tools.cancel')}
                      </Button>
                      <Button variant="destructive" size="sm" onClick={() => void handleConfirmUninstall()}>
                        {t('ui.settings.tools.confirm_uninstall')}
                      </Button>
                    </div>
                  </Dialog>
                )}
              </section>

              <ProvidersEditor
                yaml={yaml}
                yamlError={yamlError}
                saved={saved}
                saveError={saveError}
                saving={saving}
                specs={specs}
                mode={editorMode}
                onMode={(m) => void handleEditorMode(m)}
                onYamlChange={(v) => {
                  setYaml(v);
                  setSaved(false);
                  setSaveError('');
                }}
                onSpecsChange={(next) => {
                  setSpecs(next);
                  setSaved(false);
                  setSaveError('');
                }}
                onSave={() => void handleSave()}
                onPickDir={pickDirectory}
                onPickFile={pickFile}
              />
            </>
          )}
        </div>
      </div>
    </div>
  );
}
