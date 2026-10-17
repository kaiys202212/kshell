// WeixinSettings 测试：mock api 与 qrcode，覆盖绑定/轮询/脱敏/解绑/过期各态。
// 注意：轮询周期 2s，轮询相关用例用带 interval 的 waitFor 等真实触发。
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { tt } from '../test/i18n';
import { WeixinSettings } from './WeixinSettings';

const mocks = vi.hoisted(() => ({
  weixinStatus: vi.fn(),
  weixinBind: vi.fn(),
  weixinBindStatus: vi.fn(),
  weixinUnbind: vi.fn(),
  qrcodeToDataURL: vi.fn(),
}));

vi.mock('../lib/api', () => mocks);
// qrcode 是 CJS module.exports（vite 互操作后既有命名导出也有 default），
// 两条路径都要给，否则组件里的默认导入拿不到 mock
vi.mock('qrcode', () => ({
  default: { toDataURL: mocks.qrcodeToDataURL },
  toDataURL: mocks.qrcodeToDataURL,
}));

const DATA_URL = 'data:image/png;base64,FAKEQR';

// idle 状态（未绑定且无绑定流程进行中）
const idleView = {
  bound: false,
  running: false,
  wxid: '',
  pending: 0,
  undelivered: 0,
  binding: 'idle',
};

function mockBound(over: Partial<typeof idleView> = {}) {
  mocks.weixinStatus.mockResolvedValue({ ...idleView, ...over });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.qrcodeToDataURL.mockResolvedValue(DATA_URL);
  mockBound();
  mocks.weixinBind.mockResolvedValue('https://login.weixin.qq.com/xyz');
  mocks.weixinBindStatus.mockResolvedValue('pending');
  mocks.weixinUnbind.mockResolvedValue(undefined);
});

afterEach(() => cleanup());

// boundAs 取「已绑定 NNNN」所在 span（wxid 与运行状态在同一 span 的多个文本节点里）
const boundAs = (masked: string) =>
  screen.findByText(
    (_, el) => el?.tagName === 'SPAN' && el.textContent === `${tt('ui.weixin.bound_as').replace('{{0}}', masked)} · ${tt('ui.weixin.running')}`,
  );

describe('WeixinSettings', () => {
  it('未绑定时渲染「绑定微信」按钮', async () => {
    render(<WeixinSettings />);
    expect(
      await screen.findByRole('button', { name: tt('ui.weixin.bind') }),
    ).toBeInTheDocument();
  });

  it('点击绑定后展示二维码图片与扫码提示', async () => {
    render(<WeixinSettings />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.weixin.bind') }));
    expect(mocks.weixinBind).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(mocks.qrcodeToDataURL).toHaveBeenCalledWith('https://login.weixin.qq.com/xyz');
    });
    expect(await screen.findByRole('img')).toHaveAttribute('src', DATA_URL);
    expect(screen.getByText(tt('ui.weixin.scan_hint'))).toBeInTheDocument();
  });

  it('绑定轮询到 ok 后刷新状态显示已绑定', async () => {
    // 初始 idle → 点击绑定后 pending → 轮询命中 ok 后回读为已绑定
    mocks.weixinStatus
      .mockResolvedValueOnce({ ...idleView, binding: 'idle' })
      .mockResolvedValueOnce({ ...idleView, binding: 'pending' })
      .mockResolvedValue({ ...idleView, bound: true, running: true, wxid: 'wxid_ab1234ef89' });
    mocks.weixinBindStatus.mockResolvedValue('ok');
    render(<WeixinSettings />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.weixin.bind') }));
    await screen.findByRole('img');
    await waitFor(
      () => {
        expect(mocks.weixinBindStatus).toHaveBeenCalled();
        expect(mocks.weixinStatus.mock.calls.length).toBeGreaterThanOrEqual(3);
      },
      { timeout: 5000, interval: 500 },
    );
    await boundAs('wxid****ef89');
  });

  it('已绑定态显示脱敏 wxid 与解绑按钮', async () => {
    mockBound({ bound: true, running: true, wxid: 'wxid_ab1234ef89' });
    render(<WeixinSettings />);
    await boundAs('wxid****ef89');
    expect(screen.getByRole('button', { name: tt('ui.weixin.unbind') })).toBeInTheDocument();
  });

  it('未送达条数展示 pending_undelivered 文案', async () => {
    mockBound({ bound: true, running: true, wxid: 'wxid_x', undelivered: 3 });
    render(<WeixinSettings />);
    expect(
      await screen.findByText(tt('ui.weixin.pending_undelivered').replace('{{count}}', '3')),
    ).toBeInTheDocument();
  });

  it('解绑走二次点击确认：第一次变确认文案，第二次执行', async () => {
    // 初始已绑定 → 解绑刷新后回未绑定
    mocks.weixinStatus
      .mockResolvedValueOnce({ ...idleView, bound: true, running: true, wxid: 'wxid_x' })
      .mockResolvedValue({ ...idleView });
    render(<WeixinSettings />);
    const btn = await screen.findByRole('button', { name: tt('ui.weixin.unbind') });
    fireEvent.click(btn);
    expect(mocks.weixinUnbind).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: tt('ui.weixin.unbind_confirm') })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.weixin.unbind_confirm') }));
    expect(mocks.weixinUnbind).toHaveBeenCalledTimes(1);
    // 解绑成功后状态刷新，回到未绑定
    expect(await screen.findByRole('button', { name: tt('ui.weixin.bind') })).toBeInTheDocument();
  });

  it('expired 态显示过期文案与「重新生成」按钮', async () => {
    mocks.weixinStatus
      .mockResolvedValueOnce({ ...idleView, binding: 'idle' })
      .mockResolvedValue({ ...idleView, binding: 'pending' });
    mocks.weixinBindStatus.mockResolvedValue('expired');
    render(<WeixinSettings />);
    fireEvent.click(await screen.findByRole('button', { name: tt('ui.weixin.bind') }));
    await screen.findByRole('img');
    await waitFor(
      () => {
        expect(screen.getByText(tt('ui.weixin.expired'))).toBeInTheDocument();
      },
      { timeout: 5000, interval: 500 },
    );
    expect(screen.getByRole('button', { name: tt('ui.weixin.rebind') })).toBeInTheDocument();
  });

  it('running=false 显示已断开与重新绑定按钮', async () => {
    mockBound({ bound: true, running: false, wxid: 'wxid_x' });
    render(<WeixinSettings />);
    // 断开文案与 wxid 在同一 span 的多个文本节点里，按完整 textContent 匹配
    expect(
      await screen.findByText(
        (_, el) =>
          el?.tagName === 'SPAN' &&
          el.textContent ===
            `${tt('ui.weixin.bound_as').replace('{{0}}', '****')} · ${tt('ui.weixin.disconnected')}`,
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.weixin.rebind') })).toBeInTheDocument();
  });

  it('weixinStatus 调用失败显示错误文案', async () => {
    mocks.weixinStatus.mockRejectedValue(new Error('boom'));
    render(<WeixinSettings />);
    expect(await screen.findByText(tt('ui.weixin.error'))).toBeInTheDocument();
  });
});
