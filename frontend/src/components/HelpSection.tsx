// 设置页帮助分区：「功能使用说明」/「升级日志」两个页签，按当前语言拉取内嵌
// Markdown 文档（appdocs embed）渲染。lang 直接透传 i18n.language，后端仅认
// zh-CN、其余回退 en；语言切换由 i18n.language 依赖触发重拉。
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getChangelogDoc, getHelpDoc } from '../lib/api';
import MarkdownPreview from './MarkdownPreview';
import { Button } from './ui/button';

type HelpTab = 'guide' | 'changelog';

export function HelpSection() {
  const { t, i18n } = useTranslation();
  const [tab, setTab] = useState<HelpTab>('guide');
  const [markdown, setMarkdown] = useState('');
  const [failed, setFailed] = useState(false);
  // attempt 只作重试触发器：点重试自增，effect 依赖它重新拉取
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setFailed(false);
    (tab === 'guide' ? getHelpDoc : getChangelogDoc)(i18n.language)
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
  }, [tab, i18n.language, attempt]);

  if (failed) {
    return (
      <section className="flex flex-col items-start gap-3 rounded border border-border bg-card p-3.5 text-sm text-muted-foreground">
        <p>{t('ui.settings.help.loadFailed')}</p>
        <Button variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
          {t('ui.settings.help.retry')}
        </Button>
      </section>
    );
  }

  // 页签样式沿用设置页既有分段控件口径（Button default/secondary + aria-pressed）
  return (
    <section className="rounded border border-border bg-card">
      <div className="flex gap-2 border-b border-border p-3">
        <Button
          variant={tab === 'guide' ? 'default' : 'secondary'}
          aria-pressed={tab === 'guide'}
          onClick={() => setTab('guide')}
        >
          {t('ui.settings.help.tab.guide')}
        </Button>
        <Button
          variant={tab === 'changelog' ? 'default' : 'secondary'}
          aria-pressed={tab === 'changelog'}
          onClick={() => setTab('changelog')}
        >
          {t('ui.settings.help.tab.changelog')}
        </Button>
      </div>
      <MarkdownPreview markdown={markdown} />
    </section>
  );
}
