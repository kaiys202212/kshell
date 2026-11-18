import { describe, expect, it } from 'vitest';
import { isOtherLikeText, isOtherOption, otherCompanionKey } from './elicitationOther';
import type { ChatElicitationField } from './api';

describe('otherCompanionKey', () => {
  const fields: ChatElicitationField[] = [
    { Key: 'question_0', Kind: 'select', Options: [{ Value: 'A' }] },
    { Key: 'question_0_custom', Kind: 'text', Title: 'Other' },
    { Key: 'question_1', Kind: 'multiselect' },
    { Key: 'notes', Kind: 'text' },
  ];

  it('按 key 前缀配对 Claude 的 _custom 伴随字段', () => {
    expect(otherCompanionKey(fields, 'question_0')).toBe('question_0_custom');
  });

  it('无前缀独立文本字段不误绑', () => {
    expect(otherCompanionKey(fields, 'question_1')).toBeUndefined();
  });

  it('匹配 _other 后缀（忽略大小写）', () => {
    expect(otherCompanionKey([
      { Key: 'pick', Kind: 'select' },
      { Key: 'pick_Other', Kind: 'text' },
    ], 'pick')).toBe('pick_Other');
  });
});

describe('isOtherLikeText', () => {
  it('命中 other 类文案（忽略大小写与空白）', () => {
    for (const s of ['Other', ' other ', '其他', '其它', '自定义', 'custom']) {
      expect(isOtherLikeText(s)).toBe(true);
    }
  });

  it('普通文本与空值不命中', () => {
    expect(isOtherLikeText('Redis')).toBe(false);
    expect(isOtherLikeText(undefined)).toBe(false);
    expect(isOtherLikeText('')).toBe(false);
  });
});

describe('isOtherOption', () => {
  it('Value 或 Title 命中即为 other 类选项', () => {
    expect(isOtherOption({ Value: 'Other' })).toBe(true);
    expect(isOtherOption({ Value: 'opt9', Title: '其他' })).toBe(true);
    expect(isOtherOption({ Value: 'bun', Title: 'Bun' })).toBe(false);
  });
});
