// agent 通知气泡的文案映射：把 agenthook payload 的 tool/event 归纳成
// 用户可读的标题片段。与 Go 侧 notifyToastText 各自独立（桌面端 toast 用英文工具名即可，
// 气泡要区分「等待确认」的高亮语义）。

// 一条已入队的 agent 通知（id 由 store 生成，用于移除）。
export interface AgentNotice {
  id: number;
  tool: string;
  event: string;
  termKey: string;
  workspace: string;
  summary: string;
}

// toolDisplayName 把工具 ID 映射成展示名；未知工具显示原文，空值回退 Agent。
export function toolDisplayName(tool: string): string {
  const names: Record<string, string> = {
    claude: 'Claude Code',
    codebuddy: 'CodeBuddy',
    codex: 'Codex',
    opencode: 'OpenCode',
    gemini: 'Gemini CLI',
    cursor: 'Cursor',
  };
  if (!tool) return 'Agent';
  return names[tool.toLowerCase()] ?? tool;
}

// eventLabel 把事件归纳成稳定语义 id（调用方拼 `ui.notify.` 取文案）：
// 完成 = Stop / agent-turn-complete / done；等待确认 = Notification / attention；
// 出错 = error（chat 后端 emit，summary 为错误文本）；
// 未知事件按「任务完成」处理（宁可误报完成，不打扰成待确认）。
export function eventLabel(event: string): 'task_done' | 'task_error' | 'waiting_confirm' {
  if (event === 'Notification' || event === 'attention') return 'waiting_confirm';
  if (event === 'error') return 'task_error';
  return 'task_done';
}
