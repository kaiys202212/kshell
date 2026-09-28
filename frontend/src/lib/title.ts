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

// ---- 会话标题清洗（渲染层兜底）----
// 与 Go 侧 internal/providers/title.go 的 cleanTitle 规则同源：新扫描的数据已由 Go 侧清洗，
// 这里再对历史数据 / 未重扫的缓存做一次同样的纯字符串剥离，避免标题里露出 XML 包装标签。

// dropBlockTags：「整块都不属于用户手写内容」的包装标签，标签连同内容一起删掉。
// 这些块是各 CLI 自己注入的机器上下文（运行提示、斜杠命令回显、IDE 选中信息一类）。
const DROP_BLOCK_TAGS = [
  'local-command-caveat',
  'local-command-stdout',
  'command-name',
  'command-message',
  'timestamp', // Codex 在每条用户消息前置的本地时间戳块
  'system-reminder',
  'user_info',
  'rules',
  'additional_data',
  'manually_attached_skills',
  'external_links',
  'environment_context',
  'user_instructions',
  'ide_opened_file',
  'ide_selection',
];

// unwrapTags：「包着真实用户输入」的标签，只删标签标记、保留里面的文本。
const UNWRAP_TAGS = ['user_query', 'user_message', 'command-args'];

// 成对块（可跨行，非贪婪）；未闭合的标签只删标记本身、不吞后面的正文
const BLOCK_RES = DROP_BLOCK_TAGS.map(
  (tag) => new RegExp(`<${tag}\\b[^>]*>.*?</${tag}\\s*>`, 'gis'),
);

// 只匹配这些已知标签的开/闭/自闭合标记，绝不碰用户正文里其它尖括号内容
const TAG_ONLY_RE = new RegExp(
  `</?(?:${[...DROP_BLOCK_TAGS, ...UNWRAP_TAGS].join('|')})\\b[^>]*/?>`,
  'gis',
);

// dropTruncatedLeadingBlock 处理「正文以已知包装标签开头、但没有对应的闭合标签」的情况：
// 这几乎只发生在读取头部被截断时（长包装块被截掉尾巴），此时整段都不是用户正文，返回空串。
// 只认开头位置：正文中间出现的未闭合标签不吞后续内容。
function dropTruncatedLeadingBlock(s: string): string {
  const trimmed = s.replace(/^[ \t\r\n]+/, '');
  if (!trimmed.startsWith('<')) return s;
  const lower = trimmed.toLowerCase();
  for (const tag of DROP_BLOCK_TAGS) {
    const open = `<${tag}`;
    if (!lower.startsWith(open)) continue;
    const rest = lower.slice(open.length);
    // 标签名边界：避免 <rulesfoo> 误命中 rules
    if (rest !== '' && rest[0] !== '>' && rest[0] !== '/' && !/\s/.test(rest[0])) continue;
    if (!lower.includes(`</${tag}`)) return '';
  }
  return s;
}

// displayTitle 清洗会话标题候选文本（与 Go 侧 cleanTitle 同规则）：
//  1. 去掉被截断的开头包装块；
//  2. 删掉 DROP_BLOCK_TAGS 的整个块（含内容）；
//  3. 去掉已知标签的标记（保留内容）；
//  4. 折叠空白。
// 返回空串表示「没有可用标题」，调用方自行决定占位文案。
export function displayTitle(raw: string): string {
  if (!raw) return '';
  let s = raw;
  // 循环删除以处理嵌套（如 <additional_data> 里还嵌着 <rules>），上限防病态输入
  for (let i = 0; i < 4; i++) {
    let next = dropTruncatedLeadingBlock(s);
    for (const re of BLOCK_RES) next = next.replace(re, ' ');
    next = next.replace(TAG_ONLY_RE, ' ');
    if (next === s) break;
    s = next;
  }
  return s.split(/\s+/).join(' ').trim();
}
