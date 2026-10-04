# 设置页重新扫描按钮 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development 逐任务实现（单文件小改动，当前会话连续执行）。

**Goal:** 设置 → 工具检测页新增「重新扫描」按钮，触发后端扫描并自动刷新工具列表。

**Architecture:** 纯前端改动：`Settings.tsx` 导入 `scanSessions`，工具检测标题行加按钮 + 本地 `rescanning` 态；`tools:updated` 到达即恢复可点。

**Tech Stack:** React 19 + vitest + testing-library。

---

### Task 1: 按钮渲染与触发行为

**Files:**
- Modify: `frontend/src/pages/Settings.tsx`（工具检测 section）
- Test: `frontend/src/pages/Settings.test.tsx`

**Step 1: 写失败测试**

`Settings.test.tsx` 的 `mocks` 加 `scanSessions: vi.fn()`；追加用例（放在工具检测相关 describe 内）：

```tsx
it('工具检测页「重新扫描」按钮触发扫描，tools:updated 后恢复可点', async () => {
  goTools();
  const btn = screen.getByRole('button', { name: '重新扫描' });
  fireEvent.click(btn);
  expect(mocks.scanSessions).toHaveBeenCalledTimes(1);
  // 扫描中禁用防连点
  expect(screen.getByRole('button', { name: '扫描中…' })).toBeDisabled();
  // DetectAll 完成推 tools:updated → 恢复
  await act(async () => {
    toolsUpdatedCb?.();
  });
  expect(await screen.findByRole('button', { name: '重新扫描' })).toBeEnabled();
});

it('「重新扫描」进行中点击不重复触发', async () => {
  goTools();
  fireEvent.click(screen.getByRole('button', { name: '重新扫描' }));
  fireEvent.click(screen.getByRole('button', { name: '扫描中…' }));
  expect(mocks.scanSessions).toHaveBeenCalledTimes(1);
  await act(async () => {
    toolsUpdatedCb?.();
  });
});
```

注意：`mocks.scanSessions` 需默认 `mockResolvedValue(undefined)`（在 beforeEach 统一设）。

**Step 2: 跑测试确认失败**

```
cd frontend; npx vitest run src/pages/Settings.test.tsx
```
预期：FAIL（找不到「重新扫描」按钮）。

**Step 3: 最小实现**

`Settings.tsx`：

1. 从 `../lib/api` 导入 `scanSessions`（若无）。
2. 组件内加 `const [rescanning, setRescanning] = useState(false);`
3. 现有 `useEffect` 的 `onToolsUpdated` 回调里补 `setRescanning(false);`（scan:done 也补，双保险幂等）。
4. 工具检测 section 标题改为行内 flex，右侧加按钮：

```tsx
<div className="mb-3 flex items-center justify-between">
  <h2 className="text-sm font-medium">工具检测</h2>
  <Button
    variant="secondary"
    type="button"
    disabled={rescanning}
    onClick={() => {
      if (rescanning) return;
      setRescanning(true);
      void scanSessions();
    }}
  >
    {rescanning ? '扫描中…' : '重新扫描'}
  </Button>
</div>
```

（原 `<h2 className="mb-3 ...">工具检测</h2>` 替换为上述结构。）

**Step 4: 跑测试确认通过 + 前端全量**

```
npx vitest run src/pages/Settings.test.tsx
npm test && npm run build
```

**Step 5: 提交**

```
git add -A && git commit -m "feat: 工具检测页加重新扫描按钮"
```

### Task 2: 冒烟条目

`docs/smoke/feat-settings-rescan-button.md`：设置 → 工具 点击「重新扫描」→ 按钮变「扫描中…」→ 稍后恢复且列表刷新。提交 `docs: 冒烟条目`。
