// 手写的 Wails 绑定调用封装：不依赖 `wails generate module` 的生成产物，
// 通过 window.go.desktop.App 动态调用 internal/desktop.App 的绑定方法，
// 事件用 wailsjs/runtime 的 EventsOn 监听。后续任务可整体替换为生成绑定。
//
// 注意命名空间：Wails v2 按「结构体定义所在包」生成运行时绑定路径——
// App 定义在 internal/desktop 包，所以是 window.go.desktop.App（不是 main.App），
// 见 wailsjs/go/desktop/App.js 的生成产物（window['go']['desktop']['App']）。
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { bumpTerminalBusy } from '../state/terminalBusy';
import type { AppearanceInfo } from './appearance';

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

/** GetSessionPreview 返回的只读对话 Markdown */
export interface SessionPreview {
  Markdown: string;
  Truncated: boolean;
}

// "scan:done" 事件 payload（internal/desktop/app.go runScan）；
// 失败时可能只有 error 字段，workspaces 等字段缺省。
export interface ScanDonePayload {
  sessions?: unknown[];
  workspaces?: Workspace[];
  failed?: number;
  error?: string;
  /** Go 侧绑定/同步了终端或聊天标题时为 true，前端据此重取镜像 */
  attached?: boolean;
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
// Text 为无行号原文（桌面端渲染用）；二进制时为空。
export interface FilePreview {
  Lines: string[];
  Text?: string;
  Truncated: boolean;
  Binary: boolean;
  Info: string;
}

// desktop.FileBytes 的 JSON 形态（internal/desktop/files_bytes.go）。
export interface FileBytes {
  Base64: string;
  Mime: string;
  Size: number;
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
  ACP?: { Available: boolean; Source: string; BinPath?: string; Package?: string };
}

// terminal.Info 的 JSON 形态（internal/terminal/manager.go）。
// Kind: session（恢复历史会话）/ new（工作区新开 agent）/
//       shell（预览区本地命令行）/ ssh（预览区远程 SSH）
// Status: running | exited
export interface TerminalInfo {
  ID: string;
  Kind: string;
  SessionID: string;
  ConnID?: string;
  Workspace: string;
  Title: string;
  ToolID: string;
  Status: string;
  ExitCode: number;
  Cols: number;
  Rows: number;
  /** 用户已提交过输入；新建会话据此立刻出现在列表里 */
  Prompted?: boolean;
}

// remote.Connection 的 JSON 形态（internal/remote/store.go）。
// Source: sshconfig / env / spring / deploy / docs / manual
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

// workspace.SearchHit 的 JSON 形态（internal/workspace/search.go）：
// Node 内嵌字段 + '/' 分隔的相对路径（前端展示「文件名 + 所在目录」用）。
export interface SearchHit extends FileNode {
  RelPath: string;
}

// workspace.EditContent 的 JSON 形态（internal/workspace/editfile.go）：
// Text 一律 \n 归一，EOL 记录原行尾（"lf"|"crlf"），保存时按原样还原。
export interface EditContent {
  Text: string;
  EOL: string;
  Size: number;
}

// desktop.DeletedProjectView 的 JSON 形态（internal/desktop/projects.go）：
// 回收站条目 = 逻辑删除的项目 + 删除时间 + 目录是否仍存在于磁盘。
export interface DeletedProject {
  path: string;
  name: string;
  at: string; // RFC3339，前端自行本地化展示
  exists: boolean;
}

// desktop.GitStatusResult 的 JSON 形态（internal/desktop/files.go）：
// Status 键为 git 原生输出的 '/' 分隔相对路径，值见 workspace.GitStatus 的状态码。
export type GitStatusCode =
  | 'modified'
  | 'added'
  | 'deleted'
  | 'renamed'
  | 'untracked'
  | 'conflicted'
  | 'ignored';
export interface GitStatusResult {
  Status: Record<string, string>;
  IsRepo: boolean;
  Branch?: string;
  DirBranches?: Record<string, string>;
}

// chat.Info 的 JSON 形态（internal/chat/types.go）
export interface ChatInfo {
  ID: string;
  Kind: string; // session | new
  SessionID: string;
  Workspace: string;
  Title: string;
  ToolID: string;
  Status: string; // starting | ready | running | exited
  ExitCode: number;
  Error: string;
  /** 用户已发送过消息；新建会话据此立刻出现在列表里 */
  Prompted?: boolean;
}

export interface ChatToolCall {
  ToolCallID: string;
  Name?: string;
  Title?: string;
  Kind?: string;
  Status?: string;
  Content?: unknown[];
  RawInput?: unknown;
  RawOutput?: unknown;
}

export interface ChatPlanEntry {
  Content: string;
  Priority?: string;
  Status?: string;
}

// chat.Update 的 JSON 形态
export interface ChatUpdate {
  Seq: number;
  Type: string; // user | assistant | thought | tool | plan | turn_done | error
  MessageID?: string;
  Text?: string;
  ToolCallID?: string;
  Tool?: ChatToolCall;
  Plan?: ChatPlanEntry[];
  StopReason?: string;
}

export interface ChatPermissionOption {
  OptionID: string;
  Name: string;
  Kind: string;
}

export interface ChatPermissionRequest {
  RequestID: string;
  SessionID: string;
  ToolCall: ChatToolCall;
  Options: ChatPermissionOption[];
}

// desktop.OpenedSession 的 JSON 形态
export interface OpenedSession {
  Kind: 'chat' | 'terminal';
  Chat?: ChatInfo;
  Terminal?: TerminalInfo;
  Fallback?: string;
}

// desktop.ModelConfigView 的 JSON 形态（internal/desktop/settings.go）：
// 密钥不回传明文，只以 APIKeySet 表示是否已设置。
export interface ModelConfigView {
  Enabled: boolean;
  Preset: string;
  OpenAIBaseURL: string;
  AnthropicBaseURL: string;
  Agents: Record<string, string>;
  APIKeySet: boolean;
}

// desktop.ModelConfigInput 的 JSON 形态：APIKey 留空表示不修改，ClearAPIKey 为真时清除。
export interface ModelConfigInput {
  Enabled: boolean;
  Preset: string;
  OpenAIBaseURL: string;
  AnthropicBaseURL: string;
  APIKey: string;
  ClearAPIKey: boolean;
  Agents: Record<string, string>;
}

export interface ModelPreset {
  ID: string;
  Name: string;
  OpenAIBaseURL: string;
  AnthropicBaseURL: string;
  RecommendedModel: string;
  Note: string;
}

// desktop.InstallRecipeView：内置工具安装配方预览（命令只读）。
export interface InstallRecipeView {
  ToolID: string;
  Name: string;
  InstallCmd: string;
  UninstallCmd: string;
  PurgeDirs: string[];
  CanPurge: boolean;
}

// desktop.ToolInstallJobView：当前安装/卸载任务快照。
export interface ToolInstallJobView {
  ToolID: string;
  Action: string;
  Running: boolean;
  Log: string;
  Error: string;
}

export interface UpdateInfo {
  Current: string;
  Latest: string;
  Notes: string;
  Source: string;
  Available: boolean;
  Skipped: boolean;
  Reason: string;
}

interface AppBindings {
  ScanSessions(): Promise<unknown>;
  GetWorkspaces(): Promise<Workspace[]>;
  GetSessions(): Promise<Session[]>;
  GetSessionPreview(id: string): Promise<SessionPreview>;
  ResumeSession(id: string): Promise<void>;
  FocusSession(id: string): Promise<boolean>;
  ListFiles(wsPath: string, relPath: string, showAll: boolean): Promise<FileNode[]>;
  PreviewFile(wsPath: string, path: string): Promise<FilePreview>;
  ReadFileBytes(wsPath: string, path: string): Promise<FileBytes>;
  SearchFiles(wsPath: string, query: string, showAll: boolean): Promise<SearchHit[]>;
  RefreshFiles(wsPath: string): Promise<void>;
  StartFileWatch(wsPath: string): Promise<void>;
  StopFileWatch(wsPath: string): void;
  RevealInExplorer(wsPath: string, path: string): Promise<void>;
  RenameEntry(wsPath: string, relPath: string, newName: string): Promise<string>;
  CreateEntry(wsPath: string, dirRelPath: string, name: string, isDir: boolean): Promise<string>;
  DeleteEntry(wsPath: string, relPath: string): Promise<void>;
  MoveEntry(wsPath: string, srcRelPath: string, dstDirRelPath: string): Promise<string>;
  ReadFileForEdit(wsPath: string, path: string): Promise<EditContent>;
  SaveFile(wsPath: string, path: string, text: string, eol: string): Promise<void>;
  GitStatus(wsPath: string): Promise<GitStatusResult>;
  NewSession(wsPath: string): Promise<void>;
  ListConnections(wsID: string): Promise<SshConnection[]>;
  OpenSSH(connID: string): Promise<void>;
  OpenSSHTerminal(connID: string, cols: number, rows: number): Promise<TerminalInfo>;
  UpsertConnection(conn: SshConnection): Promise<SshConnection>;
  DeleteConnection(id: string): Promise<void>;
  ExecRemote(connID: string, cmd: string): Promise<RemoteResult>;
  GetTools(): Promise<ToolInfo[]>;
  GetToolInstallRecipe(id: string): Promise<InstallRecipeView>;
  InstallBuiltinTool(id: string): Promise<void>;
  UninstallBuiltinTool(id: string, purgeConfig: boolean): Promise<void>;
  GetToolInstallJob(): Promise<ToolInstallJobView>;
  LoadProvidersYAML(): Promise<string>;
  SaveProvidersYAML(content: string): Promise<void>;
  RestartApp(): Promise<void>;
  GetAppVersion(): Promise<string>;
  CheckForUpdate(): Promise<UpdateInfo>;
  ApplyUpdate(): Promise<void>;
  OpenSessionTerminal(sessionId: string, cols: number, rows: number): Promise<TerminalInfo>;
  OpenWorkspaceTerminal(wsPath: string, toolId: string, cols: number, rows: number): Promise<TerminalInfo>;
  OpenShellTerminal(wsPath: string, cols: number, rows: number): Promise<TerminalInfo>;
  WriteTerminal(id: string, data: string): Promise<void>;
  ReadClipboardPaste(): Promise<ClipboardPaste>;
  ResizeTerminal(id: string, cols: number, rows: number): Promise<void>;
  CloseTerminal(id: string): Promise<void>;
  ListTerminals(): Promise<TerminalInfo[]>;
  ScrollbackTerminal(id: string): Promise<string>;
  NewSessionWithTool(wsPath: string, toolId: string): Promise<void>;
  CreateProject(): Promise<string>;
  HideProject(path: string): Promise<void>;
  RestoreProject(path: string): Promise<void>;
  GetDeletedProjects(): Promise<DeletedProject[]>;
  GetCloseBehavior(): Promise<string>;
  SetCloseBehavior(mode: string): Promise<void>;
  GetAppearance(): Promise<AppearanceInfo>;
  SetAppearanceMode(mode: string): Promise<void>;
  SetAppearanceFontSize(n: number): Promise<void>;
  GetModelConfig(): Promise<ModelConfigView>;
  SetModelConfig(input: ModelConfigInput): Promise<void>;
  ListModelPresets(): Promise<ModelPreset[]>;
  GetSessionMode(): Promise<string>;
  SetSessionMode(mode: string): Promise<void>;
  GetPermissionMode(): Promise<string>;
  SetPermissionMode(mode: string): Promise<void>;
  OpenSession(sessionID: string): Promise<OpenedSession>;
  OpenSessionACP(sessionID: string): Promise<OpenedSession>;
  OpenWorkspace(wsID: string, toolID: string): Promise<OpenedSession>;
  OpenWorkspaceACP(wsID: string, toolID: string): Promise<OpenedSession>;
  SendChatPrompt(id: string, text: string): Promise<void>;
  CancelChat(id: string): Promise<void>;
  RespondChatPermission(id: string, requestID: string, optionID: string): Promise<void>;
  CancelChatPermission(id: string, requestID: string): Promise<void>;
  CloseChat(id: string): Promise<void>;
  ListChats(): Promise<ChatInfo[]>;
  ArchivedIDs(): Promise<string[]>;
  ConfirmArchive(ref: string): Promise<void>;
  ArchiveSession(id: string): Promise<void>;
  RestoreSession(id: string): Promise<void>;
  ChatHistory(id: string): Promise<ChatUpdate[]>;
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

export async function getSessionPreview(id: string): Promise<SessionPreview> {
  const a = app();
  if (!a) return { Markdown: '', Truncated: false };
  return a.GetSessionPreview(id);
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
export async function listFiles(wsPath: string, relPath: string, showAll = false): Promise<FileNode[]> {
  const a = app();
  if (!a) return [];
  return a.ListFiles(wsPath, relPath, showAll);
}

// PreviewFile 读取文件预览（Lines 已含行号前缀）；绑定不可用时返回 null
export async function previewFile(wsPath: string, path: string): Promise<FilePreview | null> {
  const a = app();
  if (!a) return null;
  return a.PreviewFile(wsPath, path);
}

// ReadFileBytes 读取工作区内文件字节（base64 + mime，上限 20MiB）；绑定不可用时返回 null
export async function readFileBytes(wsPath: string, path: string): Promise<FileBytes | null> {
  const a = app();
  if (!a) return null;
  return a.ReadFileBytes(wsPath, path);
}

// SearchFiles 递归搜索工作区内名字包含 query 的文件/目录（大小写不敏感，上限 2000）。
// 忽略规则与文件树一致；错误向上抛。
export async function searchFiles(wsPath: string, query: string, showAll = false): Promise<SearchHit[]> {
  const a = app();
  if (!a) return [];
  return a.SearchFiles(wsPath, query, showAll);
}

// RefreshFiles 作废工作区文件树缓存并触发后台重扫；无绑定时静默返回。
export async function refreshFiles(wsPath: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.RefreshFiles(wsPath);
}

// StartFileWatch 订阅工作区目录变更（fsnotify）；无绑定时静默返回。
export async function startFileWatch(wsPath: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.StartFileWatch(wsPath);
}

// StopFileWatch 取消工作区目录监听；无绑定时静默返回。
export function stopFileWatch(wsPath: string): void {
  const a = app();
  if (!a) return;
  a.StopFileWatch(wsPath);
}

// RevealInExplorer 在系统文件管理器中定位并选中 path（绝对或相对工作区）；无绑定抛错。
export async function revealInExplorer(wsPath: string, path: string): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.RevealInExplorer(wsPath, path);
}

// onFilesChanged 订阅工作区文件树变更（Go 侧 watch/刷新后推送），返回取消订阅函数。
export function onFilesChanged(cb: (path: string) => void): () => void {
  return EventsOn('files:changed', (p: { path?: string }) => {
    if (p?.path) cb(p.path);
  });
}

// RenameEntry 重命名工作区内文件/目录（只允许改最后一段名字），返回新绝对路径。
// Go 侧会作废树缓存；错误（目标已存在、越界等）向上抛。
export async function renameEntry(wsPath: string, relPath: string, newName: string): Promise<string> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.RenameEntry(wsPath, relPath, newName);
}

// createEntry 在工作区 dirRelPath 目录下新建文件/目录，返回绝对路径。
// Go 侧会作废树缓存；错误（目标已存在、名字非法等）向上抛。
export async function createEntry(wsPath: string, dirRelPath: string, name: string, isDir: boolean): Promise<string> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.CreateEntry(wsPath, dirRelPath, name, isDir);
}

