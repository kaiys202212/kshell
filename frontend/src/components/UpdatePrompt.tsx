import * as DialogPrimitive from '@radix-ui/react-dialog';
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
  return (
    <Dialog open onOpenChange={(next) => { if (!next && !busy) onLater(); }} className="w-[min(92vw,24rem)]">
      <DialogPrimitive.Title className="mb-1 text-sm font-medium">发现新版本 {info.Latest}</DialogPrimitive.Title>
      <p className="mb-3 text-xs leading-5 text-muted-foreground">
        当前 {info.Current || '未知'}。稍后则本会话不再提醒，下次启动仍会检查。
      </p>
      {info.Notes ? (
        <pre className="mb-3 max-h-32 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs">
          {info.Notes}
        </pre>
      ) : null}
      {error ? <p className="mb-3 text-xs text-destructive">{error}</p> : null}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" disabled={busy} onClick={onLater}>
          稍后
        </Button>
        <Button size="sm" disabled={busy} onClick={onUpgrade}>
          {busy ? '升级中…' : '立即升级'}
        </Button>
      </div>
    </Dialog>
  );
}
