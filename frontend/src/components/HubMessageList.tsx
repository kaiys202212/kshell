// HubMessageList：AgentHub 消息流（与输入区 draft 解耦）。
// 自身订阅 hubMessages/chats/terminals；外层用 memo + 稳定回调，父组件
// 因 draft 重渲时跳过本树，避免整表 ReactMarkdown 重跑。
import { memo, useMemo, useState } from 'react';
import i18n from 'i18next';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { HubAskWire, HubMessage, ToolInfo } from '../lib/api';
import {
  buildMentionOptions,
  displayMentionTitles,
  stripMentionTags,
  type MentionAgent,
} from '../lib/agentMentions';
import { toolDisplayName } from '../lib/agentNotify';
import { decodePercentText } from '../lib/displayText';
import { translateBackend } from '../lib/errors';
import { formatHubTime } from '../lib/format';
import { attributionText, resolveAttribution } from '../lib/hubAttribution';
import { isHubWelcome, replyState, type HubReplyState } from '../lib/hubReply';
import { markdownBubbleClass } from '../lib/markdownStyles';
import { useAppStore } from '../state/store';

// hubHeaderLine：工具名 · 项目/会话归因 · 绝对时间；缺项跳过。用户/agent 共用。
function hubHeaderLine(m: HubMessage, attr: string): string {
  const tool = m.tool ? toolDisplayName(m.tool) : '';
  return [tool, attr, formatHubTime(m.ts)].filter(Boolean).join(' · ');
}

// 快捷操作区：继续（直发续做）/ 引用（草稿置为仅 @ 目标，就这条回复开新话题）。
// 两者显示条件一致：ACP 可投递且非欢迎词。
const QUICK_REPLIES: { key: string; action: HubQuickAction }[] = [
  { key: 'ui.hub.quick_continue', action: 'send' },
  { key: 'ui.hub.quote_reply', action: 'quote' },
];

export type HubQuickAction = 'send' | 'quote';

// listSeparator 按 UI 语言返回多选答复的连接符：答复原话会发给管理者 LLM，
// 分隔符跟随语言风格（中文顿号、其余逗号加空格）。
function listSeparator(): string {
  return i18n.language.startsWith('zh') ? '、' : ', ';
}

interface HubMessageListProps {
  tools: ToolInfo[];
  onRecall: (m: HubMessage) => void;
  onQuickReply: (m: HubMessage, action: HubQuickAction) => void;
  onAskAnswer: (answer: string) => void;
}

// ManagerAskCard：管理者反问卡片的选项交互区（hub_manager_ask 且带 ask 载荷时
// 渲染在问题正文下方）。按钮文案即答复原话：single 直接发选项，multi 勾选后
// 「确认」以顿号连接，confirm 发是/否文案。答复经 onAnswer 交 AgentHubPanel 调
// HubAskAnswer 续跑管理者循环；answered（status=answered）时整卡只读。
// 独立组件而非内联：multi 的勾选是本地受控状态，HubMessageItem 是无状态 memo 纯函数。
function ManagerAskCard({
  ask,
  answered,
  onAnswer,
}: {
  ask: HubAskWire;
  answered: boolean;
  onAnswer: (answer: string) => void;
}) {
  const { t } = useTranslation();
  const [picked, setPicked] = useState<string[]>([]);
  // 按钮样式对齐既有 quick replies；disabled 保持可读但不可点
  const optionBtn =
    'rounded border border-border bg-card px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-muted/60 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60';
  const answeredBadge = answered && (
    <span className="text-[10px] text-muted-foreground">{t('ui.hub.ask_answered')}</span>
  );

  if (ask.mode === 'multi') {
    return (
      <div className="mt-1 flex flex-col gap-1" data-testid="hub-ask-card">
        {(ask.options ?? []).map((o) => (
          <label key={o} className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <input
              type="checkbox"
              checked={picked.includes(o)}
              disabled={answered}
              onChange={(e) =>
                setPicked((p) => (e.target.checked ? [...p, o] : p.filter((x) => x !== o)))
              }
            />
            <span>{o}</span>
          </label>
        ))}
        <div>
          <button
            type="button"
            className={optionBtn}
            disabled={answered || picked.length === 0}
            onClick={() => onAnswer(picked.join(listSeparator()))}
          >
            {t('ui.hub.ask_confirm')}
          </button>
        </div>
        {answeredBadge}
      </div>
    );
  }

  if (ask.mode === 'confirm') {
    return (
      <div className="mt-1 flex flex-wrap gap-1" data-testid="hub-ask-card">
        {[t('ui.hub.ask_yes'), t('ui.hub.ask_no')].map((label) => (
          <button key={label} type="button" className={optionBtn} disabled={answered} onClick={() => onAnswer(label)}>
            {label}
          </button>
        ))}
        {answeredBadge}
      </div>
    );
  }

  // single（含 mode 缺省）：每个选项一枚按钮，点击即答复
  return (
    <div className="mt-1 flex flex-wrap gap-1" data-testid="hub-ask-card">
      {(ask.options ?? []).map((o) => (
        <button key={o} type="button" className={optionBtn} disabled={answered} onClick={() => onAnswer(o)}>
          {o}
        </button>
      ))}
      {answeredBadge}
    </div>
  );
}

