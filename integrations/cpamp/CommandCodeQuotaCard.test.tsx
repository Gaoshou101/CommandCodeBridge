import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CommandCodeQuotaCard } from './CommandCodeQuotaCard';
import { QuotaProgressBar } from './QuotaProgressBar';
import { getCommandCodeQuota, refreshCommandCodeQuota } from './commandCodeQuotaStore';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const mocks = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), store: { apiBase: 'https://panel.example', managementKey: 'fixture-manager-key', connectionStatus: 'connected' } }));
vi.mock('@/services/api/client', () => ({ apiClient: { post: mocks.post, get: mocks.get }, createScopedApiRequestConfig: (scope: unknown) => ({ scope }) }));
vi.mock('@/stores', () => ({ useAuthStore: (selector: (state: typeof mocks.store) => unknown) => selector(mocks.store) }));
vi.mock('react-i18next', async (importOriginal) => ({ ...await importOriginal<typeof import('react-i18next')>(), useTranslation: () => ({ i18n: { language: 'zh-CN' } }) }));
const file = { id: 'fixture-commandcode', name: 'fixture.json', authIndex: 'fixture-index', provider: 'commandcode' };
const quota = { subscription: { plan: 'individual-goat' }, summary: [{ key: 'remaining_credits', label: '剩余额度', value: 45, unit: 'credits' }, { key: 'monthly_credits', label: '本月剩余', value: 40, unit: 'credits' }, { key: 'subscription_reset_at', label: '月额度重置', value: Date.parse('2026-11-01T00:00:00Z') / 1000, unit: 'unix_seconds' }], groups: [
  { displayName: '5 小时窗口', buckets: [{ window: 'fiveHour', remainingFraction: .6, resetTime: '2026-10-12T04:00:00Z', description: '已用 20 / 50 credits' }] },
  { displayName: '每周窗口', buckets: [{ window: 'weekly', remainingFraction: .86, resetTime: '2026-10-16T00:00:00Z' }] },
] };
const readText = (node: unknown): string => {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(readText).join('');
  if (node && typeof node === 'object' && 'children' in node) return readText((node as { children: unknown }).children);
  return '';
};
const button = (renderer: ReactTestRenderer) => renderer.root.findByProps({ 'aria-label': '刷新 CommandCode 套餐额度' });

