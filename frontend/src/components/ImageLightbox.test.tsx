// ImageLightbox：Dialog 外壳 + readChatAttachmentData → data URL img。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ImageLightbox from './ImageLightbox';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  readChatAttachmentData: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
});

describe('ImageLightbox', () => {
  it('加载成功渲染 img，src 以 data:image/png;base64, 开头', async () => {
    mocks.readChatAttachmentData.mockResolvedValue({
      Base64: 'iVBORw0KGgo=',
      Mime: 'image/png',
      Size: 12,
      AbsPath: 'D:\\c\\1.png',
    });
    render(<ImageLightbox name="shot.png" path={'D:\\c\\1.png'} onClose={vi.fn()} />);

    const img = await screen.findByRole('img');
    expect(img).toHaveAttribute('src', expect.stringMatching(/^data:image\/png;base64,/));
    expect(img).toHaveAttribute('alt', 'shot.png');
    expect(mocks.readChatAttachmentData).toHaveBeenCalledWith('D:\\c\\1.png');
  });

  it('加载失败渲染预览失败提示文案', async () => {
    mocks.readChatAttachmentData.mockRejectedValue(new Error('boom'));
    render(<ImageLightbox path="D:\\c\\1.png" onClose={vi.fn()} />);

    expect(await screen.findByText(tt('ui.chat.attachment_preview_failed'))).toBeInTheDocument();
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
  });

  it('读取返回 null（绑定不可用）同样渲染失败提示', async () => {
    mocks.readChatAttachmentData.mockResolvedValue(null);
    render(<ImageLightbox path="D:\\c\\1.png" onClose={vi.fn()} />);

    expect(await screen.findByText(tt('ui.chat.attachment_preview_failed'))).toBeInTheDocument();
  });

  it('关闭按钮触发 onClose', async () => {
    mocks.readChatAttachmentData.mockResolvedValue({
      Base64: 'iVBORw0KGgo=',
      Mime: 'image/png',
      Size: 12,
      AbsPath: 'D:\\c\\1.png',
    });
    const onClose = vi.fn();
    render(<ImageLightbox name="shot.png" path={'D:\\c\\1.png'} onClose={onClose} />);

    await screen.findByRole('img');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.chat.attachment_close') }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
