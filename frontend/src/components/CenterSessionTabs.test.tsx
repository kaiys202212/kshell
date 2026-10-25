// CenterSessionTabs：更多菜单切会话、拖拽排序回调、文件拖入终端/聊天。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CENTER_TAB_MIME } from '../lib/centerTabOrder';
import { DRAG_MIME } from '../lib/dragPath';
import { tt } from '../test/i18n';
import CenterSessionTabs, { type CenterSessionTabItem } from './CenterSessionTabs';

const items: CenterSessionTabItem[] = [
  {
    id: 't1',
    kind: 'terminal',
    label: '修登录页',
    toolID: 'claude',
    activity: 'idle',
    titleAttr: '修登录页',
  },
  {
    id: 'c1',
    kind: 'chat',
    label: '新会话',
    toolID: 'claude',
    activity: 'running',
    titleAttr: '新会话',
  },
  {
    id: 'c2',
    kind: 'chat',
    label: '第三会话',
    toolID: 'codex',
    activity: 'idle',
    titleAttr: '第三会话',
  },
];

function setup(props: Partial<React.ComponentProps<typeof CenterSessionTabs>> = {}) {
  const onSelect = vi.fn();
  const onClose = vi.fn();
  const onReorder = vi.fn();
  const onFileDropTerminal = vi.fn();
  const onFileDropChat = vi.fn();
  render(
    <CenterSessionTabs
      items={items}
      order={['t1', 'c1', 'c2']}
      centerTab="c1"
      previewTabId="session-preview"
      filesTabId="files"
      terminalsTabId="terminals"
      toolTermCount={0}
      onSelect={onSelect}
      onClose={onClose}
      onReorder={onReorder}
      onFileDropTerminal={onFileDropTerminal}
      onFileDropChat={onFileDropChat}
      {...props}
    />,
  );
  return { onSelect, onClose, onReorder, onFileDropTerminal, onFileDropChat };
}

afterEach(cleanup);

describe('CenterSessionTabs', () => {
  it('渲染会话预览 / 会话 / 文件 / 终端钉', () => {
    setup();
    expect(screen.getByRole('tab', { name: tt('ui.workspace.session_preview') })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: '修登录页' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: '新会话' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: tt('ui.workspace.files_tab') })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: tt('ui.workspace.terminals_aria') })).toBeInTheDocument();
  });

  it('拖拽会话页签到另一页签触发 onReorder', () => {
    const { onReorder } = setup();
    const from = screen.getByText('第三会话').closest('div.group')!;
    const to = screen.getByText('修登录页').closest('div.group')!;

    fireEvent.dragStart(from, {
      dataTransfer: {
        setData: vi.fn(),
        effectAllowed: 'move',
        types: [CENTER_TAB_MIME],
      },
    });
    // jsdom 的 dataTransfer 在 dragStart/drop 之间不共享，手动在 drop 注入
    fireEvent.drop(to, {
      dataTransfer: {
        types: [CENTER_TAB_MIME],
        getData: (mime: string) => (mime === CENTER_TAB_MIME ? 'c2' : ''),
      },
    });

    expect(onReorder).toHaveBeenCalledWith(['c2', 't1', 'c1']);
  });

  it('拖文件到聊天页签：切页签并回调 onFileDropChat', () => {
    const { onSelect, onFileDropChat } = setup({ centerTab: 'session-preview' });
    const tab = screen.getByText('新会话').closest('div.group')!;
    fireEvent.drop(tab, {
      dataTransfer: {
        types: [DRAG_MIME],
        getData: (mime: string) => (mime === DRAG_MIME ? 'C:\\f.txt' : ''),
      },
    });
    expect(onSelect).toHaveBeenCalledWith('c1');
    expect(onFileDropChat).toHaveBeenCalledWith('c1', 'C:\\f.txt');
  });

  it('点关闭钮回调 onClose 且不依赖排序', () => {
    const { onClose, onSelect } = setup();
    fireEvent.click(
      screen.getByRole('button', { name: tt('ui.workspace.close_chat').replace('{{label}}', '新会话') }),
    );
    expect(onClose).toHaveBeenCalledWith('c1');
    expect(onSelect).not.toHaveBeenCalled();
  });
});

describe('CenterSessionTabs variant（分割模式）', () => {
  it('variant=agent 隐藏右钉「文件」「终端」，会话页签照常', () => {
    setup({ variant: 'agent' });
    expect(screen.getByRole('tab', { name: tt('ui.workspace.session_preview') })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: '新会话' })).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: tt('ui.workspace.files_tab') })).not.toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: tt('ui.workspace.terminals_aria') })).not.toBeInTheDocument();
  });

  it('variant=tool 只渲染右钉「文件」「终端」，无预览钉与会话页签', () => {
    setup({ variant: 'tool' });
    expect(screen.getByRole('tab', { name: tt('ui.workspace.files_tab') })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: tt('ui.workspace.terminals_aria') })).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: tt('ui.workspace.session_preview') })).not.toBeInTheDocument();
    expect(screen.queryByText('新会话')).not.toBeInTheDocument();
  });

  it('multi-tab 模式下切换按钮提示切到分割并回调 onLayoutToggle', () => {
    const onLayoutToggle = vi.fn();
    setup({ variant: 'agent', layoutMode: 'tabs', onLayoutToggle });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.workspace.layout_toggle_split') }));
    expect(onLayoutToggle).toHaveBeenCalledTimes(1);
  });

  it('split 模式下切换按钮提示切回多页签', () => {
    const onLayoutToggle = vi.fn();
    setup({ variant: 'agent', layoutMode: 'split', onLayoutToggle });
    fireEvent.click(screen.getByRole('button', { name: tt('ui.workspace.layout_toggle_tabs') }));
    expect(onLayoutToggle).toHaveBeenCalledTimes(1);
  });

  it('full 变体也可带切换按钮（多页签模式的快捷入口）', () => {
    const onLayoutToggle = vi.fn();
    setup({ layoutMode: 'tabs', onLayoutToggle });
    expect(
      screen.getByRole('button', { name: tt('ui.workspace.layout_toggle_split') }),
    ).toBeInTheDocument();
  });

  it('未传 onLayoutToggle 时不渲染切换按钮（兼容既有调用方）', () => {
    setup();
    expect(screen.queryByRole('button', { name: tt('ui.workspace.layout_toggle_split') })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: tt('ui.workspace.layout_toggle_tabs') })).not.toBeInTheDocument();
  });
});
