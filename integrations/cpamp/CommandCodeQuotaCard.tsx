import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { IconRefreshCw } from '@/components/ui/icons';
import { useAuthStore } from '@/stores';
import type { AuthFileItem } from '@/types';
import { formatQuotaResetRelative } from '../model/accountsPagePresentation';
import { QuotaProgressBar } from './QuotaProgressBar';
import { refreshCommandCodeQuota, useCommandCodeQuota } from './commandCodeQuotaStore';
import { getCommandCodeMonthlyQuota } from './commandCodeQuotaPolicy';
import styles from './CommandCodeQuotaCard.module.scss';

export function CommandCodeQuotaCard({ file, compact = false, refreshOnly = false }: { file: AuthFileItem; compact?: boolean; refreshOnly?: boolean }) {
  const { i18n } = useTranslation();
  const apiBase = useAuthStore(state => state.apiBase);
  const managementKey = useAuthStore(state => state.managementKey);
  const connectionStatus = useAuthStore(state => state.connectionStatus);
  const authIndex = String(file.authIndex ?? file.auth_index ?? '').trim();
  const scope = { apiBase, managementKey };
  const state = useCommandCodeQuota(file, scope);
  const enabled = connectionStatus === 'connected' && Boolean(authIndex && managementKey) && !file.disabled;
  const number = (value: number) => new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 0 }).format(value);
  const refresh = async () => {
    if (enabled) await refreshCommandCodeQuota(file, scope);
  };
  const buckets = (state.data?.groups ?? []).flatMap(group => group.buckets ?? []);
  const monthly = getCommandCodeMonthlyQuota(state);
  if (refreshOnly) return <div data-commandcode-quota-refresh="true">
    <Button variant="secondary" size="sm" loading={state.pending} disabled={!enabled}
      onClick={() => void refresh()} aria-label="刷新 CommandCode 套餐额度">
      {!state.pending && <IconRefreshCw size={15} />}刷新额度
    </Button>
    {state.error && <p role="alert">{state.error}</p>}
  </div>;
  const renderWindow = (window: string, label: string) => {
    const bucket = buckets.find(item => item.window === window);
    const remaining = window === 'monthly' ? monthly.remainingPercent
      : bucket && typeof bucket.remainingFraction === 'number' && Number.isFinite(bucket.remainingFraction)
        ? Math.max(0, Math.min(1, bucket.remainingFraction)) * 100 : null;
    const subscriptionReset = window === 'monthly'
      ? state.data?.summary?.find(metric => metric.key === 'subscription_reset_at')?.value : undefined;
    const bucketReset = bucket?.resetTime ? Date.parse(bucket.resetTime) : NaN;
    const resetAt = Number.isFinite(bucketReset) && bucketReset > 0
      ? bucketReset : typeof subscriptionReset === 'number' ? subscriptionReset * 1000 : NaN;
    const validReset = Number.isFinite(new Date(resetAt).getTime()) && resetAt > 0;
    const resetLabel = validReset
      ? formatQuotaResetRelative(resetAt, undefined, i18n.language) || '已到重置时间'
      : '重置未知';
    return <div className={styles.window} data-quota-window={window}>
      <div className={styles.heading}>
        <span className={styles.label}>{label}</span>
        <span className={styles.remaining}>剩余 <strong title={remaining === null ? '上游未提供额度比例' : undefined}>{remaining === null ? '—' : `${number(remaining)}%`}</strong></span>
      </div>
      <QuotaProgressBar percent={remaining} highThreshold={70} mediumThreshold={30} />
      <div className={styles.meta}>
        {window === 'monthly' && monthly.remainingCredits !== null && <span className={styles.balance}>
          {new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 3 }).format(monthly.remainingCredits)}
          {monthly.capCredits !== null ? ` / ${number(monthly.capCredits)}` : ''} credits
        </span>}
        <time className={styles.reset} dateTime={validReset ? new Date(resetAt).toISOString() : undefined}
          title={validReset ? `重置 ${new Date(resetAt).toLocaleString(i18n.language)}` : undefined}>{resetLabel}</time>
      </div>
    </div>;
  };
  return (
    <section className={`${styles.card} ${compact ? styles.compact : ''}`} data-commandcode-native-quota="true"
      onClick={event => event.stopPropagation()} onKeyDown={event => event.stopPropagation()}>
      {state.data ? <div className={styles.windows}>
        <div className={styles.shortWindows}>
          {renderWindow('fiveHour', '5h')}
          {renderWindow('weekly', '周额度')}
        </div>
        <div className={styles.monthlyWindow}>{renderWindow('monthly', '月额度')}</div>
      </div> : <p>{file.disabled ? '凭证已停用' : '点击刷新额度'}</p>}
      <Button className={styles.refresh} variant="ghost" size="xs" iconOnly loading={state.pending} disabled={!enabled}
        onClick={() => void refresh()} aria-label="刷新 CommandCode 套餐额度" title="刷新额度">
        {!state.pending && <IconRefreshCw size={14} />}
      </Button>
      {state.error && <p role="alert" className={styles.error}>{state.error}</p>}
    </section>
  );
}
