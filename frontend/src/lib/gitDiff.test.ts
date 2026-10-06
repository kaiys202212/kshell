import { describe, expect, it } from 'vitest';
import { hunkPatch, parseDiffHunks } from './gitDiff';

describe('parseDiffHunks', () => {
  const sample = `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,2 @@
-a
 b
+c
@@ -10,1 +10,1 @@
-x
+y
`;

  it('拆成两块且保留文件头', () => {
    const hs = parseDiffHunks(sample);
    expect(hs).toHaveLength(2);
    expect(hs[0].header).toContain('diff --git');
    expect(hs[0].body).toContain('@@ -1,2');
    expect(hs[0].body).not.toContain('@@ -10');
    expect(hunkPatch(hs[1])).toContain('diff --git');
    expect(hunkPatch(hs[1])).toContain('+y');
  });

  it('空文本无 hunk', () => {
    expect(parseDiffHunks('')).toEqual([]);
  });
});
