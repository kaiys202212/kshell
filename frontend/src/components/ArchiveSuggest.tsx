// agent 调用 suggest_archive 之后的确认框：归档与否由用户决定。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useTranslation } from 'react-i18next';
import { translateBackend } from '../lib/errors';
import { Dialog } from './ui/dialog';
import { Button } from './ui/button';

interface Props {
  open: boolean;
  summary: string;
  onConfirm(): void;
  onClose(): void;
}

export default function ArchiveSuggest({ open, summary, onConfirm, onClose }: Props) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onClose(); }}>
      <DialogPrimitive.Title className="mb-1 text-sm font-medium">{t('ui.archive_suggest.title')}</DialogPrimitive.Title>
      <p className="mb-3 text-xs leading-5 text-muted-foreground">
        <span className="block">{summary ? translateBackend(summary) : t('ui.archive_suggest.default_summary')}</span>
        <span className="mt-1 block">{t('ui.archive_suggest.hint')}</span>
      </p>
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onClose}>{t('ui.archive_suggest.continue')}</Button>
        <Button size="sm" onClick={onConfirm}>{t('ui.archive_suggest.archive')}</Button>
      </div>
    </Dialog>
  );
}
