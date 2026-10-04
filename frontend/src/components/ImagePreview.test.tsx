// ImagePreview：readFileBytes → data URL img。
import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ImagePreview from './ImagePreview';

const mocks = vi.hoisted(() => ({
  readFileBytes: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
});

describe('ImagePreview', () => {
  it('img src 以 data:image/png;base64, 开头', async () => {
    mocks.readFileBytes.mockResolvedValue({
      Base64: 'iVBORw0KGgo=',
      Mime: 'image/png',
      Size: 12,
    });
    render(<ImagePreview wsPath={'D:\\proj'} path={'D:\\proj\\pic.png'} />);

    const img = await screen.findByRole('img');
    expect(img).toHaveAttribute('src', expect.stringMatching(/^data:image\/png;base64,/));
    expect(img).toHaveAttribute('alt', 'pic.png');
    expect(mocks.readFileBytes).toHaveBeenCalledWith('D:\\proj', 'D:\\proj\\pic.png');
  });
});
