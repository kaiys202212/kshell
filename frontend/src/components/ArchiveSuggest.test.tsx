import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ArchiveSuggest from './ArchiveSuggest';

afterEach(cleanup);

describe('ArchiveSuggest', () => {
  it('展示摘要，确认与取消分开', () => {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(
      <ArchiveSuggest open summary="登录页空指针已修好" onConfirm={onConfirm} onClose={onClose} />,
    );
    expect(screen.getByText('登录页空指针已修好')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '归档' }));
    expect(onConfirm).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole('button', { name: '继续' }));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
