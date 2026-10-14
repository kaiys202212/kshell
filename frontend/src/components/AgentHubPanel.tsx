// AgentHubPanel：首页的跨项目 agent 汇总管理聊天窗。
// 与 ChatView 的区别：ChatView 绑定单个 chat.ID，本组件是跨会话聚合视图——
// 左侧列出所有运行中的 ACP agent（按项目分组、点击跳转），右侧合并展示
// 用户发出的消息与 agent 结束通知（summary 为 agent 回复正文）。
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { ChatInfo, ToolInfo } from '../lib/api';
import { getTools, openWorkspaceACP, readClipboardPaste, sendChatPrompt } from '../lib/api';
import { buildMentionOptions, resolveMentionTarget, type MentionAgent } from '../lib/agentMentions';
import { toolDisplayName } from '../lib/agentNotify';
import { quotePathForShell } from '../lib/dragPath';
import { backendError } from '../lib/errors';
import { sameWorkspacePath } from '../lib/workspacePath';
import { cn } from '../lib/cn';
import { LIST_ROW, LIST_ROW_ACTIVE, MONO } from '../lib/ui';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Dialog } from './ui/dialog';
import * as DialogPrimitive from '@radix-ui/react-dialog';

// 消息流里用户发出的一条（仅本地镜像；agent 回复走 store.agentNotices）。
interface SentItem {
  id: number;
  targetLabel: string;
  text: string;
}

// statusKey 把 chat.Status 映射到状态点文案 key（未知按空闲处理）。
function statusKey(status: string): string {
  if (status === 'running' || status === 'starting') return 'ui.hub.status_running';
  if (status === 'exited') return 'ui.hub.status_exited';
  return 'ui.hub.status_ready';
}

// statusDotClass 状态点配色：运行中用主色（Teal），退出用 muted，其余描边。
function statusDotClass(status: string): string {
  if (status === 'running' || status === 'starting') return 'bg-primary';
  if (status === 'exited') return 'bg-muted-foreground/40';
  return 'border border-muted-foreground bg-transparent';
}

// wsBaseName 取路径最后一段，作为项目分组标题。
function wsBaseName(ws: string): string {
  const n = ws.replace(/[\\/]+$/, '');
  const i = Math.max(n.lastIndexOf('/'), n.lastIndexOf('\\'));
  return i >= 0 ? n.slice(i + 1) : n;
}

