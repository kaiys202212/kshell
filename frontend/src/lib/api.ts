// 手写的 Wails 绑定调用封装：不依赖 `wails generate module` 的生成产物，
// 通过 window.go.desktop.App 动态调用 internal/desktop.App 的绑定方法，
// 事件用 wailsjs/runtime 的 EventsOn 监听。后续任务可整体替换为生成绑定。
//
// 注意命名空间：Wails v2 按「结构体定义所在包」生成运行时绑定路径——
// App 定义在 internal/desktop 包，所以是 window.go.desktop.App（不是 main.App），
// 见 wailsjs/go/desktop/App.js 的生成产物（window['go']['desktop']['App']）。
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

// workspace.Node 的 JSON 形态（internal/workspace/tree.go）。
// 注意：Go 侧 ListFiles 只填充当前层子项，Children 字段无意义，
// 前端目录懒加载必须再次调 listFiles(wsPath, 子目录相对路径)。
export interface FileNode {
  Name: string;
  Path: string; // 绝对路径
  IsDir: boolean;
  Expanded: boolean;
  Loaded: boolean;
}

// workspace.Preview 的 JSON 形态（internal/workspace/preview.go）；
// Lines 已带 "NNNN │ " 行号前缀，前端直接渲染、不要再加行号。
export interface FilePreview {
  Lines: string[];
  Truncated: boolean;
  Binary: boolean;
  Info: string;
}

// remote.Result 的 JSON 形态（internal/remote/exec.go）；
// Duration 是 Go time.Duration 的 JSON 序列化值（纳秒数）。
export interface RemoteResult {
  Stdout: string;
  Stderr: string;
  ExitCode: number;
  Duration: number;
}

// discovery.Tool 的 JSON 形态（internal/discovery/tools.go）。
// Source: path / install-dir / config-dir（config-dir = 只检测到配置目录，没有可执行程序）
export interface ToolInfo {
  ID: string;
  Name: string;
  BinPath: string;
  Version: string;
  Installed: boolean;
  Source: string;
}

// terminal.Info 的 JSON 形态（internal/terminal/manager.go）。
// Kind: session（恢复历史会话，同一会话幂等复用）/ new（工作区新开，每次都起新进程）
// Status: running | exited
export interface TerminalInfo {
  ID: string;
  Kind: string;
  SessionID: string;
  Workspace: string;
  Title: string;
  ToolID: string;
  Status: string;
  ExitCode: number;
  Cols: number;
  Rows: number;
}

// remote.Connection 的 JSON 形态（internal/remote/store.go）。
// Source: sshconfig / env / spring / deploy / docs（Go 侧扫描器值集，无 manual）
export interface SshConnection {
  ID: string;
  Name: string;
  Host: string;
  User: string;
  Port: number;
  IdentityFile: string;
  Workspace: string;
  Source: string;
  SourceFile: string;
  Verified: boolean;
}

interface AppBindings {
  ScanSessions(): Promise<unknown>;
  GetWorkspaces(): Promise<Workspace[]>;
  GetSessions(): Promise<Session[]>;
  ResumeSession(id: string): Promise<void>;
  FocusSession(id: string): Promise<boolean>;
  ListFiles(wsPath: string, relPath: string): Promise<FileNode[]>;
  PreviewFile(wsPath: string, path: string): Promise<FilePreview>;
  ToggleBasket(path: string): Promise<boolean>;
  GetBasket(): Promise<string[]>;
  NewSession(wsPath: string): Promise<void>;
  ListConnections(wsID: string): Promise<SshConnection[]>;
  OpenSSH(connID: string): Promise<void>;
  ExecRemote(connID: string, cmd: string): Promise<RemoteResult>;
  GetTools(): Promise<ToolInfo[]>;
  LoadProvidersYAML(): Promise<string>;
  SaveProvidersYAML(content: string): Promise<void>;
  RestartApp(): Promise<void>;
  OpenSessionTerminal(sessionId: string, cols: number, rows: number): Promise<TerminalInfo>;
  OpenWorkspaceTerminal(wsPath: string, toolId: string, cols: number, rows: number): Promise<TerminalInfo>;
  WriteTerminal(id: string, data: string): Promise<void>;
  ResizeTerminal(id: string, cols: number, rows: number): Promise<void>;
  CloseTerminal(id: string): Promise<void>;
  ListTerminals(): Promise<TerminalInfo[]>;
  ScrollbackTerminal(id: string): Promise<string>;
  NewSessionWithTool(wsPath: string, toolId: string): Promise<void>;
}

