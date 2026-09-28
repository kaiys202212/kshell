// 工作区页签：三栏布局——左会话列表、中间预览区（占位）、右侧「文件 | SSH」页签（占位）。
import { useState } from 'react';
import SessionList from '../components/SessionList';
import type { WorkspaceTab } from '../state/store';

type RightPane = 'files' | 'ssh';

export default function WorkspaceTabView({ tab }: { tab: WorkspaceTab }) {
  const [rightPane, setRightPane] = useState<RightPane>('files');

  return (
    <div className="ws-tab">
      <aside className="ws-pane ws-pane-left">
        <SessionList workspacePath={tab.id} />
      </aside>
      <main className="ws-pane ws-pane-main">
        <p className="ws-placeholder">预览区（待实现）</p>
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
        <p className="ws-placeholder">
          {rightPane === 'files' ? '文件树（待实现）' : 'SSH 连接（待实现）'}
        </p>
      </aside>
    </div>
  );
}
