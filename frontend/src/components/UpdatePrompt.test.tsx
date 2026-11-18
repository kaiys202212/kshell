import '@testing-library/jest-dom/vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import UpdatePrompt from './UpdatePrompt';
import { tt } from '../test/i18n';

const info = {
  Current: 'v0.1.0',
  Latest: 'v0.2.0',
  Notes: '修复拉起',
  Source: 'gitcode',
  Available: true,
  Ready: true,
  Skipped: false,
  Reason: '',
};

describe('UpdatePrompt', () => {
  it('展示已就绪版本并支持安装与稍后', () => {
    const onLater = vi.fn();
    const onUpgrade = vi.fn();
    render(
      <UpdatePrompt info={info} busy={false} error="" onLater={onLater} onUpgrade={onUpgrade} />,
    );
    expect(
      screen.getByText(tt('ui.update_prompt.ready_title').replace('{{version}}', 'v0.2.0')),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.update_prompt.later') }));
    expect(onLater).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: tt('ui.update_prompt.install_restart') }));
    expect(onUpgrade).toHaveBeenCalledTimes(1);
  });

  it('安装中禁用按钮并显示错误', () => {
    render(
      <UpdatePrompt info={info} busy error="校验失败" onLater={() => {}} onUpgrade={() => {}} />,
    );
    expect(screen.getByRole('button', { name: tt('ui.update_prompt.later') })).toBeDisabled();
    expect(screen.getByRole('button', { name: tt('ui.update_prompt.installing') })).toBeDisabled();
    expect(screen.getByText('校验失败')).toBeInTheDocument();
  });
});
