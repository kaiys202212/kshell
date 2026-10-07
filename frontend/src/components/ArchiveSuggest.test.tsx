import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ArchiveSuggest from './ArchiveSuggest';
import { tt } from '../test/i18n';
import en from '../locales/en.json';

afterEach(cleanup);

describe('ArchiveSuggest', () => {
  it('展示摘要，确认与取消分开', () => {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(
      <ArchiveSuggest open summary="登录页空指针已修好" onConfirm={onConfirm} onClose={onClose} />,
    );
    expect(screen.getByText('登录页空指针已修好')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.archive_suggest.archive') }));
    expect(onConfirm).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.archive_suggest.continue') }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('摘要为空时回退到默认文案', () => {
    render(<ArchiveSuggest open summary="" onConfirm={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByText(tt('ui.archive_suggest.default_summary'))).toBeInTheDocument();
  });

  it('摘要为后端注册 key 时经 translateBackend 翻译', () => {
    render(
      <ArchiveSuggest open summary="err.session_not_found" onConfirm={vi.fn()} onClose={vi.fn()} />,
    );
    expect(screen.getByText(en.err.session_not_found)).toBeInTheDocument();
  });
});
