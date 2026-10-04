import { describe, expect, it } from 'vitest';
import { mergeMirrorList, type MirrorRow } from './mirrorMerge';

function row(patch: Partial<MirrorRow> & Pick<MirrorRow, 'ID'>): MirrorRow {
  return {
    Title: '新建',
    Status: 'ready',
    SessionID: '',
    ...patch,
  };
}

describe('mergeMirrorList', () => {
  it('过期快照不能盖掉已发送的标题', () => {
    const prev = [row({ ID: 't1', Prompted: true, Title: '修复登录空指针', Status: 'running' })];
    const next = [row({ ID: 't1', Prompted: false, Title: '新建 · Claude', Status: 'ready' })];
    expect(mergeMirrorList(prev, next)).toEqual([
      row({ ID: 't1', Prompted: true, Title: '修复登录空指针', Status: 'running' }),
    ]);
  });

  it('新快照自己带 Prompted 时采用新标题与状态', () => {
    const prev = [row({ ID: 't1', Prompted: true, Title: '修复登录空指针', Status: 'running' })];
    const next = [row({ ID: 't1', Prompted: true, Title: '磁盘标题', Status: 'ready', SessionID: 's1' })];
    expect(mergeMirrorList(prev, next)).toEqual(next);
  });
});
