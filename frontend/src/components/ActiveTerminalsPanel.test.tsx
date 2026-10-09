// ActiveTerminalsPanel：运行中的 shell/ssh 列表、查看全部、点击定位。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ActiveTerminalsPanel from './ActiveTerminalsPanel';
import type { TerminalInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import { tt } from '../test/i18n';

function term(over: Partial<TerminalInfo>): TerminalInfo {
  return {
    ID: 't1',
    Key: 'shell:1',
    Kind: 'shell',
    SessionID: '',
    Workspace: 'D:\\proj-a',
    Title: '本地 shell',
    ToolID: '',
    Status: 'running',
    ExitCode: 0,
    Cols: 80,
    Rows: 24,
    ...over,
  };
}

afterEach(cleanup);

beforeEach(() => {
  useAppStore.setState({
    terminals: [
      term({ ID: 's1', Key: 'shell:1', Kind: 'shell', Title: 'shell-a', Workspace: 'D:\\proj-a' }),
      term({
        ID: 'r1',
        Key: 'ssh:1',
        Kind: 'ssh',
        Title: 'ssh-a',
        Workspace: 'D:\\proj-a',
        ConnID: 'c1',
      }),
      term({
        ID: 's2',
        Key: 'shell:2',
        Kind: 'shell',
        Title: 'shell-b',
        Workspace: 'D:\\proj-b',
      }),
      term({
        ID: 'ex',
        Key: 'shell:x',
        Kind: 'shell',
        Title: '已退出',
        Workspace: 'D:\\proj-a',
        Status: 'exited',
      }),
      term({
        ID: 'ag',
        Key: 'session:1',
        Kind: 'session',
        Title: 'agent',
        Workspace: 'D:\\proj-a',
        ToolID: 'claude',
      }),
    ],
    openTabs: [
      { id: 'D:\\proj-a', name: 'proj-a' },
      { id: 'D:\\proj-b', name: 'proj-b' },
    ],
    activeTabId: 'D:\\proj-a',
    focusTermKey: null,
  });
});

describe('ActiveTerminalsPanel', () => {
  const wsA = 'D:\\proj-a';
  const wsB = 'D:\\proj-b';

  it('默认只列当前工作区 running 的 shell/ssh，不含 agent 与已退出', () => {
    render(<ActiveTerminalsPanel wsPath={wsA} />);
    const list = screen.getByRole('list', { name: tt('ui.workspace.active_terminals_list_aria') });
    expect(list).toHaveTextContent('shell-a');
    expect(list).toHaveTextContent('ssh-a');
    expect(list).not.toHaveTextContent('shell-b');
    expect(list).not.toHaveTextContent('已退出');
    expect(list).not.toHaveTextContent('agent');
  });

  it('查看全部跨项目列出所有 running shell/ssh', () => {
    render(<ActiveTerminalsPanel wsPath={wsA} />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.workspace.active_terminals_view_all') }));
    expect(screen.getByText('shell-b')).toBeInTheDocument();
    expect(screen.getByText('proj-b')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.workspace.active_terminals_view_current') }));
    expect(screen.queryByText('shell-b')).not.toBeInTheDocument();
  });

  it('无匹配时显示空态', () => {
    useAppStore.setState({ terminals: [] });
    render(<ActiveTerminalsPanel wsPath={wsA} />);
    expect(screen.getByText(tt('ui.workspace.active_terminals_empty'))).toBeInTheDocument();
  });

  it('点击行切到对应工作区并 requestFocusTerm', () => {
    const spy = vi.spyOn(useAppStore.getState(), 'requestFocusTerm');
    const setActive = vi.spyOn(useAppStore.getState(), 'setActiveTab');
    render(<ActiveTerminalsPanel wsPath={wsA} />);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.workspace.active_terminals_view_all') }));
    fireEvent.click(screen.getByRole('button', { name: /shell-b/ }));
    expect(setActive).toHaveBeenCalledWith(wsB);
    expect(spy).toHaveBeenCalledWith('shell:2');
  });
});
