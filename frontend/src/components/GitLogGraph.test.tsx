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
  it('行内展示提交说明，悬停与选中样式区分且详情在左侧', async () => {
    const loadStat = vi.fn().mockResolvedValue({ Files: 19, Insertions: 1867, Deletions: 202 });
    const onSelect = vi.fn();
    render(
      <GitLogGraph commits={[commit]} selected="abcdef123456" onSelect={onSelect} loadStat={loadStat} />,
    );
    const row = screen.getByRole('button', { name: /hello graph/ });
    expect(row.className).toMatch(/border-l-primary/);
    fireEvent.mouseEnter(row);
    expect(row.className).toMatch(/bg-primary\/25/);
    const tip = await screen.findByRole('tooltip');
    expect(tip).toHaveTextContent('hello graph');
    expect(tip).toHaveTextContent('ann');
    expect(tip.className).toMatch(/-translate-x-full/);
    await waitFor(() => {
      expect(tip).toHaveTextContent('已更改 19 个文件');
    });
  });

  it('未选中行悬停用 muted 背景', () => {
    render(<GitLogGraph commits={[commit]} selected="" onSelect={() => {}} />);
    const row = screen.getByRole('button', { name: /hello graph/ });
    fireEvent.mouseEnter(row);
    expect(row.className).toMatch(/bg-muted/);
    expect(row.className).not.toMatch(/border-l-primary/);
  });
});
