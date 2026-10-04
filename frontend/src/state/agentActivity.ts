export type AgentActivity = 'awaiting' | 'running' | 'waiting' | 'idle';

export function resolveAgentActivity(input: {
  status: string;
  hasPermission: boolean;
  kind: 'chat' | 'terminal';
  busy?: boolean;
}): AgentActivity {
  if (input.hasPermission) return 'awaiting';
  if (input.kind === 'chat') {
    if (input.status === 'running' || input.status === 'starting') return 'running';
    if (input.status === 'ready') return 'waiting';
    return 'idle';
  }
  if (input.status === 'starting') return 'running';
  if (input.status === 'running') return input.busy ? 'running' : 'waiting';
  return 'idle';
}
