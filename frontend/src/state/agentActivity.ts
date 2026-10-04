// frontend/src/state/agentActivity.ts
export type AgentActivity = 'awaiting' | 'running' | 'completed' | 'idle';

export function resolveAgentActivity(input: {
  status: string;
  hasPermission: boolean;
  completed: boolean;
}): AgentActivity {
  if (input.hasPermission) return 'awaiting';
  if (input.status === 'running' || input.status === 'starting') return 'running';
  if (input.completed) return 'completed';
  return 'idle';
}
