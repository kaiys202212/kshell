// Markdown 渲染检索：rehype 插件把命中文本包成 <mark data-md-hit="n">。
// 只在单个文本节点内匹配（跨内联标签的长词不命中），命中序号跨节点连续递增；
// mark 标记数受 MD_SEARCH_LIMIT 限制，超出后剩余命中保留原文不再标记；
// 命中总数（不受限，供「500+」展示）经 totalRef 交还调用方。
import type { Element, Nodes, Root, Text } from 'hast';

export const MD_SEARCH_LIMIT = 500;

export function rehypeHighlightSearch(
  query: string,
  totalRef: { current: number },
): (tree: Nodes) => void {
  const kw = query.toLowerCase();
  return (tree: Nodes) => {
    totalRef.current = 0;
    if (!kw) return;
    let marks = 0; // 已标记数（受 MD_SEARCH_LIMIT 限制）
    let total = 0; // 实际命中总数（不受限，供「500+」展示）

    const visit = (parent: Element | Root): void => {
      const next: Nodes[] = [];
      for (const child of parent.children) {
        if (child.type === 'text') {
          const value = (child as Text).value;
          const lower = value.toLowerCase();
          let from = 0;
          let at = lower.indexOf(kw);
          while (at >= 0) {
            if (from < at) next.push({ type: 'text', value: value.slice(from, at) });
            const hitValue = value.slice(at, at + kw.length);
            if (marks < MD_SEARCH_LIMIT) {
              next.push({
                type: 'element',
                tagName: 'mark',
                properties: { 'data-md-hit': marks },
                children: [{ type: 'text', value: hitValue }],
              });
              marks++;
            } else {
              // 超限：保留原文
              next.push({ type: 'text', value: hitValue });
            }
            total++;
            from = at + kw.length;
            at = lower.indexOf(kw, from);
          }
          if (from < value.length) next.push({ type: 'text', value: value.slice(from) });
          continue;
        }
        // script/style 内容不参与检索
        if (child.type === 'element' && (child.tagName === 'script' || child.tagName === 'style')) {
          next.push(child);
          continue;
        }
        if (child.type === 'element') visit(child);
        next.push(child);
      }
      parent.children = next as typeof parent.children;
    };

    if (tree.type === 'root') visit(tree);
    totalRef.current = total;
  };
}
