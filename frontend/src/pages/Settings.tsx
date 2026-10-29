// 设置页：左导航分区（通用 / 模型 / 工具）+ 右侧内容。
// 通用含外观/语言/关闭/会话模式/权限/关于（检查更新、反馈问题）；模型含预设与双协议 Base URL；工具含检测与自定义表单。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  getAppearance,
  getAutostartInfo,
  getCloseBehavior,
  getModelConfig,
  getPermissionMode,
  getSessionMode,
  getToolInstallJob,
  getToolInstallRecipe,
  getTools,
  installBuiltinTool,
  listModelPresets,
  fetchAvailableModels,
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
  setAutostart,
  setAppearanceMode,
  setAppearanceFontSize,
  setAppearanceShowWhitespace,
  setCloseBehavior,
  setLanguage,
  setModelConfig,
  setPermissionMode,
  setSessionMode,
  uninstallBuiltinTool,
} from '../lib/api';
import type {
  AutostartInfoView,
  InstallRecipeView,
  ModelPlatformInput,
  ModelPlatformView,
  ModelPreset,
  ToolInfo,
  ToolInstallJobView,
  UpdateInfo,
} from '../lib/api';
import { cn } from '../lib/cn';
import { backendError, translateBackend } from '../lib/errors';
import { applyUiFontSize, clampUiFontSize } from '../lib/appearance';
import { getLanguageOptions } from '../i18n';
import { openExternal } from '../lib/openHref';
import { GITHUB_ISSUES_NEW_URL, GITHUB_REPO_URL } from '../lib/projectLinks';
import { useAppStore } from '../state/store';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';
import { Dialog } from '../components/ui/dialog';
import { HelpSection } from '../components/HelpSection';
import LoginDialog from '../components/LoginDialog';
import { ProvidersEditor } from '../components/ProvidersEditor';
import { SkillPlaza } from '../components/SkillPlaza';
import { WeixinSettings } from '../components/WeixinSettings';
import { normalizeSpec, withoutBuiltinSpecs, type CustomProviderSpec } from '../lib/providersForm';

// 语言行内置三选项；system（跟随系统）不是语言码，不在 getLanguageOptions 里
const BUILTIN_LANGUAGE_CODES = ['en', 'zh-CN', 'system'];

// 安装日志：事件文本是逐行的 wire key，须逐行翻译后再拼接（整块多行串喂
// translateBackend 会把首行后的内容当参数吞掉，导致漏译）
const translateLog = (text: string): string => text.split('\n').map(translateBackend).join('\n');

type Section = 'general' | 'weixin' | 'model' | 'tools' | 'skills' | 'help';

// PlatDraft：平台编辑态草稿（新增/编辑共用）。APIKey 只在表单里流转（空串=保持原值），
// APIKeySet / IsNew 仅为占位与交互辅助，不参与提交。
interface PlatDraft {
  ID: string;
  Name: string;
  Preset: string;
  OpenAIBaseURL: string;
  AnthropicBaseURL: string;
  APIKey: string;
  ClearAPIKey: boolean;
  Models: string[];
  ASRModel: string;
  APIKeySet: boolean;
  IsNew: boolean;
}