// deleteEntry 永久删除工作区内文件/目录（确认框逻辑在前端完成）。
// Go 侧会作废树缓存；错误向上抛。
export async function deleteEntry(wsPath: string, relPath: string): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.DeleteEntry(wsPath, relPath);
}

// moveEntry 把 srcRelPath 移入 dstDirRelPath 目录（保留原名），返回新绝对路径。
// Go 侧会作废树缓存；错误（目标已存在、移入子孙目录等）向上抛。
export async function moveEntry(wsPath: string, srcRelPath: string, dstDirRelPath: string): Promise<string> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.MoveEntry(wsPath, srcRelPath, dstDirRelPath);
}

// ReadFileForEdit 整读文本文件供编辑（上限 1MB、拒二进制）；绑定不可用时返回 null
export async function readFileForEdit(wsPath: string, path: string): Promise<EditContent | null> {
  const a = app();
  if (!a) return null;
  return a.ReadFileForEdit(wsPath, path);
}

// SaveFile 保存编辑（原子替换，eol 指定还原的行尾 "lf"|"crlf"）。
// 无绑定时抛错（与 renameEntry 同口径）：静默成功会让调用方误报「已保存」。错误向上抛。
export async function saveFile(wsPath: string, path: string, text: string, eol: string): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.SaveFile(wsPath, path, text, eol);
}

