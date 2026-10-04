// MarkdownPreview：GFM 渲染 heading 等基础元素。
import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import MarkdownPreview from './MarkdownPreview';

afterEach(cleanup);

describe('MarkdownPreview', () => {
  it('渲染 # Hi 为 heading「Hi」', () => {
    render(<MarkdownPreview markdown="# Hi" />);
    const heading = screen.getByRole('heading', { level: 1, name: 'Hi' });
    expect(heading).toBeInTheDocument();
  });
});
