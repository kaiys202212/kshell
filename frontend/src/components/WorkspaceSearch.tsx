// 会话过滤框：受控组件，过滤词由父级（SessionList）持有；
// 「匹配标题 / 工具名」的过滤逻辑在父级实现，本组件只负责输入。
interface Props {
  value: string;
  onChange(value: string): void;
}

export default function WorkspaceSearch({ value, onChange }: Props) {
  return (
    <input
      className="session-search"
      type="search"
      aria-label="过滤会话"
      placeholder="过滤会话（标题 / 工具名）"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}
