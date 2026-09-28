// ToolPicker 组件测试：触发器文案（'' → 「自动」）、展开列表与选择回调、Escape 关闭并还焦点、
// 点击面板外部关闭、次要入口「在外部终端打开」、disabled / 空列表的兜底表现。
import '@testing-library/jest-dom/vitest';
import type { ComponentProps } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ToolInfo } from '../lib/api';
import ToolPicker from './ToolPicker';

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

function setup(overrides: Partial<ComponentProps<typeof ToolPicker>> = {}) {
  const onChange = vi.fn();
  const onExternal = vi.fn();
  render(
    <ToolPicker tools={tools} value="" onChange={onChange} onExternal={onExternal} {...overrides} />,
  );
  return { onChange, onExternal, trigger: screen.getByRole('button', { name: /自动|Claude Code|无可用工具/ }) };
}

describe('ToolPicker', () => {
  it('value 为空时触发器显示「自动」', () => {
    setup({ value: '' });
    expect(screen.getByRole('button', { name: '自动' })).toBeInTheDocument();
  });

  it('value 命中工具时触发器显示工具名，value 找不到时回退「自动」', () => {
    setup({ value: 'claude' });
    expect(screen.getByRole('button', { name: 'Claude Code' })).toBeInTheDocument();

    cleanup();
    setup({ value: 'not-exist' });
    expect(screen.getByRole('button', { name: '自动' })).toBeInTheDocument();
  });

  it('默认收起：aria-expanded=false 且不渲染 listbox', () => {
    const { trigger } = setup();
    expect(trigger).toHaveAttribute('aria-haspopup', 'listbox');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('展开后列出全部工具，选中项 aria-selected 且带版本小字', () => {
    setup({ value: 'claude' });

    fireEvent.click(screen.getByRole('button', { name: 'Claude Code' }));

    const options = screen.getAllByRole('option');
    // 首项固定是「自动（该工作区最常用）」，其后是全部可用工具
    expect(options).toHaveLength(3);
    expect(options[0]).toHaveTextContent('自动');
    expect(screen.getByRole('option', { name: /Claude Code/ })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByRole('option', { name: /Codex/ })).toHaveAttribute('aria-selected', 'false');
    expect(screen.getByText('1.0.0')).toBeInTheDocument();
  });

  it('选中具体工具后可以切回「自动」（回调空串）', () => {
    const { onChange } = setup({ value: 'claude' });

    fireEvent.click(screen.getByRole('button', { name: 'Claude Code' }));
    const auto = screen.getByRole('option', { name: /自动/ });
    expect(auto).toHaveAttribute('aria-selected', 'false');
    fireEvent.click(auto);

    expect(onChange).toHaveBeenCalledWith('');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('点某项回调 onChange 并关闭面板', () => {
    const { onChange } = setup({ value: '' });

    fireEvent.click(screen.getByRole('button', { name: '自动' }));
    fireEvent.click(screen.getByRole('option', { name: /Codex/ }));

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith('codex');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: '自动' })).toHaveAttribute('aria-expanded', 'false');
  });

  it('Escape 关闭面板并把焦点还给触发器', () => {
    const { trigger } = setup();

    fireEvent.click(trigger);
    expect(screen.getByRole('listbox')).toBeInTheDocument();

    fireEvent.keyDown(trigger, { key: 'Escape' });

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('点击面板外部（document pointerdown）关闭', () => {
    const { trigger } = setup();

    fireEvent.click(trigger);
    expect(screen.getByRole('listbox')).toBeInTheDocument();

    fireEvent.pointerDown(document.body);

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('面板内 pointerdown 不关闭（点击项自身照常走选择逻辑）', () => {
    const { onChange } = setup();

    fireEvent.click(screen.getByRole('button', { name: '自动' }));
    fireEvent.pointerDown(screen.getByRole('option', { name: /Claude Code/ }));
    expect(screen.getByRole('listbox')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('option', { name: /Claude Code/ }));
    expect(onChange).toHaveBeenCalledWith('claude');
  });

  it('传了 onExternal 时渲染「在外部终端打开」并点击后回调', () => {
    const { onExternal } = setup();

    fireEvent.click(screen.getByRole('button', { name: '自动' }));
    const external = screen.getByRole('button', { name: '在外部终端打开' });
    fireEvent.click(external);

    expect(onExternal).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('未传 onExternal 时不渲染「在外部终端打开」', () => {
    setup({ onExternal: undefined });

    fireEvent.click(screen.getByRole('button', { name: '自动' }));
    expect(screen.queryByRole('button', { name: '在外部终端打开' })).not.toBeInTheDocument();
  });

  it('disabled 时按钮禁用且点击不展开', () => {
    const { trigger } = setup({ disabled: true });

    expect(trigger).toBeDisabled();
    fireEvent.click(trigger);
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('tools 为空时按钮显示「无可用工具」并禁用，不崩', () => {
    setup({ tools: [] });

    const trigger = screen.getByRole('button', { name: '无可用工具' });
    expect(trigger).toBeDisabled();
    fireEvent.click(trigger);
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });
});
