import { useSyncExternalStore } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { IconRefreshCw } from '@/components/ui/icons';
import { apiClient, createScopedApiRequestConfig } from '@/services/api/client';
import { useAuthStore } from '@/stores';
import type { AuthFileItem } from '@/types';
import { QuotaProgressBar } from './QuotaProgressBar';
import styles from './CommandCodeQuotaCard.module.scss';

interface NativeQuota {
  subscription?: { plan?: string };
  summary?: { key: string; label: string; value: number; unit?: string }[];
  groups?: {
    displayName?: string;
    buckets?: { window?: string; remainingFraction: number; resetTime?: string; description?: string }[];
  }[];
}
interface QuotaState {
  data?: NativeQuota;
  pending?: boolean;
  error?: string;
  observedAt?: number;
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
  const number = (value: number) => new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 3 }).format(value);
  const date = (value: string | number) => {
    const at = typeof value === 'number' ? value * 1000 : Date.parse(value);
    return Number.isFinite(at) && at > 0 ? new Date(at).toLocaleString(i18n.language) : '—';
  };
  const refresh = async () => {
    if (!enabled || cache.get(key)?.pending) return;
    const old = cache.get(key) ?? empty;
    update(key, { ...old, pending: true, error: undefined });
    try {
      const data = await apiClient.post<NativeQuota>('/quota/fetch', { auth_index: authIndex, provider: 'commandcode' }, {
        ...createScopedApiRequestConfig({ apiBase, managementKey }), timeout: 20000,
      });
      update(key, { data, observedAt: Date.now() });
    } catch {
      update(key, { ...old, pending: false, error: '额度刷新失败，保留上次结果' });
    }
  };
  const metrics = (state.data?.summary ?? []).filter(metric => Number.isFinite(metric.value));
  const visibleMetrics = compact ? metrics.filter(metric => ['remaining_credits', 'monthly_credits'].includes(metric.key)) : metrics;
  const groups = state.data?.groups ?? [];
  const hasMonthlyWindow = groups.some(group => group.buckets?.some(bucket => bucket.window === 'monthly'));
  const monthlyReset = metrics.find(metric => metric.key === 'subscription_reset_at');
  return (
    <section className={`${styles.card} ${compact ? styles.compact : ''}`} data-commandcode-native-quota="true"
      onClick={event => event.stopPropagation()} onKeyDown={event => event.stopPropagation()}>
      <div className={styles.heading}>
        <strong>{state.data?.subscription?.plan || 'CommandCode 套餐'}</strong>
        <Button variant="secondary" size="xs" loading={state.pending} disabled={!enabled}
          onClick={() => void refresh()} aria-label="刷新 CommandCode 套餐额度"><IconRefreshCw size={13} /> 刷新额度</Button>
      </div>
      {!state.data && <p>{file.disabled ? '凭证已停用' : '点击刷新，查询当前套餐余量'}</p>}
      <div className={styles.metrics}>{visibleMetrics.map(metric => (
        <div key={metric.key}><span>{metric.label}</span><strong>{metric.unit === 'unix_seconds' ? date(metric.value) : `${number(metric.value)} ${metric.unit ?? ''}`}</strong></div>
      ))}</div>
      {groups.map((group, groupIndex) => (group.buckets ?? []).filter(bucket => Number.isFinite(bucket.remainingFraction)).map((bucket, bucketIndex) => {
        const remaining = Math.max(0, Math.min(1, bucket.remainingFraction)) * 100;
        return <div className={styles.window} key={`${groupIndex}-${bucketIndex}`}>
          <div className={styles.heading}><span>{group.displayName || bucket.window}</span><strong>剩余 {number(remaining)}%</strong></div>
          <QuotaProgressBar percent={remaining} highThreshold={70} mediumThreshold={30} />
          {!compact && bucket.description && <small>{bucket.description}</small>}
          <small>重置 {bucket.resetTime ? date(bucket.resetTime) : '未知'}</small>
        </div>;
      }))}
      {state.data && !hasMonthlyWindow && <small>月上限未知{monthlyReset ? ` · 重置 ${date(monthlyReset.value)}` : ''}</small>}
      {state.error && <p role="alert" className={styles.error}>{state.error}</p>}
      {state.observedAt && <small>更新 {new Date(state.observedAt).toLocaleString(i18n.language)}{state.error ? ' · 已过期' : ''}</small>}
    </section>
  );
}