// genPlatformID：新平台 ID 用 UUID；兜底时间戳串（测试环境可能无 crypto.randomUUID）。
const genPlatformID = (): string =>
  typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `p-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;

const SECTIONS: { id: Section; labelKey: string }[] = [
  { id: 'general', labelKey: 'ui.settings.nav.general' },
  { id: 'weixin', labelKey: 'ui.settings.nav.weixin' },
  { id: 'model', labelKey: 'ui.settings.nav.model' },
  { id: 'tools', labelKey: 'ui.settings.nav.tools' },
  { id: 'skills', labelKey: 'ui.settings.nav.skills' },
  { id: 'help', labelKey: 'ui.settings.nav.help' },
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
  const [showWhitespace, setShowWhitespaceLocal] = useState(false);
  const [closeBehavior, setCloseBehaviorLocal] = useState<string>('tray');
  // 开机自启动：null=查询中/不支持（区块隐藏），Supported 才渲染
  const [autostartInfo, setAutostartInfo] = useState<AutostartInfoView | null>(null);
  const [sessionMode, setSessionModeLocal] = useState('tui');
  const [permissionMode, setPermissionModeLocal] = useState('default');
  const [presets, setPresets] = useState<ModelPreset[]>([]);
  const [modelEnabled, setModelEnabled] = useState(false);
  const [modelPlatforms, setModelPlatforms] = useState<ModelPlatformView[]>([]);
  const [modelDefault, setModelDefault] = useState('');
  const [modelActive, setModelActive] = useState('');
  // 平台编辑态：null=未在编辑；草稿字段见 PlatDraft
  const [platDraft, setPlatDraft] = useState<PlatDraft | null>(null);
  const [platNameError, setPlatNameError] = useState('');
  const [modelNewModel, setModelNewModel] = useState('');
  const [modelFetched, setModelFetched] = useState<string[] | null>(null);
  const [modelFetchedFilter, setModelFetchedFilter] = useState('');
  const [modelFetchErrors, setModelFetchErrors] = useState<string[]>([]);
  const [modelFetching, setModelFetching] = useState(false);
  const [modelDirtyHint, setModelDirtyHint] = useState(false);
  const [modelSaving, setModelSaving] = useState(false);
  const [recipes, setRecipes] = useState<Record<string, InstallRecipeView>>({});
  const [job, setJob] = useState<ToolInstallJobView | null>(null);
  const [activeId, setActiveId] = useState('');
  const [installLog, setInstallLog] = useState('');
  const [pendingId, setPendingId] = useState('');
  const [uninstallTarget, setUninstallTarget] = useState<ToolInfo | null>(null);
  const [purgeConfig, setPurgeConfig] = useState(false);
  // 登录弹窗目标：工具卡片「登录」按钮打开（out 态才显示按钮）
  const [loginTool, setLoginTool] = useState<ToolInfo | null>(null);
  const [appVersion, setAppVersion] = useState('');
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [updateError, setUpdateError] = useState('');
  const notify = useAppStore((s) => s.notify);
  const notifySound = useAppStore((s) => s.notifySound);
  const setNotifySound = useAppStore((s) => s.setNotifySound);
  const layoutMode = useAppStore((s) => s.layoutMode);
  const setLayoutMode = useAppStore((s) => s.setLayoutMode);
  const { t, i18n } = useTranslation();
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
          // 挂载时后端已有未完成任务的日志（多行 wire key），逐行翻译
          setInstallLog(translateLog(j.Log));
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
          setShowWhitespaceLocal(!!info.showWhitespace);
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
    getAutostartInfo()
      .then((info) => {
        if (!cancelled) setAutostartInfo(info);
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
        setModelPlatforms(v.Platforms ?? []);
        // 默认平台未显式指定时后端回落第一个，前端同样回落保证选中态可见
        setModelDefault(v.DefaultPlatform || v.Platforms?.[0]?.ID || '');
        setModelActive(v.ActiveModel ?? '');
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

  const handleShowWhitespace = async (show: boolean) => {
    setShowWhitespaceLocal(show);
    const cur = useAppStore.getState().appearance;
    useAppStore.getState().setAppearance({ ...cur, showWhitespace: show });
    try {
      await setAppearanceShowWhitespace(show);
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
      setInstallLog((prev) => (prev ? `${prev}\n${translateBackend(p.text)}` : translateBackend(p.text)));
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

  // handleAutostart：乐观更新开关，SetAutostart 失败回滚到原状态并提示
  const handleAutostart = async (on: boolean) => {
    if (!autostartInfo || on === autostartInfo.Enabled) return;
    setAutostartInfo({ ...autostartInfo, Enabled: on });
    try {
      await setAutostart(on);
    } catch (e: unknown) {
      setAutostartInfo((prev) => (prev ? { ...prev, Enabled: !on } : prev));
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

  // presetLabel：已知预设译为当前语言名称（preset.<id>.name），未知预设显示原始标识
  const presetLabel = (preset: string): string => {
    if (!preset) return '';
    const key = `preset.${preset}.name`;
    return i18n.exists(key) ? (i18n.t(key) as string) : preset;
  };

  // openPlatEditor：进入平台编辑态；p 缺省为新增（新 UUID），否则沿用原平台字段。
  // 密钥不回填明文：APIKey 留空=保持原值。
  const openPlatEditor = (p?: ModelPlatformView) => {
    setPlatNameError('');
    setModelNewModel('');
    setModelFetched(null);
    setModelFetchErrors([]);
    setModelFetchedFilter('');
    setModelDirtyHint(false);
    setPlatDraft(
      p
        ? {
            ID: p.ID,
            Name: p.Name,
            Preset: p.Preset || 'custom',
            OpenAIBaseURL: p.OpenAIBaseURL,
            AnthropicBaseURL: p.AnthropicBaseURL,
            APIKey: '',
            ClearAPIKey: false,
            Models: [...(p.Models ?? [])],
            ASRModel: p.ASRModel || '',
            APIKeySet: p.APIKeySet,
            IsNew: false,
          }
        : {
            ID: genPlatformID(),
            Name: '',
            Preset: 'custom',
            OpenAIBaseURL: '',
            AnthropicBaseURL: '',
            APIKey: '',
            ClearAPIKey: false,
            Models: [],
            ASRModel: '',
            APIKeySet: false,
            IsNew: true,
          },
    );
  };

  const closePlatEditor = () => {
    setPlatDraft(null);
    setPlatNameError('');
    setModelNewModel('');
    setModelFetched(null);
    setModelFetchErrors([]);
    setModelFetchedFilter('');
  };

  // applyPlatPreset：预设作用到编辑中的平台——填双 URL；
  // 推荐模型：强制（按钮）时未含即加入，切换预设时仅在列表为空时兜底。
  const applyPlatPreset = (id: string, forceModels: boolean) => {
    if (!platDraft) return;
    setModelDirtyHint(false);
    const next = { ...platDraft, Preset: id };
    if (id !== 'custom') {
      const p = presets.find((x) => x.ID === id);
      if (p) {
        next.OpenAIBaseURL = p.OpenAIBaseURL;
        next.AnthropicBaseURL = p.AnthropicBaseURL;
        if (
          p.RecommendedModel &&
          (forceModels ? !next.Models.includes(p.RecommendedModel) : !next.Models.length)
        ) {
          next.Models = [...next.Models, p.RecommendedModel];
          setModelDirtyHint(true);
        }
      }
    }
    setPlatDraft(next);
  };

  // handleSetDefault：本地改默认平台，随下一次整包保存落盘。
  // 只切徽标/按钮，不动 ActiveModel 与下拉——它们跟随已保存的默认平台，
  // 保存后重新拉取时由后端清理悬空模型并刷新列表。
  const handleSetDefault = (id: string) => {
    setModelDefault(id);
    setModelDirtyHint(true);
  };

  // handleDeletePlatform：window.confirm 确认后本地移除；删的是默认平台时清空选择
  //（后端回落第一个），并同步清掉不属于新默认平台的默认模型。
  const handleDeletePlatform = (p: ModelPlatformView) => {
    if (!window.confirm(t('ui.settings.model.delete_confirm', { 0: p.Name }))) return;
    const rest = modelPlatforms.filter((x) => x.ID !== p.ID);
    setModelPlatforms(rest);
    const newDefault = modelDefault === p.ID ? '' : modelDefault;
    setModelDefault(newDefault);
    const target = rest.find((x) => x.ID === newDefault) ?? rest[0];
    if (target && target.Models.length > 0 && !target.Models.includes(modelActive)) {
      setModelActive('');
    }
    setModelDirtyHint(true);
  };

  // mergeDraftPlatforms：把编辑中的平台按 ID 合并进列表并转成提交结构；
  // 已存在平台不携带密钥（APIKey 空串=后端保持原值）。
  const mergeDraftPlatforms = (): ModelPlatformInput[] => {
    const draftInput: ModelPlatformInput | null = platDraft
      ? {
          ID: platDraft.ID,
          Name: platDraft.Name.trim(),
          Preset: platDraft.Preset,
          OpenAIBaseURL: platDraft.OpenAIBaseURL.trim(),
          AnthropicBaseURL: platDraft.AnthropicBaseURL.trim(),
          APIKey: platDraft.APIKey,
          ClearAPIKey: platDraft.ClearAPIKey,
          Models: [...platDraft.Models],
          ASRModel: platDraft.ASRModel.trim(),
        }
      : null;
    const inputs = modelPlatforms.map((p) =>
      p.ID === platDraft?.ID && draftInput
        ? draftInput
        : {
            ID: p.ID,
            Name: p.Name,
            Preset: p.Preset,
            OpenAIBaseURL: p.OpenAIBaseURL.trim(),
            AnthropicBaseURL: p.AnthropicBaseURL.trim(),
            APIKey: '',
            ClearAPIKey: false,
            Models: [...p.Models],
            ASRModel: p.ASRModel || '',
          },
    );
    if (draftInput && platDraft && !modelPlatforms.some((p) => p.ID === platDraft.ID)) {
      inputs.push(draftInput);
    }
    return inputs;
  };

  const handleSaveModel = async () => {
    if (modelSaving) return;
    // 前端校验：编辑中的平台名称必填（后端同 key 校验兜底，前端拦截省一次往返）
    if (platDraft && !platDraft.Name.trim()) {
      setPlatNameError(t('ui.settings.model.platform_name_required'));
      return;
    }
    const platforms = mergeDraftPlatforms();
    setModelSaving(true);
    try {
      await setModelConfig({
        Enabled: modelEnabled,
        Platforms: platforms,
        DefaultPlatform: modelDefault,
        ActiveModel: modelActive.trim(),
      });
      // 保存后重拉：密钥设置状态只在后端有真相；重拉失败不影响已成功的保存
      try {
        const v = await getModelConfig();
        setModelPlatforms(v.Platforms ?? []);
        setModelDefault(v.DefaultPlatform || v.Platforms?.[0]?.ID || '');
        setModelActive(v.ActiveModel ?? '');
      } catch {
        // 忽略：保存已成功，仅回显失败
      }
      closePlatEditor();
      setModelDirtyHint(false);
      notify(t('ui.settings.model.saved'), 'info');
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    } finally {
      setModelSaving(false);
    }
  };

  // addToPlatModels：TrimSpace、去空、去重后追加进编辑中平台的模型列表。
  const addToPlatModels = (raw: string) => {
    if (!platDraft) return;
    const v = raw.trim();
    if (!v || platDraft.Models.includes(v)) return;
    setPlatDraft({ ...platDraft, Models: [...platDraft.Models, v] });
  };

  const handleAddModel = () => {
    addToPlatModels(modelNewModel);
    setModelNewModel('');
  };

  const handleFetchModels = async () => {
    if (!platDraft || modelFetching) return;
    setModelFetching(true);
    setModelFetchedFilter('');
    try {
      const v = await fetchAvailableModels(
        platDraft.ID,
        platDraft.OpenAIBaseURL.trim(),
        platDraft.AnthropicBaseURL.trim(),
        platDraft.APIKey,
      );
      setModelFetched(v.Models ?? []);
      setModelFetchErrors(v.Errors ?? []);
    } catch (e: unknown) {
      setModelFetched([]);
      setModelFetchErrors([backendError(e)]);
    } finally {
      setModelFetching(false);
    }
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

  // 拉取结果本地检索：大小写不敏感子串匹配；空过滤词时原样返回全部
  const fetchedQuery = modelFetchedFilter.trim().toLowerCase();
  const filteredFetched =
    modelFetched === null
      ? []
      : fetchedQuery
        ? modelFetched.filter((m) => m.toLowerCase().includes(fetchedQuery))
        : modelFetched;

  const busyAny = !!pendingId || !!job?.Running;

  // 徽标跟随本地待保存的默认平台（空时回落第一个），保证点击后全列表只有一个 Default
  const effectiveDefault = modelDefault || modelPlatforms[0]?.ID || '';
  // 默认模型下拉只跟随已保存的默认平台（IsDefault）：点「设为默认」不立即换列表，
  // 保存成功重新拉取后才刷新；IsDefault 缺失时回落第一个平台
  const savedDefaultPlat = modelPlatforms.find((p) => p.IsDefault) ?? modelPlatforms[0];

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
        {/* skills / help 内容宽（卡片列表、长文档），放开限宽占满内容区；其余表单类分区保持窄栏易读 */}
        <div className={cn(section !== 'skills' && section !== 'help' && 'max-w-2xl')}>
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
                <label className="mt-3 flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={showWhitespace}
                    onChange={(e) => void handleShowWhitespace(e.currentTarget.checked)}
                    aria-label={t('ui.settings.show_whitespace')}
                  />
                  {t('ui.settings.show_whitespace')}
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
                <h2 className="mb-3 text-sm font-medium">{t('ui.settings.center_layout.title')}</h2>
                <div className="flex gap-2">
                  {(
                    [
                      { value: 'tabs', labelKey: 'ui.settings.center_layout.tabs' },
                      { value: 'split', labelKey: 'ui.settings.center_layout.split' },
                    ] as { value: typeof layoutMode; labelKey: string }[]
                  ).map((opt) => (
                    <Button
                      key={opt.value}
                      variant={layoutMode === opt.value ? 'default' : 'secondary'}
                      aria-pressed={layoutMode === opt.value}
                      onClick={() => setLayoutMode(opt.value)}
                    >
                      {t(opt.labelKey)}
                    </Button>
                  ))}
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {t('ui.settings.center_layout.desc')}
                </p>
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

              {autostartInfo?.Supported && (
                <section className="mb-5 rounded border border-border bg-card p-3.5">
                  <h2 className="mb-3 text-sm font-medium">{t('ui.settings.autostart.title')}</h2>
                  <label className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={autostartInfo.Enabled}
                      onChange={(e) => void handleAutostart(e.currentTarget.checked)}
                      aria-label={t('ui.settings.autostart.desc')}
                    />
                    {t('ui.settings.autostart.desc')}
                  </label>
                </section>
              )}

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
                  <Button variant="secondary" onClick={() => openExternal(GITHUB_REPO_URL)}>
                    {t('ui.settings.about.open_source')}
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
                            // 重启被拦截时 busy 复位，避免按钮永久禁用（同 App.tsx）。
                            setUpdateBusy(false);
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

          {section === 'weixin' && <WeixinSettings />}

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
                <label className="text-xs text-muted-foreground" htmlFor="model-active">
                  {t('ui.settings.model.active_model')}
                </label>
                <select
                  id="model-active"
                  aria-label={t('ui.settings.model.active_model')}
                  className={inputClass}
                  value={modelActive}
                  onChange={(e) => setModelActive(e.target.value)}
                >
                  {(savedDefaultPlat?.Models ?? []).map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
                {(!savedDefaultPlat || savedDefaultPlat.Models.length === 0) && (
                  <p className="text-xs text-muted-foreground">{t('ui.settings.model.models_empty')}</p>
                )}

                {modelPlatforms.length === 0 && (
                  <p className="text-xs text-muted-foreground">{t('ui.settings.model.platforms_empty')}</p>
                )}
                {modelPlatforms.length > 0 && (
                  <ul className="grid gap-2">
                    {modelPlatforms.map((p) => (
                      <li key={p.ID} className="rounded border border-border p-2.5">
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex min-w-0 items-center gap-2">
                            <span className="truncate text-sm font-medium">{p.Name}</span>
                            {p.ID === effectiveDefault ? (
                              <Badge>{t('ui.settings.model.default_badge')}</Badge>
                            ) : (
                              <Button
                                size="sm"
                                variant="secondary"
                                type="button"
                                disabled={!!platDraft}
                                onClick={() => handleSetDefault(p.ID)}
                              >
                                {t('ui.settings.model.set_default')}
                              </Button>
                            )}
                          </div>
                          <div className="flex shrink-0 gap-1">
                            <Button
                              size="sm"
                              variant="secondary"
                              type="button"
                              disabled={!!platDraft}
                              onClick={() => openPlatEditor(p)}
                            >
                              {t('ui.settings.model.edit_platform')}
                            </Button>
                            <Button
                              size="sm"
                              variant="secondary"
                              type="button"
                              disabled={!!platDraft}
                              onClick={() => handleDeletePlatform(p)}
                            >
                              {t('ui.settings.model.delete_platform')}
                            </Button>
                          </div>
                        </div>
                        <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
                          {p.OpenAIBaseURL || '…'} / {p.AnthropicBaseURL || '…'}
                        </p>
                        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                          {p.Preset && <span>{presetLabel(p.Preset)}</span>}
                          <span>
                            {p.APIKeySet
                              ? t('ui.settings.model.api_key_set')
                              : t('ui.settings.model.api_key_unset')}
                          </span>
                          <span>{t('ui.settings.model.models_count', { 0: p.Models.length })}</span>
                        </div>
                      </li>
                    ))}
                  </ul>
                )}
                <div>
                  <Button
                    variant="secondary"
                    type="button"
                    disabled={!!platDraft}
                    onClick={() => openPlatEditor()}
                  >
                    {t('ui.settings.model.add_platform')}
                  </Button>
                </div>

                {platDraft && (
                  <div className="mt-1 grid gap-2 rounded border border-border bg-background p-2.5">
                    <p className="text-sm font-medium">{t('ui.settings.model.edit_platform')}</p>
                    <label className="text-xs text-muted-foreground" htmlFor="platform-name">
                      {t('ui.settings.model.platform_name')}
                    </label>
                    <input
                      id="platform-name"
                      aria-label={t('ui.settings.model.platform_name')}
                      className={inputClass}
                      placeholder={t('ui.settings.model.platform_name_placeholder')}
                      value={platDraft.Name}
                      onChange={(e) => {
                        setPlatDraft({ ...platDraft, Name: e.target.value });
                        setPlatNameError('');
                      }}
                    />
                    {platNameError && <p className="text-xs text-destructive">{platNameError}</p>}
                    <label className="text-xs text-muted-foreground" htmlFor="platform-preset">
                      {t('ui.settings.model.preset')}
                    </label>
                    <select
                      id="platform-preset"
                      aria-label={t('ui.settings.model.preset')}
                      className={inputClass}
                      value={platDraft.Preset}
                      onChange={(e) => applyPlatPreset(e.target.value, false)}
                    >
                      {presets.map((p) => (
                        <option key={p.ID} value={p.ID}>
                          {translateBackend(p.Name)}
                          {p.Note ? ` — ${translateBackend(p.Note)}` : ''}
                        </option>
                      ))}
                    </select>
                    {modelDirtyHint && (
                      <p className="text-xs text-muted-foreground">{t('ui.settings.model.dirty')}</p>
                    )}
                    <label className="text-xs text-muted-foreground" htmlFor="model-openai-url">
                      {t('ui.settings.model.openai_base_url')}
                    </label>
                    <input
                      id="model-openai-url"
                      aria-label={t('ui.settings.model.openai_base_url')}
                      className={inputClass}
                      placeholder="https://..."
                      value={platDraft.OpenAIBaseURL}
                      onChange={(e) => {
                        setPlatDraft({ ...platDraft, OpenAIBaseURL: e.target.value });
                        setModelDirtyHint(true);
                      }}
                    />
                    <label className="text-xs text-muted-foreground" htmlFor="model-anthropic-url">
                      {t('ui.settings.model.anthropic_base_url')}
                    </label>
                    <input
                      id="model-anthropic-url"
                      aria-label={t('ui.settings.model.anthropic_base_url')}
                      className={inputClass}
                      placeholder="https://..."
                      value={platDraft.AnthropicBaseURL}
                      onChange={(e) => {
                        setPlatDraft({ ...platDraft, AnthropicBaseURL: e.target.value });
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
                        !platDraft.IsNew && platDraft.APIKeySet
                          ? t('ui.settings.model.api_key_keep_hint')
                          : t('ui.settings.model.api_key_unset')
                      }
                      value={platDraft.APIKey}
                      onChange={(e) => setPlatDraft({ ...platDraft, APIKey: e.target.value })}
                    />
                    {!platDraft.IsNew && platDraft.APIKeySet && (
                      <label className="flex items-center gap-2 text-xs text-muted-foreground">
                        <input
                          type="checkbox"
                          checked={platDraft.ClearAPIKey}
                          onChange={(e) => setPlatDraft({ ...platDraft, ClearAPIKey: e.target.checked })}
                          aria-label={t('ui.settings.model.clear_key')}
                        />
                        {t('ui.settings.model.clear_key')}
                      </label>
                    )}
                    <label className="text-xs text-muted-foreground" htmlFor="model-asr-model">
                      {t('ui.settings.model.asr_model')}
                    </label>
                    <input
                      id="model-asr-model"
                      aria-label={t('ui.settings.model.asr_model')}
                      className={inputClass}
                      placeholder="FunAudioLLM/SenseVoiceSmall"
                      value={platDraft.ASRModel}
                      onChange={(e) => setPlatDraft({ ...platDraft, ASRModel: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">{t('ui.settings.model.asr_model_hint')}</p>
                    <p className="text-xs text-muted-foreground">{t('ui.settings.model.models')}</p>
                    {platDraft.Models.length === 0 && (
                      <p className="text-xs text-muted-foreground">{t('ui.settings.model.models_empty')}</p>
                    )}
                    <ul className="grid gap-1">
                      {platDraft.Models.map((m) => (
                        <li key={m} className="flex items-center gap-2 text-sm">
                          <span className="flex-1 truncate" title={m}>{m}</span>
                          <Button
                            variant="secondary"
                            type="button"
                            onClick={() => {
                              setPlatDraft({
                                ...platDraft,
                                Models: platDraft.Models.filter((x) => x !== m),
                              });
                              setModelDirtyHint(true);
                            }}
                          >
                            {t('ui.settings.model.remove')}
                          </Button>
                        </li>
                      ))}
                    </ul>
                    <div className="flex gap-2">
                      <input
                        aria-label={t('ui.settings.model.add_model')}
                        placeholder={t('ui.settings.model.add_model')}
                        className={inputClass}
                        value={modelNewModel}
                        onChange={(e) => setModelNewModel(e.target.value)}
                      />
                      <Button type="button" onClick={handleAddModel}>
                        {t('ui.settings.model.add')}
                      </Button>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <Button type="button" variant="secondary" disabled={modelFetching} onClick={() => void handleFetchModels()}>
                        {modelFetching ? t('ui.settings.model.fetching') : t('ui.settings.model.fetch_models')}
                      </Button>
                      <span className="text-xs text-muted-foreground">{t('ui.settings.model.fetch_hint')}</span>
                    </div>
                    {modelFetchErrors.length > 0 && (
                      <ul className="text-xs text-warning">
                        {modelFetchErrors.map((k) => (
                          <li key={k}>{translateBackend(k)}</li>
                        ))}
                      </ul>
                    )}
                    {modelFetched !== null && (
                      <>
                        <input
                          type="search"
                          className={inputClass}
                          placeholder={t('ui.settings.model.fetch_filter_placeholder')}
                          aria-label={t('ui.settings.model.fetch_filter_placeholder')}
                          value={modelFetchedFilter}
                          onChange={(e) => setModelFetchedFilter(e.target.value)}
                        />
                        <ul
                          data-testid="model-fetch-list"
                          className="grid max-h-64 gap-1 overflow-y-auto rounded border border-border p-1"
                        >
                          {modelFetched.length === 0 && modelFetchErrors.length === 0 && (
                            <li className="px-1 text-xs text-muted-foreground">{t('ui.settings.model.fetch_empty')}</li>
                          )}
                          {modelFetched.length > 0 && filteredFetched.length === 0 && (
                            <li className="px-1 text-xs text-muted-foreground">
                              {t('ui.settings.model.fetch_filter_empty')}
                            </li>
                          )}
                          {filteredFetched.map((m) => (
                            <li key={m} className="flex items-center gap-2 text-sm">
                              <span className="flex-1 truncate" title={m}>{m}</span>
                              {!platDraft.Models.includes(m) && (
                                <Button variant="secondary" type="button" onClick={() => addToPlatModels(m)}>
                                  {t('ui.settings.model.add')}
                                </Button>
                              )}
                            </li>
                          ))}
                        </ul>
                      </>
                    )}
                    <div className="flex flex-wrap gap-2">
                      <Button
                        variant="secondary"
                        type="button"
                        disabled={platDraft.Preset === 'custom'}
                        onClick={() => applyPlatPreset(platDraft.Preset, true)}
                      >
                        {t('ui.settings.model.apply_recommended')}
                      </Button>
                      <Button variant="secondary" type="button" onClick={closePlatEditor}>
                        {t('ui.settings.tools.cancel')}
                      </Button>
                    </div>
                  </div>
                )}

                <div className="mt-1 flex flex-wrap gap-2">
                  <Button onClick={() => void handleSaveModel()} disabled={modelSaving}>
                    {t('ui.settings.model.save')}
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
                    title={t('ui.settings.tools.rescan_hint')}
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
                              {/* 登录态徽标：in→已登录（绿）、out→未登录（红）；unknown/缺省不标注 */}
                              {tool.Installed && !isBroken && tool.Auth === 'in' && (
                                <Badge variant="success">{t('ui.auth.logged_in')}</Badge>
                              )}
                              {tool.Installed && !isBroken && tool.Auth === 'out' && (
                                <Badge variant="destructive">{t('ui.auth.not_logged_in')}</Badge>
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
                            <div className="flex shrink-0 items-center gap-1.5">
                              {/* 未登录的已安装工具：快捷打开登录弹窗（与卡片徽标同源） */}
                              {tool.Installed && !isBroken && tool.Auth === 'out' && (
                                <Button size="sm" variant="secondary" onClick={() => setLoginTool(tool)}>
                                  {t('ui.auth.login')}
                                </Button>
                              )}
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

          {section === 'skills' && <SkillPlaza />}
          {section === 'help' && <HelpSection />}
        </div>
      </div>
      {loginTool && (
        <LoginDialog
          toolID={loginTool.ID}
          toolName={loginTool.Name}
          onClose={() => setLoginTool(null)}
        />
      )}
    </div>
  );
}
