import type { GitSCMEntry } from './api';

export type ChangeNode = {
  name: string;
  path: string;
  dir: boolean;
  entry?: GitSCMEntry;
  children: ChangeNode[];
};

export function buildChangeTree(entries: GitSCMEntry[]): ChangeNode {
  const root: ChangeNode = { name: '', path: '', dir: true, children: [] };
  for (const e of entries) {
    const parts = e.Path.replace(/\\/g, '/').split('/').filter(Boolean);
    let cur = root;
    let acc = '';
    for (let i = 0; i < parts.length; i++) {
      const name = parts[i];
      acc = acc ? `${acc}/${name}` : name;
      const isLeaf = i === parts.length - 1;
      let child = cur.children.find((c) => c.name === name);
      if (!child) {
        child = { name, path: acc, dir: !isLeaf, children: [] };
        cur.children.push(child);
      }
      if (isLeaf) {
        child.dir = false;
        child.entry = e;
      } else {
        child.dir = true;
      }
      cur = child;
    }
  }
  sortTree(root);
  return root;
}

function sortTree(n: ChangeNode) {
  n.children.sort((a, b) => {
    if (a.dir !== b.dir) return a.dir ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
  n.children.forEach(sortTree);
}

export function collectPaths(node: ChangeNode): string[] {
  if (!node.dir) {
    return node.entry ? [node.entry.Path] : [];
  }
  return node.children.flatMap(collectPaths);
}
