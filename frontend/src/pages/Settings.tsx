// 设置页（顶部固定页签）：工具检测状态 + providers.yaml 自定义工具编辑保存。
// 工具状态来自 GetTools（最近一次扫描，含未安装项）：未安装灰显，
// Source=config-dir 表示「只检测到配置目录没有可执行程序」，视为未验证，显示徽标。
// providers.yaml 来自 LoadProvidersYAML（文件缺失时 Go 侧回填模板），
// 保存走 SaveProvidersYAML（Go 侧先校验 YAML 再原子写回）——
// 保存成功不热生效，需重启应用后由重扫装配，UI 明确提示这一点。
import { useEffect, useState } from 'react';
import {
  getAppearance,
  getCloseBehavior,
  getModelConfig,
  getTools,
  loadProvidersYAML,
  restartApp,
  saveProvidersYAML,
  setAppearanceMode,
  setCloseBehavior,
  setModelConfig,
} from '../lib/api';
import type { ToolInfo } from '../lib/api';
import { cn } from '../lib/cn';
import { useAppStore } from '../state/store';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';

// 模型注入区域覆盖的内置 agent：与 Go 侧 model config 的 Agents 键保持一致
const MODEL_AGENTS = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex CLI' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'opencode', label: 'OpenCode' },
];

