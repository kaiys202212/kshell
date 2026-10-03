// 设置页（顶部固定页签）：工具检测状态 + providers.yaml 自定义工具编辑保存。
// 工具状态来自 GetTools（最近一次扫描，含未安装项）：未安装灰显，
// Source=config-dir 表示「只检测到配置目录没有可执行程序」，视为未验证，显示徽标。
// providers.yaml 来自 LoadProvidersYAML（文件缺失时 Go 侧回填模板），
// 保存走 SaveProvidersYAML（Go 侧先校验 YAML 再原子写回）——
// 保存成功不热生效，需重启应用后由重扫装配，UI 明确提示这一点。
import { useEffect, useState } from 'react';
import {
  getAppearance,
  getTools,
  loadProvidersYAML,
  restartApp,
  saveProvidersYAML,
  setAppearanceMode,
} from '../lib/api';
import type { ToolInfo } from '../lib/api';
import { cn } from '../lib/cn';
import { useAppStore } from '../state/store';
import { Badge } from '../components/ui/badge';
import { Button } from '../components/ui/button';

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
    return () => {
      cancelled = true;
    };
  }, []);

  const handleAppearance = async (mode: string) => {
    try {
      await setAppearanceMode(mode);
      setAppearanceLocal(mode);
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
                onClick={() => void handleAppearance(opt.value)}
              >
                {opt.label}
              </Button>
            ))}
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
