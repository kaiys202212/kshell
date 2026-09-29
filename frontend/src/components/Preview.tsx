// 文件预览（工作区页签中间栏）：只读展示 + 文本编辑。
// Go 侧 PreviewFile 返回的 Lines 已带 "NNNN │ " 行号前缀，前端直接渲染、不再加行号；
// Truncated 显示截断提示，Binary 只展示 Info 元信息。
// 编辑模式：readFileForEdit 整读（上限 1MB、拒二进制），textarea 编辑 + Ctrl/Cmd+S
// 或保存按钮落盘（Go 侧原子替换并按原行尾还原），保存后刷新 git 状态镜像。
// 头部提供加入/移出篮子按钮（与 FileTree 共用 lib/basket 的 toggleAndSync 同步逻辑）。
import { useEffect, useRef, useState } from 'react';
import { previewFile, readFileForEdit, saveFile } from '../lib/api';
import type { FilePreview } from '../lib/api';
import { toggleAndSync } from '../lib/basket';
import { refreshGitStatus } from '../lib/git';
import { useAppStore } from '../state/store';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';

export default function Preview({ wsPath, path }: { wsPath: string; path: string | null }) {
  const [data, setData] = useState<FilePreview | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const basket = useAppStore((s) => s.basket);

  // 编辑态
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  const [eol, setEol] = useState('lf');
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  // 保存成功后重新预览：主 effect 依赖 reload，bump 即触发
  const [reload, setReload] = useState(0);
  // 当前「生效」的文件路径：异步回调（整读/保存在途）用它判断自己是否还有效，
  // 防止用户在请求途中切换文件后，旧文件的回调把内容写进新文件（数据覆盖）
  const activePathRef = useRef<string | null>(path);

  useEffect(() => {
    // 切换文件即放弃未保存的编辑（简化口径：编辑内容不跟随文件保存草稿，误切会丢改动）
    activePathRef.current = path;
    setEditing(false);
    setDirty(false);
    if (!path) {
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

  // 篮子按钮只在「文本内容加载完成」时显示（加载中/错误/二进制态不提供操作入口）；
  // 编辑态下隐藏，避免误触
  const inBasket = path !== null && basket.includes(path);
  const showBasketButton =
    path !== null && !loading && !error && data !== null && !data.Binary && !editing;
  const showEditButton = path !== null && !loading && !error && data !== null && !data.Binary;

  return (
    <div className="flex min-h-0 flex-col text-sm">
      <div className="sticky top-0 z-10 flex items-center border-b border-border bg-card py-2">
        <span
          className="truncate font-mono text-xs text-muted-foreground"
          title={path ?? ''}
        >
          {path ?? '未选择文件'}
          {editing && dirty && <span className="ml-1 text-orange-500">●</span>}
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
        {!editing && showEditButton && (
          <Button size="sm" variant="secondary" className="ml-auto shrink-0" onClick={enterEdit}>
            编辑
          </Button>
        )}
        {showBasketButton && (
          <Button
            size="sm"
            variant={inBasket ? 'secondary' : 'default'}
            className={!editing && showEditButton ? 'ml-1.5 shrink-0' : 'ml-auto shrink-0'}
            onClick={() => void toggleAndSync(path)}
          >
            {inBasket ? '移出篮子' : '加入篮子'}
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
      {!loading && !error && editing && (
        <textarea
          className="mt-2 min-h-[240px] w-full flex-1 resize-y overflow-auto rounded-md border border-border bg-card p-3 font-mono text-[13px] leading-relaxed outline-none focus:border-primary"
          value={text}
          spellCheck={false}
          aria-label="编辑文件内容"
          onChange={(e) => {
            setText(e.target.value);
            setDirty(true);
          }}
          onKeyDown={(e) => {
            if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
              e.preventDefault();
              if (dirty) void handleSave();
            }
          }}
        />
      )}
      {!loading && !error && !editing && data?.Binary && (
        <p className="text-sm text-muted-foreground">{data.Info || '二进制文件，无法预览'}</p>
      )}
      {!loading && !error && !editing && data && !data.Binary && (
        <>
          {data.Info && <p className="mb-2 text-xs text-muted-foreground">{data.Info}</p>}
          {/* 截断行数 500 与 Go 侧 internal/workspace/preview.go 的预览行数上限耦合，改一处需同步 */}
          {data.Truncated && (
            <Badge variant="outline" className="mb-2 w-fit">
              内容已截断：仅显示前 500 行
            </Badge>
          )}
          <pre className="overflow-auto rounded-md border border-border bg-card p-3 font-mono text-[13px] leading-relaxed whitespace-pre">
            {data.Lines.join('\n')}
          </pre>
        </>
      )}
    </div>
  );
}
