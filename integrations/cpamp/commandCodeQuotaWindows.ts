import type { AccountQuotaDisplayWindow } from './accountQuotaDisplayWindows';
import type { AccountRow } from './accountRows';
import type { CommandCodeQuotaState } from '../components/commandCodeQuotaStore';
import { formatCommandCodePlan, getCommandCodeMonthlyQuota } from '../components/commandCodeQuotaPolicy';

const timestamp = (value: string | undefined) => {
  const result = value ? Date.parse(value) : NaN;
  return Number.isFinite(result) && result > 0 ? result : null;
};
export function buildCommandCodeQuotaWindows(state: CommandCodeQuotaState): AccountQuotaDisplayWindow[] {
  if (!state.data) return [];
  const buckets = state.data.groups?.flatMap(group => group.buckets ?? []) ?? [];
  const unavailable = state.usage?.unavailable ?? [];
  const subscriptionAvailable = !unavailable.includes('subscription') && state.usage?.status !== 'error';
  const subscriptionStart = subscriptionAvailable ? timestamp(state.usage?.subscription_start) : null;
  const subscriptionEnd = subscriptionAvailable ? timestamp(state.usage?.subscription_end) : null;
  const summaryEnd = state.data.summary?.find(metric => metric.key === 'subscription_reset_at')?.value;
  const monthly = getCommandCodeMonthlyQuota(state);
  const windows: AccountQuotaDisplayWindow[] = [];
  for (const [name, key, kind, label, duration] of [
    ['fiveHour', 'commandcode-five-hour', 'five_hour', '5 小时限额', 18000],
    ['weekly', 'commandcode-weekly', 'weekly', '周限额', 604800],
    ['monthly', 'commandcode-monthly', 'monthly', '月额度', null],
  ] as const) {
    const bucket = buckets.find(item => item.window === name);
    let end = timestamp(bucket?.resetTime);
    if (name === 'monthly' && end === null) {
      end = subscriptionEnd ?? (typeof summaryEnd === 'number' && Number.isFinite(summaryEnd) && summaryEnd > 0 ? summaryEnd * 1000 : null);
    }
    if (!bucket && (name !== 'monthly' || (end === null && monthly.remainingCredits === null))) continue;
    const fraction = bucket?.remainingFraction;
    const remaining = name === 'monthly' ? monthly.remainingPercent
      : typeof fraction === 'number' && Number.isFinite(fraction) ? Math.max(0, Math.min(1, fraction)) * 100 : null;
    const start = end !== null && duration !== null ? end - duration * 1000
      : name === 'monthly' && end === subscriptionEnd ? subscriptionStart : null;
    const validRange = start !== null && end !== null && start > 0 && start < end;
    const seconds = duration ?? (validRange ? (end! - start!) / 1000 : null);
    windows.push({
      key, kind, label, remainingPercent: remaining, usedPercent: remaining === null ? null : 100 - remaining,
      resetLabel: '', resetAtMs: end, resetAccuracy: end === null ? 'unknown' : 'exact',
      limitWindowSeconds: seconds, fromMs: validRange ? start : null, toMs: end,
      source: 'commandcode', observationSource: 'api_query', observedAtMs: state.observedAtMs ?? null,
      quotaProgressObservedAtMs: state.error || remaining === null ? null : state.observedAtMs ?? null,
      windowMode: validRange ? name === 'monthly' ? 'calendar' : 'fixed' : 'unknown',
      cycleStartMs: validRange ? start : null, cycleEndMs: end,
      modelScope: { kind: 'all', complete: true },
    });
  }
  return windows;
}

export function applyCommandCodeQuotaToRow(row: AccountRow, state: CommandCodeQuotaState): AccountRow {
  if (row.provider !== 'commandcode') return row;
  const plan = formatCommandCodePlan(state.data?.subscription?.plan ?? row.planType);
  if (!state.data && !state.error && !state.pending) return { ...row, planType: plan, canonicalPlanType: plan?.toLowerCase() ?? null };
  const windows = buildCommandCodeQuotaWindows(state);
  const known = windows.filter(window => window.remainingPercent !== null);
  const min = known.length ? Math.min(...known.map(window => window.remainingPercent!)) : null;
  const first = known.find(window => window.remainingPercent === min);
  return { ...row, planType: plan, canonicalPlanType: plan?.trim().toLowerCase() ?? null,
    quota: { ...row.quota,
      status: row.disabled ? 'disabled' : state.error ? 'error' : state.pending && !state.data ? 'loading'
        : min === null ? 'unknown' : min <= 0 ? 'exhausted' : min <= 30 ? 'low' : 'ok',
      remainingPercent: min, usedPercent: min === null ? null : 100 - min,
      resetAtMs: first?.resetAtMs ?? null, resetAccuracy: first?.resetAccuracy ?? 'unknown',
      planType: plan, source: state.data ? 'cache' : 'none',
      fetchedAtMs: state.observedAtMs, failedAtMs: state.failedAtMs,
      error: state.error, errorStatus: state.errorStatus,
    } };
}
