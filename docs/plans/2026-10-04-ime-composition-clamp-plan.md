# IME 组合全程钳制 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 组合输入全程钳制 xterm `.composition-view` 与 helper textarea，避免拼音贴右缘挤偏 agent TUI。

**Architecture:** 纯函数 `computeImeClampStyles` 计算 left/top/maxWidth；`TerminalView` 在 compositionstart/update 与 onRender（组合中）应用到 DOM，对抗 xterm 的 `updateCompositionElements` 重定位。

**Tech Stack:** React 19、@xterm/xterm 6、vitest + jsdom、TypeScript strict。

## Global Constraints

- 规格见 `docs/plans/2026-10-04-ime-composition-clamp-design.md`
- 钳制比例沿用 `IME_MAX_COL_RATIO = 0.6`
- 中文注释；提交信息 `type: 简述`（仅用户要求时提交）
- 不改 Go、不升级 xterm、不做反色光标启发式

---

### Task 1: 扩展 imeAnchor 纯函数与测试

**Files:**
- Modify: `frontend/src/lib/imeAnchor.ts`
- Modify: `frontend/src/lib/imeAnchor.test.ts`

**Interfaces:**
- Consumes: 现有 `ImeAnchorInput`、`computeImeAnchor`、`IME_MAX_COL_RATIO`
- Produces: `ImeClampStyles { left, top, maxWidth }`；`computeImeClampStyles(input): ImeClampStyles | null`

- [ ] **Step 1: 写失败测试**

在 `imeAnchor.test.ts` 增加：

```ts
describe('computeImeClampStyles', () => {
  it('复用钳制后的 left/top，并给出剩余视口 maxWidth', () => {
    const got = computeImeClampStyles({ ...base, cursorX: 90 });
    expect(got).not.toBeNull();
    const maxCol = Math.floor(base.cols * IME_MAX_COL_RATIO);
    expect(got!.left).toBe(maxCol * (base.viewportWidth / base.cols));
    expect(got!.top).toBe(base.cursorY * (base.viewportHeight / base.rows));
    expect(got!.maxWidth).toBe(base.viewportWidth - got!.left);
    expect(got!.maxWidth).toBeGreaterThan(0);
  });

  it('退化输入返回 null', () => {
    expect(computeImeClampStyles({ ...base, cols: 0 })).toBeNull();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend; npx vitest run src/lib/imeAnchor.test.ts`
Expected: FAIL（`computeImeClampStyles` 未导出）

- [ ] **Step 3: 最小实现**

```ts
export interface ImeClampStyles {
  left: number;
  top: number;
  maxWidth: number;
}

export function computeImeClampStyles(input: ImeAnchorInput): ImeClampStyles | null {
  const anchor = computeImeAnchor(input);
  if (!anchor) return null;
  const maxWidth = Math.max(input.viewportWidth - anchor.left, 1);
  return { left: anchor.left, top: anchor.top, maxWidth };
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend; npx vitest run src/lib/imeAnchor.test.ts`
Expected: PASS

---

### Task 2: TerminalView 组合全程应用钳制

**Files:**
- Modify: `frontend/src/components/TerminalView.tsx`

**Interfaces:**
- Consumes: `computeImeClampStyles`
- Produces: 组合期间 DOM 样式持续钳制

- [ ] **Step 1: 将 compositionstart 单次钳制改为全程应用**

替换现有 IME 块：维护 `composing` 标志；`applyImeClamp` 查 `.xterm-screen` / `.composition-view` / textarea，用 `computeImeClampStyles` 写样式；监听 compositionstart/update/end；`onRender` 在 composing 时调用 `applyImeClamp`；清理时移除监听并 dispose onRender。

要点（伪代码）：

```ts
let composing = false;
const applyImeClamp = () => {
  const screen = host.querySelector<HTMLElement>('.xterm-screen');
  const compositionView = host.querySelector<HTMLElement>('.composition-view');
  const buf = instance.buffer.active;
  if (!textarea || !screen) return;
  const styles = computeImeClampStyles({
    cols: instance.cols,
    rows: instance.rows,
    cursorX: buf.cursorX,
    cursorY: buf.cursorY,
    viewportWidth: screen.clientWidth,
    viewportHeight: screen.clientHeight,
  });
  if (!styles) return;
  textarea.style.left = `${styles.left}px`;
  textarea.style.top = `${styles.top}px`;
  if (compositionView) {
    compositionView.style.left = `${styles.left}px`;
    compositionView.style.top = `${styles.top}px`;
    compositionView.style.maxWidth = `${styles.maxWidth}px`;
    compositionView.style.overflow = 'hidden';
  }
};
const onCompositionStart = () => { composing = true; applyImeClamp(); };
const onCompositionUpdate = () => { applyImeClamp(); };
const onCompositionEnd = () => { composing = false; };
textarea?.addEventListener('compositionstart', onCompositionStart);
textarea?.addEventListener('compositionupdate', onCompositionUpdate);
textarea?.addEventListener('compositionend', onCompositionEnd);
const renderSub = instance.onRender(() => { if (composing) applyImeClamp(); });
// cleanup: remove listeners + renderSub.dispose()
```

用 `requestAnimationFrame` 或微任务再应用一次亦可，以压过同帧内 xterm 的 `updateCompositionElements`（若单次不够再加 rAF）。

- [ ] **Step 2: 跑前端测试**

Run: `cd frontend; npm test`
Expected: PASS（含 imeAnchor 与 TerminalView 现有用例）

- [ ] **Step 3: 更新冒烟清单**

新建 `docs/smoke/fix-ime-composition-clamp.md`：预编辑拼音与候选窗不贴右缘、TUI 不横向偏移。

---

### Task 3: 验证

- [ ] **Step 1: 全量前端验证**

Run: `cd frontend; npm test; npm run build`
Expected: 全绿

- [ ] **Step 2: 对照规格自查**

- 组合全程钳制 composition-view + textarea：有
- maxWidth 防溢出：有
- 不引入方案 B/C：有
