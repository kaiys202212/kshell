// FileTree 组件测试：目录懒加载（展开才调 ListFiles、relPath 用 / 拼接）、
// 点文件回调、加载错误提示、
// 搜索（防抖后走后端递归搜索出平铺结果）、行内重命名（树重建）、git 状态标记。
// api 层整体打桩（vi.mock），与 SessionList.test 同一套模式。
import '@testing-library/jest-dom/vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import FileTree from './FileTree';
import type { FileNode } from '../lib/api';
import { DRAG_MIME, REL_MIME } from '../lib/dragPath';
import { useAppStore } from '../state/store';

const mocks = vi.hoisted(() => ({
  listFiles: vi.fn(),
  searchFiles: vi.fn(),
  renameEntry: vi.fn(),
  createEntry: vi.fn(),
  deleteEntry: vi.fn(),
  moveEntry: vi.fn(),
  gitStatus: vi.fn(),
  refreshFiles: vi.fn().mockResolvedValue(undefined),
  startFileWatch: vi.fn().mockResolvedValue(undefined),
  stopFileWatch: vi.fn(),
  onFilesChanged: vi.fn<(cb: (path: string) => void) => () => void>(() => () => {}),
  revealInExplorer: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../lib/api', () => mocks);

// jsdom 没有 Clipboard：stub writeText 供「复制路径」断言
const writeText = vi.fn().mockResolvedValue(undefined);

const node = (name: string, isDir: boolean, rel: string): FileNode => ({
  Name: name,
  Path: `D:\\proj\\${rel.replace(/\//g, '\\')}`,
  IsDir: isDir,
  Expanded: false,
  Loaded: false,
});

const root: FileNode[] = [node('src', true, 'src'), node('README.md', false, 'README.md')];
const srcChildren: FileNode[] = [
  node('lib', true, 'src/lib'),
  node('main.ts', false, 'src/main.ts'),
];

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState({ toasts: [], gitStatus: {} });
  mocks.gitStatus.mockResolvedValue({ Status: {}, IsRepo: false }); // 挂载时刷新静默通过
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    configurable: true,
  });
  writeText.mockClear();
});

