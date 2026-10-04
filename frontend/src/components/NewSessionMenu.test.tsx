// NewSessionMenu 组件测试：触发器文案（新建会话 / 启动中… / 无可用工具）、
// 展开列表与选择回调（onChange 记忆 + onSelect 立即新建）、上次使用项打勾、
// Escape 关闭并还焦点、点击面板外部关闭、disabled 与空列表的兜底表现。
import '@testing-library/jest-dom/vitest';
import type { ComponentProps } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ToolInfo } from '../lib/api';
import NewSessionMenu from './NewSessionMenu';

afterEach(cleanup);

// 父级已过滤：这里只可能是「已安装且有可执行文件」的工具
const tools: ToolInfo[] = [
  {
    ID: 'claude',
    Name: 'Claude Code',
    BinPath: 'C:\\bin\\claude.cmd',
    Version: '1.0.0',
    Installed: true,
    Source: 'path',
  },
  {
    ID: 'codex',
    Name: 'Codex',
    BinPath: 'C:\\bin\\codex.exe',
    Version: '',
    Installed: true,
    Source: 'path',
  },
];

function setup(overrides: Partial<ComponentProps<typeof NewSessionMenu>> = {}) {
  const onChange = vi.fn();
  const onSelect = vi.fn();
  render(<NewSessionMenu tools={tools} value="" onChange={onChange} onSelect={onSelect} {...overrides} />);
  return {
    onChange,
    onSelect,
    // 触发器文案随状态变化，统一按 aria-haspopup 定位
    trigger: screen.getByRole('button', { expanded: false }),
  };
}

describe('NewSessionMenu', () => {
  it('触发器就是「新建会话」按钮（下拉箭头 + aria-haspopup=menu），默认收起', () => {
    const { trigger } = setup();
    expect(trigger).toHaveTextContent('新建会话');
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('上次使用的工具体现在 title 里（空值时不额外标注）', () => {
    setup({ value: 'claude' });
    expect(screen.getByRole('button', { expanded: false })).toHaveAttribute(
      'title',
      '新建会话（上次用 Claude Code）',
    );

    cleanup();
    setup({ value: '' });
    expect(screen.getByRole('button', { expanded: false })).toHaveAttribute('title', '新建会话');
  });

  it('展开后列出「自动」与全部工具，上次使用项 aria-checked=true 并带版本小字', () => {
    setup({ value: 'claude' });

    fireEvent.click(screen.getByRole('button', { expanded: false }));

    const items = screen.getAllByRole('menuitemradio');
    // 首项固定是「自动（该工作区最常用）」，其后是全部可用工具
    expect(items).toHaveLength(3);
    expect(items[0]).toHaveTextContent('自动');
    expect(screen.getByRole('menuitemradio', { name: /Claude Code/ })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(screen.getByRole('menuitemradio', { name: /Codex/ })).toHaveAttribute(
      'aria-checked',
      'false',
    );
    expect(screen.getByText('1.0.0')).toBeInTheDocument();
  });

  it('选中具体工具：onChange（记住选择）+ onSelect（立即新建）并收起面板', () => {
    const { onChange, onSelect } = setup({ value: '' });

    fireEvent.click(screen.getByRole('button', { expanded: false }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Codex/ }));

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith('codex');
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('codex');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('「自动」项也会触发回调（交给 Go 侧选该工作区最常用的工具）', () => {
    const { onChange, onSelect } = setup({ value: 'claude' });

    fireEvent.click(screen.getByRole('button', { expanded: false }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: /自动/ }));

    expect(onChange).toHaveBeenCalledWith('');
    expect(onSelect).toHaveBeenCalledWith('');
  });

  it('Escape 关闭面板并把焦点还给触发器', () => {
    const { trigger } = setup();

    fireEvent.click(trigger);
    expect(screen.getByRole('menu')).toBeInTheDocument();

    fireEvent.keyDown(trigger, { key: 'Escape' });

    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('点击面板外部（document pointerdown）关闭；面板内不关闭', () => {
    const { trigger } = setup();

    fireEvent.click(trigger);
    fireEvent.pointerDown(screen.getByRole('menuitemradio', { name: /Claude Code/ }));
    expect(screen.getByRole('menu')).toBeInTheDocument();

    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('disabled（启动中）时按钮显示「启动中…」并禁用，点击不展开', () => {
    const { trigger } = setup({ disabled: true });

    expect(trigger).toBeDisabled();
    expect(trigger).toHaveTextContent('启动中…');
    fireEvent.click(trigger);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('tools 为空时显示「无可用工具」并禁用，不崩', () => {
    setup({ tools: [] });

    const trigger = screen.getByRole('button', { name: /无可用工具/ });
    expect(trigger).toBeDisabled();
    fireEvent.click(trigger);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('不再提供「在外部终端打开」入口（该路径会弹系统控制台窗口）', () => {
    setup();

    fireEvent.click(screen.getByRole('button', { expanded: false }));
    expect(screen.queryByRole('button', { name: '在外部终端打开' })).not.toBeInTheDocument();
  });

  it('工具同时支持 ACP 时下拉仍只有工具名，点选走 onSelect 不强制 ACP', () => {
    const acpTools: ToolInfo[] = [
      {
        ...tools[0],
        ACP: { Available: true, Source: 'path', BinPath: 'claude-agent-acp' },
      },
    ];
    const { onSelect } = setup({ tools: acpTools });

    fireEvent.click(screen.getByRole('button', { expanded: false }));

    expect(screen.queryByText('Claude Code（ACP）')).not.toBeInTheDocument();
    expect(screen.getAllByRole('menuitemradio')).toHaveLength(2);
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Claude Code/ }));
    expect(onSelect).toHaveBeenCalledWith('claude');
  });
});
