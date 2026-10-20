// HubMessageList：AgentHub 消息流（与输入区 draft 解耦）。
// 自身订阅 hubMessages/chats/terminals；外层用 memo + 稳定回调，父组件
// 因 draft 重渲时跳过本树，避免整表 ReactMarkdown 重跑。
import { memo, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { HubMessage, ToolInfo } from '../lib/api';
import { confirmHubTool } from '../lib/api';
import {
  buildMentionOptions,
  displayMentionTitles,
  stripMentionTags,
  type MentionAgent,
} from '../lib/agentMentions';
import { toolDisplayName } from '../lib/agentNotify';
import { translateBackend } from '../lib/errors';
import { attributionText, resolveAttribution } from '../lib/hubAttribution';
import { replyState, type HubReplyState } from '../lib/hubReply';
import { markdownBubbleClass } from '../lib/markdownStyles';
import { useAppStore } from '../state/store';
import HubConfirmCard from './HubConfirmCard';

const QUICK_REPLIES: { key: string; action: 'send' | 'rephrase' }[] = [
  { key: 'ui.hub.quick_continue', action: 'send' },
  { key: 'ui.hub.quick_rephrase', action: 'rephrase' },
];

export type HubQuickAction = 'send' | 'rephrase';

interface HubMessageListProps {
  tools: ToolInfo[];
  onRecall: (m: HubMessage) => void;
  onQuickReply: (m: HubMessage, action: HubQuickAction) => void;
}

interface HubMessageItemProps {
  message: HubMessage;
  attr: string;
  replySt: HubReplyState;
  userText: string;
  onRecall: (m: HubMessage) => void;
  onQuickReply: (m: HubMessage, action: HubQuickAction) => void;
}

const HubMessageItem = memo(function HubMessageItem({
  message: m,
  attr,
  replySt: st,
  userText,
  onRecall,
  onQuickReply,
}: HubMessageItemProps) {
  const { t } = useTranslation();

  if (m.kind === 'user') {
    return (
      <div className="text-right">
        {attr && <div className="mb-0.5 text-[11px] text-muted-foreground">{attr}</div>}
        <div className="group inline-block max-w-[85%]">
          {m.status === 'recalled' ? (
            <div className="selectable inline-block break-words overflow-hidden rounded-md bg-muted px-3 py-1.5 text-left text-muted-foreground">
              <span className="mb-0.5 block text-[11px] opacity-80">{t('ui.hub.recalled')}</span>
              <span className="whitespace-pre-wrap opacity-70">{userText}</span>
            </div>
          ) : (
            <div className="selectable inline-block break-words overflow-hidden rounded-md bg-primary px-3 py-1.5 text-left text-primary-foreground">
              <span className="whitespace-pre-wrap">{userText}</span>
            </div>
          )}
          {m.status === 'queued' && (
            <div className="mt-1">
              <button
                type="button"
                className="rounded border border-border bg-card px-2 py-0.5 text-[11px] text-muted-foreground hover:text-destructive"
                onClick={() => onRecall(m)}
              >
                {t('ui.hub.recall')}
              </button>
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="text-left">
      <div className="group inline-block max-w-[85%]">
        <div className="selectable break-words overflow-hidden rounded-md bg-muted px-3 py-1.5 [&_pre]:whitespace-pre-wrap">
          <div className="mb-0.5 text-[11px] text-muted-foreground">
            {[toolDisplayName(m.tool ?? ''), attr].filter(Boolean).join(' · ')}
            {st !== 'acp' && (
              <span
                className="ml-1 rounded bg-background px-1 text-[10px] text-muted-foreground ring-1 ring-border"
                title={t(st === 'tool' ? 'ui.hub.readonly_tool' : 'ui.hub.readonly_terminal')}
              >
                {t('ui.hub.readonly_badge')}
              </span>
            )}
          </div>
          {m.event === 'tool_confirm' ? (
            <HubConfirmCard
              message={m}
              onConfirm={(pendingID, approved) => confirmHubTool(m.chatId ?? '', pendingID, approved)}
            />
          ) : (
            <div className={markdownBubbleClass}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{translateBackend(m.text)}</ReactMarkdown>
            </div>
          )}
        </div>
        {st === 'acp' && (
          <div className="mt-1 flex flex-wrap gap-1">
            {QUICK_REPLIES.map((q) => (
              <button
                key={q.key}
                type="button"
                className="rounded border border-border bg-card px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                onClick={() => onQuickReply(m, q.action)}
              >
                {t(q.key)}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
});

function HubMessageList({ tools, onRecall, onQuickReply }: HubMessageListProps) {
  const hubMessages = useAppStore((s) => s.hubMessages);
  const chats = useAppStore((s) => s.chats);
  const terminals = useAppStore((s) => s.terminals);

  const mentionAgents = useMemo(
    () =>
      chats.map<MentionAgent>((c) => ({
        id: c.ID,
        toolId: c.ToolID,
        title: c.Title,
        workspace: c.Workspace,
      })),
    [chats],
  );
  const options = useMemo(() => buildMentionOptions(mentionAgents), [mentionAgents]);

  return (
    <>
      {hubMessages.map((m) => {
        const attr = attributionText(resolveAttribution(m, chats, terminals));
        const st = replyState(m, tools, terminals);
        const userText =
          m.kind === 'user' ? stripMentionTags(displayMentionTitles(m.text, mentionAgents), options) : '';
        return (
          <HubMessageItem
            key={`h${m.id}`}
            message={m}
            attr={attr}
            replySt={st}
            userText={userText}
            onRecall={onRecall}
            onQuickReply={onQuickReply}
          />
        );
      })}
    </>
  );
}

export default memo(HubMessageList);
