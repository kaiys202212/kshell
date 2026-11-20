// 设置页「本地语音转写」区块：安装/卸载本地引擎（后台任务 + 流式日志），
// 以及本地转写的持久化配置（启用、优先级、语言、ITN、镜像、自定义命令）。
// 安装过程日志同时存在后端任务里：挂载时拉一次全量，事件只追加增量。
import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  getASRInstallLog,
  getASRStatus,
  installASRLocal,
  onASRInstallDone,
  onASRInstallLog,
  onASRInstallProgress,
  saveASRSettings,
  uninstallASRLocal,
  type ASRProgressView,
  type ASRStatusView,
} from '../lib/api';
import { backendError } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';

// 解绑式二次点击确认的自动复位时长（ms）
const CONFIRM_RESET = 3000;

const inputClass = 'rounded border border-input bg-card px-2 py-1 text-sm';

// 语言取值与后端 config.asrLanguages 一致：auto/zh/en/ja/ko/yue。
const LANGS = ['auto', 'zh', 'en', 'ja', 'ko', 'yue'] as const;

// 阶段标识 → i18n key（与 internal/desktop/asr.go 的 asrPhase* 常量一致）。
const PHASE_KEYS: Record<string, string> = {
  runtime: 'ui.asr.phase_runtime',
  'model.int8.onnx': 'ui.asr.phase_model',
  'tokens.txt': 'ui.asr.phase_tokens',
  extract: 'ui.asr.phase_extract',
  done: 'ui.asr.phase_done',
};

const EMPTY_PROGRESS: ASRProgressView = { phase: '', pct: 0, overall: 0 };

