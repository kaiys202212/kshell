// main.tsx bootstrap 守卫测试（评审项 A/B）：
// A. language:changed 订阅抛错（window.runtime 缺失）时 bootstrap 仍必须完成 render，不得白屏；
// B. 启动与事件统一切 Go payload 的 resolved（applang 解析），不再走前端 navigator 双解析。
// 每个用例 vi.resetModules 后动态导入 './main'，i18next/store 也按同代模块取，避免跨轮实例。
import '@testing-library/jest-dom/vitest';
import { cleanup, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { LanguageInfo } from './lib/api';

const mocks = vi.hoisted(() => ({
  getLanguage: vi.fn(),
  loadExternalLocales: vi.fn(),
  onLanguageChanged: vi.fn(),
}));
vi.mock('./lib/api', () => mocks);

// App 整体重 mock：本文件只关心 bootstrap 接线，不渲染真实主框架
vi.mock('./App', () => ({ default: () => <div data-testid="app-stub" /> }));

async function importMain() {
  await import('./main');
  // 与 main.tsx 同代的模块实例（resetModules 后动态导入），断言才指向同一 i18next/store
  const i18next = (await import('i18next')).default;
  const { useAppStore } = await import('./state/store');
  return { i18next, useAppStore };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.resetModules();
  document.body.innerHTML = '<div id="root"></div>';
});

afterEach(cleanup);

describe('bootstrap 接线', () => {
  it('启动与事件统一用 Go resolved：system 配置按 payload resolved 初始化与切换', async () => {
    mocks.getLanguage.mockResolvedValue({ configured: 'system', resolved: 'zh-CN' });
    mocks.loadExternalLocales.mockResolvedValue({});
    let eventCb: ((info: LanguageInfo) => void) | undefined;
    mocks.onLanguageChanged.mockImplementation((cb: (info: LanguageInfo) => void) => {
      eventCb = cb;
      return () => {};
    });

    const { i18next, useAppStore } = await importMain();
    await screen.findByTestId('app-stub');

    // 旧实现传 configured('system') → initI18n 走 navigator（jsdom=en-US）解析成 en；
    // 统一用 Go resolved 后应为 zh-CN
    expect(i18next.language).toBe('zh-CN');
    expect(useAppStore.getState().language).toEqual({ configured: 'system', resolved: 'zh-CN' });

    // 事件同理：configured 与 resolved 分歧时以 resolved 为准
    eventCb?.({ configured: 'ja', resolved: 'zh-CN' });
    await waitFor(() => {
      expect(useAppStore.getState().language.configured).toBe('ja');
    });
    expect(i18next.language).toBe('zh-CN');
  });

  it('language:changed 订阅抛错（运行时缺失）时 bootstrap 仍完成 render', async () => {
    mocks.getLanguage.mockResolvedValue({ configured: 'en', resolved: 'en' });
    mocks.loadExternalLocales.mockResolvedValue({});
    mocks.onLanguageChanged.mockImplementation(() => {
      throw new TypeError('window.runtime is undefined');
    });

    await importMain();
    // bootstrap 是 void 启动的异步流程：订阅失败只告警，render 必然执行
    await expect(screen.findByTestId('app-stub')).resolves.toBeInTheDocument();
  });
});
