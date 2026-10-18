// 共享命令层新命令别名识别测试（AgentHubPanel 先查旧命令表，未命中才走这里）。
import { describe, expect, it } from 'vitest';
import { isHubNumber, isNewHubCommand } from './hubCommands';

describe('isHubNumber', () => {
  it('纯数字识别为编号输入', () => {
    expect(isHubNumber('3')).toBe(true);
  });

  it('前导 0 的数字串按字面匹配也视为编号（parseInt 语义）', () => {
    expect(isHubNumber('03')).toBe(true);
  });

  it('负数/非数字/空串/带空白都不是编号', () => {
    expect(isHubNumber('-1')).toBe(false);
    expect(isHubNumber('abc')).toBe(false);
    expect(isHubNumber('')).toBe(false);
    expect(isHubNumber(' 3 ')).toBe(false);
  });

  it('全角数字归一化后识别为编号', () => {
    expect(isHubNumber('１２')).toBe(true);
    expect(isHubNumber('０')).toBe(false);
  });
});

describe('isNewHubCommand', () => {
  it('命中新命令别名（单字母与组合）', () => {
    expect(isNewHubCommand('#?')).toBe(true);
    expect(isNewHubCommand('#l')).toBe(true);
    expect(isNewHubCommand('#la')).toBe(true);
    expect(isNewHubCommand('#en')).toBe(true);
    expect(isNewHubCommand('#n')).toBe(true);
    expect(isNewHubCommand('#s')).toBe(true);
    expect(isNewHubCommand('#m')).toBe(true);
    expect(isNewHubCommand('#g')).toBe(true);
    expect(isNewHubCommand('#f')).toBe(true);
    expect(isNewHubCommand('#st')).toBe(true);
    expect(isNewHubCommand('#c')).toBe(true);
    expect(isNewHubCommand('#h')).toBe(true);
  });

  it('全角＃/冒号/空格归一化后识别', () => {
    expect(isNewHubCommand('＃en')).toBe(true);
    expect(isNewHubCommand('＃g：1 hi')).toBe(true);
    expect(isNewHubCommand('#g　2　hi')).toBe(true);
    // ＃ 后不是已知命令：不命中（普通文本）
    expect(isNewHubCommand('＃话题')).toBe(false);
  });

  it('大小写不敏感且带参数时只看首词', () => {
    expect(isNewHubCommand('#G 2 hi')).toBe(true);
    expect(isNewHubCommand('#f claude')).toBe(true);
  });

  it('旧命令与未知命令不命中', () => {
    expect(isNewHubCommand('#new')).toBe(false);
    expect(isNewHubCommand('#easynew')).toBe(false);
    expect(isNewHubCommand('#xyz')).toBe(false);
  });

  it('非 # 开头或纯数字不命中', () => {
    expect(isNewHubCommand('hello')).toBe(false);
    expect(isNewHubCommand('3')).toBe(false);
  });
});
