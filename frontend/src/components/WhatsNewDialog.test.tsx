// WhatsNewDialog 测试：mock api 层，覆盖打开拉取升级日志渲染、
// 确认写已读标记并触发关闭、拉取失败重试。与 HelpSection.test 同一套 mock 模式。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { tt } from '../test/i18n';
import WhatsNewDialog from './WhatsNewDialog';

const mocks = vi.hoisted(() => ({
  getChangelogDoc: vi.fn(),
  markVersionSeen: vi.fn(),
}));

vi.mock('../lib/api', () => mocks);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getChangelogDoc.mockResolvedValue('# Changelog v0.1.17');
  mocks.markVersionSeen.mockResolvedValue(undefined);
});

afterEach(cleanup);

function renderDialog(open = true) {
  const onOpenChange = vi.fn();
  render(
    <WhatsNewDialog open={open} onOpenChange={onOpenChange} currentVersion="v0.1.17" />,
  );
  return onOpenChange;
}

describe('WhatsNewDialog', () => {
  it('打开时按当前语言拉取升级日志并渲染', async () => {
    renderDialog();
    expect(mocks.getChangelogDoc).toHaveBeenCalledWith('en');
    expect(await screen.findByText('Changelog v0.1.17')).toBeInTheDocument();
  });

  it('关闭状态不拉取文档', () => {
    renderDialog(false);
    expect(mocks.getChangelogDoc).not.toHaveBeenCalled();
  });

  it('点「我知道了」写入已读标记并触发关闭', async () => {
    const onOpenChange = renderDialog();
    await screen.findByText('Changelog v0.1.17');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.whatsNew.confirm') }));
    await waitFor(() => expect(mocks.markVersionSeen).toHaveBeenCalledWith('v0.1.17'));
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it('markVersionSeen 失败也不阻塞关闭', async () => {
    mocks.markVersionSeen.mockRejectedValue(new Error('boom'));
    const onOpenChange = renderDialog();
    await screen.findByText('Changelog v0.1.17');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.whatsNew.confirm') }));
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it('拉取失败显示失败文案与重试，点重试重新拉取', async () => {
    mocks.getChangelogDoc.mockRejectedValueOnce(new Error('boom')).mockResolvedValue('# OK');
    renderDialog();
    expect(await screen.findByText(tt('ui.settings.help.loadFailed'))).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.settings.help.retry') }));
    expect(mocks.getChangelogDoc).toHaveBeenCalledTimes(2);
    expect(await screen.findByText('OK')).toBeInTheDocument();
  });
});
