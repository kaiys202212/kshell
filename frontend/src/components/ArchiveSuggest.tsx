// agent 调用 suggest_archive 之后的确认框：归档与否由用户决定。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { Dialog } from './ui/dialog';
import { Button } from './ui/button';

interface Props {
  open: boolean;
  summary: string;
  onConfirm(): void;
  onClose(): void;
}

export default function ArchiveSuggest({ open, summary, onConfirm, onClose }: Props) {
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onClose(); }}>
      <DialogPrimitive.Title className="mb-1 text-sm font-medium">归档这个会话？</DialogPrimitive.Title>
      <p className="mb-3 text-xs leading-5 text-muted-foreground">
        <span className="block">{summary || '当前任务看起来已经完成。'}</span>
        <span className="mt-1 block">归档后可在会话列表勾选「归档」查看，并随时还原。</span>
      </p>
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onClose}>继续</Button>
        <Button size="sm" onClick={onConfirm}>归档</Button>
      </div>
    </Dialog>
  );
}