describe('FileTree', () => {
  it('挂载时只请求根层（relPath 为空串），目录未展开前不发起子层请求', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    expect(await screen.findByText('README.md')).toBeInTheDocument();
    expect(mocks.listFiles).toHaveBeenCalledTimes(1);
    expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', '', false);
    expect(screen.queryByText('main.ts')).not.toBeInTheDocument();
  });

  it('点击目录懒加载子层，嵌套目录 relPath 用 / 拼接', async () => {
    mocks.listFiles.mockResolvedValueOnce(root).mockResolvedValueOnce(srcChildren);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.click(await screen.findByText('src'));
    expect(await screen.findByText('main.ts')).toBeInTheDocument();
    expect(mocks.listFiles).toHaveBeenLastCalledWith('D:\\proj', 'src', false);

    mocks.listFiles.mockResolvedValueOnce([node('tree.go', false, 'src/lib/tree.go')]);
    fireEvent.click(screen.getByText('lib'));
    expect(await screen.findByText('tree.go')).toBeInTheDocument();
    expect(mocks.listFiles).toHaveBeenLastCalledWith('D:\\proj', 'src/lib', false);
  });

  it('收起再展开已加载的目录不重复请求（子层缓存）', async () => {
    mocks.listFiles.mockResolvedValueOnce(root).mockResolvedValueOnce(srcChildren);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.click(await screen.findByText('src'));
    await screen.findByText('main.ts');
    fireEvent.click(screen.getByText('src')); // 收起
    expect(screen.queryByText('main.ts')).not.toBeInTheDocument();
    fireEvent.click(screen.getByText('src')); // 再展开
    expect(await screen.findByText('main.ts')).toBeInTheDocument();
    expect(mocks.listFiles).toHaveBeenCalledTimes(2);
  });

  it('点文件回调 onOpenFile（绝对路径）', async () => {
    mocks.listFiles.mockResolvedValue(root);
    const onOpen = vi.fn();
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={onOpen} />);

    fireEvent.click(await screen.findByText('README.md'));
    expect(onOpen).toHaveBeenCalledWith('D:\\proj\\README.md');
  });

  it('双击文件回调 onEditFile', async () => {
    mocks.listFiles.mockResolvedValue(root);
    const onOpen = vi.fn();
    const onEdit = vi.fn();
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={onOpen} onEditFile={onEdit} />);

    fireEvent.doubleClick(await screen.findByText('README.md'));
    expect(onEdit).toHaveBeenCalledWith('D:\\proj\\README.md');
  });

  it('ListFiles 失败时显示错误提示', async () => {
    mocks.listFiles.mockRejectedValue(new Error('路径越出工作区范围'));
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    expect(await screen.findByText(/路径越出工作区范围/)).toBeInTheDocument();
  });

  it('工作区没有可显示的文件时仍展示虚拟根目录', async () => {
    mocks.listFiles.mockResolvedValue([]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    expect(await screen.findByText('proj')).toBeInTheDocument();
    expect(screen.queryByText('没有可显示的文件')).not.toBeInTheDocument();
  });

  it('子目录加载失败：只在目标目录行内提示 + 重试入口，不整树替换', async () => {
    mocks.listFiles
      .mockResolvedValueOnce(root)
      .mockRejectedValueOnce(new Error('子目录读取失败'));
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.click(await screen.findByText('src'));
    // 错误出现在 src 目录行内（带重试按钮），根层其他条目仍在
    expect(await screen.findByText(/子目录读取失败/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重试加载 src' })).toBeInTheDocument();
    expect(screen.getByText('README.md')).toBeInTheDocument();

    // 重试成功后子层正常加载，错误消失
    mocks.listFiles.mockResolvedValueOnce(srcChildren);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '重试加载 src' }));
    });
    expect(await screen.findByText('main.ts')).toBeInTheDocument();
    expect(screen.queryByText(/子目录读取失败/)).not.toBeInTheDocument();
  });

  it('git 标记：按 relPath 渲染状态色标，嵌套文件展开后也能渲染', async () => {
    // 挂载时会调 GitStatus 刷新镜像，mock 直接返回状态（避免预置 store 被覆盖）
    mocks.gitStatus.mockResolvedValue({
      Status: { 'README.md': 'modified', 'src/main.ts': 'untracked' },
      IsRepo: true,
    });
    mocks.listFiles.mockResolvedValueOnce(root).mockResolvedValueOnce(srcChildren);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    await screen.findByText('README.md');
    expect(await screen.findByText('M')).toBeInTheDocument();
    expect(screen.getByTitle('git：已修改')).toBeInTheDocument();
    // 未加载的子层不渲染（懒加载）
    expect(screen.queryByText('N')).not.toBeInTheDocument();
    // 展开子目录后，嵌套文件按自身 relPath 渲染色标
    fireEvent.click(screen.getByText('src'));
    expect(await screen.findByText('N')).toBeInTheDocument();
  });

  it('显示全部：切换后根层与已展开目录均带 showAll=true', async () => {
    mocks.listFiles.mockResolvedValueOnce(root).mockResolvedValueOnce(srcChildren);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');
    fireEvent.click(screen.getByText('src'));
    await screen.findByText('main.ts');
    mocks.listFiles.mockClear();
    mocks.listFiles.mockImplementation(async (_ws, rel) => (rel === 'src' ? srcChildren : root));
    fireEvent.click(screen.getByRole('checkbox', { name: '显示全部' }));
    await waitFor(() => {
      expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', '', true);
      expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', 'src', true);
    });
  });

  it('git 标记：未跟踪为 N，忽略为 I 且行淡化', async () => {
    mocks.gitStatus.mockResolvedValue({
      Status: { 'README.md': 'untracked', 'skip.log': 'ignored' },
      IsRepo: true,
    });
    mocks.listFiles.mockResolvedValue([
      node('README.md', false, 'README.md'),
      node('skip.log', false, 'skip.log'),
    ]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    expect(await screen.findByText('N')).toBeInTheDocument();
    expect(screen.getByTitle('git：未跟踪')).toBeInTheDocument();
    expect(screen.getByText('I')).toBeInTheDocument();
    expect(screen.getByTitle('git：已忽略')).toBeInTheDocument();
    expect(screen.getByText('skip.log').className).toMatch(/opacity|muted/);
  });

  it('搜索：输入防抖后调 searchFiles，结果平铺展示（文件名 + 所在目录），点文件回调', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.searchFiles.mockResolvedValue([
      { Name: 'main.go', Path: 'D:\\proj\\src\\main.go', IsDir: false, Expanded: false, Loaded: false, RelPath: 'src/main.go' },
    ]);
    const onOpen = vi.fn();
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={onOpen} />);
    await screen.findByText('README.md');

    fireEvent.change(screen.getByRole('textbox', { name: '搜索文件' }), {
      target: { value: 'main' },
    });
    // 防抖 200ms + 渲染，findBy 默认 1s 超时足够
    expect(await screen.findByText('main.go')).toBeInTheDocument();
    expect(screen.getByText('src')).toBeInTheDocument(); // 所在目录后缀
    expect(mocks.searchFiles).toHaveBeenCalledWith('D:\\proj', 'main', false);

    fireEvent.click(screen.getByText('main.go'));
    expect(onOpen).toHaveBeenCalledWith('D:\\proj\\src\\main.go');
  });

  it('刷新按钮：作废缓存并重建树（恢复展开）', async () => {
    mocks.listFiles
      .mockResolvedValueOnce(root)
      .mockResolvedValueOnce(srcChildren)
      .mockResolvedValueOnce(root)
      .mockResolvedValueOnce(srcChildren);
    mocks.refreshFiles.mockResolvedValue(undefined);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');
    fireEvent.click(screen.getByText('src'));
    await screen.findByText('main.ts');

    fireEvent.click(screen.getByRole('button', { name: '刷新文件树' }));
    await waitFor(() => {
      expect(mocks.refreshFiles).toHaveBeenCalledWith('D:\\proj');
    });
    await waitFor(() => {
      expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', '', false);
      expect(mocks.listFiles).toHaveBeenCalledWith('D:\\proj', 'src', false);
    });
    expect(await screen.findByText('main.ts')).toBeInTheDocument();
  });

  it('files:changed 匹配 wsPath 时触发重载', async () => {
    let changedCb: ((p: string) => void) | null = null;
    mocks.onFilesChanged.mockImplementation((cb) => {
      changedCb = cb;
      return () => {
        changedCb = null;
      };
    });
    mocks.listFiles.mockResolvedValue(root);
    mocks.refreshFiles.mockResolvedValue(undefined);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');
    const calls = mocks.listFiles.mock.calls.length;
    await act(async () => {
      // 分隔符/大小写不同也应匹配（sameWorkspacePath）
      changedCb?.('D:/proj');
    });
    await waitFor(() => {
      expect(mocks.refreshFiles).toHaveBeenCalled();
      expect(mocks.listFiles.mock.calls.length).toBeGreaterThan(calls);
    });
  });

  it('挂载调 startFileWatch，卸载调 stopFileWatch', async () => {
    mocks.listFiles.mockResolvedValue(root);
    const { unmount } = render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');
    await waitFor(() => {
      expect(mocks.startFileWatch).toHaveBeenCalledWith('D:\\proj');
    });
    expect(mocks.onFilesChanged).toHaveBeenCalled();
    unmount();
    expect(mocks.stopFileWatch).toHaveBeenCalledWith('D:\\proj');
  });

  it('StartFileWatch 失败时弹出 error toast', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.startFileWatch.mockRejectedValueOnce(new Error('监视不可用'));
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');
    await waitFor(() => {
      expect(useAppStore.getState().toasts.some((t) => t.tone === 'error' && t.title.includes('监视不可用'))).toBe(
        true,
      );
    });
  });

  it('搜索空结果给空态文案，清空搜索恢复树', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.searchFiles.mockResolvedValue([]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');

    const input = screen.getByRole('textbox', { name: '搜索文件' });
    fireEvent.change(input, { target: { value: '不存在' } });
    expect(await screen.findByText(/没有匹配「不存在」的文件/)).toBeInTheDocument();

    fireEvent.change(input, { target: { value: '' } });
    expect(await screen.findByText('README.md')).toBeInTheDocument();
    expect(screen.queryByText(/没有匹配/)).not.toBeInTheDocument();
  });

  it('行内重命名：铅笔按钮 → input → Enter 提交；Go 返回新路径后重建树', async () => {
    mocks.listFiles
      .mockResolvedValueOnce(root)
      .mockResolvedValueOnce([node('RENAMED.md', false, 'RENAMED.md')]);
    mocks.renameEntry.mockResolvedValue('D:\\proj\\RENAMED.md');
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');

    fireEvent.click(screen.getByRole('button', { name: '重命名 README.md' }));
    const input = screen.getByRole('textbox', { name: '重命名 README.md' });
    expect(input).toHaveValue('README.md');

    fireEvent.change(input, { target: { value: 'RENAMED.md' } });
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' });
    });

    expect(mocks.renameEntry).toHaveBeenCalledWith('D:\\proj', 'README.md', 'RENAMED.md');
    expect(useAppStore.getState().toasts.some((t) => t.tone === 'success')).toBe(true);
    expect(await screen.findByText('RENAMED.md')).toBeInTheDocument();
  });

  it('重命名失败（如目标已存在）提示错误且不重建树', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.renameEntry.mockRejectedValue(new Error('目标已存在'));
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');

    fireEvent.click(screen.getByRole('button', { name: '重命名 README.md' }));
    const input = screen.getByRole('textbox', { name: '重命名 README.md' });
    fireEvent.change(input, { target: { value: 'readme.md' } });
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' });
    });

    expect(useAppStore.getState().toasts.some((t) => t.title.includes('目标已存在'))).toBe(true);
    expect(screen.getByText('README.md')).toBeInTheDocument();
  });
});

