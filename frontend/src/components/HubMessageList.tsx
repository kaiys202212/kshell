// HubMessageList：AgentHub 消息流（与输入区 draft 解耦）。
// 自身订阅 hubMessages/chats/terminals；外层用 memo + 稳定回调，父组件
// 因 draft 重渲时跳过本树，避免整表 ReactMarkdown 重跑。
import { memo, useMemo, useState } from 'react';
import i18n from 'i18next';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { HubAskWire, HubMessage, HubStep, ToolInfo } from '../lib/api';
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
import { cn } from '../lib/cn';
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
  onAskAnswer: (answer: string) => Promise<void>;
}

// —— ManagerAskCard 样式常量：与整体 UI 风格（token 体系）统一 ——
// 未选态：卡片底 + 边框，hover 提亮；选中态：主色边框/底色 + ring 强调。
const askOptionIdle =
  'border-border bg-card text-muted-foreground hover:border-primary/40 hover:text-foreground';
const askOptionSelected = 'border-primary bg-primary/10 text-foreground ring-1 ring-primary';
// 步骤导航次级按钮
const askNavBtn =
  'rounded border border-border bg-card px-2 py-1 text-[11px] text-muted-foreground hover:bg-muted/60 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50';

// AskOptionButton：单枚选项按钮，自带选中指示器（multi 方形打勾 / 其余圆形圆点），
// 替代原生 input/checkbox——点击面积更大且选中态可样式化。
function AskOptionButton({
  label,
  selected,
  multi,
  disabled,
  onClick,
}: {
  label: string;
  selected: boolean;
  multi: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-pressed={selected}
      onClick={onClick}
      className={cn(
        'flex items-center gap-2 rounded-md border px-2.5 py-1 text-left text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-60',
        selected ? askOptionSelected : askOptionIdle,
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          'flex h-3.5 w-3.5 shrink-0 items-center justify-center border transition-colors',
          multi ? 'rounded-sm' : 'rounded-full',
          selected ? 'border-primary bg-primary' : 'border-muted-foreground/50',
        )}
      >
        {selected &&
          (multi ? (
            <span className="text-[9px] leading-none text-primary-foreground">✓</span>
          ) : (
            <span className="h-1.5 w-1.5 rounded-full bg-primary-foreground" />
          ))}
      </span>
      <span className="break-words">{label}</span>
    </button>
  );
}

// AskProcessing：点击/提交后的「处理中」紧凑指示（spinner + 文案），选项区
// 即刻收起为此形态，让用户明确交互已生效。
function AskProcessing({ label }: { label: string }) {
  return (
    <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted-foreground" data-testid="hub-ask-card">
      <span
        aria-hidden="true"
        className="h-3 w-3 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent"
      />
      <span>{label}</span>
    </div>
  );
}

