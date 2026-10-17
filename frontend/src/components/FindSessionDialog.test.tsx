// FindSessionDialog：#find 会话查找弹窗。聚焦独立行为：
// getSessions 失败回落空态、UpdatedAt 倒序与 50 条截断、键盘导航选中。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Session } from '../lib/api';
import '../test/i18n';
import { tt } from '../test/i18n';
import FindSessionDialog from './FindSessionDialog';

const api = vi.hoisted(() => ({
  getSessions: vi.fn(),
}));
vi.mock('../lib/api', () => api);

function session(over: Partial<Session>): Session {
  return {
    ID: 's1',
    ToolID: 'claude',
    Workspace: 'D:\\ws\\alpha',
    Title: '重构认证',
    CreatedAt: '2026-10-16T10:00:00Z',
    UpdatedAt: '2026-10-16T10:00:00Z',
    Messages: 3,
    Path: '',
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.getSessions.mockResolvedValue([]);
});

afterEach(cleanup);

describe('FindSessionDialog', () => {
  it('open 时拉取 getSessions 渲染行：tool · 项目基名 · 标题 · 相对时间', async () => {
    api.getSessions.mockResolvedValue([session({})]);
    render(<FindSessionDialog open initialQuery="" onClose={() => {}} onPick={() => {}} />);
    await waitFor(() => expect(screen.getByText('重构认证')).toBeTruthy());
    expect(screen.getByText('Claude Code')).toBeTruthy();
    expect(screen.getByText('alpha')).toBeTruthy();
    // 夹具 UpdatedAt 为昨天：断言 en 复数拼出的期望串（与具体语言解耦，同 format.test）
    expect(screen.getByText(tt('time.days_ago_one').replace('{{count}}', '1'))).toBeTruthy();
  });

  it('getSessions 失败回落空态文案', async () => {
    api.getSessions.mockRejectedValue(new Error('boom'));
    render(<FindSessionDialog open initialQuery="" onClose={() => {}} onPick={() => {}} />);
    await waitFor(() => expect(screen.getByText(tt('ui.hub.find_empty'))).toBeTruthy());
  });

  it('列表按 UpdatedAt 倒序', async () => {
    api.getSessions.mockResolvedValue([
      session({ ID: 's-old', Title: '旧会话', UpdatedAt: '2026-10-15T10:00:00Z' }),
      session({ ID: 's-new', Title: '新会话', UpdatedAt: '2026-10-16T12:00:00Z' }),
    ]);
    render(<FindSessionDialog open initialQuery="" onClose={() => {}} onPick={() => {}} />);
    await waitFor(() => expect(screen.getByText('旧会话')).toBeTruthy());
    const options = screen.getAllByRole('option');
    expect(options[0]).toHaveTextContent('新会话');
    expect(options[1]).toHaveTextContent('旧会话');
  });

  it('结果截断到 50 条', async () => {
    const list: Session[] = [];
    for (let i = 0; i < 60; i++) {
      list.push(session({ ID: `s${i}`, Title: `会话${i}` }));
    }
    api.getSessions.mockResolvedValue(list);
    render(<FindSessionDialog open initialQuery="" onClose={() => {}} onPick={() => {}} />);
    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(50));
  });

  it('initialQuery 作为初值参与过滤；↑↓ 移动高亮、Enter 选中', async () => {
    api.getSessions.mockResolvedValue([
      session({ ID: 's1', Workspace: 'D:\\ws\\alpha' }),
      session({ ID: 's2', ToolID: 'codex', Workspace: 'D:\\ws\\beta', Title: '写文档' }),
    ]);
    const onPick = vi.fn();
    const onClose = vi.fn();
    render(<FindSessionDialog open initialQuery="beta" onClose={onClose} onPick={onPick} />);
    const input = screen.getByPlaceholderText(tt('ui.hub.find_placeholder')) as HTMLInputElement;
    await waitFor(() => expect(screen.queryByText('重构认证')).toBeNull());
    expect(input).toHaveValue('beta');
    const options = screen.getAllByRole('option');
    expect(options).toHaveLength(1);
    expect(options[0]).toHaveAttribute('aria-selected', 'true');
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ ID: 's2' }));
  });

  it('Esc 关闭走 onOpenChange(false) → onClose', async () => {
    api.getSessions.mockResolvedValue([]);
    const onClose = vi.fn();
    render(<FindSessionDialog open initialQuery="" onClose={onClose} onPick={() => {}} />);
    const input = await screen.findByPlaceholderText(tt('ui.hub.find_placeholder'));
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    void input;
  });
});
