// 工作区页签：三栏布局（左右两栏宽度可拖动）。
//   左栏：新建会话（含工具选择）+ 会话列表（「恢复」开中心区内嵌终端）
//   中栏：上下文篮 + 中心区页签（预览 / 每个内嵌终端一个页签）
//   右栏：文件 | SSH 子页签（点文件自动切到中栏的预览页签）
// 终端页签一旦打开就常挂载（非激活用 hidden），xterm 缓冲与焦点不丢；
// 工作区页签本身也由 App 常挂载，因此只有关闭页签才会真正结束终端进程。
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  closeTerminal,
  getTools,
  listTerminals,
  onScanDone,
  openSessionTerminal,
  openWorkspaceTerminal,
} from '../lib/api';
import type { Session, TerminalInfo, ToolInfo } from '../lib/api';
import { badgeFor } from '../lib/toolBadge';
import { cn } from '../lib/cn';
import BasketBar from '../components/BasketBar';
import FileTree from '../components/FileTree';
import Preview from '../components/Preview';
import ResizeHandle from '../components/ResizeHandle';
import SessionList from '../components/SessionList';
import SshPanel from '../components/SshPanel';
import TerminalView from '../components/TerminalView';
import ToolPicker from '../components/ToolPicker';
import { Button } from '../components/ui/button';
import { LAYOUT_DEFAULT, useAppStore } from '../state/store';
import type { WorkspaceTab } from '../state/store';

type RightPane = 'files' | 'ssh';

// 中心区固定页签「预览」的保留 id（终端 id 形如 t1，不会冲突）
const PREVIEW_TAB = 'preview';

// 右栏「文件 | SSH」子页签：扁平下划线式
const paneTabBase =
  'flex h-7 items-center px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
const paneTabActive = 'font-medium text-foreground';

// 中心区页签：与标题栏同语言（下划线激活），终端页签带工具徽标与关闭键
const centerTabBase =
  'group relative flex h-8 max-w-56 shrink-0 items-center gap-1.5 px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground';
const centerTabActive = 'font-medium text-foreground';

// 工作区路径归一化比较（会话记录里的 cwd 可能大小写/分隔符不一致）
function sameWorkspace(a: string, b: string): boolean {
  return a.replace(/\\/g, '/').toLowerCase() === b.replace(/\\/g, '/').toLowerCase();
}

