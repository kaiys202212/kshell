import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import GitDiffView from './GitDiffView';

const mocks = vi.hoisted(() => ({
  gitDiff: vi.fn(),
  gitStageHunk: vi.fn().mockResolvedValue(undefined),
  gitUnstageHunk: vi.fn().mockResolvedValue(undefined),
  gitDiscardHunk: vi.fn().mockResolvedValue(undefined),
  gitStage: vi.fn().mockResolvedValue(undefined),
  gitUnstage: vi.fn().mockResolvedValue(undefined),
  gitDiscard: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../lib/api', () => mocks);
vi.mock('../lib/git', () => ({ refreshGitStatus: vi.fn().mockResolvedValue(undefined) }));

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.gitDiff.mockResolvedValue({
    Text: `diff --git a/a.ts b/a.ts
--- a/a.ts
+++ b/a.ts
@@ -1 +1 @@
-old
+new
`,
    Binary: false,
    Untracked: false,
  });
});

describe('GitDiffView', () => {
  it('渲染 hunk 与暂存此块', async () => {
    render(
      <GitDiffView wsPath="D:\\proj" repoRel="" path="a.ts" side="working" />,
    );
    expect(await screen.findByText('暂存此块')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '暂存此块' }));
    await waitFor(() => expect(mocks.gitStageHunk).toHaveBeenCalled());
  });

  it('discard hunk 取消确认则不调 API', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(
      <GitDiffView wsPath="D:\\proj" repoRel="" path="a.ts" side="working" />,
    );
    fireEvent.click(await screen.findByRole('button', { name: '丢弃此块' }));
    expect(mocks.gitDiscardHunk).not.toHaveBeenCalled();
  });
});
