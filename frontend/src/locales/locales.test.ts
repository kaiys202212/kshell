// locales:中英两份 locale 资源必须成对维护(AGENTS.md i18n 规约),
// 这里守住新增 key 的成对存在,防止只补一份导致另一语言回退显示 key。
import { describe, expect, it } from 'vitest';
import en from './en.json';
import zh from './zh-CN.json';

// 按点路径取嵌套对象值,取不到返回 undefined
function at(obj: unknown, path: string): unknown {
  let cur: unknown = obj;
  for (const seg of path.split('.')) {
    if (typeof cur !== 'object' || cur === null) return undefined;
    cur = (cur as Record<string, unknown>)[seg];
  }
  return cur;
}

const PAIRED_KEYS = [
  'err.chat.auth_required',
  'err.chat.bad_elicitation_action',
  'err.session.acp_only',
  'ui.chat.status_authenticating',
];

describe('locales 成对校验', () => {
  it.each(PAIRED_KEYS)('%s 在 zh-CN 与 en 中均为非空字符串', (key) => {
    for (const bundle of [zh, en]) {
      const v = at(bundle, key);
      expect(typeof v).toBe('string');
      expect(v as string).not.toBe('');
    }
  });

  it('JSON 资源可整体解析且为对象', () => {
    expect(typeof zh).toBe('object');
    expect(typeof en).toBe('object');
  });
});
