// QuickSwitcher 测试：两组条目渲染与过滤（大小写不敏感）、↑↓/Enter 导航激活、
// 数据全空的引导空态。store 直接注入，不涉及 api 层。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import QuickSwitcher from './QuickSwitcher';
import type { Workspace } from '../lib/api';
import { useAppStore } from '../state/store';

const workspaces: Workspace[] = [
  {
    Path: 'D:\\proj-a',
    Name: 'proj-a',
    LastUsed: '',
    SessionCount: 0,
    ToolCounts: {},
    Source: 'sessions',
  },
  {
    Path: 'D:\\lib-x',
    Name: 'lib-x',
    LastUsed: '',
    SessionCount: 0,
    ToolCounts: {},
    Source: 'git',
  },
];

afterEach(cleanup);

beforeEach(() => {
  useAppStore.setState({
    openTabs: [{ id: 'D:\\proj-a', name: 'proj-a' }],
    activeTabId: 'D:\\proj-a',
    workspaces,
  });
});

describe('QuickSwitcher', () => {
  it('列出「已打开的页签」与「工作区」两组条目，输入按名称/路径过滤（大小写不敏感）', () => {
    render(<QuickSwitcher open onOpenChange={() => {}} />);

    expect(screen.getByText('已打开的页签')).toBeInTheDocument();
    // 组标题是 <p>，列表项右侧的「工作区」标注是 <span>，用 selector 区分
    expect(screen.getByText('工作区', { selector: 'p' })).toBeInTheDocument();
    expect(screen.getAllByText('proj-a').length).toBe(2); // 页签组 + 工作区组各一条
    expect(screen.getByText('lib-x')).toBeInTheDocument();

    const input = screen.getByLabelText('搜索页签或工作区');
    fireEvent.change(input, { target: { value: 'LIB' } });
    expect(screen.queryByText('proj-a')).not.toBeInTheDocument();
    expect(screen.getByText('lib-x')).toBeInTheDocument();
  });

  it('↑↓ 移动高亮，Enter 确认：工作区条目 openTab，页签条目仅 setActiveTab', async () => {
    const onOpenChange = vi.fn();
    render(<QuickSwitcher open onOpenChange={onOpenChange} />);
    const input = screen.getByLabelText('搜索页签或工作区');

    // 初始高亮第 1 条（已开页签 proj-a）：Enter 只激活，不新增页签
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => {
      expect(useAppStore.getState().activeTabId).toBe('D:\\proj-a');
    });
    expect(useAppStore.getState().openTabs).toHaveLength(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);

    // ↓ 两次到第 3 条（工作区 lib-x）：Enter 打开为新页签
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => {
      expect(useAppStore.getState().openTabs.some((t) => t.id === 'D:\\lib-x')).toBe(true);
    });
    expect(useAppStore.getState().activeTabId).toBe('D:\\lib-x');
  });

  it('数据全空时给引导空态，提示先回首页扫描', () => {
    useAppStore.setState({ openTabs: [], workspaces: [] });
    render(<QuickSwitcher open onOpenChange={() => {}} />);

    expect(screen.getByText('暂无工作区，请先在首页完成扫描')).toBeInTheDocument();
  });

  it('点击列表项直接确认并关闭面板', async () => {
    const onOpenChange = vi.fn();
    render(<QuickSwitcher open onOpenChange={onOpenChange} />);

    fireEvent.click(screen.getByRole('button', { name: /lib-x/ }));
    await waitFor(() => {
      expect(useAppStore.getState().activeTabId).toBe('D:\\lib-x');
    });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
