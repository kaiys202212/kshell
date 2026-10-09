// 会话过滤框：受控组件，过滤词由父级（SessionList）持有；
// 「匹配标题 / 工具名」的过滤逻辑在父级实现，本组件只负责输入。
// 监听全局 kshell:focus-search 事件（App 的 Ctrl+F 派发）：收到即聚焦输入框，
// 不引 ref 链，卸载时移除监听。
import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { Input } from './ui/input';

interface Props {
  value: string;
  onChange(value: string): void;
}

export default function WorkspaceSearch({ value, onChange }: Props) {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const focus = (e: Event) => {
      // Markdown 预览等更高优先级的检索已消费该事件（preventDefault）时退让
      if (e.defaultPrevented) return;
      inputRef.current?.focus();
    };
    window.addEventListener('kshell:focus-search', focus);
    return () => window.removeEventListener('kshell:focus-search', focus);
  }, []);

  return (
    <Input
      ref={inputRef}
      type="search"
      aria-label={t('ui.session_list.search_aria')}
      placeholder={t('ui.session_list.search_placeholder')}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}
