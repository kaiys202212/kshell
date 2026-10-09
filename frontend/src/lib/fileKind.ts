// 按路径扩展名推断预览种类与 CodeMirror 语言 id。

export type PreviewKind = 'text' | 'markdown' | 'image' | 'pdf' | 'binary';

const MARKDOWN_EXT = new Set(['md', 'markdown', 'mdx']);
const IMAGE_EXT = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico']);
const HTML_EXT = new Set(['html', 'htm']);
const TEXT_EXT = new Set([
  'ts',
  'tsx',
  'js',
  'jsx',
  'json',
  'go',
  'py',
  'rs',
  'java',
  'c',
  'h',
  'cpp',
  'cs',
  'rb',
  'php',
  'html',
  'css',
  'scss',
  'less',
  'vue',
  'svelte',
  'yaml',
  'yml',
  'toml',
  'xml',
  'sh',
  'bash',
  'zsh',
  'ksh',
  'fish',
  'ps1',
  'psm1',
  'psd1',
  'bat',
  'cmd',
  'sql',
  'graphql',
  'dockerfile',
  'txt',
  'log',
  'env',
  'gitignore',
  'editorconfig',
]);

const SPECIAL_TEXT_BASENAMES = new Set(['dockerfile', 'makefile', 'cmakelists.txt']);

const SHELL_EXT = new Set(['sh', 'bash', 'zsh', 'ksh', 'fish', 'bat', 'cmd']);
const POWERSHELL_EXT = new Set(['ps1', 'psm1', 'psd1']);

function pathBasename(path: string): string {
  const normalized = path.replace(/\\/g, '/');
  const parts = normalized.split('/');
  return parts[parts.length - 1] ?? '';
}

/** 小写扩展名；点开头的隐藏文件取点后的名（如 .gitignore → gitignore）。 */
function fileExtension(path: string): string {
  const base = pathBasename(path);
  const dot = base.lastIndexOf('.');
  if (dot === -1) return '';
  if (dot === 0) return base.slice(1).toLowerCase();
  return base.slice(dot + 1).toLowerCase();
}

function isSpecialTextBasename(path: string): boolean {
  return SPECIAL_TEXT_BASENAMES.has(pathBasename(path).toLowerCase());
}

/** 文本/Markdown 默认可编辑；图、PDF、二进制仅预览。 */
export function isEditableKind(kind: PreviewKind): boolean {
  return kind === 'text' || kind === 'markdown';
}

/** html/htm：源码可编辑，另可开浏览器页签渲染预览。 */
export function isHtmlPath(path: string): boolean {
  return HTML_EXT.has(fileExtension(path));
}

export function previewKind(path: string): PreviewKind {
  if (isSpecialTextBasename(path)) return 'text';

  const ext = fileExtension(path);
  if (MARKDOWN_EXT.has(ext)) return 'markdown';
  if (IMAGE_EXT.has(ext)) return 'image';
  if (ext === 'pdf') return 'pdf';
  if (TEXT_EXT.has(ext)) return 'text';
  // 未知扩展先当文本；后端 Binary 标志可再改为 binary。
  return 'text';
}

const LANG_BY_EXT: Record<string, string> = {
  ts: 'typescript',
  tsx: 'tsx',
  js: 'javascript',
  jsx: 'javascript',
  json: 'json',
  go: 'go',
  py: 'python',
  html: 'html',
  css: 'css',
  scss: 'css',
  less: 'css',
  md: 'markdown',
  markdown: 'markdown',
  mdx: 'markdown',
  yaml: 'yaml',
  yml: 'yaml',
  toml: 'toml',
  dockerfile: 'shell',
};

export function codeLanguage(path: string): string {
  if (pathBasename(path).toLowerCase() === 'dockerfile') return 'shell';
  if (pathBasename(path).toLowerCase() === 'makefile') return 'shell';

  const ext = fileExtension(path);
  if (LANG_BY_EXT[ext]) return LANG_BY_EXT[ext];
  if (POWERSHELL_EXT.has(ext)) return 'powershell';
  if (SHELL_EXT.has(ext)) return 'shell';
  if (MARKDOWN_EXT.has(ext)) return 'markdown';
  return 'plaintext';
}
