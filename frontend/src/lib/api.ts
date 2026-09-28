// 手写的 Wails 绑定调用封装：不依赖 `wails generate module` 的生成产物，
// 通过 window.go.main.App 动态调用 internal/desktop.App 的绑定方法，
// 事件用 wailsjs/runtime 的 EventsOn 监听。后续任务可整体替换为生成绑定。
import { EventsOn } from '../../wailsjs/runtime/runtime';

// discovery.Workspace 的 JSON 形态（internal/discovery/workspaces.go）
export interface Workspace {
  Path: string;
  Name: string;
  LastUsed: string; // JSON 序列化后的 RFC3339 时间
  SessionCount: number;
  ToolCounts: Record<string, number>;
  Source: string; // sessions | git
}

// "scan:done" 事件 payload（internal/desktop/app.go runScan）；
// 失败时可能只有 error 字段，workspaces 等字段缺省。
export interface ScanDonePayload {
  sessions?: unknown[];
  workspaces?: Workspace[];
  failed?: number;
  error?: string;
}

interface AppBindings {
  ScanSessions(): Promise<unknown>;
  GetWorkspaces(): Promise<Workspace[]>;
}

declare global {
  interface Window {
    go?: { main: { App: AppBindings } };
  }
}

// 绑定对象只在 Wails 运行时里存在；拿不到（如纯浏览器调试）时调用方拿到空数据
function app(): AppBindings | null {
  return window.go?.main?.App ?? null;
}

// GetWorkspaces 返回最近一次扫描的工作区列表（未扫完时为空数组）
export async function getWorkspaces(): Promise<Workspace[]> {
  const a = app();
  if (!a) return [];
  return a.GetWorkspaces();
}

// ScanSessions 触发后台扫描并立即返回缓存结果；首次可能为 null/未就绪错误，
// 调用方应等 "scan:done" 事件后再刷新，这里只吞掉错误不向上抛。
export async function scanSessions(): Promise<void> {
  const a = app();
  if (!a) return;
  try {
    await a.ScanSessions();
  } catch {
    // 未就绪等错误交给 scan:done 流程兜底
  }
}

// onScanDone 订阅扫描完成事件，返回取消订阅函数
export function onScanDone(cb: (payload: ScanDonePayload) => void): () => void {
  return EventsOn('scan:done', (payload: ScanDonePayload) => cb(payload ?? {}));
}