declare global {
  interface Window {
    go?: { desktop: { App: AppBindings } };
  }
}

// 绑定对象只在 Wails 运行时里存在；拿不到（如纯浏览器调试）时调用方拿到空数据，
// 并只警告一次，避免每次刷新都刷屏。
let warnedNoBinding = false;
function app(): AppBindings | null {
  const a = window.go?.desktop?.App ?? null;
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

// onWindowClosed 订阅终端窗口关闭事件，payload 为完整窗口标题（terminalTitle 形态）
export function onWindowClosed(cb: (title: string) => void): () => void {
  return EventsOn('window:closed', (title: string) => cb(title ?? ''));
}

// ListFiles 列出工作区内 relPath 目录的子项（懒加载；relPath 空串/"." 为根层）。
// relPath 统一用 / 拼接：Go 侧 filepath.Clean 会归一化为平台分隔符。
// 错误（路径越界等）向上抛，由调用方呈现。
export async function listFiles(wsPath: string, relPath: string): Promise<FileNode[]> {
  const a = app();
  if (!a) return [];
  return a.ListFiles(wsPath, relPath);
}

// PreviewFile 读取文件预览（Lines 已含行号前缀）；绑定不可用时返回 null
export async function previewFile(wsPath: string, path: string): Promise<FilePreview | null> {
  const a = app();
  if (!a) return null;
  return a.PreviewFile(wsPath, path);
}

// ToggleBasket 把文件加入/移出上下文篮，返回操作后是否在篮中
//（篮子已满时加入失败也返回 false）。错误向上抛。
export async function toggleBasket(path: string): Promise<boolean> {
  const a = app();
  if (!a) return false;
  return a.ToggleBasket(path);
}

// GetBasket 返回上下文篮内容（Go 侧副本）
export async function getBasket(): Promise<string[]> {
  const a = app();
  if (!a) return [];
  return a.GetBasket();
}

// NewSession 在工作区新建会话（Go 侧自动带上篮子内容作为初始提示）；
// 错误向上抛，由调用方决定如何呈现
export async function newSession(wsPath: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.NewSession(wsPath);
}

// ListConnections 返回连接列表：wsID 传工作区路径 = 该工作区绑定连接 + 全局连接，
// 传空串返回全部。错误向上抛，由调用方呈现。
export async function listConnections(wsID: string): Promise<SshConnection[]> {
  const a = app();
  if (!a) return [];
  return a.ListConnections(wsID);
}

// OpenSSH 弹出该连接的交互式 SSH 终端窗口（BatchMode 恒定，绝不卡密码提示）。
// 同一连接重复调用 Go 侧幂等转聚焦。错误向上抛，由调用方呈现。
export async function openSSH(connID: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.OpenSSH(connID);
}

// ExecRemote 在指定连接上非交互执行命令。非 0 退出码不是异常，结果里带 ExitCode。
// 错误（连接不存在、ssh 缺失等）向上抛，由调用方呈现。
export async function execRemote(connID: string, cmd: string): Promise<RemoteResult> {
  const a = app();
  if (!a) return { Stdout: '', Stderr: '', ExitCode: 0, Duration: 0 };
  return a.ExecRemote(connID, cmd);
}

// GetTools 返回最近一次扫描的工具安装状态（含未安装项，前端灰显）
export async function getTools(): Promise<ToolInfo[]> {
  const a = app();
  if (!a) return [];
  return a.GetTools();
}

// LoadProvidersYAML 读出当前自定义工具定义全文（文件缺失时 Go 侧回填模板）。
// 错误向上抛，由调用方呈现。
export async function loadProvidersYAML(): Promise<string> {
  const a = app();
  if (!a) return '';
  return a.LoadProvidersYAML();
}

// SaveProvidersYAML 保存自定义工具定义（Go 侧先校验 YAML 再原子写回）。
// 注意：保存后不热生效，需重启应用。错误（含 YAML 解析失败）向上抛。
export async function saveProvidersYAML(content: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.SaveProvidersYAML(content);
}

// RestartApp 重启应用（Go 侧先启动新实例再走托盘退出路径）。错误向上抛。
export async function restartApp(): Promise<void> {
  const a = app();
  if (!a) return;
  await a.RestartApp();
}

// ---- 内嵌终端（中心区命令行）----
// 约定：所有终端数据都走 base64，避免任意字节被 JSON/UTF-16 转换破坏
//（xterm 的 onData 可能给出任意字节，Go 侧输出含 ANSI 与半截多字节序列）。

// openSessionTerminal 在中心区打开（或复用）某历史会话的内嵌终端。错误向上抛。
export async function openSessionTerminal(
  sessionId: string,
  cols: number,
  rows: number,
): Promise<TerminalInfo> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenSessionTerminal(sessionId, cols, rows);
}

