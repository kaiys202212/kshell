import { describe, expect, it } from 'vitest';
import { tt } from '../test/i18n';
import {
  formatAbsoluteTime,
  formatStat,
  layoutGitGraph,
  primaryBranchLabel,
  type GraphCommit,
} from './gitGraph';

const c = (hash: string, parents: string[], subject = hash): GraphCommit => ({
  hash,
  parents,
  subject,
  author: 't',
  date: '2026-01-01T00:00:00Z',
  decorations: [],
});

describe('primaryBranchLabel', () => {
  it('优先本地分支，剥离 HEAD -> 与 tag', () => {
    expect(primaryBranchLabel(['HEAD -> main', 'origin/main'])).toBe('main');
    expect(primaryBranchLabel(['tag: v1', 'feature/x'])).toBe('feature/x');
    expect(primaryBranchLabel([])).toBe('');
  });
});

describe('layoutGitGraph', () => {
  it('直线历史都在第 0 列', () => {
    const rows = layoutGitGraph([c('a', ['b']), c('b', ['c']), c('c', [])]);
    expect(rows.map((r) => r.column)).toEqual([0, 0, 0]);
  });

  it('有 decorations 的 lane 连线带分支 label', () => {
    const tip: GraphCommit = {
      ...c('a', ['b'], 'tip'),
      decorations: ['HEAD -> topic', 'origin/topic'],
    };
    const rows = layoutGitGraph([tip, c('b', [])]);
    expect(rows[0].edges.some((e) => e.label === 'topic')).toBe(true);
  });

  it('合并侧道用侧支 tip 名，不继承主线 origin/master', () => {
    const rows = layoutGitGraph([
      { ...c('M', ['C', 'T'], 'merge'), decorations: ['HEAD -> master', 'origin/master'] },
      { ...c('T', ['R'], 'topic'), decorations: ['topic'] },
      { ...c('C', ['R'], 'mainline'), decorations: [] },
      c('R', []),
    ]);
    const mergeEdges = rows[0].edges.filter((e) => e.from === 0);
    const first = mergeEdges.find((e) => !e.merge);
    const side = mergeEdges.find((e) => e.merge);
    expect(first?.label).toBe('master');
    expect(side?.label).toBe('topic');
    // 侧支后续贯穿线仍是 topic
    const topicRow = rows.find((r) => r.commit.hash === 'T');
    expect(topicRow?.edges.some((e) => e.label === 'topic')).toBe(true);
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
    const expected = tt('ui.git.status_summary')
      .replace('{{0}}', '19')
      .replace('{{1}}', '1867')
      .replace('{{2}}', '202');
    expect(formatStat({ files: 19, insertions: 1867, deletions: 202 })).toBe(expected);
  });
});

describe('formatAbsoluteTime', () => {
  const iso = '2026-01-01T00:00:00Z';

  it('正常 locale（zh-CN/en）输出可读日期：不抛错且含年份', () => {
    for (const loc of ['zh-CN', 'en']) {
      const out = formatAbsoluteTime(iso, loc);
      expect(out).toContain('2026');
      expect(out).not.toBe(iso);
    }
  });

  it('非法 locale（3 字母区域 en-ABC）不抛错，回退稳定格式', () => {
    expect(() => formatAbsoluteTime(iso, 'en-ABC')).not.toThrow();
    expect(formatAbsoluteTime(iso, 'en-ABC')).toBe('2026-01-01 00:00');
  });
});
