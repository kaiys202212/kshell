// 升级后首次启动的「新版本特性」弹窗：渲染完整升级日志 + 「我知道了」确认。
// open 由 App 控制：挂载时比对 GetAppVersion 与 GetLastSeenVersion 决定是否弹出。
// Esc / 点遮罩关闭被禁用（dismissible=false），确保版本标记只随确认按钮落盘，
// 未确认前下次启动会再次提醒。文档拉取与 HelpSection 同口径：lang 直接透传
// i18n.language，后端仅认 zh-CN、其余回退 en；失败复用帮助分区的失败文案与重试。
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getChangelogDoc, markVersionSeen } from '../lib/api';
import MarkdownPreview from './MarkdownPreview';
import { Button } from './ui/button';
import { Dialog } from './ui/dialog';

export interface WhatsNewDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentVersion: string;
}

export default function WhatsNewDialog({ open, onOpenChange, currentVersion }: WhatsNewDialogProps) {
  const { t, i18n } = useTranslation();
  const [markdown, setMarkdown] = useState('');
  const [failed, setFailed] = useState(false);
  // attempt 只作重试触发器：点重试自增，effect 依赖它重新拉取
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setFailed(false);
    // 重拉（重试/切语言）前清掉旧内容，避免短暂显示错配文档
    setMarkdown('');
    getChangelogDoc(i18n.language)
      .then((md) => {
        if (cancelled) return;
        setMarkdown(md);
      })
      .catch(() => {
        if (cancelled) return;
        setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [open, i18n.language, attempt]);

  const handleConfirm = () => {
    // 标记写成败都不阻塞关闭：markVersionSeen 无绑定时本就静默跳过
    void markVersionSeen(currentVersion)
      .catch(() => {})
      .then(() => onOpenChange(false));
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      dismissible={false}
      className="w-[36rem] max-w-[90vw]"
    >
      <DialogPrimitive.Title className="text-sm font-medium">
        {t('ui.whatsNew.title', { version: currentVersion })}
      </DialogPrimitive.Title>
      <div className="mt-2 max-h-[55vh] overflow-auto text-sm">
        {failed ? (
          <div className="flex flex-col items-start gap-3 text-muted-foreground">
            <p>{t('ui.settings.help.loadFailed')}</p>
            <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
              {t('ui.settings.help.retry')}
            </Button>
          </div>
        ) : (
          <MarkdownPreview markdown={markdown} />
        )}
      </div>
      <div className="mt-3 flex justify-end">
        <Button type="button" onClick={handleConfirm}>
          {t('ui.whatsNew.confirm')}
        </Button>
      </div>
    </Dialog>
  );
}
