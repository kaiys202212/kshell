// 中心区 ACP 聊天页签：时间线 + 输入区 + 权限弹窗。常挂载、由父级 hidden 切换。
import { useEffect, useRef, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { ChatInfo, ChatToolCall } from '../lib/api';
import { cancelChat, cancelChatPermission, respondChatPermission, sendChatPrompt } from '../lib/api';
import { useAppStore } from '../state/store';
import type { TimelineItem } from '../state/chatUpdate';
import { cn } from '../lib/cn';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';
import * as DialogPrimitive from '@radix-ui/react-dialog';

interface Props {
  chat: ChatInfo;
  active: boolean;
}

// 模块级稳定空数组：无消息时避免每次渲染返回新引用，防止自动滚动 effect 空转
const EMPTY_ITEMS: TimelineItem[] = [];

// 工具调用结果的可读文本：文本内容直接拼接；diff 无高亮能力时降级为纯文本。
function toolContentText(t?: ChatToolCall): string {
  if (!t?.Content) return '';
  const parts: string[] = [];
  for (const raw of t.Content) {
    const item = raw as { type?: string; content?: { type?: string; text?: string }; path?: string; newText?: string };
    if (item?.type === 'content' && item.content?.type === 'text' && item.content.text) {
      parts.push(item.content.text);
    } else if (item?.type === 'diff') {
      parts.push(`--- ${item.path ?? ''}\n${item.newText ?? ''}`);
    }
  }
  return parts.join('\n');
}

export default function ChatView({ chat, active }: Props) {
  const id = chat.ID;
  const items = useAppStore((s) => s.chatItems[id]) ?? EMPTY_ITEMS;
  const permission = useAppStore((s) => s.chatPermissions[id]) ?? null;
  const [draft, setDraft] = useState('');
  const scrollRef = useRef<HTMLDivElement | null>(null);
  // 用户是否停在底部（近底 40px 内）：仅在底部时才自动滚动，避免读历史时被新消息拽走
  const atBottom = useRef(true);
  const running = chat.Status === 'running';

  useEffect(() => {
    if (!active) return;
    if (!atBottom.current) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [items, active]);

  const handleScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight <= 40;
  };

  const send = () => {
    const text = draft.trim();
    if (!text || running) return;
    setDraft('');
    // 乐观置 running：turn_done/error 的 chat:update 会把它改回 ready
    useAppStore.getState().upsertChat({ ...chat, Status: 'running' });
    // 发送失败要回退状态并提示，否则 optimistic running 会一直卡住
    sendChatPrompt(id, text).catch((e: unknown) => {
      useAppStore.getState().upsertChat({ ...chat, Status: 'ready' });
      useAppStore.getState().notify(`发送失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    });
  };

  return (
    <div className="relative flex h-full min-h-0 flex-col">
      {chat.Status === 'exited' && (
        <div className="shrink-0 bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          会话已退出{chat.ExitCode ? `（退出码 ${chat.ExitCode}）` : ''}{chat.Error ? `：${chat.Error}` : ''}
        </div>
      )}
      <div ref={scrollRef} onScroll={handleScroll} className="flex-1 overflow-y-auto px-3 py-2">
        {items.map((it) => {
          if (it.type === 'user' || it.type === 'assistant') {
            return (
              <div key={it.key} className={cn('mb-3', it.type === 'user' ? 'text-right' : '')}>
                <div className={cn('inline-block max-w-[85%] rounded-md px-3 py-1.5 text-sm',
                  it.type === 'user' ? 'bg-primary text-primary-foreground' : 'bg-muted')}>
                  {it.type === 'assistant'
                    ? <ReactMarkdown remarkPlugins={[remarkGfm]}>{it.text ?? ''}</ReactMarkdown>
                    : <span className="whitespace-pre-wrap">{it.text}</span>}
                </div>
              </div>
            );
          }
          if (it.type === 'thought') {
            return (
              <details key={it.key} className="mb-3 text-xs text-muted-foreground">
                <summary className="cursor-pointer select-none italic">思考</summary>
                <div className="mt-1 whitespace-pre-wrap rounded border border-border px-2 py-1 italic">
                  {it.text}
                </div>
              </details>
            );
          }
          if (it.type === 'tool' && it.tool) {
            const content = toolContentText(it.tool);
            return (
              <div key={it.key} className="mb-2 rounded border border-border px-2 py-1 text-xs">
                <span className="font-medium">{it.tool.Title || it.tool.Name || '工具'}</span>
                <span className="ml-2 text-muted-foreground">{it.tool.Kind}{it.tool.Status ? ` · ${it.tool.Status}` : ''}</span>
                {content && (
                  <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap text-muted-foreground">
                    {content}
                  </pre>
                )}
              </div>
            );
          }
          if (it.type === 'plan') {
            return (
              <ul key={it.key} className="mb-2 list-disc pl-5 text-xs text-muted-foreground">
                {(it.plan ?? []).map((e, i) => <li key={i}>{e.Content}</li>)}
              </ul>
            );
          }
          if (it.type === 'error') {
            return <div key={it.key} className="mb-2 text-xs text-destructive">错误：{it.text}</div>;
          }
          return null;
        })}
      </div>

      <div className="shrink-0 border-t border-border p-2">
        <textarea
          className="min-h-16 w-full resize-none rounded border border-border bg-card p-2 text-sm"
          placeholder="输入消息，Enter 发送，Shift+Enter 换行"
          value={draft}
          disabled={chat.Status !== 'ready' && chat.Status !== 'running'}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
          }}
        />
        <div className="mt-1 flex justify-end gap-2">
          {running
            ? <Button variant="outline" size="sm" onClick={() => void cancelChat(id)}>停止</Button>
            : <Button size="sm" onClick={send}>发送</Button>}
        </div>
      </div>

      {permission && (
        <Dialog
          open
          onOpenChange={(next) => {
            if (next) return;
            // Esc / 遮罩点击关闭等同「拒绝」：先乐观清掉弹窗，再通知后端取消该次请求
            useAppStore.getState().setChatPermission(id, null);
            void cancelChatPermission(id, permission.RequestID);
          }}
          className="w-96"
        >
          <DialogPrimitive.Title className="mb-1 text-sm font-medium">请求权限</DialogPrimitive.Title>
          <p className="mb-3 text-xs text-muted-foreground">{permission.ToolCall.Title || permission.ToolCall.Name || '工具调用'}</p>
          <div className="flex flex-col gap-2">
            {permission.Options.map((o) => (
              <button key={o.OptionID} className="rounded border border-border px-3 py-1.5 text-sm"
                onClick={() => {
                  // 先乐观清掉弹窗，避免 api 往返期间遮罩不消失
                  useAppStore.getState().setChatPermission(id, null);
                  void respondChatPermission(id, permission.RequestID, o.OptionID);
                }}>
                {o.Name}
              </button>
            ))}
            <button className="rounded px-3 py-1.5 text-xs text-muted-foreground"
              onClick={() => {
                useAppStore.getState().setChatPermission(id, null);
                void cancelChatPermission(id, permission.RequestID);
              }}>拒绝</button>
          </div>
        </Dialog>
      )}
    </div>
  );
}
