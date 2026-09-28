// BasketBar 组件测试：条目展示 / 计数 / 移除同步（以 Go 返回值为准）。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import BasketBar from './BasketBar';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  toggleBasket: vi.fn(),
}));
vi.mock('../lib/api', () => mocks);

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ basket: [] });
});

describe('BasketBar', () => {
  it('空篮显示占位文案与计数', () => {
    render(<BasketBar />);
    expect(screen.getByText('未选择文件')).toBeInTheDocument();
    expect(screen.getByText('上下文篮（0/20）')).toBeInTheDocument();
  });

  it('展示 store 篮子条目（文件名 + 完整路径 title）', () => {
    useAppStore.setState({
      basket: ['D:\\proj-a\\src\\main.ts', 'D:\\proj-a\\README.md'],
    });
    render(<BasketBar />);

    expect(screen.getByText('上下文篮（2/20）')).toBeInTheDocument();
    expect(screen.getByText('main.ts')).toBeInTheDocument();
    expect(screen.getByText('README.md')).toBeInTheDocument();
    expect(screen.getByTitle('D:\\proj-a\\src\\main.ts')).toBeInTheDocument();
  });

  it('点移出调用 ToggleBasket，并按 Go 返回值（false）从 store 移除', async () => {
    useAppStore.setState({ basket: ['D:\\proj-a\\README.md'] });
    mocks.toggleBasket.mockResolvedValue(false);
    render(<BasketBar />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '移出 D:\\proj-a\\README.md' }));
    });

    expect(mocks.toggleBasket).toHaveBeenCalledWith('D:\\proj-a\\README.md');
    expect(useAppStore.getState().basket).not.toContain('D:\\proj-a\\README.md');
    expect(screen.getByText('未选择文件')).toBeInTheDocument();
  });

  it('移除后仍返回 true（理论上不该发生）时保持条目，不误删', async () => {
    useAppStore.setState({ basket: ['D:\\proj-a\\README.md'] });
    mocks.toggleBasket.mockResolvedValue(true);
    render(<BasketBar />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '移出 D:\\proj-a\\README.md' }));
    });

    expect(useAppStore.getState().basket).toContain('D:\\proj-a\\README.md');
  });

  it('store 的轻量提示（篮满/操作失败）在篮子栏行内展示', () => {
    useAppStore.setState({ message: '篮子已满（20 个文件），请先移出部分文件再加入' });
    render(<BasketBar />);

    expect(screen.getByText(/篮子已满/)).toBeInTheDocument();
  });
});
