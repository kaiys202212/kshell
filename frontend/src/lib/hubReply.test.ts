// replyState 三态判定：与后端 wxExecutor/launch.ACPCapable 同一语义，
// 两侧测试互为镜像（后端改动必须同步这里）。
import { describe, expect, it } from 'vitest';
import { isACPTool, replyState, terminalActive } from './hubReply';
import type { HubMessage, TerminalInfo, ToolInfo } from './api';

const tools: ToolInfo[] = [
  {
    ID: 'claude',
    Name: 'Claude Code',
    BinPath: '',
    Version: '',
    Installed: true,
    Source: '',
    ACP: { Available: true, Source: 'npx' },
  },
  { ID: 'codex', Name: 'Codex', BinPath: '', Version: '', Installed: true, Source: '' },
];

const msg = (p: Partial<HubMessage>): HubMessage => ({ id: 1, kind: 'agent', text: '', ts: 0, ...p });

const term = (p: Partial<TerminalInfo>): TerminalInfo => ({ ID: 't1', ...p }) as TerminalInfo;

describe('isACPTool', () => {
  it('适配器可用为真；无探测为假；未知工具为假', () => {
    expect(isACPTool(tools, 'claude')).toBe(true);
    expect(isACPTool(tools, 'codex')).toBe(false);
    expect(isACPTool(tools, 'mystery')).toBe(false);
    expect(isACPTool(tools, undefined)).toBe(false);
  });
  it('cursor 按已安装近似判定（官方直连是探测之外路径）', () => {
    expect(isACPTool([{ ...tools[0], ID: 'cursor', Installed: true }], 'cursor')).toBe(true);
    expect(isACPTool([{ ...tools[0], ID: 'cursor', Installed: false }], 'cursor')).toBe(false);
  });
});

describe('terminalActive', () => {
  const terms: TerminalInfo[] = [term({ Key: 'session:s1' })];
  it('session: 前缀且在册为真', () => {
    expect(terminalActive('session:s1', terms)).toBe(true);
  });
  it('new: 前缀不参与判定（chat/terminal 共用 new:<n> 形态，防误伤 ACP 新建会话）', () => {
    expect(terminalActive('new:1', [term({ Key: 'new:1' })])).toBe(false);
  });
  it('不在册/空 termKey 为假', () => {
    expect(terminalActive('session:s9', terms)).toBe(false);
    expect(terminalActive(undefined, terms)).toBe(false);
  });
});

describe('replyState', () => {
  const terms: TerminalInfo[] = [term({ Key: 'session:s1' })];
  it('非 ACP 工具 → tool（永久只读）', () => {
    expect(replyState(msg({ tool: 'codex', termKey: 'session:s1' }), tools, terms)).toBe('tool');
  });
  it('ACP 工具 + 终端在册 → terminal（激活期只读）', () => {
    expect(replyState(msg({ tool: 'claude', termKey: 'session:s1' }), tools, terms)).toBe('terminal');
  });
  it('ACP 工具 + 离线 → acp（PENDING 激活回复）', () => {
    expect(replyState(msg({ tool: 'claude', termKey: 'session:s9' }), tools, terms)).toBe('acp');
  });
});
