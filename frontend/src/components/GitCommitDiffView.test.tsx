import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import GitCommitDiffView from './GitCommitDiffView';
import { tt } from '../test/i18n';

const mocks = vi.hoisted(() => ({
  gitCommitDiff: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.gitCommitDiff.mockResolvedValue({
    Text: `diff --git a/a.ts b/a.ts
--- a/a.ts
+++ b/a.ts
@@ -1 +1,2 @@
 one
+two
`,
    Binary: false,
    Untracked: false,
  });
});

describe('GitCommitDiffView', () => {
  it('只读渲染 commit diff，无 stage 按钮', async () => {
    render(<GitCommitDiffView wsPath="D:\\proj" repoRel="" hash="abcdef123456" />);
    expect(await screen.findByText('+two')).toBeInTheDocument();
    expect(screen.getByText('abcdef1')).toBeInTheDocument();
    expect(screen.queryByText(tt('ui.git.stage_hunk'))).not.toBeInTheDocument();
    expect(screen.queryByText(tt('ui.git.stage_file'))).not.toBeInTheDocument();
  });

  it('binary 时提示不可预览', async () => {
    mocks.gitCommitDiff.mockResolvedValue({ Text: '', Binary: true, Untracked: false });
    render(<GitCommitDiffView wsPath="D:\\proj" repoRel="" hash="abcdef123456" />);
    expect(await screen.findByText(tt('ui.git.binary_no_diff'))).toBeInTheDocument();
  });
});
