import type { ChatElicitationField, ChatElicitationOption } from './api';

// other 类选项/字段名归一：值或文案命中即视为「其他」类（忽略大小写与首尾空白）。
const OTHER_LIKE = /^(other|其他|其它|自定义|custom)$/i;

// Claude Code 的 AskUserQuestion 会为每题生成伴随自由文本字段
// （question_<n>_custom，title "Other"）。用 key 前缀配对，避免把
// 无关的独立文本字段误绑到下拉上。
export function otherCompanionKey(fields: ChatElicitationField[], selectKey: string): string | undefined {
  const companion = fields.find(
    (f) =>
      f.Kind === 'text' &&
      f.Key.length > selectKey.length &&
      f.Key.startsWith(selectKey) &&
      /^_(custom|other)$/i.test(f.Key.slice(selectKey.length)),
  );
  return companion?.Key;
}

export function isOtherLikeText(text: unknown): boolean {
  return typeof text === 'string' && OTHER_LIKE.test(text.trim());
}

export function isOtherOption(o: Pick<ChatElicitationOption, 'Value' | 'Title'>): boolean {
  return isOtherLikeText(o.Value) || isOtherLikeText(o.Title);
}
