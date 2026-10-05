export type AgentActivity = 'awaiting' | 'running' | 'waiting' | 'idle';

export function resolveAgentActivity(input: {
  status: string;
  hasPermission: boolean;
  kind: 'chat' | 'terminal';
  busy?: boolean;
}): AgentActivity {
  if (input.hasPermission) return 'awaiting';
  if (input.kind === 'chat') {
    // starting 是握手，用户还没提交任务，不能当「执行中」。
    if (input.status === 'running') return 'running';
    if (input.status === 'ready' || input.status === 'starting') return 'waiting';
    return 'idle';
  }
  if (input.status === 'starting') return 'running';
  if (input.status === 'running') return input.busy ? 'running' : 'waiting';
  return 'idle';
}
