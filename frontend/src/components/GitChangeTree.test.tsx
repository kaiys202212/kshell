import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { GitChangeTree } from './GitChangeTree';
import type { GitSCMEntry } from '../lib/api';
import { tt } from '../test/i18n';

afterEach(cleanup);

const e = (path: string): GitSCMEntry => ({
  Path: path,
  X: ' ',
  Y: 'M',
  Staged: false,
  Unstaged: true,
  Untracked: false,
  Conflicted: false,
});

describe('GitChangeTree', () => {
  it('目录加号递归暂存全部叶子', () => {
    const onStage = vi.fn();
    render(
      <GitChangeTree
        title="更改"
        entries={[e('pkg/a.go'), e('pkg/b.go')]}
        side="working"
        onOpen={() => {}}
        onStage={onStage}
        onUnstage={null}
        onDiscard={null}
      />,
    );
    fireEvent.click(
      screen.getByRole('button', { name: tt('ui.git.stage_aria').replace('{{path}}', 'pkg') }),
    );
    expect(onStage).toHaveBeenCalledWith(['pkg/a.go', 'pkg/b.go']);
  });

  it('目录行用文件夹图标、文件行用文件图标', () => {
    const { container } = render(
      <GitChangeTree
        title="更改"
        entries={[e('pkg/a.go')]}
        side="working"
        onOpen={() => {}}
        onStage={null}
        onUnstage={null}
        onDiscard={null}
      />,
    );
    expect(container.querySelector('[data-icon="folder-open"], [data-icon="folder"]')).toBeTruthy();
    expect(container.querySelector('[data-icon="file"]')).toBeTruthy();
    expect(container.querySelector('[data-icon="chevron"]')).toBeTruthy();
  });
});
