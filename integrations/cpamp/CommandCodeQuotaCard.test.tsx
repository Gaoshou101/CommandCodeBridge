import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { describe, expect, it, vi } from 'vitest';
import { CommandCodeQuotaCard } from './CommandCodeQuotaCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const mocks = vi.hoisted(() => ({ post: vi.fn(), store: { apiBase: 'https://panel.example', managementKey: 'fixture-manager-key', connectionStatus: 'connected' } }));
vi.mock('@/services/api/client', () => ({ apiClient: { post: mocks.post }, createScopedApiRequestConfig: (scope: unknown) => ({ scope }) }));
vi.mock('@/stores', () => ({ useAuthStore: (selector: (state: typeof mocks.store) => unknown) => selector(mocks.store) }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ i18n: { language: 'zh-CN' } }) }));
const file = { id: 'fixture-commandcode', name: 'fixture.json', authIndex: 'fixture-index', provider: 'commandcode' };
const quota = { subscription: { plan: 'individual-goat' }, summary: [{ key: 'remaining_credits', label: '剩余额度', value: 45, unit: 'credits' }, { key: 'monthly_credits', label: '本月剩余', value: 40, unit: 'credits' }], groups: [{ displayName: '5 小时窗口', buckets: [{ window: 'fiveHour', remainingFraction: .6, resetTime: '2026-11-01T00:00:00Z' }] }] };
const readText = (node: unknown): string => {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(readText).join('');
  if (node && typeof node === 'object' && 'children' in node) return readText((node as { children: unknown }).children);
  return '';
};
const button = (renderer: ReactTestRenderer) => renderer.root.findByProps({ 'aria-label': '刷新 CommandCode 套餐额度' });

describe('CommandCode native credential quotas', () => {
  it('refreshes explicitly, shares observations with the drawer, preserves failures and scopes credential versions', async () => {
    mocks.post.mockReset();
    let grid!: ReactTestRenderer, detail!: ReactTestRenderer;
    await act(async () => { grid = create(<CommandCodeQuotaCard file={file} compact />); });
    expect(mocks.post).not.toHaveBeenCalled();
    mocks.post.mockResolvedValueOnce(quota);
    await act(async () => { button(grid).props.onClick(); });
    expect(mocks.post).toHaveBeenCalledWith('/quota/fetch', { auth_index: 'fixture-index', provider: 'commandcode' }, expect.objectContaining({ timeout: 20000 }));
    expect(readText(grid.toJSON())).toContain('individual-goat');
    expect(readText(grid.toJSON())).toContain('45 credits');
    expect(readText(grid.toJSON())).toContain('剩余 60%');
    expect(readText(grid.toJSON())).toContain('月上限未知');
    await act(async () => { detail = create(<CommandCodeQuotaCard file={file} />); });
    expect(readText(detail.toJSON())).toContain('45 credits');
    expect(mocks.post).toHaveBeenCalledTimes(1);
    mocks.post.mockRejectedValueOnce(new Error('private upstream error with fixture-secret'));
    await act(async () => { button(detail).props.onClick(); });
    expect(readText(grid.toJSON())).toContain('45 credits');
    expect(readText(grid.toJSON())).toContain('已过期');
    expect(readText(grid.toJSON())).not.toContain('fixture-secret');
    await act(async () => { grid.update(<CommandCodeQuotaCard file={{ ...file, modified: 123 }} compact />); });
    expect(readText(grid.toJSON())).not.toContain('45 credits');
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
    expect(readText(renderer.toJSON())).not.toContain('45 credits');
    await act(async () => { renderer.unmount(); });
    mocks.store.managementKey = 'fixture-manager-key';
  });
});