// GitStatus 取工作区 git 状态；非 git 仓库 IsRepo=false。错误向上抛（调用方可静默）
export async function gitStatus(wsPath: string): Promise<GitStatusResult | null> {
  const a = app();
  if (!a) return null;
  return a.GitStatus(wsPath);
}

// NewSession 在工作区新建会话；错误向上抛，由调用方决定如何呈现
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

// OpenSSH 弹出该连接的交互式 SSH 终端窗口（兼容保留；前端主路径改用 openSSHTerminal）。
export async function openSSH(connID: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.OpenSSH(connID);
}

// openSSHTerminal 在预览区内嵌打开 SSH 终端（每次新开）。错误向上抛。
export async function openSSHTerminal(
  connID: string,
  cols: number,
  rows: number,
): Promise<TerminalInfo> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenSSHTerminal(connID, cols, rows);
}

// upsertConnection 新建（ID 空）或更新 SSH 连接。错误向上抛。
export async function upsertConnection(conn: Partial<SshConnection> & { Host: string }): Promise<SshConnection> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.UpsertConnection({
    ID: conn.ID ?? '',
    Name: conn.Name ?? '',
    Host: conn.Host,
    User: conn.User ?? '',
    Port: conn.Port ?? 22,
    IdentityFile: conn.IdentityFile ?? '',
    Workspace: conn.Workspace ?? '',
    Source: conn.Source ?? '',
    SourceFile: conn.SourceFile ?? '',
    Verified: conn.Verified ?? false,
  });
}

