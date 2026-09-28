// 会话过滤框：受控组件，过滤词由父级（SessionList）持有；
// 「匹配标题 / 工具名」的过滤逻辑在父级实现，本组件只负责输入。
// 监听全局 kshell:focus-search 事件（App 的 Ctrl+F 派发）：收到即聚焦输入框，
// 不引 ref 链，卸载时移除监听。
import { useEffect, useRef } from 'react';

interface Props {
  value: string;
  onChange(value: string): void;
}

export default function WorkspaceSearch({ value, onChange }: Props) {
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const focus = () => inputRef.current?.focus();
    window.addEventListener('kshell:focus-search', focus);
    return () => window.removeEventListener('kshell:focus-search', focus);
  }, []);

  return (
    <input
      ref={inputRef}
      className="h-8 rounded-md border border-input bg-card px-2.5 text-sm text-foreground outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring"
      type="search"
      aria-label="过滤会话"
      placeholder="过滤会话（标题 / 工具名）"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}
