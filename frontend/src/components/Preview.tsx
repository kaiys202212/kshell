// 文件预览/编辑：可编辑文件打开即可写 CodeEditor（无编辑/预览切换）；
// 图/PDF/二进制只读。Ctrl/Cmd+S 保存后仍留在编辑器。草稿随本实例，父级常挂载页签。
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { previewFile, readFileForEdit, saveFile } from '../lib/api';
import type { FilePreview } from '../lib/api';
import { backendError, translateBackend } from '../lib/errors';
import { isEditableKind, previewKind, type PreviewKind } from '../lib/fileKind';
import { refreshGitStatus } from '../lib/git';
import { useAppStore } from '../state/store';
import CodeEditor from './CodeEditor';
import ImagePreview from './ImagePreview';
import PdfPreview from './PdfPreview';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';

/** 去掉 Go PreviewFile 注入的行号前缀（无 Text 时的回退）。 */
export function stripLinePrefix(lines: string[]): string {
  return lines.map((l) => l.replace(/^\s*\d+\s*│\s?/, '')).join('\n');
}

function previewText(data: FilePreview): string {
  if (typeof data.Text === 'string') return data.Text;
  return stripLinePrefix(data.Lines ?? []);
}

/** CodeEditor 主题：优先 appearance.resolved，其次 html.dark class。 */
function resolveEditorTheme(resolved: 'light' | 'dark'): 'light' | 'dark' {
  if (resolved === 'light' || resolved === 'dark') return resolved;
  if (typeof document !== 'undefined' && document.documentElement.classList.contains('dark')) {
    return 'dark';
  }
  return 'light';
}

export default function Preview({
  wsPath,
  path,
  onDirtyChange,
  onEdited,
}: {
  wsPath: string;
  path: string | null;
  onDirtyChange?: (dirty: boolean) => void;
  onEdited?: () => void;
}) {
  const { t } = useTranslation();
  const [data, setData] = useState<FilePreview | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [text, setText] = useState('');
  const [eol, setEol] = useState('lf');
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editable, setEditable] = useState(false);
  const activePathRef = useRef<string | null>(path);
  const baselineRef = useRef('');

  const resolvedTheme = useAppStore((s) => s.appearance.resolved);
  const showWhitespace = useAppStore((s) => !!s.appearance.showWhitespace);
  const kind: PreviewKind | null = path ? previewKind(path) : null;
  const isMedia = kind === 'image' || kind === 'pdf';
  const wantEdit = !!path && !!kind && isEditableKind(kind) && !isMedia;

  useEffect(() => {
    activePathRef.current = path;
    setDirty(false);
    baselineRef.current = '';
    onDirtyChange?.(false);
    setEditable(false);
    if (!path) {
      setData(null);
      setError('');
      setLoading(false);
      setText('');
      return;
    }
    if (isMedia) {
      setData(null);
      setError('');
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError('');
    setData(null);
    const reqPath = path;
    const loadPreviewOnly = () =>
      previewFile(wsPath, reqPath)
        .then((p) => {
          if (cancelled || activePathRef.current !== reqPath) return;
          if (!p) {
            setError(t('ui.files.binding_missing'));
            return;
          }
          setData(p);
        })
        .catch((e: unknown) => {
          if (cancelled || activePathRef.current !== reqPath) return;
          setError(backendError(e));
        });

    const done = () => {
      if (!cancelled) setLoading(false);
    };

    if (wantEdit) {
      readFileForEdit(wsPath, reqPath)
        .then((ec) => {
          if (cancelled || activePathRef.current !== reqPath) return;
          if (!ec) {
            setError(t('ui.files.binding_missing'));
            return;
          }
          setText(ec.Text);
          baselineRef.current = ec.Text;
          setEol(ec.EOL === 'crlf' ? 'crlf' : 'lf');
          setEditable(true);
        })
        .catch(() => loadPreviewOnly())
        .finally(done);
    } else {
      loadPreviewOnly().finally(done);
    }
    return () => {
      cancelled = true;
    };
    // onDirtyChange 仅在切路径时清脏，不列入依赖以免父级重渲染反复加载；
    // t 也不列入依赖：本 effect 会重置 dirty/baseline，若切语言重跑会丢弃未保存草稿。
    // 错误串在设置时即经 backendError 翻译固化，切语言不回溯重译属可接受的瞬时状态。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wsPath, path, wantEdit, isMedia]);

  const notify = useAppStore.getState().notify;

  const handleSave = () => {
    if (!path || saving || !editable) return;
    const reqPath = path;
    setSaving(true);
    saveFile(wsPath, reqPath, text, eol)
      .then(() => {
        if (activePathRef.current !== reqPath) return;
        notify(t('ui.files.saved'), 'success');
        baselineRef.current = text;
        setDirty(false);
        onDirtyChange?.(false);
        void refreshGitStatus(wsPath);
      })
      .catch((e: unknown) => {
        if (activePathRef.current !== reqPath) return;
        notify(backendError(e), 'error');
      })
      .finally(() => setSaving(false));
  };

  const onEditorKeyDown = (e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      e.preventDefault();
      if (dirty) void handleSave();
    }
  };

  const cmTheme = resolveEditorTheme(resolvedTheme);

  const bodyContent = () => {
    if (!path) return null;
    if (kind === 'image') {
      return (
        <div className="min-h-0 flex-1 overflow-auto">
          <ImagePreview wsPath={wsPath} path={path} />
        </div>
      );
    }
    if (kind === 'pdf') {
      return (
        <div className="min-h-0 flex-1 overflow-hidden">
          <PdfPreview wsPath={wsPath} path={path} />
        </div>
      );
    }
    if (editable) return null;
    if (!data) return null;
    if (data.Binary) {
      return (
        <p className="text-sm text-muted-foreground">
          {translateBackend(data.Info) || t('ui.files.binary_preview_unavailable')}
        </p>
      );
    }
    const content = previewText(data);
    return (
      <>
        {data.Info && (
          <p className="shrink-0 mb-2 text-xs text-muted-foreground">{translateBackend(data.Info)}</p>
        )}
        <div className="min-h-0 flex-1 overflow-hidden">
          <CodeEditor
            value={content}
            readOnly
            path={path}
            theme={cmTheme}
            showWhitespace={showWhitespace}
          />
        </div>
      </>
    );
  };

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden text-sm">
      <div
        data-testid="preview-body"
        className="flex min-h-0 flex-1 flex-col overflow-hidden"
      >
        {!path && <EmptyState title={t('ui.files.pick_from_tree')} />}
        {loading && (
          <div className="flex flex-col gap-2">
            {[0, 1, 2, 3, 4, 5].map((i) => (
              <Skeleton key={i} className="h-4" style={{ width: `${88 - (i % 3) * 18}%` }} />
            ))}
          </div>
        )}
        {error && <p className="text-sm text-destructive">{error}</p>}
        {!loading && !error && editable && path && (
          <div className="min-h-0 flex-1 overflow-hidden" onKeyDown={onEditorKeyDown}>
            <CodeEditor
              value={text}
              readOnly={false}
              path={path}
              theme={cmTheme}
              showWhitespace={showWhitespace}
              onChange={(v) => {
                const nextDirty = v !== baselineRef.current;
                setText(v);
                setDirty(nextDirty);
                if (nextDirty !== dirty) onDirtyChange?.(nextDirty);
                if (nextDirty && !dirty) onEdited?.();
              }}
            />
          </div>
        )}
        {!loading && !error && !editable && bodyContent()}
      </div>
    </div>
  );
}