// deleteConnection 删除指定连接。错误向上抛。
export async function deleteConnection(id: string): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.DeleteConnection(id);
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

export async function getToolInstallRecipe(id: string): Promise<InstallRecipeView> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.GetToolInstallRecipe(id);
}

export async function installBuiltinTool(id: string): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.InstallBuiltinTool(id);
}

export async function uninstallBuiltinTool(id: string, purgeConfig: boolean): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.UninstallBuiltinTool(id, purgeConfig);
}

export async function getToolInstallJob(): Promise<ToolInstallJobView> {
  const a = app();
  if (!a) {
    return { ToolID: '', Action: '', Running: false, Log: '', Error: '' };
  }
  return a.GetToolInstallJob();
}

export function onToolInstallLog(cb: (p: { toolID: string; text: string }) => void): () => void {
  return EventsOn('tool:install:log', (p: { toolID?: string; text?: string }) =>
    cb({ toolID: p?.toolID ?? '', text: p?.text ?? '' }),
  );
}

export function onToolInstallDone(
  cb: (p: { toolID: string; action: string; ok: boolean; error?: string }) => void,
): () => void {
  return EventsOn('tool:install:done', (p: { toolID?: string; action?: string; ok?: boolean; error?: string }) =>
    cb({ toolID: p?.toolID ?? '', action: p?.action ?? '', ok: Boolean(p?.ok), error: p?.error }),
  );
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

export async function getAppVersion(): Promise<string> {
  const a = app();
  if (!a) return '';
  return a.GetAppVersion();
}

export async function checkForUpdate(): Promise<UpdateInfo> {
  const a = app();
  if (!a) {
    return {
      Current: '',
      Latest: '',
      Notes: '',
      Source: '',
      Available: false,
      Skipped: true,
      Reason: '未检测到桌面端绑定',
    };
  }
  return a.CheckForUpdate();
}

export async function applyUpdate(): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.ApplyUpdate();
}

