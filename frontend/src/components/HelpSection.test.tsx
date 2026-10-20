// HelpSection 测试：mock api 层，覆盖页签切换内容来源、按语言拉取、失败重试。
// i18n 由 vitest setup 统一初始化为 en；语言用例通过 i18next.changeLanguage 触发。
import '@testing-library/jest-dom/vitest';
import i18next from 'i18next';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { tt } from '../test/i18n';
import { HelpSection } from './HelpSection';

const mocks = vi.hoisted(() => ({
  getHelpDoc: vi.fn(),
  getChangelogDoc: vi.fn(),
}));

vi.mock('../lib/api', () => mocks);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getHelpDoc.mockResolvedValue('# Guide EN');
  mocks.getChangelogDoc.mockResolvedValue('# Changelog EN');
});

afterEach(async () => {
  cleanup();
  // 语言是 i18next 全局单例状态，用例结束必须还原，避免污染其他测试
  await i18next.changeLanguage('en');
});

describe('HelpSection', () => {
  it('挂载时按当前语言拉取功能说明并渲染', async () => {
    render(<HelpSection />);
    expect(mocks.getHelpDoc).toHaveBeenCalledWith(i18next.language);
    expect(await screen.findByText('Guide EN')).toBeInTheDocument();
  });

  it('切换到升级日志页签后改拉 changelog 文档', async () => {
    render(<HelpSection />);
    await screen.findByText('Guide EN');
    fireEvent.click(
      screen.getByRole('button', { name: tt('ui.settings.help.tab.changelog') }),
    );
    expect(mocks.getChangelogDoc).toHaveBeenCalledWith(i18next.language);
    expect(await screen.findByText('Changelog EN')).toBeInTheDocument();
  });

  it('语言切换后重新按新语言拉取文档并替换内容', async () => {
    // mock 按 lang 返回不同内容，断言渲染内容真正替换而非只看调用参数
    mocks.getHelpDoc.mockImplementation((lang: string) =>
      Promise.resolve(lang === 'zh-CN' ? '# 指南 ZH' : '# Guide EN'),
    );
    render(<HelpSection />);
    await screen.findByText('Guide EN');
    await act(async () => {
      await i18next.changeLanguage('zh-CN');
    });
    await waitFor(() => {
      expect(mocks.getHelpDoc).toHaveBeenLastCalledWith('zh-CN');
    });
    expect(await screen.findByText('指南 ZH')).toBeInTheDocument();
    expect(screen.queryByText('Guide EN')).not.toBeInTheDocument();
  });

  it('拉取失败显示失败文案与重试按钮，点重试后重新拉取', async () => {
    mocks.getHelpDoc.mockRejectedValueOnce(new Error('boom')).mockResolvedValue('# Guide OK');
    render(<HelpSection />);
    expect(await screen.findByText(tt('ui.settings.help.loadFailed'))).toBeInTheDocument();
    // 失败态页签栏仍保留
    expect(screen.getByRole('button', { name: tt('ui.settings.help.tab.guide') })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.settings.help.tab.changelog') })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.help.retry') }));
    expect(mocks.getHelpDoc).toHaveBeenCalledTimes(2);
    expect(await screen.findByText('Guide OK')).toBeInTheDocument();
  });
});
