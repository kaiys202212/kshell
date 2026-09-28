// 终端窗口标题归一化：与 Go 侧 WindowManager.TerminalTitle 逐字对齐——
// "kshell · " 前缀 + 超过 80 rune 截到 79 rune 并补省略号。
// windowStatus 的键与 window:closed 事件 payload 都是该形态，
// 因此匹配时用严格相等即可，无需前缀兜底。
const PREFIX = 'kshell · ';
const MAX_RUNES = 80;

export function terminalTitle(text: string): string {
  const full = PREFIX + text;
  const runes = Array.from(full); // 按 rune（码点）而非 UTF-16 码元切分
  if (runes.length <= MAX_RUNES) {
    return full;
  }
  return runes.slice(0, MAX_RUNES - 1).join('') + '…';
}