export function onUpdateAvailable(cb: (info: UpdateInfo) => void): () => void {
  return EventsOn('update:available', (p: UpdateInfo) => {
    if (p?.Available) cb(p);
  });
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

// openShellTerminal 在预览区新开一个本地 shell 终端。错误向上抛。
export async function openShellTerminal(
  wsPath: string,
  cols: number,
  rows: number,
): Promise<TerminalInfo> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenShellTerminal(wsPath, cols, rows);
}

// ClipboardPaste 原生剪贴板快照（internal/desktop.ClipboardPaste）：路径优先于文本。
export interface ClipboardPaste {
  Text: string;
  Path: string;
}

// readClipboardPaste 读系统剪贴板（文本 / 文件 / 位图落盘路径），供终端 Ctrl+V 注入。
export async function readClipboardPaste(): Promise<ClipboardPaste> {
  const a = app();
  if (!a) return { Text: '', Path: '' };
  return a.ReadClipboardPaste();
}

// writeTerminal 把键盘输入写进终端（data 为 base64 编码的 UTF-8 字节）。
export async function writeTerminal(id: string, data: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.WriteTerminal(id, data);
  bumpTerminalBusy(id);
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

// ---- 项目表（新建 / 逻辑删除 / 回收站）----

// createProject 弹出原生目录选择器并登记为新项目（显示名取目录名）。
// 用户取消返回空串（不算错误）；失败（目录不存在等）向上抛。
export async function createProject(): Promise<string> {
  const a = app();
  if (!a) return '';
  return a.CreateProject();
}

// hideProject 逻辑删除项目（只从列表隐藏并进回收站，磁盘内容不动）。错误向上抛。
export async function hideProject(path: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.HideProject(path);
}

// restoreProject 从回收站还原项目。错误向上抛。
export async function restoreProject(path: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.RestoreProject(path);
}

// getDeletedProjects 返回回收站列表（最近删除的在前）；绑定不可用时返回空数组。
export async function getDeletedProjects(): Promise<DeletedProject[]> {
  const a = app();
  if (!a) return [];
  return a.GetDeletedProjects();
}

// onProjectsChanged 订阅项目表变更（新建/删除/还原），payload 带最新工作区列表，
// 返回取消订阅函数。调用方据此刷新列表或关闭已消失工作区的页签。
export function onProjectsChanged(cb: (payload: { workspaces: Workspace[] }) => void): () => void {
  return EventsOn('projects:changed', (p: { workspaces?: Workspace[] }) =>
    cb({ workspaces: p?.workspaces ?? [] }),
  );
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

// ---- 颜色模式 ----

// getAppearance 返回当前模式与解析后的明暗；绑定不可用时返回跟随系统的兜底值。
export async function getAppearance(): Promise<AppearanceInfo> {
  const a = app();
  if (!a) {
    const dark =
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-color-scheme: dark)').matches;
    return { mode: 'system', resolved: dark ? 'dark' : 'light', fontSize: 13 };
  }
  return a.GetAppearance();
}

// setAppearanceMode 设置颜色模式（写回 config.yaml），错误向上抛。
export async function setAppearanceMode(mode: string): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.SetAppearanceMode(mode);
}

