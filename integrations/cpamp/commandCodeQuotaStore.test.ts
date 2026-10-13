import { createElement } from 'react';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('@/services/api/client', () => ({ apiClient: mocks,
  createScopedApiRequestConfig: (scope: unknown) => ({ scope }) }));

describe('CommandCode persisted observations', () => {
  const scope = { apiBase: 'https://snapshot.example', managementKey: 'fixture-manager' };
  const file = { id: 'account.json', name: 'account.json', provider: 'commandcode', authIndex: 'account-index' };
  const observed = '2026-10-12T00:00:00Z';
  const quota = { subscription: { plan: 'individual-goat' }, summary: [{ key: 'monthly_credits', value: 60 }] };
  const snapshot = () => ({ credentials: [{ id: 'account', file_name: file.name, credential_revision: 'revision-a',
    quota, usage: { status: 'ok', updated_at: observed }, recent_requests: [{ timestamp_ms: 1791763261000, failed: false }] }] });
  beforeEach(() => { vi.resetModules(); mocks.get.mockReset(); mocks.post.mockReset(); });
  afterEach(() => { vi.useRealTimers(); });

  it('loads on mount and polls new requests every 30 seconds, then stops on unmount', async () => {
    vi.useFakeTimers();
    const store = await import('./commandCodeQuotaStore');
    mocks.get.mockResolvedValue(snapshot());
    const files = [file];
    const Watcher = () => {
      store.useCommandCodeCredentials(files, scope, true);
      return null;
    };
    let renderer!: ReactTestRenderer;
    await act(async () => { renderer = create(createElement(Watcher)); });
    expect(mocks.get).toHaveBeenCalledTimes(1);
    const next = snapshot();
    next.credentials[0].recent_requests.unshift({ timestamp_ms: 1791763290000, failed: false });
    mocks.get.mockResolvedValue(next);
    await act(async () => { await vi.advanceTimersByTimeAsync(30000); });
    expect(store.getCommandCodeQuota(file, scope).recentRequests).toHaveLength(2);
    await act(async () => { renderer.unmount(); });
    await vi.advanceTimersByTimeAsync(60000);
    expect(mocks.get).toHaveBeenCalledTimes(2);
  });

  it('restores quota after a page reload without querying upstream or changing the observation time', async () => {
    let store = await import('./commandCodeQuotaStore');
    mocks.get.mockResolvedValue(snapshot());
    await store.syncCommandCodeCredentials([file], scope);
    expect(store.getCommandCodeQuota(file, scope).data).toEqual(quota);
    vi.resetModules();
    store = await import('./commandCodeQuotaStore');
    expect(store.getCommandCodeQuota(file, scope).data).toBeUndefined();
    await store.syncCommandCodeCredentials([file], scope);
    expect(store.getCommandCodeQuota(file, scope).observedAtMs).toBe(Date.parse(observed));
    expect(mocks.post).not.toHaveBeenCalled();
    expect(store.getCommandCodeQuota({ ...file, modified: 42, updated_at: 'later', lastRefresh: 50 }, scope).data).toEqual(quota);
    expect(store.getCommandCodeQuota(file, { ...scope, managementKey: 'other-login' }).data).toBeUndefined();
  });

  it('updates recent request bars while retaining last quota when polling fails', async () => {
    const store = await import('./commandCodeQuotaStore');
    mocks.get.mockResolvedValueOnce(snapshot());
    await store.syncCommandCodeCredentials([file], scope);
    const next = snapshot();
    next.credentials[0].recent_requests.unshift({ timestamp_ms: 1791763290000, failed: false });
    mocks.get.mockResolvedValueOnce(next);
    await store.syncCommandCodeCredentials([file], scope);
    expect(store.getCommandCodeQuota(file, scope).recentRequests?.[0].timestamp_ms).toBe(1791763290000);
    mocks.get.mockRejectedValueOnce(new Error('connection unavailable'));
    await store.syncCommandCodeCredentials([file], scope);
    expect(store.getCommandCodeQuota(file, scope).recentRequests).toHaveLength(2);
    expect(store.getCommandCodeQuota(file, scope).data).toEqual(quota);
  });

  it('clears observations when the same file contains a different key', async () => {
    const store = await import('./commandCodeQuotaStore');
    mocks.get.mockResolvedValueOnce(snapshot());
    await store.syncCommandCodeCredentials([file], scope);
    mocks.get.mockResolvedValueOnce({ credentials: [{ id: 'account', file_name: file.name,
      credential_revision: 'revision-b', recent_requests: [] }] });
    await store.syncCommandCodeCredentials([file], scope);
    expect(store.getCommandCodeQuota(file, scope).data).toBeUndefined();
    expect(store.getCommandCodeQuota(file, scope).recentRequests).toEqual([]);
  });

  it('marks failed restored data as stale without treating the failed check as a new observation', async () => {
    const store = await import('./commandCodeQuotaStore');
    mocks.get.mockResolvedValueOnce({ credentials: [{ ...snapshot().credentials[0],
      usage: { status: 'error', updated_at: observed, checked_at: '2026-10-13T00:00:00Z' } }] });
    await store.syncCommandCodeCredentials([file], scope);
    expect(store.getCommandCodeQuota(file, scope).observedAtMs).toBe(Date.parse(observed));
    expect(store.getCommandCodeQuota(file, scope).error).toBeTruthy();
  });

  it('does not clear a failed refresh merely because the server still has the same old snapshot', async () => {
    const store = await import('./commandCodeQuotaStore');
    mocks.get.mockResolvedValue(snapshot());
    await store.syncCommandCodeCredentials([file], scope);
    mocks.post.mockRejectedValueOnce(new Error('quota unavailable'));
    await store.refreshCommandCodeQuota(file, scope);
    await store.syncCommandCodeCredentials([file], scope);
    expect(store.getCommandCodeQuota(file, scope).error).toBeTruthy();
    expect(store.getCommandCodeQuota(file, scope).observedAtMs).toBe(Date.parse(observed));
  });
});
