// NotificationBubble：右下角 agent 通知气泡。
// 覆盖入队渲染、3 条上限折叠、6s 自动消失（fake timers）、hover 暂停、
// 点击按 termKey 发起页签聚焦并关闭气泡。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TerminalInfo } from '../lib/api';
import NotificationBubble from './NotificationBubble';
import { useAppStore } from '../state/store';

const term: TerminalInfo = {
  ID: 't1',
  Key: 'session:s1',
  Kind: 'session',
  SessionID: 's1',
  Workspace: 'D:\\proj-a',
  Title: '修登录页',
  ToolID: 'claude',
  Status: 'running',
  ExitCode: 0,
  Cols: 80,
  Rows: 24,
};

interface NoticeInput {
  tool: string;
  event: string;
  termKey: string;
  workspace: string;
  summary: string;
}

function push(overrides: Partial<NoticeInput> = {}) {
  return useAppStore.getState().pushAgentNotice({
    tool: 'claude',
    event: 'Stop',
    termKey: 'session:s1',
    workspace: 'D:\\proj-a',
    summary: '登录页修复完成',
    ...overrides,
  });
}

beforeEach(() => {
  useAppStore.setState({
    agentNotices: [],
    focusTermKey: null,
    openTabs: [],
    activeTabId: null,
    terminals: [],
    workspaces: [],
  });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe('NotificationBubble', () => {
  it('队列为空时不渲染', () => {
    const { container } = render(<NotificationBubble />);
    expect(container).toBeEmptyDOMElement();
  });

  it('标题 = 工具展示名 + 事件语义，正文 = summary', () => {
    push();
    render(<NotificationBubble />);
    expect(screen.getByRole('button', { name: /Claude Code 任务完成/ })).toBeInTheDocument();
    expect(screen.getByText('登录页修复完成')).toBeInTheDocument();
  });

  it('error 事件标题为「任务出错」，不落入完成文案', () => {
    push({ event: 'error', summary: '后端进程崩溃' });
    render(<NotificationBubble />);
    expect(screen.getByRole('button', { name: /Claude Code 任务出错/ })).toBeInTheDocument();
    expect(screen.getByText('后端进程崩溃')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /任务完成/ })).not.toBeInTheDocument();
  });

  it('最多同时展示 3 条（最新 3 条），超出折叠为 +N，最新在最上', () => {
    for (let i = 0; i < 5; i++) push({ summary: `第 ${i} 条` });
    render(<NotificationBubble />);
    const buttons = screen.getAllByRole('button');
    expect(buttons).toHaveLength(3);
    expect(screen.getByText('+2')).toBeInTheDocument();
    // 最旧两条被折叠，可见的是最新 3 条
    expect(screen.queryByText('第 0 条')).not.toBeInTheDocument();
    expect(screen.queryByText('第 1 条')).not.toBeInTheDocument();
    // DOM 顺序自上而下：最新的在最上面
    const summaries = buttons.map((b) => b.textContent ?? '');
    expect(summaries[0]).toContain('第 4 条');
    expect(summaries[1]).toContain('第 3 条');
    expect(summaries[2]).toContain('第 2 条');
  });

  it('6s 自动消失', () => {
    vi.useFakeTimers();
    push();
    render(<NotificationBubble />);
    act(() => {
      vi.advanceTimersByTime(6000);
    });
    expect(useAppStore.getState().agentNotices).toHaveLength(0);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('hover 暂停倒计时，移开后按剩余时间消失', () => {
    vi.useFakeTimers();
    push();
    render(<NotificationBubble />);
    const card = screen.getByRole('button');
    act(() => {
      vi.advanceTimersByTime(5000); // 剩 1s
    });
    fireEvent.mouseEnter(card);
    act(() => {
      vi.advanceTimersByTime(6000); // 暂停期间不消失
    });
    expect(screen.getByRole('button')).toBeInTheDocument();
    fireEvent.mouseLeave(card);
    act(() => {
      vi.advanceTimersByTime(999); // 剩余时间未到
    });
    expect(screen.getByRole('button')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(useAppStore.getState().agentNotices).toHaveLength(0);
  });

  it('点击：按 termKey 发起聚焦请求并关闭该气泡', () => {
    useAppStore.setState({ terminals: [term], openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }] });
    push();
    render(<NotificationBubble />);
    fireEvent.click(screen.getByRole('button'));
    expect(useAppStore.getState().focusTermKey).toEqual({ termKey: 'session:s1', seq: 1 });
    expect(useAppStore.getState().agentNotices).toHaveLength(0);
  });

  it('点击：termKey 无法归因到任何页签（外部窗口）时只关闭气泡', () => {
    push({ termKey: 'window:some-title' });
    render(<NotificationBubble />);
    fireEvent.click(screen.getByRole('button'));
    expect(useAppStore.getState().focusTermKey).toBeNull();
    expect(useAppStore.getState().agentNotices).toHaveLength(0);
  });
});