// setAppearanceFontSize 设置全局字号（写回 config.yaml），错误向上抛。
export async function setAppearanceFontSize(n: number): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.SetAppearanceFontSize(n);
}

// onAppearanceChanged 订阅颜色模式/系统明暗变化，返回取消订阅函数。
export function onAppearanceChanged(cb: (info: AppearanceInfo) => void): () => void {
  return EventsOn('appearance:changed', (p: AppearanceInfo) =>
    cb({
      mode: p?.mode ?? 'system',
      resolved: p?.resolved ?? 'dark',
      fontSize: typeof p?.fontSize === 'number' ? p.fontSize : 13,
    }),
  );
}

// ---- 关闭行为 ----

// getCloseBehavior 返回当前关闭行为（tray | exit）；绑定不可用时兜底 tray。
export async function getCloseBehavior(): Promise<string> {
  const a = app();
  if (!a) return 'tray';
  return a.GetCloseBehavior();
}

// setCloseBehavior 设置关闭行为（Go 侧校验并写回 config.yaml），错误向上抛。
export async function setCloseBehavior(mode: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.SetCloseBehavior(mode);
}

// ---- 模型配置 ----

// getModelConfig 返回模型注入配置（密钥只回传是否已设置的布尔）；绑定不可用时兜底空配置。
export async function getModelConfig(): Promise<ModelConfigView> {
  const a = app();
  if (!a) {
    return {
      Enabled: false,
      Preset: '',
      OpenAIBaseURL: '',
      AnthropicBaseURL: '',
      Agents: {},
      APIKeySet: false,
    };
  }
  return a.GetModelConfig();
}

