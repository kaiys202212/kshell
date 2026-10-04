import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import PreviewToolPane, { PREVIEW_SUB, toolTermLabel } from './PreviewToolPane';
import type { TerminalInfo } from '../lib/api';

vi.mock('./Preview', () => ({
  default: ({ path }: { path: string | null }) => <div data-testid="preview">{path ?? 'empty'}</div>,
}));
vi.mock('./SessionTranscript', () => ({
  default: ({ title }: { title: string }) => <div data-testid="session-transcript">{title}</div>,
}));
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
    expect(toolTermLabel(list, list[0])).toBe('终端');
    expect(toolTermLabel(list, list[1])).toBe('终端 2');
    expect(toolTermLabel(list, list[2])).toBe('生产机');
  });
});

describe('PreviewToolPane', () => {
  it('渲染预览子页签与 +，点 + 触发 onNewShell', () => {
    const onNewShell = vi.fn();
    render(
      <PreviewToolPane
        wsPath="D:\\proj"
        previewPath={null}
        terms={[]}
        active
        subTab={PREVIEW_SUB}
        onSubTab={() => {}}
        onCloseTerminal={() => {}}
        onNewShell={onNewShell}
      />,
    );
    expect(screen.getByRole('tab', { name: '文件预览' })).toHaveAttribute('aria-selected', 'true');
    fireEvent.click(screen.getByRole('button', { name: '新建终端' }));
    expect(onNewShell).toHaveBeenCalled();
  });

  it('切换到终端子页签并显示 TerminalView', () => {
    const onSubTab = vi.fn();
    const t = term({ ID: 'sh1' });
    render(
      <PreviewToolPane
        wsPath="D:\\proj"
        previewPath="D:\\proj\\a.ts"
        terms={[t]}
        active
        subTab="sh1"
        onSubTab={onSubTab}
        onCloseTerminal={() => {}}
        onNewShell={() => {}}
      />,
    );
    expect(screen.getByTestId('terminal-sh1')).toHaveAttribute('data-active', 'true');
    fireEvent.click(screen.getByRole('tab', { name: '文件预览' }));
    expect(onSubTab).toHaveBeenCalledWith(PREVIEW_SUB);
  });

  it('关闭按钮调用 onCloseTerminal', () => {
    const onClose = vi.fn();
    const t = term({ ID: 'sh1' });
    render(
      <PreviewToolPane
        wsPath="D:\\proj"
        previewPath={null}
        terms={[t]}
        active
        subTab="sh1"
        onSubTab={() => {}}
        onCloseTerminal={onClose}
        onNewShell={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: '关闭 终端' }));
    expect(onClose).toHaveBeenCalledWith('sh1');
  });

  it('有 sessionPreview 时出现可关闭的会话预览子页签，不挡住文件预览子页签', () => {
    const onClosePreview = vi.fn();
    const onActivate = vi.fn();
    render(
      <PreviewToolPane
        wsPath="D:\\proj"
        previewPath="D:\\proj\\a.ts"
        terms={[]}
        active
        subTab="session-preview"
        onSubTab={() => {}}
        onCloseTerminal={() => {}}
        onNewShell={() => {}}
        sessionPreview={{ sessionID: 's1', title: '修登录' }}
        onCloseSessionPreview={onClosePreview}
        onActivateSessionPreview={onActivate}
      />,
    );
    expect(screen.getByRole('tab', { name: '文件预览' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: '会话预览' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('session-transcript')).toHaveTextContent('修登录');
    fireEvent.click(screen.getByRole('button', { name: '关闭会话预览' }));
    expect(onClosePreview).toHaveBeenCalled();
  });
});
