// HubConfirmCard：AgentHub 里写操作确认卡（event=tool_confirm 的 agent 消息）。
// 后端把摘要与 pending 参数放同一条消息（Text = 摘要 + 尾部 JSON），这里剥离
// JSON 展示摘要，确认/拒绝经 onConfirm 回调走 ConfirmHubTool 绑定。
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { HubMessage } from '../lib/api';
import { backendError } from '../lib/errors';
import { Button } from './ui/button';

// extractPending 从消息文本尾部 JSON 提取 {pending, tool}；无 JSON 返回 null。
function extractPending(text: string): { pending: string; tool: string } | null {
  const i = text.lastIndexOf('{');
  if (i < 0) return null;
  try {
    const v = JSON.parse(text.slice(i)) as { pending?: unknown; tool?: unknown };
    if (typeof v.pending === 'string' && v.pending) {
      return { pending: v.pending, tool: typeof v.tool === 'string' ? v.tool : '' };
    }
    return null;
  } catch {
    return null;
  }
}

export default function HubConfirmCard({
  message,
  onConfirm,
}: {
  message: HubMessage;
  onConfirm: (pendingID: string, approved: boolean) => Promise<void>;
}) {
  const { t } = useTranslation();
  // done/error 二态：确认成功后固定文案；失败回显错误并恢复按钮可重试。
  const [state, setState] = useState<'idle' | 'busy' | 'done' | 'error'>('idle');
  const [errMsg, setErrMsg] = useState('');

  const pending = extractPending(message.text ?? '');
  // 摘要 = 剥离最后一个 { 起的尾部 JSON（提取失败则原样展示）
  const summary = pending ? message.text.slice(0, message.text.lastIndexOf('{')).trimEnd() : message.text;

  const submit = async (approved: boolean) => {
    if (!pending || state === 'busy') return;
    setState('busy');
    try {
      await onConfirm(pending.pending, approved);
      setState('done');
    } catch (e) {
      setErrMsg(backendError(e));
      setState('error');
    }
  };

  return (
    <div className="break-words overflow-hidden rounded-md border border-border bg-card px-3 py-1.5">
      <div className="mb-0.5 text-[11px] font-medium text-muted-foreground">
        {t('ui.hub.confirm_title')}
        {pending?.tool ? ` · ${pending.tool}` : ''}
      </div>
      <div className="whitespace-pre-wrap text-sm">{summary}</div>
      {state === 'done' ? (
        <div className="mt-1 text-xs text-muted-foreground">{t('ui.hub.confirm_done')}</div>
      ) : pending ? (
        <>
          <div className="mt-1.5 flex gap-2">
            <Button
              size="sm"
              data-testid="hub-confirm-approve"
              disabled={state === 'busy'}
              onClick={() => void submit(true)}
            >
              {t('ui.hub.confirm_approve')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              data-testid="hub-confirm-reject"
              disabled={state === 'busy'}
              onClick={() => void submit(false)}
            >
              {t('ui.hub.confirm_reject')}
            </Button>
          </div>
          {state === 'error' && errMsg && (
            <div className="mt-1 text-xs text-destructive">
              {t('ui.hub.confirm_failed', { err: errMsg })}
            </div>
          )}
        </>
      ) : null}
    </div>
  );
}