export default function WorkspaceTabView({ tab, visible }: { tab: WorkspaceTab; visible: boolean }) {
  const [rightPane, setRightPane] = useState<RightPane>('files');
  const [previewPath, setPreviewPath] = useState<string | null>(null);
  const [centerTab, setCenterTab] = useState<string>(PREVIEW_TAB);
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [busy, setBusy] = useState(false);
  // 「新建会话」先弹 agent 选择：展开态提升到父级，选中后由 startSession 直接启动
  const [pickerOpen, setPickerOpen] = useState(false);

  const layout = useAppStore((s) => s.layout);
  const setLayout = useAppStore((s) => s.setLayout);
  const terminals = useAppStore((s) => s.terminals);
  const notify = useAppStore((s) => s.notify);
  // 新建会话的工具选择：全局持久化（'' = 自动），跨页签/重启记住用户的选择
  const toolId = useAppStore((s) => s.newSessionTool);
  const setToolId = useAppStore((s) => s.setNewSessionTool);


  // 本工作区的内嵌终端（按创建顺序）
  const terms = useMemo<TerminalInfo[]>(
    () => terminals.filter((t) => sameWorkspace(t.Workspace, tab.id)),
    [terminals, tab.id],
  );

  useEffect(() => {
    // 挂载时补一次镜像：页签是常挂载的，但终端可能在别的工作区页签里被创建
    listTerminals()
      .then((list) => useAppStore.getState().setTerminals(list))
      .catch(() => {});
  }, []);

  useEffect(() => {
    // 工具列表只在挂载时取一次（用于「选择 agent」下拉），而扫描是异步的：
    // 必须订阅 scan:done 再取一次，否则挂载早于扫描完成时下拉会一直禁用，
    // 表现为「没有选择 agent 的选项」。
    const refresh = () => {
      getTools()
        .then((list) => setTools(list.filter((t) => t.Installed && t.BinPath !== '')))
        .catch(() => {});
    };
    refresh();
    return onScanDone(refresh);
  }, []);

  useEffect(() => {
    // 当前中心区页签指向的终端已不存在（被关闭/退出后清理）时退回预览
    if (centerTab !== PREVIEW_TAB && !terms.some((t) => t.ID === centerTab)) {
      setCenterTab(PREVIEW_TAB);
    }
  }, [terms, centerTab]);

  // 恢复历史会话 → 中心区内嵌终端（Go 侧同一会话幂等复用进程）
  const openTerminalForSession = useCallback(
    (s: Session) => {
      void openSessionTerminal(s.ID, 0, 0)
        .then((info) => {
          useAppStore.getState().upsertTerminal(info);
          setCenterTab(info.ID);
        })
        .catch((e: unknown) => {
          notify(`打开终端失败：${e instanceof Error ? e.message : String(e)}`, 'error');
        });
    },
    [notify],
  );

  // 新建会话：先让用户选 agent（含「自动」），选中后开中心区内嵌终端。
  // 不再直接启动——避免用户想选 agent 时没有入口，也避免走到会弹系统黑窗的外部终端路径。
  const handleNewSession = () => {
    if (busy) return;
    if (tools.length === 0) {
      notify('未检测到可用的 agent：请先安装 Claude Code / Codex 等 CLI，或点顶栏「重扫」', 'error');
      return;
    }
    setPickerOpen(true);
  };

  // 选中 agent 后启动内嵌终端（空 id = 交给 Go 侧按该工作区最常用的工具选）
  const startSession = async (id: string) => {
    if (busy) return;
    setBusy(true);
    try {
      const info = await openWorkspaceTerminal(tab.id, id, 0, 0);
      useAppStore.getState().upsertTerminal(info);
      setCenterTab(info.ID);
    } catch (e: unknown) {
      notify(`新建会话失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setBusy(false);
    }
  };

  const handleCloseTerminal = (id: string) => {
    useAppStore.getState().removeTerminal(id);
    closeTerminal(id).catch(() => {});
    if (centerTab === id) setCenterTab(PREVIEW_TAB);
  };

  const openFile = (path: string) => {
    setPreviewPath(path);
    setCenterTab(PREVIEW_TAB);
  };

  return (
    <div className="flex min-h-0 flex-1">
      <aside
        className="flex shrink-0 flex-col gap-2 overflow-y-auto border-r border-border bg-card p-3"
        style={{ width: layout.left }}
        aria-label="会话列表栏"
      >
        <div className="flex items-center gap-1">
          <Button className="min-w-0 flex-1" disabled={busy} onClick={handleNewSession}>
            {busy ? '启动中…' : '新建会话'}
          </Button>
          <ToolPicker
            tools={tools}
            value={toolId}
            onChange={setToolId}
            open={pickerOpen}
            onOpenChange={setPickerOpen}
            onSelect={(id) => void startSession(id)}
          />
        </div>
        <SessionList workspacePath={tab.id} onOpenTerminal={openTerminalForSession} />
      </aside>

      <ResizeHandle
        side="left"
        width={layout.left}
        onResize={(w) => setLayout({ left: w })}
        defaultWidth={LAYOUT_DEFAULT.left}
        label="调整会话列表宽度"
      />

      <main className="flex min-w-0 flex-1 flex-col">
        <BasketBar />
        {/* 中心区页签条：预览固定，其后是本工作区的内嵌终端 */}
        <div
          className="flex shrink-0 items-stretch overflow-x-auto border-b border-border"
          role="tablist"
          aria-label="中心区页签"
        >
          <button
            role="tab"
            aria-selected={centerTab === PREVIEW_TAB}
            className={cn(centerTabBase, centerTab === PREVIEW_TAB && centerTabActive)}
            onClick={() => setCenterTab(PREVIEW_TAB)}
          >
            预览
            {centerTab === PREVIEW_TAB && (
              <span className="absolute inset-x-2 bottom-0 h-0.5 bg-primary" />
            )}
          </button>
          {terms.map((t) => {
            const badge = badgeFor(t.ToolID);
            const active = centerTab === t.ID;
            return (
              <div
                key={t.ID}
                className={cn(centerTabBase, active && centerTabActive)}
                onAuxClick={(e) => {
                  if (e.button === 1) {
                    e.preventDefault();
                    handleCloseTerminal(t.ID);
                  }
                }}
              >
                {t.Status === 'exited' && (
                  <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground" />
                )}
                <button
                  role="tab"
                  aria-selected={active}
                  className="min-w-0 truncate text-xs"
                  title={`${t.Title}${t.ToolID ? `（${badge.label}）` : ''}`}
                  onClick={() => setCenterTab(t.ID)}
                >
                  {t.Title}
                </button>
                {t.ToolID && (
                  <span className="shrink-0 text-[10px] text-muted-foreground">{badge.label}</span>
                )}
                <button
                  className={cn(
                    'ml-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-sm leading-none text-muted-foreground transition-opacity hover:bg-muted hover:text-foreground',
                    active ? 'opacity-70 hover:opacity-100' : 'opacity-0 group-hover:opacity-100',
                  )}
                  aria-label={`关闭终端 ${t.Title}`}
                  onClick={() => handleCloseTerminal(t.ID)}
                >
                  ×
                </button>
                {active && <span className="absolute inset-x-2 bottom-0 h-0.5 bg-primary" />}
              </div>
            );
          })}
        </div>

        <div className="min-h-0 flex-1 overflow-hidden">
          {/* 预览页签：外层负责滚动与内边距，终端页签各自撑满（xterm 自己管滚动） */}
          <div className={cn('h-full overflow-y-auto p-3', centerTab !== PREVIEW_TAB && 'hidden')}>
            <Preview wsPath={tab.id} path={previewPath} />
          </div>
          {terms.map((t) => (
            <div key={t.ID} className={cn('h-full', centerTab !== t.ID && 'hidden')}>
              <TerminalView term={t} active={visible && centerTab === t.ID} />
            </div>
          ))}
        </div>
      </main>

      <ResizeHandle
        side="right"
        width={layout.right}
        onResize={(w) => setLayout({ right: w })}
        defaultWidth={LAYOUT_DEFAULT.right}
        label="调整文件面板宽度"
      />

      <aside
        className="flex shrink-0 flex-col overflow-y-auto border-l border-border bg-card p-3"
        style={{ width: layout.right }}
        aria-label="文件与 SSH 面板"
      >
        <div className="mb-2 flex gap-0.5 border-b border-border">
          <button
            className={cn(paneTabBase, rightPane === 'files' && paneTabActive)}
            aria-pressed={rightPane === 'files'}
            onClick={() => setRightPane('files')}
          >
            文件
          </button>
          <button
            className={cn(paneTabBase, rightPane === 'ssh' && paneTabActive)}
            aria-pressed={rightPane === 'ssh'}
            onClick={() => setRightPane('ssh')}
          >
            SSH
          </button>
        </div>
        {/* 双面板常挂载，仅用 hidden 切换显示：切「文件|SSH」页签不再卸载重载，
            文件树展开态与 SSH 连接列表/命令历史得以保留 */}
        <div className={cn('min-h-0 flex-1', rightPane !== 'files' && 'hidden')}>
          <FileTree wsPath={tab.id} onOpenFile={openFile} />
        </div>
        <div className={cn('min-h-0 flex-1', rightPane !== 'ssh' && 'hidden')}>
          <SshPanel wsPath={tab.id} />
        </div>
      </aside>
    </div>
  );
}
