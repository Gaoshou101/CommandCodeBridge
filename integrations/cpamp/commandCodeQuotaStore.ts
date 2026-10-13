import { useEffect, useSyncExternalStore } from 'react';
import type { AuthFileItem } from '@/types';
import type { MonitoringAccountLatestRequest } from '@/services/api/usageService';
import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from '@/services/api/client';

export interface CommandCodeNativeQuota {
  subscription?: { plan?: string };
  summary?: { key: string; value: number }[];
  groups?: { buckets?: { window?: string; remainingFraction?: number; resetTime?: string }[] }[];
}
export interface CommandCodeUsage {
  status?: string;
  http_status?: number;
  unavailable?: string[];
  subscription_start?: string;
  subscription_end?: string;
  checked_at?: string;
  updated_at?: string;
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
  recentRequests?: MonitoringAccountLatestRequest[];
  credentialUpdatedAt?: string | null;
  credentialRevision?: string;
}
interface CredentialSnapshot {
  id: string;
  file_name?: string;
  routing_id?: string;
  key_updated_at?: string | null;
  credential_revision?: string;
  quota?: CommandCodeNativeQuota;
  usage?: CommandCodeUsage;
  recent_requests?: MonitoringAccountLatestRequest[];
}
const empty: CommandCodeQuotaState = {};
const cache = new Map<string, CommandCodeQuotaState>();
const inFlight = new Map<string, Promise<CommandCodeQuotaState>>();
const snapshotInFlight = new Map<string, Promise<void>>();
const listeners = new Set<() => void>();
let revision = 0;
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
const scopeKey = (file: AuthFileItem, scope: ApiClientRequestScope) => JSON.stringify([
  scope.apiBase, scope.managementKey, file.id, file.authIndex ?? file.auth_index,
  file.name, file.disabled, file.key_updated_at, file.proxy_url,
]);
const credentialMatches = (item: CredentialSnapshot, file: AuthFileItem) =>
  (item.file_name ? item.file_name === file.name : item.id === file.name.replace(/\.json$/i, '')) ||
  item.routing_id === file.id || item.id === file.id;
const observationTime = (usage: CommandCodeUsage | undefined) => {
  const at = usage?.updated_at ?? (usage?.status !== 'error' ? usage?.checked_at : undefined);
  const ms = at ? Date.parse(at) : NaN;
  return Number.isFinite(ms) && ms > 0 && ms <= Date.now() + 1000 ? ms : undefined;
};
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
// Restore the sampler's persisted data and actual request history using a
// single redacted management read, without one upstream quota query per card.
export function syncCommandCodeCredentials(files: AuthFileItem[], scope: ApiClientRequestScope): Promise<void> {
  const targets = files.filter(file => (file.provider ?? file.type) === 'commandcode');
  if (!targets.length || !scope.managementKey) return Promise.resolve();
  const batchKey = JSON.stringify([scope.apiBase, scope.managementKey, targets.map(file => scopeKey(file, scope)).sort()]);
  const pending = snapshotInFlight.get(batchKey);
  if (pending) return pending;
  const request = (async () => {
    try {
      const result = await apiClient.get<{ credentials?: CredentialSnapshot[] }>('/commandcodebridge/credentials',
        { ...createScopedApiRequestConfig(scope), timeout: 20000 });
      for (const file of targets) {
        const item = result.credentials?.find(credential => credentialMatches(credential, file));
        if (!item) continue;
        const key = scopeKey(file, scope);
        const old = cache.get(key) ?? empty;
        const observedAtMs = observationTime(item.usage);
        const newer = observedAtMs !== undefined && (old.observedAtMs === undefined || observedAtMs >= old.observedAtMs);
        const replaced = old.credentialRevision !== undefined && old.credentialRevision !== item.credential_revision;
        let state: CommandCodeQuotaState = replaced ? {} : { ...old };
        const recovered = !old.error || old.failedAtMs === undefined ||
          (observedAtMs !== undefined && observedAtMs > old.failedAtMs);
        if (!old.pending && item.quota && (newer || replaced || !old.data)) {
          state = { ...state, data: item.quota, usage: item.usage, observedAtMs,
            error: item.usage?.status === 'error' || Boolean(item.usage?.unavailable?.length)
              ? '刷新失败，保留上次结果' : recovered ? undefined : old.error,
            errorStatus: item.usage?.status === 'error' ? item.usage.http_status : recovered ? undefined : old.errorStatus,
            failedAtMs: item.usage?.status === 'error' && item.usage.checked_at
              ? Date.parse(item.usage.checked_at) : recovered ? undefined : old.failedAtMs };
        }
        update(key, { ...state, credentialUpdatedAt: item.key_updated_at ?? null, credentialRevision: item.credential_revision,
          recentRequests: item.recent_requests ?? state.recentRequests });
      }
    } catch { /* Keep the last snapshot while the connection is unavailable. */ }
  })();
  snapshotInFlight.set(batchKey, request);
  void request.finally(() => { if (snapshotInFlight.get(batchKey) === request) snapshotInFlight.delete(batchKey); });
  return request;
}
export function useCommandCodeCredentials(files: AuthFileItem[], scope: ApiClientRequestScope, enabled: boolean) {
  useEffect(() => {
    if (!enabled) return;
    const sync = () => {
      if (typeof document === 'undefined' || document.visibilityState !== 'hidden') void syncCommandCodeCredentials(files, scope);
    };
    sync();
    const timer = setInterval(sync, 30000);
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', sync);
    return () => {
      clearInterval(timer);
      if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', sync);
    };
  }, [files, scope.apiBase, scope.managementKey, enabled]);
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
      let snapshot: CredentialSnapshot | undefined;
      try {
        const result = await apiClient.get<{ credentials?: CredentialSnapshot[] }>('/commandcodebridge/credentials', config);
        snapshot = result.credentials?.find(credential => credentialMatches(credential, file));
        usage = snapshot?.usage;
      } catch { /* Older plugins still support native percentage/reset display. */ }
      const latest = cache.get(key);
      if (old.credentialRevision !== undefined && (latest?.credentialRevision !== old.credentialRevision ||
        (snapshot?.credential_revision !== undefined && snapshot.credential_revision !== old.credentialRevision))) {
        const state = { ...latest, pending: false };
        update(key, state);
        return state;
      }
      const state: CommandCodeQuotaState = { ...latest, data, usage, pending: false, error: undefined,
        errorStatus: undefined, failedAtMs: undefined, observedAtMs: observationTime(usage) ?? Date.now(),
        credentialRevision: snapshot?.credential_revision ?? latest?.credentialRevision,
        recentRequests: snapshot?.recent_requests ?? latest?.recentRequests };
      update(key, state);
      return state;
    } catch (error) {
      const status = (error as { response?: { status?: number }; status?: number })?.response?.status
        ?? (error as { status?: number })?.status;
      const latest = cache.get(key) ?? old;
      const state = { ...latest, pending: false, error: latest.data ? '刷新失败，上次结果已过期' : '刷新失败，请重试',
        errorStatus: typeof status === 'number' ? status : undefined, failedAtMs: Date.now() };
      update(key, state);
      return state;
    }
  })();
  inFlight.set(key, request);
  void request.finally(() => { if (inFlight.get(key) === request) inFlight.delete(key); });
  return request;
}