// setModelConfig 保存模型注入配置（APIKey 留空不改、ClearAPIKey 为真清除），错误向上抛。
export async function setModelConfig(input: ModelConfigInput): Promise<void> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  await a.SetModelConfig(input);
}

export async function listModelPresets(): Promise<ModelPreset[]> {
  const a = app();
  if (!a) return [];
  return a.ListModelPresets();
}

export async function getSessionMode(): Promise<string> {
  const a = app();
  if (!a) return 'tui';
  return a.GetSessionMode();
}

export async function setSessionMode(mode: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.SetSessionMode(mode);
}

export async function getPermissionMode(): Promise<string> {
  const a = app();
  if (!a) return 'default';
  return a.GetPermissionMode();
}

export async function setPermissionMode(mode: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.SetPermissionMode(mode);
}

// ---- ACP 聊天 ----

export async function openSession(sessionID: string): Promise<OpenedSession> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenSession(sessionID);
}

export async function openSessionACP(sessionID: string): Promise<OpenedSession> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenSessionACP(sessionID);
}

export async function openWorkspace(wsID: string, toolID: string): Promise<OpenedSession> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenWorkspace(wsID, toolID);
}

export async function openWorkspaceACP(wsID: string, toolID: string): Promise<OpenedSession> {
  const a = app();
  if (!a) throw new Error('未检测到桌面端绑定');
  return a.OpenWorkspaceACP(wsID, toolID);
}

export async function sendChatPrompt(id: string, text: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.SendChatPrompt(id, text);
}

export async function cancelChat(id: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.CancelChat(id);
}

export async function respondChatPermission(id: string, requestID: string, optionID: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.RespondChatPermission(id, requestID, optionID);
}

export async function cancelChatPermission(id: string, requestID: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.CancelChatPermission(id, requestID);
}

export async function closeChat(id: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.CloseChat(id);
}

export async function listChats(): Promise<ChatInfo[]> {
  const a = app();
  if (!a) return [];
  return a.ListChats();
}

export async function chatHistory(id: string): Promise<ChatUpdate[]> {
  const a = app();
  if (!a) return [];
  return a.ChatHistory(id);
}

export function onTerminalMeta(cb: (info: TerminalInfo) => void): () => void {
  return EventsOn('terminal:meta', (p: TerminalInfo) => {
    if (p?.ID) cb(p);
  });
}

export function onChatMeta(cb: (info: ChatInfo) => void): () => void {
  return EventsOn('chat:meta', (p: ChatInfo) => {
    if (p?.ID) cb(p);
  });
}

export function onArchiveSuggest(cb: (p: { ref: string; summary: string }) => void): () => void {
  return EventsOn('archive:suggest', (p: { ref?: string; summary?: string }) => {
    if (p?.ref) cb({ ref: p.ref, summary: p.summary ?? '' });
  });
}

export function onArchiveChanged(cb: () => void): () => void {
  return EventsOn('archive:changed', () => cb());
}

export async function archivedIDs(): Promise<string[]> {
  const a = app();
  if (!a) return [];
  return (await a.ArchivedIDs()) ?? [];
}

export async function confirmArchive(ref: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.ConfirmArchive(ref);
}

export async function archiveSession(id: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.ArchiveSession(id);
}

export async function restoreSession(id: string): Promise<void> {
  const a = app();
  if (!a) return;
  await a.RestoreSession(id);
}

export function onChatUpdate(cb: (p: { id: string; update: ChatUpdate }) => void): () => void {
  return EventsOn('chat:update', (p: { id?: string; update?: ChatUpdate }) =>
    cb({ id: p?.id ?? '', update: p?.update as ChatUpdate }),
  );
}

export function onChatPermission(cb: (p: { id: string; request: ChatPermissionRequest }) => void): () => void {
  return EventsOn('chat:permission', (p: { id?: string; request?: ChatPermissionRequest }) =>
    cb({ id: p?.id ?? '', request: p?.request as ChatPermissionRequest }),
  );
}

export function onChatExit(cb: (p: { id: string; exitCode: number; error: string }) => void): () => void {
  return EventsOn('chat:exit', (p: { id?: string; exitCode?: number; error?: string }) =>
    cb({ id: p?.id ?? '', exitCode: p?.exitCode ?? 0, error: p?.error ?? '' }),
  );
}
