import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SessionTranscript from './SessionTranscript';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  getSessionPreview: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);
vi.mock('./MarkdownPreview', () => ({
  default: ({ markdown }: { markdown: string }) => <div data-testid="md">{markdown}</div>,
}));

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getSessionPreview.mockResolvedValue({ Markdown: '## 用户\n\nhi', Truncated: false });
});

describe('SessionTranscript', () => {
  it('加载 Markdown 并提供激活按钮', async () => {
    const onActivate = vi.fn();
    render(<SessionTranscript sessionID="s1" title="修登录" onActivate={onActivate} />);
    expect(await screen.findByTestId('md')).toHaveTextContent('## 用户');
    fireEvent.click(screen.getByRole('button', { name: tt('ui.transcript.activate') }));
    expect(onActivate).toHaveBeenCalled();
  });

  it('失败时仍可激活', async () => {
    mocks.getSessionPreview.mockRejectedValueOnce(new Error('读失败'));
    const onActivate = vi.fn();
    render(<SessionTranscript sessionID="s1" title="修登录" onActivate={onActivate} />);
    expect(await screen.findByText(tt('ui.transcript.load_failed'))).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.transcript.activate') }));
    expect(onActivate).toHaveBeenCalled();
  });

  it('截断提示', async () => {
    mocks.getSessionPreview.mockResolvedValueOnce({ Markdown: 'x', Truncated: true });
    render(<SessionTranscript sessionID="s1" title="t" onActivate={() => {}} />);
    await waitFor(() => expect(screen.getByText(tt('ui.transcript.truncated'))).toBeInTheDocument());
  });
});
