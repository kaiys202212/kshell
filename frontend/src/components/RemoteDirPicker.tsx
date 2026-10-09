// 远端目录浏览器：经 ListRemoteDir 浏览一层目录，仅允许选目录确认。
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { listRemoteDir, type RemoteDirEntry } from '../lib/api';
import { backendError } from '../lib/errors';
import { MONO } from '../lib/ui';
import { Button } from './ui/button';
import { EmptyState } from './ui/empty-state';
import { Skeleton } from './ui/skeleton';

export interface RemoteDirPickerProps {
  connID: string;
  /** 选中目录后回调（绝对路径） */
  onConfirm: (remotePath: string) => void;
  onCancel: () => void;
}

function parentDir(p: string): string {
  const clean = p.replace(/\/+$/, '') || '/';
  if (clean === '/') return '/';
  const i = clean.lastIndexOf('/');
  if (i <= 0) return '/';
  return clean.slice(0, i) || '/';
}

function joinRemote(cwd: string, name: string): string {
  if (!cwd || cwd === '/') return `/${name}`;
  return `${cwd.replace(/\/+$/, '')}/${name}`;
}

export default function RemoteDirPicker({ connID, onConfirm, onCancel }: RemoteDirPickerProps) {
  const { t } = useTranslation();
  const [cwd, setCwd] = useState('');
  const [entries, setEntries] = useState<RemoteDirEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(
    async (dir: string) => {
      setLoading(true);
      setError('');
      try {
        const res = await listRemoteDir(connID, dir);
        setCwd(res.dir);
        setEntries(res.entries);
      } catch (err) {
        setError(backendError(err));
        setEntries([]);
      } finally {
        setLoading(false);
      }
    },
    [connID],
  );

  useEffect(() => {
    void load('');
  }, [load]);

  const dirs = entries.filter((e) => e.isDir).sort((a, b) => a.name.localeCompare(b.name));
  const files = entries.filter((e) => !e.isDir).sort((a, b) => a.name.localeCompare(b.name));
  const atRoot = !cwd || cwd === '/';

  return (
    <div className="flex flex-col gap-2" data-testid="remote-dir-picker">
      <div className="flex items-center gap-1.5">
        <span className={`min-w-0 flex-1 truncate ${MONO}`} title={cwd || undefined}>
          {cwd || t('ui.home.remote_loading_path')}
        </span>
        <Button
          size="sm"
          variant="secondary"
          disabled={loading || atRoot}
          onClick={() => void load(parentDir(cwd))}
        >
          {t('ui.home.remote_up')}
        </Button>
        <Button size="sm" variant="secondary" disabled={loading} onClick={() => void load(cwd || '')}>
          {t('ui.home.remote_refresh')}
        </Button>
      </div>

      <div className="max-h-64 min-h-[10rem] overflow-y-auto rounded border border-border">
        {loading ? (
          <div className="flex flex-col gap-1 p-2">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-7 w-full" />
            ))}
          </div>
        ) : error ? (
          <EmptyState className="py-6" title={t('ui.home.remote_list_failed')} hint={error} />
        ) : dirs.length === 0 && files.length === 0 ? (
          <EmptyState className="py-6" title={t('ui.home.remote_dir_empty')} />
        ) : (
          <ul className="m-0 list-none p-0">
            {dirs.map((e) => (
              <li key={`d:${e.name}`}>
                <button
                  type="button"
                  className="flex w-full items-center gap-2 px-2 py-1.5 text-left text-xs hover:bg-muted"
                  onClick={() => void load(joinRemote(cwd, e.name))}
                >
                  <span className="text-muted-foreground">{t('ui.home.remote_dir_mark')}</span>
                  <span className="truncate">{e.name}</span>
                </button>
              </li>
            ))}
            {files.map((e) => (
              <li
                key={`f:${e.name}`}
                className="flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground"
                title={t('ui.home.remote_file_hint')}
              >
                <span>{t('ui.home.remote_file_mark')}</span>
                <span className="truncate">{e.name}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCancel}>
          {t('ui.home.create_cancel')}
        </Button>
        <Button
          size="sm"
          disabled={loading || !cwd || !!error}
          onClick={() => onConfirm(cwd)}
        >
          {t('ui.home.remote_use_dir')}
        </Button>
      </div>
    </div>
  );
}
