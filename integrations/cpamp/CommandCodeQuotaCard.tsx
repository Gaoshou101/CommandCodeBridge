import { useSyncExternalStore } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { IconRefreshCw } from '@/components/ui/icons';
import { apiClient, createScopedApiRequestConfig } from '@/services/api/client';
import { useAuthStore } from '@/stores';
import type { AuthFileItem } from '@/types';
import { formatQuotaResetRelative } from '../model/accountsPagePresentation';
import { QuotaProgressBar } from './QuotaProgressBar';
import styles from './CommandCodeQuotaCard.module.scss';

interface NativeQuota {
  summary?: { key: string; value: number }[];
  groups?: {
    buckets?: { window?: string; remainingFraction: number; resetTime?: string }[];
  }[];
}
interface QuotaState {
  data?: NativeQuota;
  pending?: boolean;
  error?: string;
}

const empty: QuotaState = {};
const cache = new Map<string, QuotaState>();
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
function update(key: string, state: QuotaState) {
  if (cache.size >= 256 && !cache.has(key)) cache.delete(cache.keys().next().value!);
  cache.set(key, state);
  listeners.forEach(listener => listener());
}

export function CommandCodeQuotaCard({ file, compact = false }: { file: AuthFileItem; compact?: boolean }) {
  const { i18n } = useTranslation();
  const apiBase = useAuthStore(state => state.apiBase);
  const managementKey = useAuthStore(state => state.managementKey);
  const connectionStatus = useAuthStore(state => state.connectionStatus);
  const authIndex = String(file.authIndex ?? file.auth_index ?? '').trim();
  // Endpoint, login and credential version all scope the in-memory observation.
  // No tokens, balances or errors are written to browser storage.
  const key = JSON.stringify([apiBase, managementKey, file.id, authIndex, file.name,
    file.modified, file.updated_at, file.lastRefresh, file.disabled]);
  const state = useSyncExternalStore(subscribe, () => cache.get(key) ?? empty, () => empty);
  const enabled = connectionStatus === 'connected' && Boolean(authIndex && managementKey) && !file.disabled;
  const number = (value: number) => new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 0 }).format(value);
  const refresh = async () => {
    if (!enabled || cache.get(key)?.pending) return;
    const old = cache.get(key) ?? empty;
    update(key, { ...old, pending: true, error: undefined });
    try {
      const data = await apiClient.post<NativeQuota>('/quota/fetch', { auth_index: authIndex, provider: 'commandcode' }, {
        ...createScopedApiRequestConfig({ apiBase, managementKey }), timeout: 20000,
      });
      update(key, { data });
    } catch {
      update(key, { ...old, pending: false, error: old.data ? '刷新失败，上次结果已过期' : '刷新失败，请重试' });
    }
  };
  const buckets = (state.data?.groups ?? []).flatMap(group => group.buckets ?? []);
  const renderWindow = (window: string, label: string) => {
    const bucket = buckets.find(item => item.window === window);
    const remaining = bucket && Number.isFinite(bucket.remainingFraction)
      ? Math.max(0, Math.min(1, bucket.remainingFraction)) * 100 : null;
    // A subscription end can supply a reset date, never a made-up monthly cap.
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
      <time className={styles.reset} dateTime={validReset ? new Date(resetAt).toISOString() : undefined}
        title={validReset ? `重置 ${new Date(resetAt).toLocaleString(i18n.language)}` : undefined}>{resetLabel}</time>
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
