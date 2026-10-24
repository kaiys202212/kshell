// 聊天图片附件 lightbox：Dialog 外壳 + readChatAttachmentData → data URL img。
// 附件 meta 的 Path 是落盘缓存绝对路径（可能在工作区外），走专用绑定读取。
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { readChatAttachmentData } from '../lib/api';
import { Dialog } from './ui/dialog';

export interface ImageLightboxProps {
  name?: string;
  path: string;
  onClose: () => void;
}

export default function ImageLightbox({ name, path, onClose }: ImageLightboxProps) {
  const { t } = useTranslation();
  const [src, setSrc] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setSrc(null);
    setFailed(false);
    (async () => {
      try {
        const bytes = await readChatAttachmentData(path);
        if (cancelled) return;
        // 绑定不可用返回 null 与读取异常同样按失败处理
        if (!bytes) {
          setFailed(true);
          return;
        }
        setSrc(`data:${bytes.Mime};base64,${bytes.Base64}`);
      } catch {
        if (cancelled) return;
        setFailed(true);
      }
    })();
    return () => {
      cancelled = true;
    };
    // t 不列入依赖：切语言不应重跑加载
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
      className="max-w-[90vw]"
    >
      <div className="mb-2 flex items-start justify-between gap-4">
        <DialogPrimitive.Title className="min-w-0 truncate text-sm font-medium">{name ?? ''}</DialogPrimitive.Title>
        <DialogPrimitive.Close
          aria-label={t('ui.chat.attachment_close')}
          className="shrink-0 rounded px-1 text-muted-foreground hover:text-foreground"
        >
          ×
        </DialogPrimitive.Close>
      </div>
      {failed ? (
        <p className="text-sm text-destructive">{t('ui.chat.attachment_preview_failed')}</p>
      ) : src ? (
        <img
          src={src}
          alt={name}
          className="max-h-[80vh] max-w-full object-contain"
          onError={() => setFailed(true)}
        />
      ) : (
        <p className="text-sm text-muted-foreground">{t('ui.files.loading')}</p>
      )}
    </Dialog>
  );
}
