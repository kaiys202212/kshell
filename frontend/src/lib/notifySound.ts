// 气泡提示音：Web Audio 程序化合成，零音频资产、零构建体积。
// 轻音 = 任务完成（短促柔和单音）；重音 = 等待确认 / 出错（两连音更紧迫）。
// 播放前读 store 的 notifySound 开关；任何异常静默吞掉——
// 提示音失败绝不影响通知入队与气泡渲染。
import { useAppStore } from '../state/store';

export type NotifyTone = 'light' | 'attention';

// AudioContext 单例：通知稀疏，复用避免每次播放开销
let ctx: AudioContext | null = null;

function context(): AudioContext | null {
  if (ctx) return ctx;
  try {
    const Ctor =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctor) return null;
    ctx = new Ctor();
  } catch {
    return null;
  }
  return ctx;
}

// blip 在 at 秒后发一发正弦短音：freq 频率、durMs 时长、peak 峰值音量
function blip(c: AudioContext, freq: number, durMs: number, peak: number, at: number): void {
  const osc = c.createOscillator();
  const gain = c.createGain();
  osc.type = 'sine';
  osc.frequency.value = freq;
  const t0 = c.currentTime + at;
  const end = t0 + durMs / 1000;
  gain.gain.setValueAtTime(0.0001, t0);
  gain.gain.exponentialRampToValueAtTime(peak, t0 + 0.01);
  gain.gain.exponentialRampToValueAtTime(0.0001, end);
  osc.connect(gain);
  gain.connect(c.destination);
  osc.start(t0);
  osc.stop(end + 0.02);
}

export function playNotifySound(tone: NotifyTone): void {
  try {
    if (!useAppStore.getState().notifySound) return;
    const c = context();
    if (!c) return;
    // 浏览器自动播放策略：首帧 suspended 时尝试恢复，失败忽略
    if (c.state === 'suspended') void c.resume().catch(() => {});
    if (tone === 'attention') {
      blip(c, 660, 150, 0.3, 0);
      blip(c, 440, 150, 0.3, 0.16);
    } else {
      blip(c, 880, 120, 0.15, 0);
    }
  } catch {
    // 静默：提示音不影响通知链路
  }
}
