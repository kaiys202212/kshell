// 工作区页签：三栏布局——左会话列表（含新建会话按钮）、
// 中间上下文篮 + 文件预览、右侧「文件 | SSH」页签。
import { useState } from 'react';
import { newSession } from '../lib/api';
import BasketBar from '../components/BasketBar';
import FileTree from '../components/FileTree';
import Preview from '../components/Preview';
import SessionList from '../components/SessionList';
import SshPanel from '../components/SshPanel';
import type { WorkspaceTab } from '../state/store';

type RightPane = 'files' | 'ssh';

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
    <div className="ws-tab">
      <aside className="ws-pane ws-pane-left">
        <div className="session-header">
          <button className="new-session-btn" onClick={() => void handleNewSession()}>
            新建会话
          </button>
          {newErr && <p className="new-session-err">{newErr}</p>}
        </div>
        <SessionList workspacePath={tab.id} />
      </aside>
      <main className="ws-pane ws-pane-main">
        <BasketBar />
        <Preview wsPath={tab.id} path={previewPath} />
      </main>
      <aside className="ws-pane ws-pane-right">
        <div className="ws-right-tabs">
          <button
            className={rightPane === 'files' ? 'active' : ''}
            onClick={() => setRightPane('files')}
          >
            文件
          </button>
          <button
            className={rightPane === 'ssh' ? 'active' : ''}
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
