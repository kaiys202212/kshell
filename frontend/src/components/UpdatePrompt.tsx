import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useTranslation } from 'react-i18next';
import type { UpdateInfo } from '../lib/api';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

interface Props {
  info: UpdateInfo;
  busy: boolean;
  error: string;
  onLater(): void;
  onUpgrade(): void;
}

export default function UpdatePrompt({ info, busy, error, onLater, onUpgrade }: Props) {
  const { t } = useTranslation();
  // Ready：安装包已下载并校验，弹窗只负责确认安装；否则退回旧文案（兜底）。
  const titleKey = info.Ready ? 'ui.update_prompt.ready_title' : 'ui.update_prompt.title';
  const bodyKey = info.Ready ? 'ui.update_prompt.ready_body' : 'ui.update_prompt.body';
  const actionLabel = busy
    ? t(info.Ready ? 'ui.update_prompt.installing' : 'ui.update_prompt.upgrading')
    : t(info.Ready ? 'ui.update_prompt.install_restart' : 'ui.update_prompt.upgrade');
  return (
    <Dialog open onOpenChange={(next) => { if (!next && !busy) onLater(); }} className="w-[min(92vw,24rem)]">
      <DialogPrimitive.Title className="mb-1 text-sm font-medium">
        {t(titleKey, { version: info.Latest })}
      </DialogPrimitive.Title>
      <p className="mb-3 text-xs leading-5 text-muted-foreground">
        {t(bodyKey, { current: info.Current || t('ui.update_prompt.unknown') })}
      </p>
      {info.Notes ? (
        <pre className="mb-3 max-h-32 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs">
          {info.Notes}
        </pre>
      ) : null}
      {error ? <p className="mb-3 text-xs text-destructive">{error}</p> : null}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" disabled={busy} onClick={onLater}>
          {t('ui.update_prompt.later')}
        </Button>
        <Button size="sm" disabled={busy} onClick={onUpgrade}>
          {actionLabel}
        </Button>
      </div>
    </Dialog>
  );
}
