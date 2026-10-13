import { useSyncExternalStore } from 'react';
import type { AuthFileItem } from '@/types';
import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from '@/services/api/client';

export interface CommandCodeNativeQuota {
  subscription?: { plan?: string };
  summary?: { key: string; value: number }[];
  groups?: { buckets?: { window?: string; remainingFraction?: number; resetTime?: string }[] }[];
}
export interface CommandCodeUsage {
  status?: string;
  unavailable?: string[];
  subscription_start?: string;
  subscription_end?: string;
  checked_at?: string;
  windows?: { name: string; used?: number; cap?: number; reset_at?: string }[];
}
export interface CommandCodeQuotaState {
  data?: CommandCodeNativeQuota;
  usage?: CommandCodeUsage;
  observedAtMs?: number;
  pending?: boolean;
  error?: string;
  errorStatus?: number;
  failedAtMs?: number;
}
const empty: CommandCodeQuotaState = {};
const cache = new Map<string, CommandCodeQuotaState>();
const inFlight = new Map<string, Promise<CommandCodeQuotaState>>();
const listeners = new Set<() => void>();
let revision = 0;
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
const scopeKey = (file: AuthFileItem, scope: ApiClientRequestScope) => JSON.stringify([
  scope.apiBase, scope.managementKey, file.id, file.authIndex ?? file.auth_index,
  file.name, file.modified, file.updated_at, file.lastRefresh, file.disabled,
]);
function update(key: string, value: CommandCodeQuotaState) {
  if (cache.size >= 256 && !cache.has(key)) cache.delete(cache.keys().next().value!);
  cache.set(key, value);
  revision += 1;
  listeners.forEach(listener => listener());
}
export function useCommandCodeQuotaRevision() {
  return useSyncExternalStore(subscribe, () => revision, () => 0);
}
export function getCommandCodeQuota(file: AuthFileItem, scope: ApiClientRequestScope) {
  return cache.get(scopeKey(file, scope)) ?? empty;
}
export function useCommandCodeQuota(file: AuthFileItem, scope: ApiClientRequestScope) {
  const key = scopeKey(file, scope);
  return useSyncExternalStore(subscribe, () => cache.get(key) ?? empty, () => empty);
}
export function refreshCommandCodeQuota(file: AuthFileItem, scope: ApiClientRequestScope): Promise<CommandCodeQuotaState> {
  const key = scopeKey(file, scope);
  const authIndex = String(file.authIndex ?? file.auth_index ?? '').trim();
  if (file.disabled || !authIndex || !scope.managementKey) return Promise.resolve(empty);
  const pending = inFlight.get(key);
  if (pending) return pending;
  const old = cache.get(key) ?? empty;
  update(key, { ...old, pending: true, error: undefined, errorStatus: undefined });
  const request = (async () => {
    try {
      const config = { ...createScopedApiRequestConfig(scope), timeout: 20000 };
      const data = await apiClient.post<CommandCodeNativeQuota>('/quota/fetch', { auth_index: authIndex, provider: 'commandcode' }, config);
      // The native RPC omits observation times and the subscription start. Read
      // those from the plugin's existing redacted, cached management endpoint.
      // The native response remains the authority for fresh quota percentages.
      let usage: CommandCodeUsage | undefined;
      try {
        const result = await apiClient.get<{ credentials?: { id: string; usage?: CommandCodeUsage }[] }>('/commandcodebridge/credentials', config);
        const ids = [file.id, file.name].map(value => String(value ?? '').replace(/\.json$/i, ''));
        usage = result.credentials?.find(credential => ids.includes(credential.id))?.usage;
      } catch { /* Older plugins still support native percentage/reset display. */ }
      const checkedAt = usage?.checked_at ? Date.parse(usage.checked_at) : NaN;
      const state: CommandCodeQuotaState = { data, usage,
        observedAtMs: Number.isFinite(checkedAt) && checkedAt > 0 && checkedAt <= Date.now() + 1000 ? checkedAt : Date.now() };
      update(key, state);
      return state;
    } catch (error) {
      const status = (error as { response?: { status?: number }; status?: number })?.response?.status
        ?? (error as { status?: number })?.status;
      const state = { ...old, pending: false, error: old.data ? '刷新失败，上次结果已过期' : '刷新失败，请重试',
        errorStatus: typeof status === 'number' ? status : undefined, failedAtMs: Date.now() };
      update(key, state);
      return state;
    }
  })();
  inFlight.set(key, request);
  void request.finally(() => { if (inFlight.get(key) === request) inFlight.delete(key); });
  return request;
}