export default function Settings() {
  const [tools, setTools] = useState<ToolInfo[] | null>(null);
  const [toolsError, setToolsError] = useState('');
  const [yaml, setYaml] = useState<string | null>(null);
  const [yamlError, setYamlError] = useState('');
  const [saved, setSaved] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [appearance, setAppearanceLocal] = useState<string>('system');
  const [closeBehavior, setCloseBehaviorLocal] = useState<string>('tray');
  const [modelEnabled, setModelEnabled] = useState(false);
  const [modelBaseURL, setModelBaseURL] = useState('');
  const [modelApiKey, setModelApiKey] = useState('');
  const [modelApiKeySet, setModelApiKeySet] = useState(false);
  const [modelClearKey, setModelClearKey] = useState(false);
  const [modelAgents, setModelAgents] = useState<Record<string, string>>({});
  const [modelSaving, setModelSaving] = useState(false);
  const notify = useAppStore((s) => s.notify);

  useEffect(() => {
    let cancelled = false;
    getTools()
      .then((list) => {
        if (!cancelled) setTools(list);
      })
      .catch((e: unknown) => {
        if (!cancelled) setToolsError(e instanceof Error ? e.message : String(e));
      });
    loadProvidersYAML()
      .then((content) => {
        if (!cancelled) setYaml(content);
      })
      .catch((e: unknown) => {
        if (!cancelled) setYamlError(e instanceof Error ? e.message : String(e));
      });
    getAppearance()
      .then((info) => {
        if (!cancelled) setAppearanceLocal(info.mode);
      })
      .catch(() => {});
    getCloseBehavior()
      .then((mode) => {
        if (!cancelled) setCloseBehaviorLocal(mode);
      })
      .catch(() => {});
    getModelConfig()
      .then((v) => {
        if (cancelled) return;
        setModelEnabled(v.Enabled);
        setModelBaseURL(v.BaseURL);
        setModelApiKeySet(v.APIKeySet);
        setModelAgents(v.Agents ?? {});
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

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
        BaseURL: modelBaseURL.trim(),
        APIKey: modelApiKey,
        ClearAPIKey: modelClearKey,
        Agents: modelAgents,
      });
      if (modelApiKey) setModelApiKeySet(true);
      if (modelClearKey) setModelApiKeySet(false);
      setModelApiKey('');
      setModelClearKey(false);
      notify('已保存，对新启动的会话生效', 'info');
    } catch (e: unknown) {
      notify(e instanceof Error ? e.message : String(e), 'error');
    } finally {
      setModelSaving(false);
    }
  };

  // 立即重启：成功后进程退出（按钮保持禁用直到窗口消失）；失败恢复按钮并轻量提示
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

  return (
    <div className="flex-1 overflow-y-auto p-6">
      <div className="max-w-2xl">
        <h1 className="mb-4 text-lg font-semibold">设置</h1>

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
          <h2 className="mb-3 text-sm font-medium">模型（对所有 agent 启动时注入）</h2>
          <label className="mb-2 flex items-center gap-2 text-sm">
            <input type="checkbox" checked={modelEnabled} onChange={(e) => setModelEnabled(e.target.checked)} />
            启用模型配置
          </label>
          <div className="grid gap-2">
            <label className="text-xs text-muted-foreground" htmlFor="model-base-url">模型 Base URL</label>
            <input
              id="model-base-url"
              aria-label="模型 Base URL"
              className="rounded border border-input bg-card px-2 py-1 text-sm"
              placeholder="https://..."
              value={modelBaseURL}
              onChange={(e) => setModelBaseURL(e.target.value)}
            />
            <label className="text-xs text-muted-foreground" htmlFor="model-api-key">模型 API Key</label>
            <input
              id="model-api-key"
              aria-label="模型 API Key"
              type="password"
              className="rounded border border-input bg-card px-2 py-1 text-sm"
              placeholder={modelApiKeySet ? '已设置（留空不修改）' : '未设置'}
              value={modelApiKey}
              onChange={(e) => setModelApiKey(e.target.value)}
            />
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <input type="checkbox" checked={modelClearKey} onChange={(e) => setModelClearKey(e.target.checked)} />
              清除密钥
            </label>
            {MODEL_AGENTS.map((a) => (
              <div key={a.id} className="grid gap-1">
                <label className="text-xs text-muted-foreground" htmlFor={`model-${a.id}`}>{a.label} 模型</label>
                <input
                  id={`model-${a.id}`}
                  aria-label={`${a.label} 模型`}
                  className="rounded border border-input bg-card px-2 py-1 text-sm"
                  value={modelAgents[a.id] ?? ''}
                  onChange={(e) => setModelAgents((prev) => ({ ...prev, [a.id]: e.target.value }))}
                />
              </div>
            ))}
            <div>
              <Button onClick={() => void handleSaveModel()} disabled={modelSaving}>保存模型配置</Button>
            </div>
            <p className="text-xs text-muted-foreground">
              启动时注入，不改各工具自身配置文件。Claude 受 ~/.claude/settings.json 的 env 影响，若不生效需先清掉该段。
            </p>
          </div>
        </section>

        <section className="mb-5 rounded border border-border bg-card p-3.5">
          <h2 className="mb-3 text-sm font-medium">工具检测</h2>
          {toolsError && <p className="text-sm text-destructive">{toolsError}</p>}
          {tools === null && !toolsError && (
            <p className="text-sm text-muted-foreground">加载中……</p>
          )}
          {tools !== null && tools.length === 0 && (
            <p className="text-sm text-muted-foreground">未检测到任何工具</p>
          )}
          {tools !== null && tools.length > 0 && (
            <ul className="divide-y divide-border rounded border border-border">
              {tools.map((t) => (
                <li
                  key={t.ID}
                  className={cn(
                    'flex items-center gap-2 px-2.5 py-1.5 text-xs transition-colors hover:bg-muted',
                    !t.Installed && 'opacity-50',
                  )}
                  title={
                    t.Source === 'config-dir'
                      ? '只检测到配置目录，没有可执行程序，可用性未验证'
                      : t.BinPath
                  }
                >
                  <span className="font-medium">{t.Name}</span>
                  {t.Source === 'config-dir' && (
                    <Badge variant="warning">未验证</Badge>
                  )}
                  {t.Installed ? (
                    t.Version && <span className="text-xs text-muted-foreground">{t.Version}</span>
                  ) : (
                    <span className="ml-auto text-xs text-muted-foreground">未安装</span>
                  )}
                </li>
              ))}
            </ul>
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
      </div>
    </div>
  );
}
