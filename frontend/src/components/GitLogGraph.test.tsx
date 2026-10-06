import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { GitLogGraph } from './GitLogGraph';

afterEach(cleanup);

describe('GitLogGraph', () => {
  it('渲染提交说明与短 hash', () => {
    render(
      <GitLogGraph
        commits={[
          {
            hash: 'abcdef123456',
            parents: [],
            subject: 'hello graph',
            author: 'ann',
            date: '2026-01-01T00:00:00Z',
            decorations: ['main'],
          },
        ]}
        selected=""
        onSelect={() => {}}
      />,
    );
    expect(screen.getByText('hello graph')).toBeInTheDocument();
    expect(screen.getByText('abcdef1')).toBeInTheDocument();
    expect(screen.getByText('main')).toBeInTheDocument();
  });
});
