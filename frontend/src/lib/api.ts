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

// providers.Session 的 JSON 形态（internal/providers/provider.go）
export interface Session {
  ID: string;
  ToolID: string;
  Workspace: string;
  Title: string;
  CreatedAt: string; // RFC3339
  UpdatedAt: string; // RFC3339
  Messages: number;
  Path: string;
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
  GetSessions(): Promise<Session[]>;
  ResumeSession(id: string): Promise<void>;
  FocusSession(id: string): Promise<boolean>;
}

declare global {
  interface Window {
    go?: { main: { App: AppBindings } };
  }
}

// 绑定对象只在 Wails 运行时里存在；拿不到（如纯浏览器调试）时调用方拿到空数据，
// 并只警告一次，避免每次刷新都刷屏。
let warnedNoBinding = false;
function app(): AppBindings | null {
  const a = window.go?.main?.App ?? null;
  if (!a && !warnedNoBinding) {
    warnedNoBinding = true;
    console.warn('未检测到 kshell 桌面端绑定，请在桌面端运行');
  }
  return a;
}

// GetWorkspaces 返回最近一次扫描的工作区列表（未扫完时为空数组）
export async function getWorkspaces(): Promise<Workspace[]> {
  const a = app();
  if (!a) return [];
  return a.GetWorkspaces();
}

// GetSessions 返回最近一次扫描的会话列表（只读缓存，不触发新的后台扫描）
export async function getSessions(): Promise<Session[]> {
  const a = app();
  if (!a) return [];
  return a.GetSessions();
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

// ResumeSession 恢复会话（弹窗 + 启动 agent CLI）；错误向上抛，由调用方决定如何呈现
export async function resumeSession(id: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.ResumeSession(id);
}

// FocusSession 聚焦会话已打开的终端窗口；未打开过返回 false
export async function focusSession(id: string): Promise<boolean> {
  const a = app();
  if (!a) return false;
  return a.FocusSession(id);
}

// onScanDone 订阅扫描完成事件，返回取消订阅函数
export function onScanDone(cb: (payload: ScanDonePayload) => void): () => void {
  return EventsOn('scan:done', (payload: ScanDonePayload) => cb(payload ?? {}));
}

// onWindowClosed 订阅终端窗口关闭事件，payload 为完整窗口标题（超长会被 Go 侧截断）
export function onWindowClosed(cb: (title: string) => void): () => void {
  return EventsOn('window:closed', (title: string) => cb(title ?? ''));
}
