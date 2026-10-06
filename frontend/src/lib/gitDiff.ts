/** unified diff 拆 hunk：头部 + 各 @@ 块。apply 时拼 header+hunk。 */

export type DiffHunk = {
  header: string;
  body: string;
};

export function parseDiffHunks(text: string): DiffHunk[] {
  if (!text.trim()) return [];
  const lines = text.replace(/\r\n/g, '\n').split('\n');
  const firstHunk = lines.findIndex((l) => l.startsWith('@@'));
  if (firstHunk < 0) return [];
  const header = lines.slice(0, firstHunk).join('\n') + '\n';
  const hunks: DiffHunk[] = [];
  let start = firstHunk;
  for (let i = firstHunk + 1; i <= lines.length; i++) {
    if (i === lines.length || lines[i].startsWith('@@')) {
      const body = lines.slice(start, i).join('\n') + '\n';
      hunks.push({ header, body });
      start = i;
    }
  }
  return hunks;
}

export function hunkPatch(h: DiffHunk): string {
  return h.header + h.body;
}
