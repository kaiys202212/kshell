// 全局渲染错误边界：任一子树渲染期抛错时兜住，展示可恢复错误页，
// 而非让 React 卸载整棵树（frameless 深色窗口只剩纯黑——「查看全部」黑屏的
// 直接机制）。底层错误原样展示便于用户反馈定位；「重新加载」整页刷新自愈。
import React from 'react';
import { useTranslation } from 'react-i18next';

type Props = { children: React.ReactNode };
type State = { error: Error | null };

// fallback 用函数组件取 i18n 文案（class 组件不能用 hook）
function Fallback({ error }: { error: Error }) {
  const { t } = useTranslation();
  return (
    <div
      role="alert"
      className="flex h-screen flex-col items-center justify-center gap-3 bg-background p-6 text-sm"
    >
      <p className="text-base font-medium">{t('ui.error_boundary.title')}</p>
      <p className="max-w-md text-center text-muted-foreground">
        {t('ui.error_boundary.detail')}
      </p>
      <pre className="max-w-md overflow-auto rounded bg-muted p-2.5 font-mono text-xs whitespace-pre-wrap text-destructive">
        {error.message || String(error)}
      </pre>
      <button
        type="button"
        className="rounded bg-primary px-3 py-1.5 text-primary-foreground transition-colors hover:bg-primary/90"
        onClick={() => window.location.reload()}
      >
        {t('ui.error_boundary.reload')}
      </button>
    </div>
  );
}

export class ErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    // 留控制台痕迹：开发调试与用户反馈时可在 devtools 里看到完整堆栈
    console.error('[ErrorBoundary] 渲染错误', error, info.componentStack);
  }

  render(): React.ReactNode {
    if (this.state.error) {
      return <Fallback error={this.state.error} />;
    }
    return this.props.children;
  }
}
