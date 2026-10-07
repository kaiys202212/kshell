// 气泡提示音（Web Audio 程序化合成）：开关关闭不发声、
// 轻音单音 / 重音两连音、AudioContext 不可用时静默不抛。
import { beforeEach, describe, expect, it, vi } from 'vitest';

interface FakeOsc {
  type: string;
  frequency: { value: number };
  start: (t: number) => void;
  stop: (t: number) => void;
  connect: (node: unknown) => void;
}

// 每个用例都要拿到全新的模块图（notifySound 缓存 AudioContext 单例，
// store 也是单例），所以用 resetModules + 动态导入而不是顶部静态 import。
async function fresh() {
  vi.resetModules();
  const sound = await import('./notifySound');
  const { useAppStore } = await import('../state/store');
  return { playNotifySound: sound.playNotifySound, useAppStore };
}

function stubAudio(opts: { throwOnConstruct?: boolean; suspended?: boolean } = {}) {
  const created: { oscillators: FakeOsc[]; resume: () => Promise<void> }[] = [];
  class FakeAudioContext {
    state = opts.suspended ? 'suspended' : 'running';
    currentTime = 0;
    destination = {};
    oscillators: FakeOsc[] = [];
    constructor() {
      if (opts.throwOnConstruct) throw new Error('blocked');
      created.push(this);
    }
    createOscillator(): FakeOsc {
      const osc: FakeOsc = {
        type: '',
        frequency: { value: 0 },
        start: vi.fn(),
        stop: vi.fn(),
        connect: vi.fn(),
      };
      this.oscillators.push(osc);
      return osc;
    }
    createGain() {
      return {
        connect: vi.fn(),
        gain: {
          setValueAtTime: vi.fn(),
          exponentialRampToValueAtTime: vi.fn(),
        },
      };
    }
    resume = vi.fn(async () => {});
  }
  vi.stubGlobal('AudioContext', FakeAudioContext);
  return created;
}

beforeEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe('playNotifySound', () => {
  it('轻音：单个 880Hz 正弦短音', async () => {
    const created = stubAudio();
    const { playNotifySound, useAppStore } = await fresh();
    useAppStore.setState({ notifySound: true });
    playNotifySound('light');
    expect(created).toHaveLength(1);
    expect(created[0].oscillators).toHaveLength(1);
    expect(created[0].oscillators[0].frequency.value).toBe(880);
  });

  it('播放会真正启动并停止振荡器（防泄漏：start/stop 成对调用）', async () => {
    const created = stubAudio();
    const { playNotifySound, useAppStore } = await fresh();
    useAppStore.setState({ notifySound: true });
    playNotifySound('light');
    const osc = created[0].oscillators[0];
    expect(osc.start).toHaveBeenCalled();
    expect(osc.stop).toHaveBeenCalled();
  });

  it('suspended 状态：播放时尝试 resume 恢复音频', async () => {
    const created = stubAudio({ suspended: true });
    const { playNotifySound, useAppStore } = await fresh();
    useAppStore.setState({ notifySound: true });
    playNotifySound('light');
    expect(created[0].resume).toHaveBeenCalled();
  });

  it('重音：两连音 660Hz → 440Hz', async () => {
    const created = stubAudio();
    const { playNotifySound, useAppStore } = await fresh();
    useAppStore.setState({ notifySound: true });
    playNotifySound('attention');
    expect(created[0].oscillators.map((o) => o.frequency.value)).toEqual([660, 440]);
  });

  it('开关关闭：不创建 AudioContext、不发声', async () => {
    const created = stubAudio();
    const { playNotifySound, useAppStore } = await fresh();
    useAppStore.setState({ notifySound: false });
    playNotifySound('light');
    playNotifySound('attention');
    expect(created).toHaveLength(0);
  });

  it('AudioContext 构造被策略拦截时静默不抛', async () => {
    stubAudio({ throwOnConstruct: true });
    const { playNotifySound, useAppStore } = await fresh();
    useAppStore.setState({ notifySound: true });
    expect(() => playNotifySound('light')).not.toThrow();
  });

  it('环境无 AudioContext（老 WebView）时静默不抛', async () => {
    vi.stubGlobal('AudioContext', undefined);
    const { playNotifySound, useAppStore } = await fresh();
    useAppStore.setState({ notifySound: true });
    expect(() => playNotifySound('attention')).not.toThrow();
  });
});
