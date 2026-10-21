// hubAttribution 单测：归因解析的命中/回退/缺省路径。
import { describe, expect, it } from 'vitest';
import type { ChatInfo, TerminalInfo } from './api';
import { attributionText, baseName, resolveAttribution } from './hubAttribution';

function chat(over: Partial<ChatInfo>): ChatInfo {
  return {
    ID: 'c1',
    Key: 'new:1',
    Kind: 'new',
    SessionID: '',
    Workspace: 'D:\\ws\\a',
    Title: '重构认证',
    ToolID: 'claude',
    Status: 'ready',
    ExitCode: 0,
    Error: '',
    Prompted: true,
    ...over,
  };
}

function term(over: Partial<TerminalInfo>): TerminalInfo {
  return {
    ID: 't1',
    Key: 'session:s1',
    Kind: 'session',
    SessionID: 's1',
    Workspace: 'D:\\ws\\b',
    Title: '修复登录',
    ToolID: 'codex',
    Status: 'running',
    ExitCode: 0,
    Cols: 80,
    Rows: 24,
    ...over,
  };
}

describe('baseName', () => {
  it('取路径末段（正反斜杠、尾随分隔符）', () => {
    expect(baseName('D:\\ws\\a')).toBe('a');
    expect(baseName('/home/u/proj/')).toBe('proj');
    expect(baseName('plain')).toBe('plain');
  });
});

describe('resolveAttribution', () => {
  it('chatId 命中 chats：项目取 workspace 基名，session 取标题', () => {
    const a = resolveAttribution(
      { workspace: 'D:\\ws\\alpha', chatId: 'c1' },
      [chat({})],
      [],
    );
    expect(a.project).toBe('alpha');
    expect(a.session).toBe('重构认证');
  });

  it('消息缺 workspace 时按 chatId 反查 chats 的 Workspace', () => {
    const a = resolveAttribution({ chatId: 'c1' }, [chat({ Workspace: 'E:\\repo\\beta' })], []);
    expect(a.project).toBe('beta');
  });

  it('仅 termKey：chats Key 未命中时回落 terminals，session 取终端标题', () => {
    const a = resolveAttribution(
      { workspace: 'D:\\ws\\gamma', termKey: 'session:s1' },
      [chat({ Key: 'new:9' })],
      [term({})],
    );
    expect(a.project).toBe('gamma');
    expect(a.session).toBe('修复登录');
  });

  it('chatId 查不到：session 回退 #id；无 workspace 无反查则无 project', () => {
    const a = resolveAttribution({ chatId: 'cX' }, [], []);
    expect(a.project).toBeUndefined();
    expect(a.session).toBe('#cX');
  });

  it('chats 反查不到时用消息自带 sessionTitle 快照（会话关闭/重启场景）', () => {
    const a = resolveAttribution({ chatId: 'c1', sessionTitle: '重构认证' }, [], []);
    expect(a.session).toBe('重构认证');
  });

  it('chats 命中标题优先于 sessionTitle 快照（跟随最新标题）', () => {
    const a = resolveAttribution(
      { chatId: 'c1', sessionTitle: '旧标题' },
      [chat({ Title: '新标题' })],
      [],
    );
    expect(a.session).toBe('新标题');
  });

  it('termKey 命中但标题为空：回退 #termKey', () => {
    const a = resolveAttribution(
      { termKey: 'session:s1' },
      [],
      [term({ Title: '', Workspace: '' })],
    );
    expect(a.session).toBe('#session:s1');
  });

  it('全缺字段：返回空对象，attributionText 为空串', () => {
    const a = resolveAttribution({}, [], []);
    expect(a.project).toBeUndefined();
    expect(a.session).toBeUndefined();
    expect(attributionText(a)).toBe('');
  });

  it('attributionText 拼接：项目 · 会话，缺项跳过', () => {
    expect(attributionText({ project: 'a', session: 'b' })).toBe('a · b');
    expect(attributionText({ project: 'a' })).toBe('a');
    expect(attributionText({ session: 'b' })).toBe('b');
  });

  it('termKey 与 ACP chat 撞 new:N：工具不一致时用终端标题，不串 Cursor 会话名', () => {
    const a = resolveAttribution(
      {
        tool: 'codebuddy',
        workspace: 'D:\\ws\\strategy',
        termKey: 'new:2',
        sessionTitle: 'Analyze Error Screenshot', // 历史误写快照
      },
      [chat({ Key: 'new:2', ToolID: 'cursor', Title: 'Analyze Error Screenshot', Workspace: 'D:\\ws\\kshell' })],
      [term({ Key: 'new:2', ToolID: 'codebuddy', Title: 'Debug browser', Workspace: 'D:\\ws\\strategy' })],
    );
    expect(a.project).toBe('strategy');
    expect(a.session).toBe('Debug browser');
  });

  it('chatId 指向异工具会话：忽略该 chat 标题，回落 termKey/快照', () => {
    const a = resolveAttribution(
      {
        tool: 'codebuddy',
        chatId: 'c1',
        termKey: 'new:2',
        sessionTitle: '误写快照',
      },
      [chat({ ID: 'c1', Key: 'new:2', ToolID: 'cursor', Title: 'Cursor 会话' })],
      [term({ Key: 'new:2', ToolID: 'codebuddy', Title: 'CodeBuddy 终端' })],
    );
    expect(a.session).toBe('CodeBuddy 终端');
  });
});