describe('CommandCode native credential quotas', () => {
  afterEach(() => { vi.restoreAllMocks(); });
  it('deduplicates native refreshes and scopes late responses to the original credential and connection', async () => {
    const scope = { apiBase: 'https://isolated.example', managementKey: 'fixture-scoped-key' };
    const original = { ...file, id: 'fixture', name: 'fixture.json' };
    const changed = { ...original, authIndex: 'replacement-index', modified: 456 };
    let finish!: (value: typeof quota) => void;
    mocks.post.mockReset(); mocks.get.mockReset();
    mocks.post.mockReturnValueOnce(new Promise(resolve => { finish = resolve; }));
    mocks.get.mockResolvedValueOnce({ credentials: [{ id: 'fixture', usage: { status: 'ok',
      checked_at: '2026-10-12T00:00:00Z', subscription_start: '2026-10-02T00:00:00Z', subscription_end: '2026-11-02T00:00:00Z' } }] });
    const first = refreshCommandCodeQuota(original, scope);
    const second = refreshCommandCodeQuota(original, scope);
    expect(first).toBe(second);
    expect(mocks.post).toHaveBeenCalledTimes(1);
    finish(quota); await first;
    expect(getCommandCodeQuota(original, scope).usage?.subscription_start).toBe('2026-10-02T00:00:00Z');
    expect(getCommandCodeQuota(changed, scope).data).toBeUndefined();
    expect(getCommandCodeQuota(original, { ...scope, apiBase: 'https://different.example' }).data).toBeUndefined();
    expect(getCommandCodeQuota(original, { ...scope, managementKey: 'different-login' }).data).toBeUndefined();
    expect(mocks.get).toHaveBeenCalledWith('/commandcodebridge/credentials', expect.objectContaining({ scope }));
  });
  it('refreshes explicitly, shares observations with the drawer, preserves failures and scopes credential versions', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-10-12T00:00:00Z'));
    mocks.post.mockReset();
    let grid!: ReactTestRenderer, detail!: ReactTestRenderer;
    await act(async () => { grid = create(<CommandCodeQuotaCard file={file} compact />); });
    expect(mocks.post).not.toHaveBeenCalled();
    mocks.post.mockResolvedValueOnce(quota);
    await act(async () => { button(grid).props.onClick(); });
    expect(mocks.post).toHaveBeenCalledWith('/quota/fetch', { auth_index: 'fixture-index', provider: 'commandcode' }, expect.objectContaining({ timeout: 20000 }));
    expect(readText(grid.toJSON())).toContain('5h');
    expect(readText(grid.toJSON())).toContain('周额度');
    expect(readText(grid.toJSON())).toContain('剩余 60%');
    expect(readText(grid.toJSON())).toContain('剩余 86%');
    expect(readText(grid.toJSON())).toContain('4 小时后');
    expect(readText(grid.toJSON())).toContain('4 天后');
    expect(readText(grid.toJSON())).toContain('月额度剩余 57%');
    expect(readText(grid.toJSON())).toContain('40 / 70 credits');
    expect(readText(grid.toJSON())).toContain('20 天后');
    expect(grid.root.findAllByType(QuotaProgressBar).map(node => node.props.percent)).toEqual([60, 86, 40 / 70 * 100]);
    expect(grid.root.findAllByType('time').map(node => node.props.dateTime)).toEqual(['2026-10-12T04:00:00.000Z', '2026-10-16T00:00:00.000Z', '2026-11-01T00:00:00.000Z']);
    for (const removed of ['individual-goat', '月上限未知', '更新', '已用']) {
      expect(readText(grid.toJSON())).not.toContain(removed);
    }
    await act(async () => { detail = create(<CommandCodeQuotaCard file={file} />); });
    expect(readText(detail.toJSON())).toBe(readText(grid.toJSON()));
    expect(mocks.post).toHaveBeenCalledTimes(1);
    mocks.post.mockRejectedValueOnce(new Error('private upstream error with fixture-secret'));
    await act(async () => { button(detail).props.onClick(); });
    expect(readText(grid.toJSON())).toContain('剩余 60%');
    expect(readText(grid.toJSON())).toContain('已过期');
    expect(readText(grid.toJSON())).not.toContain('fixture-secret');
    await act(async () => { grid.update(<CommandCodeQuotaCard file={{ ...file, modified: 123 }} compact />); });
    expect(readText(grid.toJSON())).not.toContain('剩余 60%');
    await act(async () => { grid.unmount(); detail.unmount(); });
  });
  it('never requests quota for a disabled credential or shares it with a new manager login', async () => {
    mocks.post.mockReset();
    let renderer!: ReactTestRenderer;
    await act(async () => { renderer = create(<CommandCodeQuotaCard file={{ ...file, disabled: true }} />); });
    expect(button(renderer).props.disabled).toBe(true);
    expect(mocks.post).not.toHaveBeenCalled();
    mocks.store.managementKey = 'fixture-different-key';
    await act(async () => { renderer.update(<CommandCodeQuotaCard file={file} />); });
    expect(readText(renderer.toJSON())).not.toContain('剩余 60%');
    await act(async () => { renderer.unmount(); });
    mocks.store.managementKey = 'fixture-manager-key';
  });
  it('prefers native monthly percentages and otherwise uses the confirmed GOAT allowance', async () => {
    mocks.post.mockReset();
    let renderer!: ReactTestRenderer;
    await act(async () => { renderer = create(<CommandCodeQuotaCard file={{ ...file, id: 'monthly-fixture' }} />); });
    mocks.post.mockResolvedValueOnce({ ...quota, groups: [
      { buckets: [{ window: 'monthly', remainingFraction: .25, resetTime: '2026-11-02T00:00:00Z' }] },
      ...quota.groups,
    ] });
    await act(async () => { button(renderer).props.onClick(); });
    expect(readText(renderer.toJSON())).toContain('月额度剩余 25%');
    expect(renderer.root.findAllByType(QuotaProgressBar).map(node => node.props.percent)).toEqual([60, 86, 25]);
    expect(renderer.root.findAllByType('time')[2].props.dateTime).toBe('2026-11-02T00:00:00.000Z');
    mocks.post.mockResolvedValueOnce({ ...quota, groups: [{ buckets: [{ window: 'monthly', remainingFraction: NaN }] }] });
    await act(async () => { button(renderer).props.onClick(); });
    expect(renderer.root.findAllByType(QuotaProgressBar).map(node => node.props.percent)).toEqual([null, null, 40 / 70 * 100]);
    expect(renderer.root.findAllByType('time')[2].props.dateTime).toBe('2026-11-01T00:00:00.000Z');
    await act(async () => { renderer.unmount(); });
  });
});
