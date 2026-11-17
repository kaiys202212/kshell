// 设置页微信桥区块：扫码绑定（qrcode 本地生成）、状态展示、二次点击确认解绑。
// 轮询策略：绑定流程进行中（binding === 'pending'）每 2s 轮询 BindStatus，
// 其余态不轮询——状态只在挂载与关键动作后刷新一次。
import { useCallback, useEffect, useRef, useState } from 'react';
import QRCode from 'qrcode';
import { useTranslation } from 'react-i18next';
import {
  weixinBind,
  weixinBindStatus,
  weixinStatus,
  weixinUnbind,
  type WeixinStatusView,
} from '../lib/api';
import { backendError } from '../lib/errors';
import { useAppStore } from '../state/store';
import { Button } from './ui/button';

// 轮询间隔（绑定进行中）与解绑确认态自动复位时长（ms）
const POLL_INTERVAL = 2000;
const CONFIRM_RESET = 3000;

// 脱敏 wxid：前 4 后 4，中间 ****；过短整体打码。
function maskWxid(wxid: string): string {
  if (wxid.length <= 8) return '****';
  return `${wxid.slice(0, 4)}****${wxid.slice(-4)}`;
}

export function WeixinSettings() {
  const { t } = useTranslation();
  const notify = useAppStore((s) => s.notify);
  const [status, setStatus] = useState<WeixinStatusView | null>(null);
  const [failed, setFailed] = useState(false);
  const [qrUrl, setQrUrl] = useState(''); // 二维码 dataURL
  const [qrError, setQrError] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const confirmTimer = useRef(0);

  const refresh = useCallback(async () => {
    try {
      const v = await weixinStatus();
      setStatus(v);
      setFailed(v === null);
    } catch {
      setFailed(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
    return () => window.clearTimeout(confirmTimer.current);
  }, [refresh]);

  // 绑定流程进行中轮询 BindStatus；ok 时回读全量状态，expired 等终止态自动停轮询
  const binding = status?.binding ?? 'idle';
  useEffect(() => {
    if (binding !== 'pending') return;
    let cancelled = false;
    const tmr = window.setInterval(() => {
      weixinBindStatus()
        .then((s) => {
          if (cancelled) return;
          if (s === 'ok') {
            void refresh();
          } else if (s === 'expired') {
            setStatus((prev) => (prev ? { ...prev, binding: 'expired' } : prev));
          }
        })
        .catch(() => {});
    }, POLL_INTERVAL);
    return () => {
      cancelled = true;
      window.clearInterval(tmr);
    };
  }, [binding, refresh]);

  const startBind = async () => {
    setQrError(false);
    try {
      const url = await weixinBind();
      const dataUrl = await QRCode.toDataURL(url);
      setQrUrl(dataUrl);
      // 立即拉一次，让 binding 进入 pending 以启动轮询
      await refresh();
    } catch (e: unknown) {
      setQrError(true);
      notify(backendError(e), 'error');
    }
  };

  const handleUnbind = async () => {
    // 二次点击确认模式：第一次点只变确认文案，3s 未确认自动复位
    if (!confirming) {
      setConfirming(true);
      window.clearTimeout(confirmTimer.current);
      confirmTimer.current = window.setTimeout(() => setConfirming(false), CONFIRM_RESET);
      return;
    }
    window.clearTimeout(confirmTimer.current);
    setConfirming(false);
    try {
      await weixinUnbind();
      setQrUrl('');
      await refresh();
    } catch (e: unknown) {
      notify(backendError(e), 'error');
    }
  };

  const bound = !!status?.bound;

  return (
    <section className="mb-5 rounded border border-border bg-card p-3.5">
      <h2 className="mb-1 text-sm font-medium">{t('ui.weixin.title')}</h2>
      <p className="mb-3 text-xs text-muted-foreground">{t('ui.weixin.desc')}</p>

      {failed && <p className="text-sm text-destructive">{t('ui.weixin.error')}</p>}

      {!failed && !bound && (
        <div>
          {!qrUrl && (
            <Button size="sm" onClick={() => void startBind()}>
              {t('ui.weixin.bind')}
            </Button>
          )}
          {qrUrl && (
            <div className="flex flex-col items-start gap-2">
              {qrError ? (
                <p className="text-sm text-destructive">{t('ui.weixin.error')}</p>
              ) : (
                <img
                  src={qrUrl}
                  alt={t('ui.weixin.scan_hint')}
                  className="h-40 w-40 rounded border border-border bg-white p-1"
                />
              )}
              <p className="text-xs text-muted-foreground">{t('ui.weixin.scan_hint')}</p>
              {status?.binding === 'expired' && (
                <>
                  <p className="text-sm text-warning">{t('ui.weixin.expired')}</p>
                  <Button size="sm" variant="secondary" onClick={() => void startBind()}>
                    {t('ui.weixin.rebind')}
                  </Button>
                </>
              )}
            </div>
          )}
        </div>
      )}

      {!failed && bound && (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2 text-sm">
            <span
              className={`h-2 w-2 shrink-0 rounded-full ${status?.running ? 'bg-success' : 'bg-destructive'}`}
            />
            <span>
              {t('ui.weixin.bound_as', { 0: maskWxid(status?.wxid ?? '') })}
              {' · '}
              {status?.running ? t('ui.weixin.running') : t('ui.weixin.disconnected')}
            </span>
            {!!status?.undelivered && (
              <span className="text-xs text-muted-foreground">
                {t('ui.weixin.pending_undelivered', { count: status.undelivered })}
              </span>
            )}
          </div>
          <div className="flex gap-2">
            {!status?.running && (
              <Button size="sm" variant="secondary" onClick={() => void startBind()}>
                {t('ui.weixin.rebind')}
              </Button>
            )}
            <Button
              size="sm"
              variant={confirming ? 'destructive' : 'secondary'}
              onClick={() => void handleUnbind()}
            >
              {confirming ? t('ui.weixin.unbind_confirm') : t('ui.weixin.unbind')}
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}
