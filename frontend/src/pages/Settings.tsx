// 设置页：左导航分区（通用 / 模型 / 工具）+ 右侧内容。
// 通用含外观/关闭/会话模式/权限/关于（检查更新）；模型含预设与双协议 Base URL；工具含检测与 providers.yaml。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useState } from 'react';
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
  loadProvidersYAML,
  onScanDone,
  onToolsUpdated,
  onToolInstallDone,
  onToolInstallLog,
  applyUpdate,
  checkForUpdate,
  getAppVersion,
  restartApp,
  saveProvidersYAML,
  scanSessions,
  setAppearanceMode,
  setAppearanceFontSize,
  setCloseBehavior,
  setModelConfig,
  setPermissionMode,
  setSessionMode,
  uninstallBuiltinTool,
} from '../lib/api';
import type { InstallRecipeView, ModelPreset, ToolInfo, ToolInstallJobView, UpdateInfo } from '../lib/api';
import { cn } from '../lib/cn';
import { applyUiFontSize, clampUiFontSize } from '../lib/appearance';
import { useAppStore } from '../state/store';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';
import { Dialog } from '../components/ui/dialog';

const MODEL_AGENTS = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex CLI' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'opencode', label: 'OpenCode' },
];

type Section = 'general' | 'model' | 'tools';

