// 工作区页签：三栏布局——左会话列表（含新建会话按钮）、
// 中间上下文篮 + 文件预览、右侧「文件 | SSH」页签。
import { useState } from 'react';
import { newSession } from '../lib/api';
import { cn } from '../lib/cn';
import BasketBar from '../components/BasketBar';
import FileTree from '../components/FileTree';
import Preview from '../components/Preview';
import SessionList from '../components/SessionList';
import SshPanel from '../components/SshPanel';
import { Button } from '../components/ui/button';
import type { WorkspaceTab } from '../state/store';

type RightPane = 'files' | 'ssh';

// 右栏「文件 | SSH」子页签：胶囊式，激活态用 accent
const paneTabBase =
  'flex h-7 items-center rounded-md px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
const paneTabActive = 'bg-accent text-accent-foreground font-medium hover:bg-accent';

export default function WorkspaceTabView({ tab }: { tab: WorkspaceTab }) {
  const [rightPane, setRightPane] = useState<RightPane>('files');
  const [previewPath, setPreviewPath] = useState<string | null>(null);
  const [newErr, setNewErr] = useState('');

  // 新建会话：Go 侧自动带上上下文篮内容（篮子状态在每次增删时已按返回值同步）
  const handleNewSession = async () => {
    setNewErr('');
    try {
      await newSession(tab.id);
    } catch {
      setNewErr('新建会话失败（工作区未就绪或启动出错）');
    }
  };

  return (
    <div className="flex min-h-0 flex-1">
      <aside className="flex w-[260px] shrink-0 flex-col overflow-y-auto border-r border-border bg-card p-3">
        <div className="mb-2">
          <Button variant="secondary" className="w-full" onClick={() => void handleNewSession()}>
            新建会话
          </Button>
          {newErr && <p className="mt-1.5 text-xs text-destructive">{newErr}</p>}
        </div>
        <SessionList workspacePath={tab.id} />
      </aside>
      <main className="min-w-0 flex-1 overflow-y-auto p-3">
        <BasketBar />
        <Preview wsPath={tab.id} path={previewPath} />
      </main>
      <aside className="flex w-[260px] shrink-0 flex-col overflow-y-auto border-l border-border bg-card p-3">
        <div className="mb-2 flex gap-1">
          <button
            className={cn(paneTabBase, rightPane === 'files' && paneTabActive)}
            onClick={() => setRightPane('files')}
          >
            文件
          </button>
          <button
            className={cn(paneTabBase, rightPane === 'ssh' && paneTabActive)}
            onClick={() => setRightPane('ssh')}
          >
            SSH
          </button>
        </div>
        {rightPane === 'files' ? (
          <FileTree wsPath={tab.id} onOpenFile={setPreviewPath} />
        ) : (
          <SshPanel wsPath={tab.id} />
        )}
      </aside>
    </div>
  );
}
