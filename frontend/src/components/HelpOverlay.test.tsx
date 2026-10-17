// HelpOverlay 测试：open=false 不渲染、open=true 渲染标题与全部键位（数据源自
// SHORTCUTS 表）、Esc 触发 onClose。文案断言走 tt()，不硬编码语言字面量。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import HelpOverlay from './HelpOverlay';
import { SHORTCUTS } from '../lib/keybindings';
import { tt } from '../test/i18n';

afterEach(cleanup);

describe('HelpOverlay', () => {
  it('open=false 时不渲染任何内容', () => {
    const { container } = render(<HelpOverlay open={false} onClose={() => {}} />);
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByText(tt('ui.help.title'))).not.toBeInTheDocument();
  });

  it('open=true 渲染标题与全部 SHORTCUTS 键位（键名 + 名称 + 说明）', () => {
    render(<HelpOverlay open onClose={() => {}} />);
    expect(screen.getByText(tt('ui.help.title'))).toBeInTheDocument();
    for (const s of SHORTCUTS) {
      expect(screen.getByText(s.keys)).toBeInTheDocument();
      expect(screen.getByText(tt(`ui.help.shortcuts.${s.id}_name`))).toBeInTheDocument();
      expect(screen.getByText(tt(`ui.help.shortcuts.${s.id}_desc`))).toBeInTheDocument();
    }
  });

  it('按 Esc 触发 onClose', () => {
    const onClose = vi.fn();
    render(<HelpOverlay open onClose={onClose} />);
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('关闭状态下不监听 Esc（open=false 时按 Esc 不回调）', () => {
    const onClose = vi.fn();
    render(<HelpOverlay open={false} onClose={onClose} />);
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
  });
});
