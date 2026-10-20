// 自动滚动：默认开启时新消息把消息流滚到底；取消勾选暂停；重新勾选立即滚底；
// 状态持久化到 localStorage。
import '@testing-library/jest-dom/vitest';
import { act } from '@testing-library/react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatInfo } from '../lib/api';
import { useAppStore } from '../state/store';
import '../test/i18n';
import { tt } from '../test/i18n';
import AgentHubPanel from './AgentHubPanel';

const api = vi.hoisted(() => ({
  sendChatPrompt: vi.fn(),
  openWorkspaceACP: vi.fn(),
  getTools: vi.fn(),
  getModelConfig: vi.fn(),
  readClipboardPaste: vi.fn(),
  listChatCommands: vi.fn(),
  listChatModels: vi.fn(),
  setChatModel: vi.fn(),
  cancelChat: vi.fn(),
  closeChat: vi.fn(),
  listHubMessages: vi.fn(),
  listHubArchive: vi.fn(),
  loadHubArchive: vi.fn(),
  clearHubMessages: vi.fn(),
  recallHubMessage: vi.fn(),
}));
vi.mock('../lib/api', () => api);

function chat(over: Partial<ChatInfo>): ChatInfo {
  return {
    ID: 'c1',
    Key: 'new:1',
    Kind: 'new',
    SessionID: '',
    Workspace: 'D:\\ws\\a',
    Title: '重构认证',
    ToolID: 'claude',
    Status: 'ready',
    ExitCode: 0,
    Error: '',
    Prompted: true,
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.getTools.mockResolvedValue([]);
  api.getModelConfig.mockResolvedValue({
    Enabled: false,
    Platforms: [],
    DefaultPlatform: '',
    ActiveModel: '',
  });
  api.listHubMessages.mockResolvedValue([]);
  api.listHubArchive.mockResolvedValue([]);
  api.loadHubArchive.mockResolvedValue([]);
  useAppStore.setState({
    chats: [],
    hubMessages: [],
    workspaces: [],
    openTabs: [],
    activeTabId: null,
    lastAgentId: null,
    focusTermKey: null,
    toasts: [],
  });
});

afterEach(() => {
  cleanup();
  localStorage.clear();
});

function logEl() {
  return screen.getByRole('log') as HTMLDivElement;
}

// mock 容器滚动几何：jsdom 无布局，scrollHeight 恒 0；用 defineProperty 提供可断言的值。
function mockScrollGeometry(el: HTMLElement, height: number) {
  Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => height });
  const state = { top: 0 };
  Object.defineProperty(el, 'scrollTop', {
    configurable: true,
    get: () => state.top,
    set: (v: number) => {
      state.top = v;
    },
  });
  return state;
}

// 滚底走 double rAF（等 markdown 异步布局），jsdom 不驱动帧回调：flushRaf 执行两帧。
// 组件另挂 ResizeObserver 兜底，本测试不模拟（jsdom 无布局，观察不到高度变化）。
let rafQueue: FrameRequestCallback[] = [];
function flushRaf() {
  const q = rafQueue;
  rafQueue = [];
  q.forEach((cb) => cb(0));
}

beforeEach(() => {
  rafQueue = [];
  // cancel 真出队：否则关掉自动滚动前的旧 rAF 残留，flush 时误滚
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    rafQueue.push(cb);
    return rafQueue.length;
  });
  vi.stubGlobal('cancelAnimationFrame', (id: number) => {
    delete rafQueue[id - 1];
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  rafQueue = [];
});

describe('AgentHubPanel 自动滚动', () => {
  beforeEach(() => {
    localStorage.removeItem('kshell.hub.autoScroll');
    rafQueue = [];
  });

  it('默认开启：hubMessages 变化时滚到底（double rAF 后），且渲染自动滚动复选框', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })] });
    render(<AgentHubPanel />);
    const box = screen.getByTestId('hub-auto-scroll') as HTMLInputElement;
    expect(box.checked).toBe(true);
    const el = logEl();
    const state = mockScrollGeometry(el, 900);
    act(() => {
      useAppStore.setState({ hubMessages: [{ id: 1, kind: 'agent', tool: 'claude', text: 'r', ts: 1 }] });
    });
    flushRaf();
    flushRaf();
    expect(state.top).toBe(900);
  });

  it('取消勾选：新消息不再滚动', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })] });
    render(<AgentHubPanel />);
    const box = screen.getByTestId('hub-auto-scroll') as HTMLInputElement;
    fireEvent.click(box);
    expect(box.checked).toBe(false);
    const el = logEl();
    const state = mockScrollGeometry(el, 900);
    act(() => {
      useAppStore.setState({ hubMessages: [{ id: 1, kind: 'agent', tool: 'claude', text: 'r', ts: 1 }] });
    });
    flushRaf();
    flushRaf();
    expect(state.top).toBe(0);
    // 状态持久化
    expect(JSON.parse(localStorage.getItem('kshell.hub.autoScroll') ?? 'null')).toBe(false);
  });

  it('重新勾选：立即滚到底', () => {
    useAppStore.setState({ chats: [chat({ ID: 'c1' })] });
    render(<AgentHubPanel />);
    const box = screen.getByTestId('hub-auto-scroll') as HTMLInputElement;
    fireEvent.click(box); // 关
    fireEvent.click(box); // 开
    const el = logEl();
    const state = mockScrollGeometry(el, 500);
    act(() => {
      useAppStore.setState({ hubMessages: [{ id: 2, kind: 'agent', tool: 'claude', text: 'r', ts: 2 }] });
    });
    flushRaf();
    flushRaf();
    expect(state.top).toBe(500);
  });

  it('上次关闭的偏好被记住：重开面板仍不自动滚动', () => {
    localStorage.setItem('kshell.hub.autoScroll', 'false');
    useAppStore.setState({ chats: [chat({ ID: 'c1' })] });
    render(<AgentHubPanel />);
    const box = screen.getByTestId('hub-auto-scroll') as HTMLInputElement;
    expect(box.checked).toBe(false);
    const el = logEl();
    const state = mockScrollGeometry(el, 900);
    act(() => {
      useAppStore.setState({ hubMessages: [{ id: 1, kind: 'agent', tool: 'claude', text: 'r', ts: 1 }] });
    });
    flushRaf();
    flushRaf();
    expect(state.top).toBe(0);
  });
});
