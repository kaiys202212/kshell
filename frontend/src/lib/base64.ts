// 终端数据编解码：前后端之间的终端字节一律走 base64。
// 原因：xterm 的 onData 可能给出任意字节，Go 侧的 ConPTY 输出里既有 ANSI 序列、
// 也有被读边界切断的多字节字符，直接当 JS 字符串穿过 JSON 会被 UTF-16 转换破坏。

// 每次喂给 String.fromCharCode 的字节数：一次性展开大数组会爆栈。
const chunkSize = 0x8000;

export function bytesToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return btoa(binary);
}

export function base64ToBytes(b64: string): Uint8Array {
  if (!b64) return new Uint8Array(0);
  const binary = atob(b64);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}

// encodeTerminalInput 把 xterm 送来的输入字符串按 UTF-8 编码后转 base64。
export function encodeTerminalInput(data: string): string {
  return bytesToBase64(new TextEncoder().encode(data));
}