// openWorkspaceTerminal 在工作区新开一个内嵌终端；toolId 为空串表示由 Go 侧选首选工具。
export async function openWorkspaceTerminal(
  wsPath: string,
  toolId: string,
  cols: number,
  rows: number,
): Promise<TerminalInfo> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenWorkspaceTerminal(wsPath, toolId, cols, rows);
}

// writeTerminal 把键盘输入写进终端（data 为 base64 编码的 UTF-8 字节）。
export async function writeTerminal(id: string, data: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.WriteTerminal(id, data);
}

// resizeTerminal 同步终端尺寸（FitAddon 变化时调用）。
export async function resizeTerminal(id: string, cols: number, rows: number): Promise<void> {
  const a = app();
  if (!a) return;
  await a.ResizeTerminal(id, cols, rows);
}

// closeTerminal 关闭终端并结束其进程（幂等）。
export async function closeTerminal(id: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.CloseTerminal(id);
}

// listTerminals 返回全部内嵌终端快照（用于重挂载后还原页签）。
export async function listTerminals(): Promise<TerminalInfo[]> {
  const a = app();
  if (!a) return [];
  return a.ListTerminals();
}

// scrollbackTerminal 取终端最近输出的回放数据（base64；已退出的终端仍可读）。
export async function scrollbackTerminal(id: string): Promise<string> {
  const a = app();
  if (!a) return '';
  return a.ScrollbackTerminal(id);
}

// newSessionWithTool 在外部终端窗口新建会话（次要入口）；toolId 为空串等价于 NewSession。
export async function newSessionWithTool(wsPath: string, toolId: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.NewSessionWithTool(wsPath, toolId);
}

// onTerminalData 订阅终端输出（data 为 base64），返回取消订阅函数
export function onTerminalData(cb: (payload: { id: string; data: string }) => void): () => void {
  return EventsOn('terminal:data', (p: { id?: string; data?: string }) =>
    cb({ id: p?.id ?? '', data: p?.data ?? '' }),
  );
}

// onTerminalExit 订阅终端退出（进程结束），返回取消订阅函数
export function onTerminalExit(cb: (payload: { id: string; exitCode: number }) => void): () => void {
  return EventsOn('terminal:exit', (p: { id?: string; exitCode?: number }) =>
    cb({ id: p?.id ?? '', exitCode: p?.exitCode ?? 0 }),
  );
}
