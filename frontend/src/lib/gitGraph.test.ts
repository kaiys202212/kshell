import { describe, expect, it } from 'vitest';
import { formatStat, layoutGitGraph, type GraphCommit } from './gitGraph';

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

  it('合并后同一亲本不占两列，连线落到下一行的点或贯穿线', () => {
    const rows = layoutGitGraph([
      c('M', ['C', 'T'], 'merge'),
      c('T', ['R'], 'topic'),
      c('C', ['R'], 'mainline'),
      c('R', [], 'root'),
    ]);
    expect(rows.find((r) => r.commit.hash === 'R')).toBeTruthy();
    for (let i = 0; i < rows.length - 1; i++) {
      for (const e of rows[i].edges) {
        const next = rows[i + 1];
        const hit = next.column === e.to || next.edges.some((ne) => ne.from === e.to);
        expect(hit, `${rows[i].commit.hash} ${e.from}->${e.to}`).toBe(true);
      }
    }
  });
});

describe('formatStat', () => {
  it('拼出 VS Code 风格摘要', () => {
    expect(formatStat({ files: 19, insertions: 1867, deletions: 202 })).toBe(
      '已更改 19 个文件, 1867 行插入(+), 202 行删除(-)',
    );
  });
});
