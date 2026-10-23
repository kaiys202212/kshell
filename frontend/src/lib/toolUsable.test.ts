// toolUsable 单元测试：会话创建入口的统一过滤口径。
// 关键行为：Auth=out（确定未登录）一律不可用；unknown/缺省保守放行。
import { describe, expect, it } from 'vitest';
import type { ToolInfo } from './api';
import { usableForACP, usableForSessionMenu } from './toolUsable';

const tool = (over: Partial<ToolInfo>): ToolInfo =>
  ({
    ID: 'claude',
    Name: 'Claude Code',
    BinPath: 'C:\\bin\\claude.cmd',
    Version: '',
    Installed: true,
    Source: 'path',
    ACP: { Available: true, Source: 'builtin' },
    ...over,
  }) as ToolInfo;

describe('usableForSessionMenu（工作区新建会话下拉）', () => {
  it('已安装且有 bin：可用（Auth 缺省视为 unknown，保守放行）', () => {
    expect(usableForSessionMenu(tool({}))).toBe(true);
  });

  it('Auth=in：可用', () => {
    expect(usableForSessionMenu(tool({ Auth: 'in' }))).toBe(true);
  });

  it('Auth=unknown：保守放行，不误伤探测不稳定的场景', () => {
    expect(usableForSessionMenu(tool({ Auth: 'unknown' }))).toBe(true);
  });

  it('Auth=out（确定未登录）：不可用——入口不可见，登录收敛到设置页工具分区', () => {
    expect(usableForSessionMenu(tool({ Auth: 'out' }))).toBe(false);
  });

  it('未安装 / 无可执行文件：不可用（既有口径不变）', () => {
    expect(usableForSessionMenu(tool({ Installed: false }))).toBe(false);
    expect(usableForSessionMenu(tool({ BinPath: '' }))).toBe(false);
  });
});

describe('usableForACP（AgentHub / 需求派发）', () => {
  it('ACP 可用：可用（Auth 缺省视为 unknown，保守放行）', () => {
    expect(usableForACP(tool({}))).toBe(true);
  });

  it('Auth=out：不可用（即使 ACP 可用）', () => {
    expect(usableForACP(tool({ Auth: 'out' }))).toBe(false);
  });

  it('Auth=in / unknown：可用', () => {
    expect(usableForACP(tool({ Auth: 'in' }))).toBe(true);
    expect(usableForACP(tool({ Auth: 'unknown' }))).toBe(true);
  });

  it('ACP 不可用：不可用（既有口径不变）', () => {
    expect(usableForACP(tool({ ACP: undefined }))).toBe(false);
    expect(usableForACP(tool({ ACP: { Available: false, Source: 'builtin' } }))).toBe(false);
  });
});
