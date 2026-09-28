// 设置页（顶部固定页签）：工具检测状态 + providers.yaml 自定义工具编辑保存。
// 工具状态来自 GetTools（最近一次扫描，含未安装项）：未安装灰显，
// Source=config-dir 表示「只检测到配置目录没有可执行程序」，视为未验证，显示徽标。
// providers.yaml 来自 LoadProvidersYAML（文件缺失时 Go 侧回填模板），
// 保存走 SaveProvidersYAML（Go 侧先校验 YAML 再原子写回）——
// 保存成功不热生效，需重启应用后由重扫装配，UI 明确提示这一点。
import { useEffect, useState } from 'react';
import { getTools, loadProvidersYAML, saveProvidersYAML } from '../lib/api';
import type { ToolInfo } from '../lib/api';

export default function Settings() {
  const [tools, setTools] = useState<ToolInfo[] | null>(null);
  const [toolsError, setToolsError] = useState('');
  const [yaml, setYaml] = useState<string | null>(null);
  const [yamlError, setYamlError] = useState('');
  const [saved, setSaved] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);

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
    return () => {
      cancelled = true;
    };
  }, []);

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

  return (
    <div className="settings">
      <h1 className="settings-title">设置</h1>

      <section className="settings-section">
        <h2 className="settings-subtitle">工具检测</h2>
        {toolsError && <p className="settings-error">{toolsError}</p>}
        {tools === null && !toolsError && <p className="settings-status">加载中……</p>}
        {tools !== null && tools.length === 0 && (
          <p className="settings-status">未检测到任何工具</p>
        )}
        {tools !== null && tools.length > 0 && (
          <ul className="tool-list">
            {tools.map((t) => (
              <li
                key={t.ID}
                className={t.Installed ? 'tool-item' : 'tool-item tool-item--uninstalled'}
                title={
                  t.Source === 'config-dir'
                    ? '只检测到配置目录，没有可执行程序，可用性未验证'
                    : t.BinPath
                }
              >
                <span className="tool-name">{t.Name}</span>
                {t.Source === 'config-dir' && <span className="tool-unverified">未验证</span>}
                {t.Installed ? (
                  t.Version && <span className="tool-version">{t.Version}</span>
                ) : (
                  <span className="tool-missing">未安装</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="settings-section">
        <h2 className="settings-subtitle">自定义工具（providers.yaml）</h2>
        {yamlError && <p className="settings-error">{yamlError}</p>}
        {yaml !== null && (
          <>
            <textarea
              className="yaml-editor"
              aria-label="providers.yaml 编辑器"
              value={yaml}
              spellCheck={false}
              onChange={(e) => {
                setYaml(e.target.value);
                setSaved(false);
                setSaveError('');
              }}
            />
            <div className="settings-actions">
              <button
                className="settings-save"
                onClick={() => void handleSave()}
                disabled={saving}
              >
                保存
              </button>
              {saved && (
                <span className="settings-saved">已保存，重启应用后生效</span>
              )}
              {saveError && <span className="settings-error">{saveError}</span>}
            </div>
          </>
        )}
      </section>
    </div>
  );
}
