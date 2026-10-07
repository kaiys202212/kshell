import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18next from 'i18next';
import PreviewToolPane, { toolTermLabel } from './PreviewToolPane';
import type { TerminalInfo } from '../lib/api';
import { tt } from '../test/i18n';

vi.mock('./TerminalView', () => ({
  default: ({ term, active }: { term: TerminalInfo; active: boolean }) => (
    <div data-testid={`terminal-${term.ID}`} data-active={String(active)} />
  ),
}));

afterEach(cleanup);

function term(over: Partial<TerminalInfo>): TerminalInfo {
  return {
    ID: 't1',
    Kind: 'shell',
    SessionID: '',
    Workspace: 'D:\\proj',
    Title: '终端',
    ToolID: '',
    Status: 'running',
    ExitCode: 0,
    Cols: 80,
    Rows: 24,
    ...over,
  };
}

describe('toolTermLabel', () => {
  it('本地 shell 按顺序编号，SSH 用标题', () => {
    const list = [
      term({ ID: 'a', Kind: 'shell' }),
      term({ ID: 'b', Kind: 'shell' }),
      term({ ID: 'c', Kind: 'ssh', Title: '生产机' }),
    ];
    expect(toolTermLabel(list, list[0], i18next.t)).toBe(tt('ui.terminal.label'));
    expect(toolTermLabel(list, list[1], i18next.t)).toBe(
      tt('ui.terminal.label_n').replace('{{n}}', '2'),
    );
    expect(toolTermLabel(list, list[2], i18next.t)).toBe('生产机');
  });
});

describe('PreviewToolPane', () => {
  it('渲染 +，点 + 触发 onNewShell', () => {
    const onNewShell = vi.fn();
    render(
      <PreviewToolPane
        terms={[]}
        active
        subTab=""
        onSubTab={() => {}}
        onCloseTerminal={() => {}}
        onNewShell={onNewShell}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: tt('ui.terminal.new') }));
    expect(onNewShell).toHaveBeenCalled();
  });

  it('切换终端子页签并显示 TerminalView', () => {
    const onSubTab = vi.fn();
    const t = term({ ID: 'sh1' });
    render(
      <PreviewToolPane
        terms={[t]}
        active
        subTab="sh1"
        onSubTab={onSubTab}
        onCloseTerminal={() => {}}
        onNewShell={() => {}}
      />,
    );
    expect(screen.getByTestId('terminal-sh1')).toHaveAttribute('data-active', 'true');
    fireEvent.click(screen.getByRole('tab', { name: tt('ui.terminal.label') }));
    expect(onSubTab).toHaveBeenCalledWith('sh1');
  });

  it('关闭按钮调用 onCloseTerminal', () => {
    const onClose = vi.fn();
    const t = term({ ID: 'sh1' });
    render(
      <PreviewToolPane
        terms={[t]}
        active
        subTab="sh1"
        onSubTab={() => {}}
        onCloseTerminal={onClose}
        onNewShell={() => {}}
      />,
    );
    fireEvent.click(
      screen.getByRole('button', {
        name: tt('ui.terminal.close_tab').replace('{{label}}', tt('ui.terminal.label')),
      }),
    );
    expect(onClose).toHaveBeenCalledWith('sh1');
  });
});
