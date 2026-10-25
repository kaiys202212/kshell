// 展示层文本还原：把 URL 百分号编码片段（文件名等）解码为可读字符。
// 只用于展示，不改消息/会话存储原文；链接点击行为不受影响。

// percentRunRe 匹配连续 ≥2 组 %HH 的最大片段。中文文件名 UTF-8 每字 3 组
// 必然成串；单独一组（如孤立的 %20）多为正文里的字面量，不还原。
const percentRunRe = /(?:%[0-9A-Fa-f]{2}){2,}/g;

// hasControlChar 是否含 C0 控制字符（正常文件名不含；含之更可能是编码演示）。
function hasControlChar(s: string): boolean {
  for (const ch of s) {
    if (ch.charCodeAt(0) < 0x20) return true;
  }
  return false;
}

// decodePercentText 把文本中的百分号编码片段还原为原文；非法 UTF-8 或解码出
// 控制字符的片段保留原样，其余字符不动。幂等：解码结果中残留的 % 不再成串。
export function decodePercentText(text: string): string {
  return text.replace(percentRunRe, (run) => {
    try {
      const decoded = decodeURIComponent(run);
      if (hasControlChar(decoded)) return run;
      return decoded;
    } catch {
      return run; // 非法序列（截断/非法字节）：原样保留
    }
  });
}