const SECTIONS: { id: Section; label: string }[] = [
  { id: 'general', label: '通用' },
  { id: 'model', label: '模型' },
  { id: 'tools', label: '工具' },
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
  const [restarting, setRestarting] = useState(false);
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

  const loadRecipes = async (list: ToolInfo[]) => {
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
        if (!cancelled) setToolsError(e instanceof Error ? e.message : String(e));
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
      .then((content) => {
        if (!cancelled) setYaml(content);
      })
      .catch((e: unknown) => {
        if (!cancelled) setYamlError(e instanceof Error ? e.message : String(e));
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
      notify(e instanceof Error ? e.message : String(e), 'error');
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
          setToolsError(e instanceof Error ? e.message : String(e));
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
    });
    const offDone = onToolInstallDone((p) => {
      setPendingId('');
      setJob((prev) => (prev ? { ...prev, Running: false } : prev));
      if (p.ok === false) {
        notify(p.error || '操作失败', 'error');
      }
      getTools()
        .then(async (list) => {
          setTools(list);
          setRecipes(await loadRecipes(list));
        })
        .catch((e: unknown) => {
          setToolsError(e instanceof Error ? e.message : String(e));
        });
    });
    return () => {
      offLog();
      offDone();
    };
  }, [notify]);


  const handleAppearance = async (mode: string) => {
    if (mode === appearance) return;
    try {
      await setAppearanceMode(mode);
      setAppearanceLocal(mode);
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };

  const handleCloseBehavior = async (mode: string) => {
    if (mode === closeBehavior) return;
    try {
      await setCloseBehavior(mode);
      setCloseBehaviorLocal(mode);
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };

  const handleSessionMode = async (mode: string) => {
    if (mode === sessionMode) return;
    try {
      await setSessionMode(mode);
      setSessionModeLocal(mode);
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };

  const handlePermissionMode = async (mode: string) => {
    if (mode === permissionMode) return;
    try {
      await setPermissionMode(mode);
      setPermissionModeLocal(mode);
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
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
      await saveProvidersYAML(yaml);
      setSaved(true);
    } catch (e: unknown) {
      setSaveError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
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
      notify('已保存，对新启动的会话生效', 'info');
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setModelSaving(false);
    }
  };

  const handleRestart = async () => {
    if (restarting) return;
    setRestarting(true);
    try {
      await restartApp();
    } catch {
      notify('重启失败', 'error');
      setRestarting(false);
    }
  };

  const busyAny = !!pendingId || !!job?.Running;

  const cmdPreview = (t: ToolInfo, recipe: InstallRecipeView) => {
    if (!t.BinPath) return recipe.InstallCmd;
    return recipe.UninstallCmd || (t.BinPath ? `删除 ${t.BinPath}` : '未找到 cursor-agent 可执行文件');
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
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };

  const openUninstall = (t: ToolInfo) => {
    if (busyAny) return;
    setPurgeConfig(false);
    setUninstallTarget(t);
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
      notify(e instanceof Error ? e.message : String(e), 'error');
    }
  };

  const inputClass = 'rounded border border-input bg-card px-2 py-1 text-sm';
  const uninstallRecipe = uninstallTarget ? recipes[uninstallTarget.ID] : undefined;

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <nav
        className="flex w-36 shrink-0 flex-col gap-0.5 border-r border-border bg-card p-2"
        aria-label="设置分区"
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
            {s.label}
          </button>
        ))}
      </nav>

      <div className="flex-1 overflow-y-auto p-6">
        <div className="max-w-2xl">
          <h1 className="mb-4 text-lg font-semibold">设置</h1>

          {section === 'general' && (
            <>
              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">外观</h2>
                <div className="flex gap-2">
                  {[
                    { value: 'system', label: '跟随系统' },
                    { value: 'light', label: '浅色' },
                    { value: 'dark', label: '深色' },
                  ].map((opt) => (
                    <Button
                      key={opt.value}
                      variant={appearance === opt.value ? 'default' : 'secondary'}
                      aria-pressed={appearance === opt.value}
                      onClick={() => void handleAppearance(opt.value)}
                    >
                      {opt.label}
                    </Button>
                  ))}
                </div>
                <label className="mt-3 flex items-center gap-3 text-sm">
                  <span className="shrink-0">界面字号</span>
                  <input
                    type="range"
                    min={10}
                    max={20}
                    step={1}
                    value={fontSize}
                    aria-label="界面字号"
                    className="min-w-0 flex-1 accent-primary"
                    onChange={(e) => previewFontSize(Number(e.currentTarget.value))}
                    onPointerUp={(e) => void handleFontSizeCommit(Number(e.currentTarget.value))}
                    onKeyUp={(e) => void handleFontSizeCommit(Number(e.currentTarget.value))}
                  />
                  <span className="w-10 shrink-0 tabular-nums text-muted-foreground">{fontSize}px</span>
                </label>
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">关闭行为</h2>
                <div className="flex gap-2">
                  {[
                    { value: 'tray', label: '收进托盘' },
                    { value: 'exit', label: '直接退出' },
                  ].map((opt) => (
                    <Button
                      key={opt.value}
                      variant={closeBehavior === opt.value ? 'default' : 'secondary'}
                      aria-pressed={closeBehavior === opt.value}
                      onClick={() => void handleCloseBehavior(opt.value)}
                    >
                      {opt.label}
                    </Button>
                  ))}
                </div>
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">默认会话模式</h2>
                <p className="mb-2 text-xs text-muted-foreground">
                  影响新建/恢复的默认路径。工具不支持 ACP 时即使选了 ACP 也会走终端。
                </p>
                <div className="flex gap-2">
                  {[
                    { value: 'tui', label: 'TUI（终端）' },
                    { value: 'acp', label: 'ACP（聊天）' },
                  ].map((opt) => (
                    <Button
                      key={opt.value}
                      variant={sessionMode === opt.value ? 'default' : 'secondary'}
                      aria-pressed={sessionMode === opt.value}
                      onClick={() => void handleSessionMode(opt.value)}
                    >
                      {opt.label}
                    </Button>
                  ))}
                </div>
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">权限模式</h2>
                <div className="flex gap-2">
                  {[
                    { value: 'default', label: '默认（需确认）' },
                    { value: 'bypass', label: 'Bypass（跳过确认）' },
                  ].map((opt) => (
                    <Button
                      key={opt.value}
                      variant={permissionMode === opt.value ? 'default' : 'secondary'}
                      aria-pressed={permissionMode === opt.value}
                      onClick={() => void handlePermissionMode(opt.value)}
                    >
                      {opt.label}
                    </Button>
                  ))}
                </div>
                {permissionMode === 'bypass' && (
                  <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
                    将跳过 CLI 权限确认，并自动放行 ACP 权限弹窗。仅建议在可信环境使用。Gemini / OpenCode
                    暂无稳定跳过参数。
                  </p>
                )}
              </section>

              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">关于</h2>
                <p className="mb-2 text-sm">
                  当前版本{' '}
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
                          setUpdateError(e instanceof Error ? e.message : String(e));
                        } finally {
                          setUpdateBusy(false);
                        }
                      })();
                    }}
                  >
                    {updateBusy ? '检查中…' : '检查更新'}
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
                            setUpdateError(e instanceof Error ? e.message : String(e));
                            setUpdateBusy(false);
                          }
                        })();
                      }}
                    >
                      立即升级
                    </Button>
                  )}
                </div>
                {updateError && <p className="mt-2 text-xs text-destructive">{updateError}</p>}
                {updateInfo?.Skipped && (
                  <p className="mt-2 text-xs text-muted-foreground">{updateInfo.Reason}</p>
                )}
                {updateInfo && !updateInfo.Available && !updateInfo.Skipped && (
                  <p className="mt-2 text-xs text-muted-foreground">{updateInfo.Reason || '已是最新'}</p>
                )}
                {updateInfo?.Available && (
                  <div className="mt-2 text-xs text-muted-foreground">
                    <p>
                      新版本 {updateInfo.Latest}
                      {updateInfo.Source ? `（来源：${updateInfo.Source}）` : ''}
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
              <h2 className="mb-3 text-sm font-medium">模型（对所有 agent 启动时注入）</h2>
              <label className="mb-2 flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={modelEnabled}
                  onChange={(e) => setModelEnabled(e.target.checked)}
                />
                启用模型配置
              </label>
              <div className="grid gap-2">
                <label className="text-xs text-muted-foreground" htmlFor="model-preset">
                  提供商预设
                </label>
                <select
                  id="model-preset"
                  aria-label="提供商预设"
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
                  <p className="text-xs text-muted-foreground">字段已改（预设仍保留）</p>
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
                  模型 API Key
                </label>
                <input
                  id="model-api-key"
                  aria-label="模型 API Key"
                  type="password"
                  className={inputClass}
                  placeholder={modelApiKeySet ? '已设置（留空不修改）' : '未设置'}
                  value={modelApiKey}
                  onChange={(e) => setModelApiKey(e.target.value)}
                />
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <input
                    type="checkbox"
                    checked={modelClearKey}
                    onChange={(e) => setModelClearKey(e.target.checked)}
                    aria-label="清除密钥"
                  />
                  清除密钥
                </label>
                {MODEL_AGENTS.map((a) => (
                  <div key={a.id} className="grid gap-1">
                    <label className="text-xs text-muted-foreground" htmlFor={`model-${a.id}`}>
                      {a.label} 模型
                    </label>
                    <input
                      id={`model-${a.id}`}
                      aria-label={`${a.label} 模型`}
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
                    保存模型配置
                  </Button>
                  <Button
                    variant="secondary"
                    type="button"
                    disabled={modelPreset === 'custom'}
                    onClick={() => applyPreset(modelPreset, true)}
                  >
                    应用推荐模型
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  启动时按 agent 类型注入对应协议端点（Claude→Anthropic，Codex→OpenAI）。不改各工具自身配置文件。
                </p>
              </div>
            </section>
          )}

          {section === 'tools' && (
            <>
              <section className="mb-5 rounded border border-border bg-card p-3.5">
                <div className="mb-3 flex items-center justify-between gap-2">
                  <h2 className="text-sm font-medium">工具检测</h2>
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
                    {rescanning ? '扫描中…' : '重新扫描'}
                  </Button>
                </div>
                {toolsError && <p className="text-sm text-destructive">{toolsError}</p>}
                {tools === null && !toolsError && (
                  <p className="text-sm text-muted-foreground">加载中……</p>
                )}
                {tools !== null && tools.length === 0 && (
                  <p className="text-sm text-muted-foreground">未检测到任何工具</p>
                )}
                {tools !== null && tools.length > 0 && (
                  <ul className="divide-y divide-border rounded border border-border">
                    {tools.map((t) => {
                      const recipe = recipes[t.ID];
                      const rowBusy =
                        pendingId === t.ID || (!!job?.Running && job.ToolID === t.ID);
                      const hasBin = !!t.BinPath;
                      return (
                        <li
                          key={t.ID}
                          className={cn(
                            'flex flex-col gap-1 px-2.5 py-1.5 text-xs transition-colors hover:bg-muted',
                            !t.Installed && 'opacity-50',
                          )}
                          title={
                            t.Source === 'config-dir'
                              ? '只检测到配置目录，没有可执行程序，可用性未验证'
                              : t.BinPath
                          }
                        >
                          <div className="flex items-center justify-between gap-2">
                            <div className="flex min-w-0 items-center gap-2">
                              <span className="font-medium">{t.Name}</span>
                              {t.Source === 'config-dir' && <Badge variant="warning">未验证</Badge>}
                              {t.Installed ? (
                                t.Version && (
                                  <span className="text-xs text-muted-foreground">{t.Version}</span>
                                )
                              ) : (
                                <span className="text-xs text-muted-foreground">未安装</span>
                              )}
                            </div>
                            {recipe && (
                              <Button
                                size="sm"
                                variant={hasBin ? 'secondary' : 'default'}
                                disabled={busyAny}
                                onClick={() =>
                                  hasBin ? openUninstall(t) : void handleInstall(t.ID)
                                }
                              >
                                {rowBusy
                                  ? hasBin
                                    ? '卸载中…'
                                    : '安装中…'
                                  : hasBin
                                    ? '卸载'
                                    : '安装'}
                              </Button>
                            )}
                          </div>
                          {recipe && (
                            <p className="font-mono text-[11px] text-muted-foreground">
                              {cmdPreview(t, recipe)}
                            </p>
                          )}
                          {activeId === t.ID && installLog && (
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
                      卸载 {uninstallTarget.Name}
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
                            aria-label="同时清除配置"
                          />
                          同时清除配置
                        </label>
                        {purgeConfig && (
                          <div className="mt-2 text-xs text-muted-foreground">
                            <ul className="mb-1 list-disc pl-4">
                              {uninstallRecipe.PurgeDirs.map((d) => (
                                <li key={d}>{d}</li>
                              ))}
                            </ul>
                            <p>将删除会话历史</p>
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
                        取消
                      </Button>
                      <Button variant="destructive" size="sm" onClick={() => void handleConfirmUninstall()}>
                        确认卸载
                      </Button>
                    </div>
                  </Dialog>
                )}
              </section>

              <section className="rounded border border-border bg-card p-3.5">
                <h2 className="mb-3 text-sm font-medium">自定义工具（providers.yaml）</h2>
                {yamlError && <p className="text-sm text-destructive">{yamlError}</p>}
                {yaml !== null && (
                  <>
                    <textarea
                      className="min-h-[280px] w-full resize-y rounded border border-input bg-card p-2.5 font-mono text-xs leading-[1.55] text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      aria-label="providers.yaml 编辑器"
                      value={yaml}
                      spellCheck={false}
                      onChange={(e) => {
                        setYaml(e.target.value);
                        setSaved(false);
                        setSaveError('');
                      }}
                    />
                    <div className="mt-2 flex items-center gap-2.5">
                      <Button onClick={() => void handleSave()} disabled={saving}>
                        保存
                      </Button>
                      {saved && (
                        <>
                          <span className="text-sm text-muted-foreground">已保存，重启应用后生效</span>
                          <Button
                            size="sm"
                            variant="secondary"
                            disabled={restarting}
                            onClick={() => void handleRestart()}
                          >
                            {restarting ? '正在重启…' : '立即重启'}
                          </Button>
                        </>
                      )}
                      {saveError && <span className="text-sm text-destructive">{saveError}</span>}
                    </div>
                  </>
                )}
              </section>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