export default function AgentHubPanel() {
  const { t } = useTranslation();
  const chats = useAppStore((s) => s.chats);
  const agentNotices = useAppStore((s) => s.agentNotices);
  const lastAgentId = useAppStore((s) => s.lastAgentId);

  const emptyTarget = t('ui.hub.new_agent');
  const [draft, setDraft] = useState('');
  const [sent, setSent] = useState<SentItem[]>([]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const sentSeq = useRef(0);

  // @ 补全项：按 store.chats 顺序生成稳定唯一展示名（Claude Code#1 ...）
  const options = useMemo(
    () =>
      buildMentionOptions(
        chats.map<MentionAgent>((c) => ({
          id: c.ID,
          toolId: c.ToolID,
          title: c.Title,
          workspace: c.Workspace,
        })),
      ),
    [chats],
  );

  // 按 Workspace 分组（用 sameWorkspacePath 归并大小写/分隔符差异）
  const groups = useMemo(() => {
    const out: { workspace: string; rows: ChatInfo[] }[] = [];
    for (const c of chats) {
      const g = out.find((x) => sameWorkspacePath(x.workspace, c.Workspace));
      if (g) g.rows.push(c);
      else out.push({ workspace: c.Workspace, rows: [c] });
    }
    return out;
  }, [chats]);

  // @ 补全：草稿末尾是未闭合的 @ 片段时给出候选（v1 只按草稿尾匹配，不做光标跟踪）。
  // mentionQuery 为 null 表示当前不在 @ 输入中。
  const mentionQuery = useMemo(() => {
    const m = /(?:^|\s)@([^\s@]*)$/.exec(draft);
    return m ? m[1].toLowerCase() : null;
  }, [draft]);
  const mentionMatches = useMemo(() => {
    if (mentionQuery === null) return [];
    return options.filter((o) => o.label.toLowerCase().includes(mentionQuery));
  }, [mentionQuery, options]);

  // applyMention 把草稿尾部的 @ 片段替换成完整标签（补一个尾随空格方便继续输入）
  const applyMention = (label: string) => {
    setDraft((d) => d.replace(/@([^\s@]*)$/, `@${label} `));
  };

  // focusRow 跳转：照搬 ActiveTerminalsPanel.focusRow——切到该 workspace 页签并按 termKey 聚焦
  const focusRow = (c: ChatInfo) => {
    const { openTabs, setActiveTab, requestFocusTerm, setLastAgentId } = useAppStore.getState();
    setLastAgentId(c.ID);
    const tab = openTabs.find((x) => sameWorkspacePath(x.id, c.Workspace));
    if (tab) setActiveTab(tab.id);
    if (c.Key) requestFocusTerm(c.Key);
  };

  const send = () => {
    const text = draft.trim();
    if (!text) return;
    const targetId = resolveMentionTarget(text, options, lastAgentId);
    if (!targetId) {
      useAppStore.getState().notify(t('ui.hub.need_target'), 'error');
      return;
    }
    const opt = options.find((o) => o.id === targetId);
    // 乐观追加本地条目；发送失败再回退并提示
    const item: SentItem = { id: ++sentSeq.current, targetLabel: opt?.label ?? toolDisplayName(''), text };
    setSent((s) => [...s, item]);
    setDraft('');
    void sendChatPrompt(targetId, text)
      .then(() => {
        useAppStore.getState().setLastAgentId(targetId);
      })
      .catch((e: unknown) => {
        setSent((s) => s.filter((x) => x.id !== item.id));
        useAppStore.getState().notify(t('ui.chat.send_failed', { err: backendError(e) }), 'error');
      });
  };

  // 粘贴：优先用剪贴板里的落盘路径（图片），其次文本；路径含空格加引号避免 shell 拆词
  const handlePaste = async () => {
    try {
      const paste = await readClipboardPaste();
      const add = paste.Path ? quotePathForShell(paste.Path) : paste.Text;
      if (!add) return;
      setDraft((d) => (d ? `${d} ${add}` : add));
    } catch {
      // 读取失败静默：粘贴是辅助操作，不打断输入
    }
  };

  return (
    <section
      aria-label={t('ui.hub.title')}
      className="flex flex-col gap-2.5 rounded border border-border bg-card p-3 text-sm"
    >
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-medium">{t('ui.hub.title')}</h2>
        <Button
          size="sm"
          variant="secondary"
          aria-label={t('ui.hub.new_agent')}
          onClick={() => setDialogOpen(true)}
        >
          + {t('ui.hub.new_agent')}
        </Button>
      </div>

      {chats.length === 0 ? (
        <EmptyState title={t('ui.hub.empty')} />
      ) : (
        <div className="flex flex-col gap-2">
          {groups.map((g) => (
            <div key={g.workspace} className="flex flex-col gap-1">
              <span className={cn(MONO, 'truncate')} title={g.workspace}>
                {wsBaseName(g.workspace)}
              </span>
              <ul aria-label={t('ui.hub.title')} className="m-0 flex list-none flex-col gap-1.5 p-0">
                {g.rows.map((c) => (
                  <li key={c.ID}>
                    <button
                      type="button"
                      className={cn(
                        LIST_ROW,
                        'flex w-full min-w-0 items-center gap-1.5 px-2.5 py-1.5 text-left',
                        lastAgentId === c.ID && LIST_ROW_ACTIVE,
                      )}
                      onClick={() => focusRow(c)}
                    >
                      <span
                        aria-hidden
                        className={cn('h-1.5 w-1.5 shrink-0 rounded-full', statusDotClass(c.Status))}
                      />
                      <span className="min-w-0 truncate font-medium">
                        {c.Title || emptyTarget}
                      </span>
                      <span className="ml-auto shrink-0 truncate text-[11px] text-muted-foreground">
                        {toolDisplayName(c.ToolID)} · {t(statusKey(c.Status))}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}

      <div className="flex flex-col gap-1 overflow-y-auto" role="log" aria-live="polite">
        {sent.map((m) => (
          <div key={`s${m.id}`} className="text-right">
            <div className="selectable inline-block max-w-[85%] rounded-md bg-primary px-3 py-1.5 text-primary-foreground">
              <span className="whitespace-pre-wrap">{m.text}</span>
            </div>
          </div>
        ))}
        {agentNotices.map((n) => (
          <div key={`n${n.id}`} className="text-left">
            <div className="selectable inline-block max-w-[85%] rounded-md bg-muted px-3 py-1.5">
              <span className="mr-1.5 text-[11px] text-muted-foreground">{toolDisplayName(n.tool)}</span>
              <span className="whitespace-pre-wrap">{n.summary}</span>
            </div>
          </div>
        ))}
      </div>

      <div className="flex flex-col gap-1 border-t border-border pt-2">
        {mentionMatches.length > 0 && (
          <ul
            aria-label={t('ui.hub.mention_aria')}
            className="m-0 flex list-none flex-col gap-0.5 rounded border border-border bg-card p-1 text-xs"
          >
            {mentionMatches.map((o) => (
              <li key={o.id}>
                <button
                  type="button"
                  className="w-full rounded px-2 py-1 text-left font-mono hover:bg-muted/50"
                  onClick={() => applyMention(o.label)}
                >
                  @{o.label}
                </button>
              </li>
            ))}
          </ul>
        )}
        <textarea
          className="min-h-16 w-full resize-none rounded border border-border bg-card p-2 text-sm"
          placeholder={t('ui.hub.input_placeholder')}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onPaste={() => void handlePaste()}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              send();
            }
          }}
        />
        <div className="flex justify-end">
          <Button size="sm" onClick={send}>
            {t('ui.hub.send')}
          </Button>
        </div>
      </div>

      <NewAgentDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </section>
  );
}

// NewAgentDialog：选项目 + ACP 工具，调后端新建会话并登记到 store。
function NewAgentDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation();
  const workspaces = useAppStore((s) => s.workspaces);
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [wsPath, setWsPath] = useState('');
  const [toolId, setToolId] = useState('');
  const [busy, setBusy] = useState(false);

  // 打开对话框时才拉工具列表（避免常驻首页也发请求）
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void getTools()
      .then((list) => {
        if (cancelled) return;
        const acp = list.filter((x) => x.ACP?.Available === true);
        setTools(acp);
        setToolId((cur) => (cur && acp.some((x) => x.ID === cur) ? cur : (acp[0]?.ID ?? '')));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [open]);

  // 默认选中第一个项目
  useEffect(() => {
    if (!open) return;
    setWsPath((cur) => (cur && workspaces.some((w) => w.Path === cur) ? cur : (workspaces[0]?.Path ?? '')));
  }, [open, workspaces]);

  const create = async () => {
    if (!wsPath || !toolId || busy) return;
    setBusy(true);
    try {
      const result = await openWorkspaceACP(wsPath, toolId);
      if (result.Chat) {
        useAppStore.getState().upsertChat(result.Chat);
        useAppStore.getState().setLastAgentId(result.Chat.ID);
      }
      onOpenChange(false);
    } catch (e: unknown) {
      useAppStore.getState().notify(backendError(e), 'error');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="w-80">
      <DialogPrimitive.Title className="mb-3 text-sm font-medium">
        {t('ui.hub.new_agent_title')}
      </DialogPrimitive.Title>
      <label className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.hub.field_project')}
        <select
          className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
          value={wsPath}
          onChange={(e) => setWsPath(e.target.value)}
        >
          {workspaces.map((w) => (
            <option key={w.Path} value={w.Path}>
              {w.Name}
            </option>
          ))}
        </select>
      </label>
      <label className="mb-4 flex flex-col gap-1 text-xs text-muted-foreground">
        {t('ui.hub.field_tool')}
        <select
          className="rounded border border-border bg-card px-2 py-1 text-sm text-foreground"
          value={toolId}
          onChange={(e) => setToolId(e.target.value)}
        >
          {tools.map((x) => (
            <option key={x.ID} value={x.ID}>
              {toolDisplayName(x.ID)}
            </option>
          ))}
        </select>
      </label>
      {tools.length === 0 && <p className="mb-3 text-xs text-muted-foreground">{t('ui.hub.no_tools')}</p>}
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
          {t('ui.hub.cancel')}
        </Button>
        <Button size="sm" disabled={!wsPath || !toolId || busy} onClick={() => void create()}>
          {t('ui.hub.create')}
        </Button>
      </div>
    </Dialog>
  );
}
