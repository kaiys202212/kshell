// 文件预览（工作区页签中间栏）：按 previewKind 调度 CM6 / Markdown / 图 / PDF。
// 文本优先用 FilePreview.Text；无 Text 时 stripLinePrefix(Lines) 去掉 "NNNN │ " 前缀。
// Truncated 显示截断提示；Binary 且非图/PDF 只展示 Info 元信息。
// 编辑模式：readFileForEdit 整读（上限 1MB、拒二进制），CodeEditor 编辑 + Ctrl/Cmd+S
// 或保存按钮落盘（Go 侧原子替换并按原行尾还原），保存后刷新 git 状态镜像。
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { previewFile, readFileForEdit, saveFile } from '../lib/api';
import type { FilePreview } from '../lib/api';
import { previewKind, type PreviewKind } from '../lib/fileKind';
import { refreshGitStatus } from '../lib/git';
import { useAppStore } from '../state/store';
import CodeEditor from './CodeEditor';
import ImagePreview from './ImagePreview';
import MarkdownPreview from './MarkdownPreview';
import PdfPreview from './PdfPreview';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
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

export default function Preview({ wsPath, path }: { wsPath: string; path: string | null }) {
  const [data, setData] = useState<FilePreview | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  // 编辑态
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  const [eol, setEol] = useState('lf');
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  // Markdown 源码 | 预览；切文件时重置为 preview
  const [mdMode, setMdMode] = useState<'source' | 'preview'>('preview');
  // 保存成功后重新预览：主 effect 依赖 reload，bump 即触发
  const [reload, setReload] = useState(0);
  // 当前「生效」的文件路径：异步回调（整读/保存在途）用它判断自己是否还有效，
  // 防止用户在请求途中切换文件后，旧文件的回调把内容写进新文件（数据覆盖）
  const activePathRef = useRef<string | null>(path);

  const resolvedTheme = useAppStore((s) => s.appearance.resolved);
  const kind: PreviewKind | null = path ? previewKind(path) : null;
  const isMedia = kind === 'image' || kind === 'pdf';

  useEffect(() => {
    // 切换文件即放弃未保存的编辑（简化口径：编辑内容不跟随文件保存草稿，误切会丢改动）
    activePathRef.current = path;
    setEditing(false);
    setDirty(false);
    setMdMode('preview');
    if (!path) {
      setData(null);
      setError('');
      setLoading(false);
      return;
    }
    // 图/PDF 由专用组件读字节，无需文本 previewFile
    const pk = previewKind(path);
    if (pk === 'image' || pk === 'pdf') {
      setData(null);
      setError('');
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError('');
    setData(null);
    previewFile(wsPath, path)
      .then((p) => {
        if (cancelled) return;
        if (!p) {
          setError('未检测到 kshell 桌面端绑定，请在桌面端运行');
          return;
        }
        setData(p);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [wsPath, path, reload]);

  const notify = useAppStore.getState().notify;

  const enterEdit = () => {
    if (!path) return;
    const reqPath = path;
    readFileForEdit(wsPath, reqPath)
      .then((ec) => {
        // 在途时用户已切到别的文件：结果作废，绝不把旧内容带进新文件的编辑态
        if (activePathRef.current !== reqPath) return;
        if (!ec) {
          notify('未检测到 kshell 桌面端绑定，请在桌面端运行', 'error');
          return;
        }
        setText(ec.Text);
        setEol(ec.EOL === 'crlf' ? 'crlf' : 'lf');
        setDirty(false);
        setEditing(true);
        setMdMode('source');
      })
      .catch((e: unknown) => {
        if (activePathRef.current !== reqPath) return;
        notify(e instanceof Error ? e.message : String(e), 'error');
      });
  };

  const handleSave = () => {
    if (!path || saving) return;
    const reqPath = path;
    setSaving(true);
    saveFile(wsPath, reqPath, text, eol)
      .then(() => {
        if (activePathRef.current !== reqPath) return; // 保存途中已切走：落盘仍生效，界面不回写
        notify('已保存', 'success');
        setEditing(false);
        setDirty(false);
        void refreshGitStatus(wsPath);
        setReload((k) => k + 1); // 重新预览拿最新内容
      })
      .catch((e: unknown) => {
        if (activePathRef.current !== reqPath) return;
        notify(e instanceof Error ? e.message : String(e), 'error');
      })
      .finally(() => setSaving(false));
  };

  const cancelEdit = () => {
    if (dirty && !window.confirm('有未保存的修改，确定放弃吗？')) return;
    setEditing(false);
    setDirty(false);
  };

  const onEditorKeyDown = (e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      e.preventDefault();
      if (dirty) void handleSave();
    }
  };

  // 编辑按钮只在「文本内容加载完成」时显示（加载中/错误/二进制/媒体态不提供操作入口）
  const showEditButton =
    path !== null && !loading && !error && data !== null && !data.Binary && !isMedia;

  const cmTheme = resolveEditorTheme(resolvedTheme);

  const bodyContent = () => {
    if (!path) return null;
    if (kind === 'image') {
      return <ImagePreview wsPath={wsPath} path={path} />;
    }
    if (kind === 'pdf') {
      return <PdfPreview wsPath={wsPath} path={path} />;
    }
    if (!data) return null;

    // 后端 Binary 且非图/PDF：防扩展名伪装，只显示元信息
    if (data.Binary) {
      return <p className="text-sm text-muted-foreground">{data.Info || '二进制文件，无法预览'}</p>;
    }

    const content = previewText(data);
    return (
      <>
        {data.Info && <p className="mb-2 text-xs text-muted-foreground">{data.Info}</p>}
        {/* 截断行数 500 与 Go 侧 internal/workspace/preview.go 的预览行数上限耦合，改一处需同步 */}
        {data.Truncated && (
          <Badge variant="outline" className="mb-2 w-fit">
            内容已截断：仅显示前 500 行
          </Badge>
        )}
        {kind === 'markdown' && mdMode === 'preview' ? (
          <div className="min-h-0 flex-1">
            <MarkdownPreview markdown={content} />
          </div>
        ) : (
          <div className="mt-2 min-h-[240px] min-h-0 flex-1">
            <CodeEditor value={content} readOnly path={path} theme={cmTheme} />
          </div>
        )}
      </>
    );
  };

  return (
    <div className="flex min-h-0 flex-col text-sm">
      <div className="sticky top-0 z-10 flex items-center border-b border-border bg-card py-2">
        <span
          className="truncate font-mono text-xs text-muted-foreground"
          title={path ?? ''}
        >
          {path ?? '未选择文件'}
          {editing && dirty && <span className="ml-1 text-warning">●</span>}
        </span>
        {editing && (
          <>
            <Button
              size="sm"
              className="ml-auto shrink-0"
              disabled={!dirty || saving}
              onClick={() => void handleSave()}
            >
              {saving ? '保存中…' : '保存'}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              className="ml-1.5 shrink-0"
              disabled={saving}
              onClick={cancelEdit}
            >
              取消
            </Button>
          </>
        )}
        {!editing && kind === 'markdown' && path && !loading && !error && (
          <div className="ml-auto flex gap-1">
            <Button
              size="sm"
              variant={mdMode === 'source' ? 'default' : 'secondary'}
              onClick={() => setMdMode('source')}
            >
              源码
            </Button>
            <Button
              size="sm"
              variant={mdMode === 'preview' ? 'default' : 'secondary'}
              onClick={() => setMdMode('preview')}
            >
              预览
            </Button>
            {showEditButton && (
              <Button size="sm" variant="secondary" onClick={enterEdit}>
                编辑
              </Button>
            )}
          </div>
        )}
        {!editing && kind !== 'markdown' && showEditButton && (
          <Button size="sm" variant="secondary" className="ml-auto shrink-0" onClick={enterEdit}>
            编辑
          </Button>
        )}
      </div>
      {!path && <EmptyState title="从右侧文件树选择文件查看预览" />}
      {loading && (
        <div className="mt-3 flex flex-col gap-2">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Skeleton key={i} className="h-4" style={{ width: `${88 - (i % 3) * 18}%` }} />
          ))}
        </div>
      )}
      {error && <p className="text-sm text-destructive">{error}</p>}
      {!loading && !error && editing && path && (
        <div className="mt-2 min-h-[240px] min-h-0 flex-1" onKeyDown={onEditorKeyDown}>
          <CodeEditor
            value={text}
            readOnly={false}
            path={path}
            theme={cmTheme}
            onChange={(v) => {
              setText(v);
              setDirty(true);
            }}
          />
        </div>
      )}
      {!loading && !error && !editing && bodyContent()}
    </div>
  );
}
