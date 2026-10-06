import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { GitLogGraph } from './GitLogGraph';

afterEach(cleanup);

const commit = {
  hash: 'abcdef123456',
  parents: [] as string[],
  subject: 'hello graph',
  author: 'ann',
  date: '2026-01-01T00:00:00Z',
  decorations: ['main'],
};

describe('GitLogGraph', () => {
  it('行内展示提交说明，悬停显示作者时间与统计', async () => {
    const loadStat = vi.fn().mockResolvedValue({ Files: 19, Insertions: 1867, Deletions: 202 });
    render(<GitLogGraph commits={[commit]} selected="" onSelect={() => {}} loadStat={loadStat} />);
    const row = screen.getByRole('button', { name: /hello graph/ });
    expect(row).toBeInTheDocument();
    expect(screen.queryByText('abcdef1')).not.toBeInTheDocument();
    fireEvent.mouseEnter(row);
    expect(await screen.findByRole('tooltip')).toHaveTextContent('hello graph');
    expect(screen.getByRole('tooltip')).toHaveTextContent('ann');
    await waitFor(() => {
      expect(screen.getByRole('tooltip')).toHaveTextContent('已更改 19 个文件');
    });
  });
});
