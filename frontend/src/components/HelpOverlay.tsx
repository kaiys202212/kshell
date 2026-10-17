// 帮助面板（F1）：居中浮层 + 两列键位速查表。
// 数据源直接 map keybindings 的 SHORTCUTS 常量——帮助表与实际分发逻辑引用同一份
// 定义，避免键位文档与实现漂移。文案全部走 i18n（ui.help.*），Esc 关闭在本组件内监听。
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { SHORTCUTS } from '../lib/keybindings';

interface Props {
  open: boolean;
  onClose: () => void;
}

function HelpOverlay({ open, onClose }: Props) {
  const { t } = useTranslation();

  useEffect(() => {
    // 仅打开时挂载 Esc 监听；F1 再按关闭由全局 handleGlobalKey 的 toggleHelp 承担
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" data-testid="help-overlay">
      <div className="max-h-[80vh] w-[560px] max-w-[90vw] overflow-auto rounded-lg border border-neutral-700 bg-neutral-900 p-5 text-neutral-100 shadow-xl">
        <h2 className="mb-4 text-base font-semibold">{t('ui.help.title')}</h2>
        <table className="w-full text-sm">
          <tbody>
            {SHORTCUTS.map((s) => (
              <tr key={s.id} className="border-t border-neutral-800">
                <td className="w-36 py-1.5 pr-3 font-mono whitespace-nowrap">{s.keys}</td>
                <td className="py-1.5 pr-3 font-medium whitespace-nowrap">
                  {t(`ui.help.shortcuts.${s.id}_name`)}
                </td>
                <td className="py-1.5 text-neutral-400">{t(`ui.help.shortcuts.${s.id}_desc`)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default HelpOverlay;
