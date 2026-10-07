// 中栏文件页签内的只读 commit 全文 diff（无 stage/discard）。
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { gitCommitDiff } from '../lib/api';
import { backendError } from '../lib/errors';
import { shortHash } from '../lib/gitGraph';
import { cn } from '../lib/cn';

export default function GitCommitDiffView({
  wsPath,
  repoRel,
  hash,
}: {
  wsPath: string;
  repoRel: string;
  hash: string;
}) {
  const { t } = useTranslation();
  const [text, setText] = useState('');
  const [binary, setBinary] = useState(false);
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    try {
      const d = await gitCommitDiff(wsPath, repoRel, hash);
      setBinary(!!d.Binary);
      setText(d.Text ?? '');
      setErr('');
    } catch (e) {
      setErr(backendError(e));
    }
  }, [wsPath, repoRel, hash]);

  useEffect(() => {
    void load();
  }, [load]);

  const lines = text.replace(/\r\n/g, '\n').split('\n');

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b border-border pb-2">
        <span className="mr-auto truncate font-mono text-xs">{shortHash(hash)}</span>
      </div>
      {err && <p className="p-2 text-sm text-destructive">{err}</p>}
      {binary && <p className="p-2 text-sm text-muted-foreground">{t('ui.git.binary_no_diff')}</p>}
      {!binary && !err && (
        <div className="min-h-0 flex-1 overflow-auto font-mono text-[11px] leading-5">
          <pre className="whitespace-pre-wrap">
            {lines.map((line, j) => (
              <div
                key={j}
                className={cn(
                  line.startsWith('+') && !line.startsWith('+++') && 'bg-emerald-500/15',
                  line.startsWith('-') && !line.startsWith('---') && 'bg-destructive/15',
                  line.startsWith('@@') && 'text-muted-foreground',
                )}
              >
                {line}
              </div>
            ))}
          </pre>
        </div>
      )}
    </div>
  );
}