describe('FileTree 右键菜单', () => {
  it('右键目录行弹出菜单（含新建文件夹/删除），右键文件行不含新建文件夹', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    fireEvent.contextMenu(await screen.findByText('src'));

    const menu = screen.getByRole('menu');
    expect(menu).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '新建文件夹' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '新建文件' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '删除' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '重命名' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '复制路径' })).toBeInTheDocument();

    // 关闭后右键文件行：不含新建两项
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    fireEvent.contextMenu(screen.getByText('README.md'));
    expect(screen.queryByRole('menuitem', { name: '新建文件夹' })).not.toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '重命名' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '复制路径' })).toBeInTheDocument();
  });

  it('Esc 关闭菜单', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    fireEvent.contextMenu(await screen.findByText('src'));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('右键菜单含在资源管理器中打开并调用 revealInExplorer', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');
    fireEvent.contextMenu(screen.getByText('README.md'));
    const item = await screen.findByRole('menuitem', { name: '在资源管理器中打开' });
    await act(async () => {
      fireEvent.pointerDown(item);
    });
    expect(mocks.revealInExplorer).toHaveBeenCalledWith('D:\\proj', expect.stringContaining('README.md'));
  });

  it('空白处右键菜单不含在资源管理器中打开', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');
    fireEvent.contextMenu(screen.getByRole('tree', { name: '工作区文件树' }));
    expect(screen.queryByRole('menuitem', { name: '在资源管理器中打开' })).not.toBeInTheDocument();
  });

  it('复制路径：菜单项把绝对路径写入剪贴板并提示成功', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    fireEvent.contextMenu(await screen.findByText('README.md'));

    await act(async () => {
      fireEvent.pointerDown(screen.getByRole('menuitem', { name: '复制路径' }));
    });
    expect(writeText).toHaveBeenCalledWith('D:\\proj\\README.md');
    expect(useAppStore.getState().toasts.some((t) => t.tone === 'success')).toBe(true);
  });

  it('删除：确认框出现，确认后以 (wsPath, relPath) 调 deleteEntry 并刷新树', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.deleteEntry.mockResolvedValue(undefined);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');
    const callsBefore = mocks.listFiles.mock.calls.length;

    fireEvent.contextMenu(screen.getByText('README.md'));
    fireEvent.pointerDown(screen.getByRole('menuitem', { name: '删除' }));
    expect(await screen.findByText('删除确认')).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '删除' }));
    });
    expect(mocks.deleteEntry).toHaveBeenCalledWith('D:\\proj', 'README.md');
    expect(useAppStore.getState().toasts.some((t) => t.title.includes('已删除'))).toBe(true);
    // 树已重建（refreshRoot 多调一次 listFiles）
    expect(mocks.listFiles.mock.calls.length).toBeGreaterThan(callsBefore);
    expect(screen.queryByText('删除确认')).not.toBeInTheDocument();
  });

  it('删除确认框取消不调 deleteEntry', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    fireEvent.contextMenu(await screen.findByText('README.md'));
    fireEvent.pointerDown(screen.getByRole('menuitem', { name: '删除' }));
    expect(await screen.findByText('删除确认')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    expect(mocks.deleteEntry).not.toHaveBeenCalled();
    expect(screen.queryByText('删除确认')).not.toBeInTheDocument();
  });

  it('右键空白新建文件：输入名称回车后以 (wsPath, "", name, false) 调 createEntry', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.createEntry.mockResolvedValue('D:\\proj\\new.go');
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');

    fireEvent.contextMenu(screen.getByRole('tree', { name: '工作区文件树' }));
    fireEvent.pointerDown(screen.getByRole('menuitem', { name: '新建文件' }));
    const input = screen.getByPlaceholderText('文件名');
    fireEvent.change(input, { target: { value: 'new.go' } });
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' });
    });

    expect(mocks.createEntry).toHaveBeenCalledWith('D:\\proj', '', 'new.go', false);
    expect(useAppStore.getState().toasts.some((t) => t.tone === 'success')).toBe(true);
  });

  it('右键目录行新建文件夹：以该目录为 dirRel 调 createEntry', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.createEntry.mockResolvedValue('D:\\proj\\src\\sub');
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    fireEvent.contextMenu(await screen.findByText('src'));

    fireEvent.pointerDown(screen.getByRole('menuitem', { name: '新建文件夹' }));
    const input = screen.getByPlaceholderText('文件夹名');
    fireEvent.change(input, { target: { value: 'sub' } });
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' });
    });

    expect(mocks.createEntry).toHaveBeenCalledWith('D:\\proj', 'src', 'sub', true);
  });

  it('新建空名不提交，Esc 取消收起输入', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');

    // 右键树空白处 → 新建文件（根目录）
    fireEvent.contextMenu(screen.getByRole('tree', { name: '工作区文件树' }));
    fireEvent.pointerDown(screen.getByRole('menuitem', { name: '新建文件' }));
    const input = screen.getByPlaceholderText('文件名');

    fireEvent.keyDown(input, { key: 'Enter' });
    expect(mocks.createEntry).not.toHaveBeenCalled();
    expect(screen.getByPlaceholderText('文件名')).toBeInTheDocument();

    fireEvent.keyDown(input, { key: 'Escape' });
    expect(screen.queryByPlaceholderText('文件名')).not.toBeInTheDocument();
  });

  it('右键根容器空白（列表下方区域）弹出新建菜单', async () => {
    mocks.listFiles.mockResolvedValue(root);
    const { container } = render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');

    // 空白右键 handler 挂在根 div 上（覆盖列表下方空白）；
    // 行内 handler 已 stopPropagation，这里直接对根 div 派发验证兜底菜单
    fireEvent.contextMenu(container.firstElementChild as HTMLElement);
    expect(screen.getByRole('menu')).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '新建文件' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '新建文件夹' })).toBeInTheDocument();
  });

  it('新建失败（如名称非法）提示错误且保留输入行可重试', async () => {
    mocks.listFiles.mockResolvedValue(root);
    mocks.createEntry.mockRejectedValue(new Error('名称不合法'));
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('README.md');

    fireEvent.contextMenu(screen.getByRole('tree', { name: '工作区文件树' }));
    fireEvent.pointerDown(screen.getByRole('menuitem', { name: '新建文件' }));
    const input = screen.getByPlaceholderText('文件名');
    fireEvent.change(input, { target: { value: 'a/b' } });
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' });
    });

    expect(useAppStore.getState().toasts.some((t) => t.title.includes('名称不合法'))).toBe(true);
    expect(screen.getByPlaceholderText('文件名')).toBeInTheDocument();
  });

  it('菜单「重命名」触发行内重命名输入', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    fireEvent.contextMenu(await screen.findByText('README.md'));
    fireEvent.pointerDown(screen.getByRole('menuitem', { name: '重命名' }));

    const input = screen.getByRole('textbox', { name: '重命名 README.md' });
    expect(input).toHaveValue('README.md');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });
});

