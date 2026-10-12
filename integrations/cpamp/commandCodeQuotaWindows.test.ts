import { describe, expect, it } from 'vitest';
import type { MonitoringAccountWindowUsageItem } from '@/services/api';
import { applyCommandCodeQuotaToRow, buildCommandCodeQuotaWindows } from './commandCodeQuotaWindows';
import { buildAccountQuotaWindowDefinitions, buildAccountQuotaUsageRanges } from './accountQuotaWindowDefinitions';
import { buildAccountQuotaSnapshotWriteEntries, buildAccountQuotaSnapshotQueryAccounts } from './accountQuotaSnapshots';
import { buildAccountWindowUsageTargetEntries, buildAccountWindowUsageByKey } from './accountWindowUsageRows';
import { resolveAccountQuotaWindowUsageAndForecast } from './accountQuotaWindowUsagePresentation';
import { buildAccountRows } from './accountRows';

const now = Date.parse('2026-10-12T14:00:00Z');
const state = { observedAtMs: now, data: { subscription: { plan: 'individual-goat' }, groups: [
  { buckets: [{ window: 'fiveHour', remainingFraction: .6, resetTime: '2026-10-12T15:00:00Z' }] },
  { buckets: [{ window: 'weekly', remainingFraction: .8, resetTime: '2026-10-18T09:00:00Z' }] },
], summary: [{ key: 'monthly_credits', value: 61 }, { key: 'subscription_reset_at', value: Date.parse('2026-11-02T06:34:24Z') / 1000 }] },
usage: { status: 'ok', unavailable: [], subscription_start: '2026-10-02T06:34:24Z', subscription_end: '2026-11-02T06:34:24Z' } };
const row = buildAccountRows([{ id: 'fixture', name: 'fixture.json', provider: 'commandcode', auth_index: 'fixture-index' }],
  { antigravityQuota: {}, claudeQuota: {}, codexQuota: {}, devinQuota: {}, kimiQuota: {}, metaQuota: {}, xaiQuota: {} })[0];

describe('CommandCode native window integration', () => {
  it('uses real resets/percentages and subscription dates without fabricating a monthly cap or previous calendar range', () => {
    const windows = buildCommandCodeQuotaWindows(state);
    expect(windows.map(window => window.remainingPercent)).toEqual([60, 80, null]);
    const definitions = buildAccountQuotaWindowDefinitions(windows, now);
    expect(definitions[0].cycleStartMs).toBe(Date.parse('2026-10-12T10:00:00Z'));
    expect(definitions.map(definition => definition.durationSeconds)).toEqual([18000, 604800, 31 * 86400]);
    expect(buildAccountQuotaUsageRanges(definitions[0], now)).toEqual([
      { period: 'current', fromMs: Date.parse('2026-10-12T10:00:00Z'), toMs: now },
      { period: 'previous', fromMs: Date.parse('2026-10-12T05:00:00Z'), toMs: Date.parse('2026-10-12T10:00:00Z') },
    ]);
    expect(buildAccountQuotaUsageRanges(definitions[2], now)).toEqual([
      { period: 'current', fromMs: Date.parse(state.usage.subscription_start), toMs: now },
    ]);
    const summary = applyCommandCodeQuotaToRow(row, state);
    expect(summary.planType).toBe('individual-goat');
    expect(summary.quota.status).toBe('ok');
    expect(summary.quota.remainingPercent).toBe(60);
    const nativeMonthly = buildCommandCodeQuotaWindows({ ...state, data: { ...state.data,
      groups: [{ buckets: [{ window: 'monthly', remainingFraction: .25, resetTime: state.usage.subscription_end }] }] } });
    expect(nativeMonthly[0].remainingPercent).toBe(25);
  });

  it('feeds credential-scoped native history targets, snapshot persistence and quota forecasts', () => {
    const windows = buildCommandCodeQuotaWindows(state);
    const definitions = buildAccountQuotaWindowDefinitions(windows, now);
    const byRow = new Map([[row.selectionKey, definitions]]);
    const entries = buildAccountWindowUsageTargetEntries([row], byRow, now);
    expect(entries).toHaveLength(5);
    expect(entries.every(entry => entry.target.auth_index === 'fixture-index' && entry.target.auth_provider_snapshot === 'commandcode')).toBe(true);
    const observations = buildAccountQuotaSnapshotWriteEntries([row], byRow, { nowMs: now,
      getObservation: () => ({ source: 'api_query', observed_at_ms: now, inventory_scope_key: 'commandcode:quota-windows', inventory_mode: 'partial' }) });
    expect(observations[0].provider).toBe('commandcode');
    expect(observations[0].windows[0].model_scope_kind).toBe('all');
    expect(observations[0].windows[0].used_percent).toBe(40);
    expect(observations[0].windows[2].used_percent).toBeUndefined();
    const queries = buildAccountQuotaSnapshotQueryAccounts([row]);
    expect(queries).toHaveLength(1);
    expect(queries[0].provider).toBe('commandcode');
    expect(queries[0].account.auth_index).toBe('fixture-index');
    const current = entries.find(entry => entry.providerWindowId === windows[0].key && entry.period === 'current')!;
    const item: MonitoringAccountWindowUsageItem = { ...current.target, matched: true, total_requests: 8,
      success_calls: 7, failure_calls: 1, total_tokens: 800, total_cost: 2, success_rate: .875,
      last_seen_ms: now - 1000, sync_status: 'ready', scope_match_status: 'complete' };
    const usage = buildAccountWindowUsageByKey(entries, [item]);
    const presentation = resolveAccountQuotaWindowUsageAndForecast(row, windows[0], usage);
    expect(presentation.currentUsage?.totalRequests).toBe(8);
    expect(presentation.forecast).toEqual({ requests: 20, tokens: 2000, cost: 5, basis: 'quota' });
    expect(resolveAccountQuotaWindowUsageAndForecast(row, buildCommandCodeQuotaWindows({ ...state, error: 'expired' })[0], usage).forecast).toBeNull();
    // Persisted snapshots can restore older percentages. A failed live refresh
    // must still suppress a forecast while preserving the actual request stats.
    const failedRow = applyCommandCodeQuotaToRow(row, { ...state, error: 'expired' });
    const failedPresentation = resolveAccountQuotaWindowUsageAndForecast(failedRow, windows[0], usage);
    expect(failedPresentation.currentUsage?.totalRequests).toBe(8);
    expect(failedPresentation.forecast).toBeNull();
  });

  it('does not borrow subscription dates from failed sections or lose a valid balance on partial refreshes', () => {
    const partial = buildCommandCodeQuotaWindows({ ...state, usage: { ...state.usage, unavailable: ['subscription'] },
      data: { groups: state.data.groups } });
    expect(partial).toHaveLength(2);
    expect(applyCommandCodeQuotaToRow(row, { ...state, error: 'expired', errorStatus: 401 }).quota.status).toBe('error');
    expect(applyCommandCodeQuotaToRow(row, { ...state, error: 'expired' }).quota.remainingPercent).toBe(60);
  });
});
