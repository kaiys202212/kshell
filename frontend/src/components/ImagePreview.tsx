// 图片预览：readFileBytes → data URL → <img>。
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { readFileBytes } from '../lib/api';

export interface ImagePreviewProps {
  wsPath: string;
  path: string;
}

function pathBasename(path: string): string {
  const normalized = path.replace(/\\/g, '/');
  const parts = normalized.split('/');
  return parts[parts.length - 1] || path;
}

export default function ImagePreview({ wsPath, path }: ImagePreviewProps) {
  const { t } = useTranslation();
  const [src, setSrc] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const alt = pathBasename(path);

  useEffect(() => {
    let cancelled = false;
    setSrc(null);
    setError(null);

    (async () => {
      try {
        const bytes = await readFileBytes(wsPath, path);
        if (cancelled) return;
        if (!bytes) {
          setError(t('ui.files.image_read_failed'));
          return;
        }
        setSrc(`data:${bytes.Mime};base64,${bytes.Base64}`);
      } catch (e) {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [wsPath, path]);

  if (error) {
    return <p className="p-3 text-sm text-destructive">{error}</p>;
  }
  if (!src) {
    return <p className="p-3 text-sm text-muted-foreground">{t('ui.files.loading')}</p>;
  }

  return (
    <div className="flex h-full w-full items-center justify-center overflow-auto p-2">
      <img
        src={src}
        alt={alt}
        className="max-h-full max-w-full object-contain"
        onError={() => setError(t('ui.files.image_display_failed'))}
      />
    </div>
  );
}