export function ASRSettings() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [status, setStatus] = useState<ASRStatusView | null>(null);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [log, setLog] = useState('');
  const [progress, setProgress] = useState<ASRProgressView>(EMPTY_PROGRESS);
  const [confirming, setConfirming] = useState(false);
  const confirmTimer = useRef(0);
  // 草稿态：dirty 才允许保存（避免误触改写配置）
  const [enabled, setEnabled] = useState(false);
  const [preferCloud, setPreferCloud] = useState(false);
  const [language, setLanguage] = useState('auto');
  const [itn, setItn] = useState(true);
  const [mirrors, setMirrors] = useState('');
  const [command, setCommand] = useState('');
  const [dirty, setDirty] = useState(false);

  const applyStatus = useCallback((s: ASRStatusView) => {
    setStatus(s);
    setEnabled(s.enabled);
    setPreferCloud(s.preferCloud);
    setLanguage(s.language || 'auto');
    setItn(s.itn);
    setMirrors((s.mirrors ?? []).join('\n'));
    setCommand(s.command ?? '');
    setProgress(s.progress ?? EMPTY_PROGRESS);
    setDirty(false);
  }, []);

  const refresh = useCallback(async () => {
    const v = await getASRStatus();
    if (v) {
      setFailed(false);
      applyStatus(v);
    } else {
      setFailed(true);
    }
  }, [applyStatus]);

  useEffect(() => {
    void refresh();
    getASRInstallLog()
      .then((text) => setLog(text))
      .catch(() => {});
    const offLog = onASRInstallLog((text) => setLog((prev) => (prev ? `${prev}\n${text}` : text)));
    const offProgress = onASRInstallProgress((p) => setProgress(p));
    const offDone = onASRInstallDone((p) => {
      setBusy(false);
      if (p.ok) {
        setProgress({ phase: 'done', pct: 100, overall: 100 });
        notify(t('ui.asr.install_done'), 'info');
      } else {
        notify(p.error ? backendError(new Error(p.error)) : t('ui.asr.install_failed'), 'error');
      }
      void refresh();
    });
    return () => {
      window.clearTimeout(confirmTimer.current);
      offLog();
      offProgress();
      offDone();
    };
  }, [refresh, notify, t]);

  const handleInstall = async () => {
    setBusy(true);
    setConfirming(false);
    setLog('');
    setProgress(EMPTY_PROGRESS);
    try {
      await installASRLocal();
      await refresh();
    } catch (e: unknown) {
      setBusy(false);
      notify(backendError(e), 'error');
    }
  };

  const handleUninstall = async () => {
    // 二次点击确认：第一次点只变确认文案，3s 未确认自动复位
    if (!confirming) {
      setConfirming(true);
      window.clearTimeout(confirmTimer.current);
      confirmTimer.current = window.setTimeout(() => setConfirming(false), CONFIRM_RESET);
      return;
    }
    window.clearTimeout(confirmTimer.current);
    setConfirming(false);
    setBusy(true);
    try {
      await uninstallASRLocal();
      setLog('');
      await refresh();
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    } finally {
      setBusy(false);
    }
  };

  const handleSave = async () => {
    const list = mirrors
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean);
    setBusy(true);
    try {
      const v = await saveASRSettings({
        enabled,
        preferCloud,
        language,
        itn,
        mirrors: list,
        command: command.trim(),
      });
      applyStatus(v);
      notify(t('ui.asr.saved'), 'info');
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    } finally {
      setBusy(false);
    }
  };

  const running = !!status?.running;
  const installed = !!status?.installed;
  const stateText = !status
    ? ''
    : installed
      ? t('ui.asr.installed')
      : status.partial
        ? t('ui.asr.partial')
        : t('ui.asr.not_installed');
  const disabled = busy || running;
  const phaseKey = PHASE_KEYS[progress.phase];
  const phaseLabel = phaseKey ? t(phaseKey) : '';

  return (
    <section className="mb-5 rounded border border-border bg-card p-3.5">
      <h2 className="mb-1 text-sm font-medium">{t('ui.asr.title')}</h2>
      <p className="mb-3 text-xs text-muted-foreground">{t('ui.asr.desc')}</p>

      {failed && <p className="text-sm text-destructive">{t('ui.asr.error')}</p>}

      {!failed && status && (
        <>
          <div className="mb-2 flex flex-wrap items-center gap-2 text-sm">
            <span
              className={`h-2 w-2 shrink-0 rounded-full ${
                installed ? 'bg-success' : status.partial ? 'bg-warning' : 'bg-muted-foreground'
              }`}
            />
            <span>{stateText}</span>
            {installed && status.version && (
              <span className="text-xs text-muted-foreground">
                {t('ui.asr.version')} {status.version}
              </span>
            )}
            {installed && status.sizeMB > 0 && (
              <span className="text-xs text-muted-foreground">
                {t('ui.asr.size', { 0: status.sizeMB })}
              </span>
            )}
            <span className="text-xs text-muted-foreground">
              {t('ui.asr.platform')} {status.platform}
            </span>
          </div>

          {!status.supported && (
            <p className="mb-2 text-sm text-warning">{t('ui.asr.unsupported')}</p>
          )}

          {status.error && !running && (
            <p className="mb-2 break-all text-xs text-destructive">{status.error}</p>
          )}

          <div className="mb-2 flex flex-wrap gap-2">
            {!running && !installed && status.supported && (
              <Button size="sm" disabled={disabled} onClick={() => void handleInstall()}>
                {t('ui.asr.install')}
              </Button>
            )}
            {!running && status.partial && status.supported && (
              <Button size="sm" disabled={disabled} onClick={() => void handleInstall()}>
                {t('ui.asr.reinstall')}
              </Button>
            )}
            {!running && (installed || status.partial) && (
              <Button
                size="sm"
                variant={confirming ? 'destructive' : 'secondary'}
                disabled={disabled}
                onClick={() => void handleUninstall()}
              >
                {confirming ? t('ui.asr.uninstall_confirm') : t('ui.asr.uninstall')}
              </Button>
            )}
            {running && <span className="text-sm text-muted-foreground">{t('ui.asr.installing')}</span>}
          </div>

          {running && (
            <div className="mb-2">
              <div className="mb-1 flex items-center justify-between text-xs text-muted-foreground">
                <span>{phaseLabel}</span>
                <span aria-live="polite">{progress.overall}%</span>
              </div>
              <div
                role="progressbar"
                aria-label={t('ui.asr.progress')}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={progress.overall}
                className="h-2 w-full overflow-hidden rounded bg-muted"
              >
                <div
                  className="h-full rounded bg-primary transition-all"
                  style={{ width: `${progress.overall}%` }}
                />
              </div>
            </div>
          )}

          {!status.supported && (
            <p className="mb-2 text-xs text-muted-foreground">{t('ui.asr.unsupported_hint')}</p>
          )}

          <p className="mb-3 text-xs text-muted-foreground">{t('ui.asr.install_hint')}</p>

          {log && (
            <>
              <p className="mb-1 text-xs text-muted-foreground">{t('ui.asr.log_title')}</p>
              <pre className="mb-3 max-h-32 overflow-auto rounded bg-muted p-2 font-mono text-[11px] text-muted-foreground">
                {log}
              </pre>
            </>
          )}
        </>
      )}

      {!failed && (
        <div className="border-t border-border pt-3">
          <label className="mb-1 flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={enabled}
              disabled={disabled}
              aria-label={t('ui.asr.enabled')}
              onChange={(e) => {
                setEnabled(e.currentTarget.checked);
                setDirty(true);
              }}
            />
            {t('ui.asr.enabled')}
          </label>
          <p className="mb-3 text-xs text-muted-foreground">{t('ui.asr.enabled_hint')}</p>

          <label className="mb-1 flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={preferCloud}
              disabled={disabled}
              aria-label={t('ui.asr.prefer_cloud')}
              onChange={(e) => {
                setPreferCloud(e.currentTarget.checked);
                setDirty(true);
              }}
            />
            {t('ui.asr.prefer_cloud')}
          </label>
          <p className="mb-3 text-xs text-muted-foreground">{t('ui.asr.prefer_cloud_hint')}</p>

          <label className="mb-1 block text-sm">
            {t('ui.asr.language')}
            <select
              aria-label={t('ui.asr.language')}
              className={`mt-1 block ${inputClass}`}
              value={language}
              disabled={disabled}
              onChange={(e) => {
                setLanguage(e.currentTarget.value);
                setDirty(true);
              }}
            >
              {LANGS.map((v) => (
                <option key={v} value={v}>
                  {t(`ui.asr.lang_${v}`)}
                </option>
              ))}
            </select>
          </label>
          <p className="mb-3 text-xs text-muted-foreground">{t('ui.asr.language_hint')}</p>

          <label className="mb-3 flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={itn}
              disabled={disabled}
              aria-label={t('ui.asr.itn')}
              onChange={(e) => {
                setItn(e.currentTarget.checked);
                setDirty(true);
              }}
            />
            {t('ui.asr.itn')}
          </label>

          <label className="mb-1 block text-sm">
            {t('ui.asr.mirrors')}
            <textarea
              rows={2}
              value={mirrors}
              disabled={disabled}
              aria-label={t('ui.asr.mirrors')}
              className={`mt-1 block w-full font-mono text-xs ${inputClass}`}
              onChange={(e) => {
                setMirrors(e.currentTarget.value);
                setDirty(true);
              }}
            />
          </label>
          <p className="mb-3 text-xs text-muted-foreground">{t('ui.asr.mirrors_hint')}</p>

          <label className="mb-1 block text-sm">
            {t('ui.asr.command')}
            <input
              type="text"
              value={command}
              disabled={disabled}
              aria-label={t('ui.asr.command')}
              placeholder="my-asr {wav}"
              className={`mt-1 block w-full font-mono text-xs ${inputClass}`}
              onChange={(e) => {
                setCommand(e.currentTarget.value);
                setDirty(true);
              }}
            />
          </label>
          <p className="mb-3 text-xs text-muted-foreground">{t('ui.asr.command_hint')}</p>

          <Button size="sm" disabled={!dirty || disabled} onClick={() => void handleSave()}>
            {t('ui.asr.save')}
          </Button>
        </div>
      )}
    </section>
  );
}