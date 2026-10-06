import { describe, expect, it } from 'vitest';
import { layoutGitGraph, type GraphCommit } from './gitGraph';

const c = (hash: string, parents: string[], subject = hash): GraphCommit => ({
  hash,
  parents,
  subject,
  author: 't',
  date: '2026-01-01T00:00:00Z',
  decorations: [],
});

describe('layoutGitGraph', () => {
  it('直线历史都在第 0 列', () => {
    const rows = layoutGitGraph([c('a', ['b']), c('b', ['c']), c('c', [])]);
    expect(rows.map((r) => r.column)).toEqual([0, 0, 0]);
  });

  it('合并提交从一列连到两个亲本列', () => {
    // newest: merge M of topic T and main C; then T; then C; then root
    const rows = layoutGitGraph([
      c('M', ['C', 'T'], 'merge'),
      c('T', ['R'], 'topic'),
      c('C', ['R'], 'mainline'),
      c('R', [], 'root'),
    ]);
    expect(rows[0].column).toBe(0);
    const mergeEdges = rows[0].edges.filter((e) => e.from === 0);
    const tos = new Set(mergeEdges.map((e) => e.to));
    expect(tos.size).toBe(2);
    expect(rows.some((r) => r.column === 1)).toBe(true);
  });
});