describe('FileTree 树内拖拽移动', () => {
  const moveRoot: FileNode[] = [node('main.go', false, 'main.go'), node('pkg', true, 'pkg')];

  // jsdom 不实现 DataTransfer：合成事件对象代替（含 types/getData/preventDefault 等）
  const makeDataTransfer = (rel: string) =>
    ({
      types: [DRAG_MIME, REL_MIME],
      getData: (t: string) => (t === REL_MIME ? rel : `D:\\proj\\${rel.replace(/\//g, '\\')}`),
      setData: vi.fn(),
      effectAllowed: 'move',
      dropEffect: 'move',
      preventDefault: vi.fn(),
      stopPropagation: vi.fn(),
    }) as unknown as DataTransfer;

  it('文件拖到目录行松手：以 (wsPath, srcRel, dstDirRel) 调 moveEntry 并提示成功', async () => {
    mocks.listFiles.mockResolvedValue(moveRoot);
    mocks.moveEntry.mockResolvedValue('D:\\proj\\pkg\\main.go');
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.drop(await screen.findByText('pkg'), { dataTransfer: makeDataTransfer('main.go') });
    await act(async () => {}); // flush promise

    expect(mocks.moveEntry).toHaveBeenCalledWith('D:\\proj', 'main.go', 'pkg');
    expect(useAppStore.getState().toasts.some((t) => t.tone === 'success')).toBe(true);
  });

  it('拖到自身子孙目录行：moveEntry 不被调用', async () => {
    mocks.listFiles
      .mockResolvedValueOnce(moveRoot)
      .mockResolvedValueOnce([node('sub', true, 'pkg/sub')]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.click(await screen.findByText('pkg'));
    fireEvent.drop(await screen.findByText('sub'), { dataTransfer: makeDataTransfer('pkg') });
    await act(async () => {});

    expect(mocks.moveEntry).not.toHaveBeenCalled();
    expect(useAppStore.getState().toasts.some((t) => t.tone === 'success')).toBe(false);
  });

  it('拖到自身目录行：moveEntry 不被调用', async () => {
    mocks.listFiles.mockResolvedValue(moveRoot);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);

    fireEvent.drop(await screen.findByText('pkg'), { dataTransfer: makeDataTransfer('pkg') });
    await act(async () => {});

    expect(mocks.moveEntry).not.toHaveBeenCalled();
  });

  it('默认展开虚拟根目录，名称取工作区文件夹名', async () => {
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    expect(await screen.findByText('proj')).toBeInTheDocument();
    expect(screen.getByText('README.md')).toBeInTheDocument();
  });

  it('git 仓库虚拟根与嵌套 git 根显示分支名', async () => {
    mocks.gitStatus.mockResolvedValue({
      Status: {},
      IsRepo: true,
      Branch: 'main',
      DirBranches: { '': 'main', ext: 'dev' },
    });
    mocks.listFiles.mockResolvedValue([node('ext', true, 'ext'), node('README.md', false, 'README.md')]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    expect(await screen.findByTitle('git 分支 main')).toBeInTheDocument();
    expect(await screen.findByTitle('git 分支 dev')).toBeInTheDocument();
  });

  it('ignored 目录下展开的子项继承淡化与 I', async () => {
    mocks.gitStatus.mockResolvedValue({
      Status: { vendor: 'ignored' },
      IsRepo: true,
    });
    mocks.listFiles
      .mockResolvedValueOnce([node('vendor', true, 'vendor')])
      .mockResolvedValueOnce([node('pkg', true, 'vendor/pkg')]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('vendor');
    fireEvent.click(screen.getByText('vendor'));
    expect(await screen.findByText('pkg')).toBeInTheDocument();
    expect(screen.getByText('pkg').className).toMatch(/opacity|muted/);
    expect(screen.getAllByTitle('git：已忽略').length).toBeGreaterThanOrEqual(2);
  });

  it('目录 untracked 且子全 ignored 时目录呈 ignored（非 N）', async () => {
    mocks.gitStatus.mockResolvedValue({
      Status: { '.cursor': 'untracked', '.cursor/rules.md': 'ignored' },
      IsRepo: true,
    });
    mocks.listFiles.mockResolvedValue([node('.cursor', true, '.cursor')]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    expect(await screen.findByText('.cursor')).toBeInTheDocument();
    expect(screen.getByText('.cursor').className).toMatch(/opacity|muted/);
    expect(screen.getByTitle('git：已忽略')).toBeInTheDocument();
    expect(screen.queryByTitle('git：未跟踪')).not.toBeInTheDocument();
  });

  it('含嵌套 git 的中间层显示 ⊞，嵌套根仅分支', async () => {
    mocks.gitStatus.mockResolvedValue({
      Status: {},
      IsRepo: true,
      Branch: 'main',
      DirBranches: { '': 'main', 'ext/lib': 'dev' },
    });
    mocks.listFiles
      .mockResolvedValueOnce([node('ext', true, 'ext')])
      .mockResolvedValueOnce([node('lib', true, 'ext/lib')]);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('ext');
    expect(screen.getByTitle('含嵌套 git')).toHaveTextContent('⊞');
    fireEvent.click(screen.getByText('ext'));
    expect(await screen.findByTitle('git 分支 dev')).toBeInTheDocument();
    expect(screen.getByText('lib').closest('button')?.textContent).not.toMatch(/⊞/);
  });

  it('文件夹子树有改动时打统一圆点，文件仍用字母色标', async () => {
    mocks.gitStatus.mockResolvedValue({
      Status: { 'src/main.ts': 'modified' },
      IsRepo: true,
      Branch: 'main',
      DirBranches: { '': 'main' },
    });
    mocks.listFiles.mockResolvedValue(root);
    render(<FileTree wsPath={'D:\\proj'} onOpenFile={() => {}} />);
    await screen.findByText('src');
    expect(screen.getAllByLabelText('有未提交改动').length).toBeGreaterThanOrEqual(2);
    expect(screen.queryByText('M')).not.toBeInTheDocument();
  });
});