// ManagerAskCard：管理者反问卡片的选项交互区（hub_manager_ask 且带 ask 载荷时
// 渲染在问题正文下方）。答复经 onAnswer 交 AgentHubPanel 调 HubAskAnswer 续跑
// 管理者循环；返回 promise 供「点击即收起」——点击瞬间选项区收起为处理中指示，
// 失败恢复可重试，成功后保持收起直到 status=answered 回流（2s 兜底防卡死）。
// 多步问卷（ask.steps ≥2）走向导：步骤 chips 切换 + 每步选择本地暂存 +
// 全部作答才可提交，提交组合各步答案一次性发送。
function ManagerAskCard({
  ask,
  answered,
  onAnswer,
}: {
  ask: HubAskWire;
  answered: boolean;
  onAnswer: (answer: string) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [submitting, setSubmitting] = useState(false);
  // 经典 multi 模式的勾选暂存（多步问卷走 answers，互不相干）
  const [picked, setPicked] = useState<string[]>([]);
  // 提交：立即收起选项区；失败（panel 已 toast）立即恢复可重试；成功保持
  // 收起等 answered 回流（通常一两个 IPC 往返），超时兜底恢复。
  const submit = (answer: string) => {
    if (submitting || answered) return;
    setSubmitting(true);
    let failed = false;
    Promise.resolve(onAnswer(answer))
      .catch(() => {
        failed = true;
      })
      .finally(() => {
        if (failed) setSubmitting(false);
        else window.setTimeout(() => setSubmitting(false), 2000);
      });
  };

  // 多步问卷：每步选择暂存（string[] 兼容 multi 多选）；chips/导航自由切换。
  const wizardSteps = ask.steps && ask.steps.length >= 2 ? ask.steps : null;
  const [stepIdx, setStepIdx] = useState(0);
  const [answers, setAnswers] = useState<string[][]>(() =>
    wizardSteps ? wizardSteps.map(() => []) : [],
  );

  const answeredBadge = (
    <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground ring-1 ring-border">
      {t('ui.hub.ask_answered')}
    </span>
  );

  // 收起形态：answered（含历史已答卡片）或提交中——选项区不再停留。
  if (answered || submitting) {
    if (answered) {
      return (
        <div className="mt-1 flex items-center" data-testid="hub-ask-card">
          {answeredBadge}
        </div>
      );
    }
    return <AskProcessing label={t('ui.hub.ask_processing')} />;
  }

  if (wizardSteps) {
    const steps = wizardSteps;
    const current = steps[stepIdx];
    const picked = answers[stepIdx] ?? [];
    const allAnswered = answers.every((a) => a.length > 0);
    // 提交文案：每步「问题：答案」，多选顿号/逗号连接，步间换行。
    const composed = steps.map((s, i) => `${s.question}：${(answers[i] ?? []).join(listSeparator())}`).join('\n');
    const pick = (option: string) => {
      setAnswers((prev) => {
        const next = prev.map((a) => [...a]);
        const arr = next[stepIdx] ?? [];
        if (current.mode === 'multi') {
          next[stepIdx] = arr.includes(option) ? arr.filter((x) => x !== option) : [...arr, option];
        } else {
          next[stepIdx] = [option];
        }
        return next;
      });
    };
    return (
      <div
        className="mt-1 flex flex-col gap-2 rounded-md border border-border/60 bg-background/40 p-2"
        data-testid="hub-ask-card"
      >
        {/* 步骤指示：chips 可点跳转，done/current 两态强调 */}
        <div className="flex items-center gap-1">
          {steps.map((s, i) => {
            const done = (answers[i] ?? []).length > 0;
            const isCur = i === stepIdx;
            return (
              <button
                key={s.question}
                type="button"
                aria-label={t('ui.hub.ask_step', { i: i + 1, n: steps.length })}
                onClick={() => setStepIdx(i)}
                className={cn(
                  'flex h-6 w-6 items-center justify-center rounded-full border text-[11px] transition-colors',
                  isCur
                    ? 'border-primary bg-primary text-primary-foreground'
                    : done
                      ? 'border-primary/60 bg-primary/10 text-primary'
                      : 'border-border text-muted-foreground hover:border-primary/40 hover:text-foreground',
                )}
              >
                {i + 1}
              </button>
            );
          })}
          <span className="ml-1 text-[11px] text-muted-foreground">
            {t('ui.hub.ask_step', { i: stepIdx + 1, n: steps.length })}
          </span>
        </div>
        {/* 当前步问题 + 该步模式的选项 */}
        <div className="text-xs font-medium text-foreground">{current.question}</div>
        <div className="flex flex-col items-start gap-1.5">
          {(current.options ?? []).map((o) => (
            <AskOptionButton
              key={o}
              label={o}
              selected={picked.includes(o)}
              multi={current.mode === 'multi'}
              disabled={false}
              onClick={() => pick(o)}
            />
          ))}
        </div>
        {/* 导航 + 提交：全部步骤已选才可提交 */}
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            className={askNavBtn}
            disabled={stepIdx === 0}
            onClick={() => setStepIdx((i) => Math.max(0, i - 1))}
          >
            {t('ui.hub.ask_prev')}
          </button>
          <button
            type="button"
            className={askNavBtn}
            disabled={stepIdx === steps.length - 1}
            onClick={() => setStepIdx((i) => Math.min(steps.length - 1, i + 1))}
          >
            {t('ui.hub.ask_next')}
          </button>
          <span className="flex-1" />
          <button
            type="button"
            className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
            disabled={!allAnswered}
            onClick={() => submit(composed)}
          >
            {t('ui.hub.ask_submit')}
          </button>
        </div>
      </div>
    );
  }

  // —— 单次 ask（含 mode 缺省）三模式 ——
  if (ask.mode === 'multi') {
    return (
      <div
        className="mt-1 flex flex-col items-start gap-1.5 rounded-md border border-border/60 bg-background/40 p-2"
        data-testid="hub-ask-card"
      >
        {(ask.options ?? []).map((o) => (
          <AskOptionButton
            key={o}
            label={o}
            selected={picked.includes(o)}
            multi
            disabled={false}
            onClick={() =>
              setPicked((p) => (p.includes(o) ? p.filter((x) => x !== o) : [...p, o]))
            }
          />
        ))}
        <button
          type="button"
          className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
          disabled={picked.length === 0}
          onClick={() => submit(picked.join(listSeparator()))}
        >
          {t('ui.hub.ask_confirm')}
        </button>
      </div>
    );
  }

  if (ask.mode === 'confirm') {
    return (
      <div
        className="mt-1 flex flex-wrap gap-1.5 rounded-md border border-border/60 bg-background/40 p-2"
        data-testid="hub-ask-card"
      >
        {[t('ui.hub.ask_yes'), t('ui.hub.ask_no')].map((label) => (
          <AskOptionButton key={label} label={label} selected={false} multi={false} disabled={false} onClick={() => submit(label)} />
        ))}
      </div>
    );
  }

  // single（含 mode 缺省）：每个选项一枚按钮，点击即答复
  return (
    <div
      className="mt-1 flex flex-wrap gap-1.5 rounded-md border border-border/60 bg-background/40 p-2"
      data-testid="hub-ask-card"
    >
      {(ask.options ?? []).map((o) => (
        <AskOptionButton key={o} label={o} selected={false} multi={false} disabled={false} onClick={() => submit(o)} />
      ))}
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
  onAskAnswer: (answer: string) => Promise<void>;
}

// ManagerThinkCard：管理者思考链的按轮分组折叠渲染（hub_manager_think 且带 steps
// 时替换默认 markdown 正文）。折叠态一行紧凑条目（步数 + 箭头，整行可点切换）；
// 展开态按轮次分组 chips——round 变化即开新组，组内相邻同名工具合并计数 ×N；
// chips 用 span 保持 ask 选项按钮的外观但不可点、无 hover 变色，
// 工具名保持 wire 原名（中文映射只在后端聚合摘要里，前端不再维护一份）。
// 摘要文本展开时展示，避免聚合信息被卡片替换后丢失。
// 独立组件而非内联：折叠/展开是本地受控状态，HubMessageItem 是无状态 memo 纯函数。
function ManagerThinkCard({ steps, summary }: { steps: HubStep[]; summary: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const groups = useMemo(() => {
    const out: { round: number; items: { tool: string; count: number }[] }[] = [];
    for (const s of steps) {
      const g = out[out.length - 1];
      if (!g || g.round !== s.round) {
        out.push({ round: s.round, items: [{ tool: s.tool, count: 1 }] });
        continue;
      }
      const tail = g.items[g.items.length - 1];
      if (tail && tail.tool === s.tool) tail.count += 1;
      else g.items.push({ tool: s.tool, count: 1 });
    }
    return out;
  }, [steps]);
  const chip =
    'cursor-default rounded border border-border bg-card px-2 py-0.5 text-[11px] text-muted-foreground';
  return (
    <div className="mt-1 flex flex-col gap-1" data-testid="hub-think-card">
      <button
        type="button"
        className="flex items-center gap-1 self-start text-[11px] text-muted-foreground hover:text-foreground"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span>{t('ui.hub.think_steps', { n: steps.length })}</span>
        <span aria-hidden="true">{open ? '▾' : '▸'}</span>
      </button>
      {open && (
        <div className="flex flex-col gap-1">
          <div className="text-[11px] text-muted-foreground">{decodePercentText(summary)}</div>
          {groups.map((g, gi) => (
            <div key={`${g.round}-${gi}`} className="flex flex-wrap items-center gap-1">
              <span className="text-[10px] text-muted-foreground">{t('ui.hub.think_round', { n: g.round })}</span>
              {g.items.map((it, i) => (
                <span key={`${it.tool}-${i}`} className={chip}>
                  {it.count > 1 ? `${it.tool} ×${it.count}` : it.tool}
                </span>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
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
  // 管理者思考链卡片：带 steps 的 hub_manager_think 走按轮分组折叠渲染；
  // 无 steps 的旧消息（历史文本）不进入本分支，按普通 markdown 消息展示。
  const isThinkCard = m.event === 'hub_manager_think' && !!m.steps?.length;

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
          {isThinkCard ? (
            <ManagerThinkCard steps={m.steps!} summary={m.text} />
          ) : (
            <div className={markdownBubbleClass}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{decodePercentText(translateBackend(m.text))}</ReactMarkdown>
            </div>
          )}
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