interface HubMessageItemProps {
  message: HubMessage;
  attr: string;
  replySt: HubReplyState;
  userText: string;
  onRecall: (m: HubMessage) => void;
  onQuickReply: (m: HubMessage, action: HubQuickAction) => void;
  onAskAnswer: (answer: string) => void;
}

const HubMessageItem = memo(function HubMessageItem({
  message: m,
  attr,
  replySt: st,
  userText,
  onRecall,
  onQuickReply,
  onAskAnswer,
}: HubMessageItemProps) {
  const { t } = useTranslation();

  const header = hubHeaderLine(m, attr);
  // 管理者反问卡片：问题正文走既有 markdown 渲染，选项交互区由 ManagerAskCard 提供；
  // 无 ask 载荷的旧消息（历史文本）不进入本分支，按普通 markdown 消息展示。
  const isAskCard = m.event === 'hub_manager_ask' && !!m.ask;

  if (m.kind === 'user') {
    return (
      <div className="text-right">
        {header && <div className="mb-0.5 text-[11px] text-muted-foreground">{header}</div>}
        <div className="group inline-block max-w-[85%]">
          {m.status === 'recalled' ? (
            <div className="selectable inline-block break-words overflow-hidden rounded-md bg-muted px-3 py-1.5 text-left text-muted-foreground">
              <span className="mb-0.5 block text-[11px] opacity-80">{t('ui.hub.recalled')}</span>
              <span className="whitespace-pre-wrap opacity-70">{userText}</span>
            </div>
          ) : (
            <div className="selectable inline-block break-words overflow-hidden rounded-md bg-primary px-3 py-1.5 text-left text-primary-foreground">
              {/* 管理者代发的消息在卡片上打标签（不打进正文） */}
              {m.viaHub && (
                <div className="mb-0.5 text-[10px] opacity-80">{t('ui.chat.via_hub')}</div>
              )}
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
            {header}
            {/* 管理者循环的思考/结论消息（hub_manager_*）打徽标，与会话回复区分 */}
            {m.event?.startsWith('hub_manager') && (
              <span className="ml-1 rounded bg-background px-1 text-[10px] text-muted-foreground ring-1 ring-border">
                {t('ui.hub.manager_badge')}
              </span>
            )}
            {st !== 'acp' && (
              <span
                className="ml-1 rounded bg-background px-1 text-[10px] text-muted-foreground ring-1 ring-border"
                title={t(st === 'tool' ? 'ui.hub.readonly_tool' : 'ui.hub.readonly_terminal')}
              >
                {t('ui.hub.readonly_badge')}
              </span>
            )}
          </div>
          <div className={markdownBubbleClass}>
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{decodePercentText(translateBackend(m.text))}</ReactMarkdown>
          </div>
        </div>
        {isAskCard && m.ask && (
          <ManagerAskCard ask={m.ask} answered={m.status === 'answered'} onAnswer={onAskAnswer} />
        )}
        {/* 反问卡的续跑由卡片按钮驱动（答复即续轮），不再叠 quick replies 入口 */}
        {st === 'acp' && !isHubWelcome(m.text) && !isAskCard && (
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

function HubMessageList({ tools, onRecall, onQuickReply, onAskAnswer }: HubMessageListProps) {
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
          m.kind === 'user'
            ? decodePercentText(stripMentionTags(displayMentionTitles(m.text, mentionAgents), options))
            : '';
        return (
          <HubMessageItem
            key={`h${m.id}`}
            message={m}
            attr={attr}
            replySt={st}
            userText={userText}
            onRecall={onRecall}
            onQuickReply={onQuickReply}
            onAskAnswer={onAskAnswer}
          />
        );
      })}
    </>
  );
}

export default memo(HubMessageList);
