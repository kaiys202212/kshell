// 自定义工具：表单为主，可切到整份 YAML 源码；目录/可执行文件走系统选择器。
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from './ui/button';
import {
  emptyCustomProvider,
  sessionGlobFromDir,
  type CustomProviderSpec,
} from '../lib/providersForm';

type Mode = 'form' | 'source';

export function ProvidersEditor(props: {
  yaml: string | null;
  yamlError: string;
  saved: boolean;
  saveError: string;
  saving: boolean;
  specs: CustomProviderSpec[];
  mode: Mode;
  onMode: (m: Mode) => void;
  onYamlChange: (v: string) => void;
  onSpecsChange: (next: CustomProviderSpec[]) => void;
  onSave: () => void;
  onPickDir: (title: string) => Promise<string>;
  onPickFile: (title: string) => Promise<string>;
}) {
  const { t } = useTranslation();
  const [sel, setSel] = useState(0);
  const spec = props.specs[Math.min(sel, Math.max(0, props.specs.length - 1))];

  const patch = (partial: Partial<CustomProviderSpec>) => {
    const next = props.specs.map((s, i) => (i === sel ? { ...s, ...partial } : s));
    props.onSpecsChange(next);
  };

  const add = () => {
    const next = [...props.specs, emptyCustomProvider()];
    props.onSpecsChange(next);
    setSel(next.length - 1);
  };

  const remove = () => {
    if (!spec) return;
    const next = props.specs.filter((_, i) => i !== sel);
    props.onSpecsChange(next);
    setSel(Math.max(0, sel - 1));
  };

  return (
    <section className="rounded border border-border bg-card p-3.5">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 className="text-sm font-medium">{t('ui.providers.title')}</h2>
        <div className="flex gap-1">
          <Button
            size="sm"
            variant={props.mode === 'form' ? 'default' : 'secondary'}
            aria-pressed={props.mode === 'form'}
            onClick={() => props.onMode('form')}
          >
            {t('ui.providers.mode_form')}
          </Button>
          <Button
            size="sm"
            variant={props.mode === 'source' ? 'default' : 'secondary'}
            aria-pressed={props.mode === 'source'}
            onClick={() => props.onMode('source')}
          >
            {t('ui.providers.mode_source')}
          </Button>
        </div>
      </div>
      {props.yamlError && <p className="text-sm text-destructive">{props.yamlError}</p>}
      {props.mode === 'source' && props.yaml !== null && (
        <textarea
          className="min-h-[280px] w-full resize-y rounded border border-input bg-card p-2.5 font-mono text-xs leading-[1.55] text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label={t('ui.providers.yaml_aria')}
          value={props.yaml}
          spellCheck={false}
          onChange={(e) => props.onYamlChange(e.target.value)}
        />
      )}
      {props.mode === 'form' && (
        <div className="space-y-3">
          <div className="flex flex-wrap gap-2">
            <Button size="sm" type="button" onClick={add}>
              {t('ui.providers.add')}
            </Button>
            {spec && (
              <Button size="sm" variant="secondary" type="button" onClick={remove}>
                {t('ui.providers.remove_current')}
              </Button>
            )}
          </div>
          {props.specs.length === 0 && (
            <p className="text-xs text-muted-foreground">{t('ui.providers.empty')}</p>
          )}
          {props.specs.length > 0 && (
            <div className="flex gap-2">
              <select
                className="rounded border border-input bg-card px-2 py-1 text-xs"
                aria-label={t('ui.providers.current_aria')}
                value={String(sel)}
                onChange={(e) => setSel(Number(e.target.value))}
              >
                {props.specs.map((s, i) => (
                  <option key={i} value={i}>
                    {s.ID || s.Name || t('ui.providers.unnamed', { n: i + 1 })}
                  </option>
                ))}
              </select>
            </div>
          )}
          {spec && (
            <div className="grid gap-2 text-xs">
              <Field label="ID">
                <input
                  className="w-full rounded border border-input bg-card px-2 py-1"
                  aria-label={t('ui.providers.id_aria')}
                  value={spec.ID}
                  onChange={(e) => patch({ ID: e.target.value })}
                />
              </Field>
              <Field label={t('ui.providers.field_name')}>
                <input
                  className="w-full rounded border border-input bg-card px-2 py-1"
                  aria-label={t('ui.providers.name_aria')}
                  value={spec.Name}
                  onChange={(e) => patch({ Name: e.target.value })}
                />
              </Field>
              <Field label={t('ui.providers.field_exec')}>
                <div className="flex gap-1">
                  <input
                    className="min-w-0 flex-1 rounded border border-input bg-card px-2 py-1 font-mono"
                    aria-label={t('ui.providers.detect_cmd_aria')}
                    value={spec.Detect.Command}
                    onChange={(e) => patch({ Detect: { ...spec.Detect, Command: e.target.value } })}
                  />
                  <Button
                    size="sm"
                    variant="secondary"
                    type="button"
                    onClick={() => {
                      void props.onPickFile(t('ui.providers.pick_exec_title')).then((p) => {
                        if (p) patch({ Detect: { ...spec.Detect, Command: p } });
                      });
                    }}
                  >
                    {t('ui.providers.pick_file')}
                  </Button>
                </div>
              </Field>
              <Field label={t('ui.providers.field_detect_dirs')}>
                <div className="space-y-1">
                  {(spec.Detect.Dirs ?? []).map((d, i) => (
                    <input
                      key={i}
                      className="w-full rounded border border-input bg-card px-2 py-1 font-mono"
                      aria-label={t('ui.providers.detect_dir_aria', { n: i + 1 })}
                      value={d}
                      onChange={(e) => {
                        const dirs = [...spec.Detect.Dirs];
                        dirs[i] = e.target.value;
                        patch({ Detect: { ...spec.Detect, Dirs: dirs } });
                      }}
                    />
                  ))}
                  <Button
                    size="sm"
                    variant="secondary"
                    type="button"
                    onClick={() => {
                      void props.onPickDir(t('ui.providers.pick_detect_dir_title')).then((p) => {
                        if (p) patch({ Detect: { ...spec.Detect, Dirs: [...spec.Detect.Dirs, p] } });
                      });
                    }}
                  >
                    {t('ui.providers.pick_folder')}
                  </Button>
                </div>
              </Field>
              <Field label={t('ui.providers.field_session_glob')}>
                <div className="flex gap-1">
                  <input
                    className="min-w-0 flex-1 rounded border border-input bg-card px-2 py-1 font-mono"
                    aria-label={t('ui.providers.session_glob_aria')}
                    value={spec.Sessions.Glob}
                    onChange={(e) =>
                      patch({ Sessions: { ...spec.Sessions, Glob: e.target.value } })
                    }
                  />
                  <Button
                    size="sm"
                    variant="secondary"
                    type="button"
                    onClick={() => {
                      void props.onPickDir(t('ui.providers.pick_session_dir_title')).then((p) => {
                        if (p)
                          patch({
                            Sessions: { ...spec.Sessions, Glob: sessionGlobFromDir(p) },
                          });
                      });
                    }}
                  >
                    {t('ui.providers.pick_session_dir')}
                  </Button>
                </div>
              </Field>
              <Field label={t('ui.providers.field_format')}>
                <select
                  className="rounded border border-input bg-card px-2 py-1"
                  aria-label={t('ui.providers.session_format_aria')}
                  value={spec.Sessions.Format}
                  onChange={(e) =>
                    patch({ Sessions: { ...spec.Sessions, Format: e.target.value } })
                  }
                >
                  <option value="jsonl">jsonl</option>
                  <option value="json">json</option>
                </select>
              </Field>
              <Field label={t('ui.providers.field_fields')}>
                <div className="grid grid-cols-2 gap-1">
                  <input
                    aria-label={t('ui.providers.field_cwd_aria')}
                    className="rounded border border-input px-2 py-1 font-mono"
                    value={spec.Fields.CWD}
                    onChange={(e) => patch({ Fields: { ...spec.Fields, CWD: e.target.value } })}
                  />
                  <input
                    aria-label={t('ui.providers.field_id_aria')}
                    className="rounded border border-input px-2 py-1 font-mono"
                    value={spec.Fields.ID}
                    onChange={(e) => patch({ Fields: { ...spec.Fields, ID: e.target.value } })}
                  />
                  <input
                    aria-label={t('ui.providers.field_timestamp_aria')}
                    className="rounded border border-input px-2 py-1 font-mono"
                    value={spec.Fields.Timestamp}
                    onChange={(e) =>
                      patch({ Fields: { ...spec.Fields, Timestamp: e.target.value } })
                    }
                  />
                  <input
                    aria-label={t('ui.providers.field_title_aria')}
                    className="rounded border border-input px-2 py-1 font-mono"
                    value={spec.Fields.Title}
                    onChange={(e) => patch({ Fields: { ...spec.Fields, Title: e.target.value } })}
                  />
                </div>
              </Field>
              <Field label={t('ui.providers.field_title_fallbacks')}>
                <input
                  className="w-full rounded border border-input bg-card px-2 py-1 font-mono"
                  aria-label={t('ui.providers.title_fallbacks_aria')}
                  value={(spec.Fields.TitleFallbacks ?? []).join(' ')}
                  onChange={(e) =>
                    patch({
                      Fields: {
                        ...spec.Fields,
                        TitleFallbacks: e.target.value.split(/\s+/).filter(Boolean),
                      },
                    })
                  }
                />
              </Field>
              <Field label={t('ui.providers.field_resume')}>
                <input
                  className="w-full rounded border border-input bg-card px-2 py-1 font-mono"
                  aria-label={t('ui.providers.resume_aria')}
                  value={spec.Resume.Args.join(' ')}
                  onChange={(e) =>
                    patch({
                      Resume: { Args: e.target.value.split(/\s+/).filter(Boolean) },
                    })
                  }
                />
              </Field>
              <label className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={spec.Verified}
                  onChange={(e) => patch({ Verified: e.target.checked })}
                />
                {t('ui.providers.verified')}
              </label>
            </div>
          )}
        </div>
      )}
      {props.yaml !== null && (
        <div className="mt-2 flex items-center gap-2.5">
          <Button onClick={() => props.onSave()} disabled={props.saving}>
            {t('ui.providers.save')}
          </Button>
          {props.saved && (
            <span className="text-sm text-muted-foreground">{t('ui.providers.saved')}</span>
          )}
          {props.saveError && <span className="text-sm text-destructive">{props.saveError}</span>}
        </div>
      )}
    </section>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

