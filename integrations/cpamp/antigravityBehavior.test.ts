import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TFunction } from 'i18next';
import type { AuthFileItem } from '@/types';
import { fetchAntigravityQuota } from '@/utils/quota/providerRequests';
import { buildAccountRows, type AccountQuotaStores } from '@/features/accounts/model/accountRows';
import { buildAccountQuotaDisplayWindows } from '@/features/accounts/model/accountQuotaDisplayWindows';
import { buildAccountQuotaWindowDefinitions } from '@/features/accounts/model/accountQuotaWindowDefinitions';
import { buildAccountQuotaSnapshotWriteEntries } from '@/features/accounts/model/accountQuotaSnapshots';
import { buildQuotaCredentialIdentity, getQuotaCredentialStoreKey } from '@/utils/quota/credentialScope';
import { isDemoMode, setDemoMode } from '@/features/demo/demoMode';
import fixture from './antigravity-behavior-fixtures.json';

type WireRequest = { authIndex: string; method: string; url: string; header: Record<string, string>; data: string };
const t = ((key: string) => key) as TFunction;
const t0 = Math.floor(Date.now() / 1000) * 1000;
let server: Server;
let base: string;
let scenario: string;
let requests: WireRequest[];
let invalidPaths: string[];

function hubError(header: Record<string, string>): string | null {
  const values = Object.entries(header).filter(([key]) => key.toLowerCase() === 'user-agent').map(([, value]) => value);
  if (!values.length) return 'ua_missing';
  if (new Set(values).size !== 1) return 'ua_conflicting';
  const match = /^antigravity\/hub\/(\d+)\.(\d+)\.(\d+) [a-z0-9_-]+\/[a-z0-9_-]+$/i.exec(values[0]);
  if (!match) return 'ua_invalid';
  const parts = match.slice(1, 4).map(Number);
  for (let i = 0; i < 3; i++) {
    if (parts[i] < fixture.minimumHubVersion[i]) return 'ua_below_minimum';
    if (parts[i] > fixture.minimumHubVersion[i]) return null;
  }
  return null;
}

function summary(authIndex: string, partial = false, missingWindow = false) {
  return { groups: fixture.groups.filter((group) => !partial || group.scope !== 'gemini').map((group) => ({
    displayName: group.displayName,
    buckets: group.buckets.map((bucket) => ({
      bucketId: bucket.bucketId, displayName: bucket.displayName,
      ...(!missingWindow ? { window: bucket.window } : {}),
      remainingFraction: bucket.remainingFraction - (authIndex === 'ag-fixture-b' ? 0.1 : 0),
      resetTime: new Date(t0 + bucket.resetOffsetSeconds * 1000).toISOString(),
    })),
  })) };
}

beforeEach(async () => {
  vi.stubEnv('NO_PROXY', '127.0.0.1,localhost');
  vi.stubEnv('no_proxy', '127.0.0.1,localhost');
  setDemoMode(false);
  scenario = 'full'; requests = []; invalidPaths = [];
  server = createServer(async (req, res) => {
    let text = '';
    for await (const chunk of req) text += chunk;
    res.setHeader('Content-Type', 'application/json');
    res.setHeader('Date', new Date(t0).toUTCString());
    if (req.url !== '/v0/management/api-call' || req.method !== 'POST' || req.headers.authorization !== 'Bearer fixture-management') {
      invalidPaths.push(req.url ?? ''); res.writeHead(400); res.end('{}'); return;
    }
    const wire: WireRequest = JSON.parse(text);
    requests.push(wire);
    const subscription = wire.url.endsWith(':loadCodeAssist');
    const models = wire.url.endsWith(':fetchAvailableModels');
    let status = 200;
    let body: unknown;
    const effective = { ...wire.header };
    if (scenario.startsWith('ua:')) {
      delete effective['User-Agent'];
      if (scenario !== 'ua:missing') effective['User-Agent'] = scenario.slice(3);
    }
    if (subscription) {
      status = scenario === 'subscription429' ? 429 : scenario === 'subscription403' ? 403 : 200;
      body = fixture.subscription;
    } else if (scenario === 'quota429') { status = 429; body = { error: 'rate limit' }; }
    else if (scenario === 'empty') body = { groups: [] };
    else if (scenario === 'unknown') body = {};
    else if (scenario === 'models' && !models) { status = 404; body = {}; }
    else if (scenario === 'fallback' && !models && requests.filter((item) => item.url.endsWith(':retrieveUserQuotaSummary')).length === 1) { status = 404; body = {}; }
    else if (models) body = { models: fixture.models };
    else body = summary(wire.authIndex, Boolean(hubError(effective)) || scenario === 'partial', scenario === 'missingWindow');
    res.end(JSON.stringify({ status_code: status, header: { Date: new Date(t0).toUTCString() }, body: JSON.stringify(body) }));
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterEach(async () => {
  await new Promise<void>((resolve, reject) => { server.close((error) => error ? reject(error) : resolve()); server.closeAllConnections(); });
  vi.unstubAllEnvs();
  expect(invalidPaths).toEqual([]);
});

async function run(file: AuthFileItem = fixture.accounts[0]) {
  const result = await fetchAntigravityQuota(file, t, { apiBase: base, managementKey: 'fixture-management' });
  const stores: AccountQuotaStores = { antigravityQuota: {}, codexQuota: {}, claudeQuota: {}, devinQuota: {}, kimiQuota: {}, metaQuota: {}, xaiQuota: {} };
  stores.antigravityQuota[getQuotaCredentialStoreKey(file)] = { ...result, status: 'success', fetchedAtMs: t0, ...buildQuotaCredentialIdentity(file) };
  const [row] = buildAccountRows([file], stores);
  const windows = buildAccountQuotaDisplayWindows(row, { stores, t, translateQuotaWindowLabel: (label) => label ?? '', nowMs: t0 });
  const definitions = buildAccountQuotaWindowDefinitions(windows, t0);
  const entries = buildAccountQuotaSnapshotWriteEntries([row], new Map([[row.selectionKey, definitions]]), { nowMs: t0 });
  return { result, windows, definitions, entries };
}

function requireComplete(value: Awaited<ReturnType<typeof run>>, accountOffset = 0) {
  for (const group of fixture.groups) for (const bucket of group.buckets) {
    const definition = value.definitions.find((item) => item.providerWindowId.endsWith(`:${bucket.bucketId}`));
    if (!definition) throw new Error(group.scope === 'gemini' ? 'missing_gemini_window' : 'missing_claude_window');
    if (definition.durationSeconds !== bucket.durationSeconds) throw new Error('missing_window_duration');
    expect(definition.kind).toBe(bucket.window === '5h' ? 'five_hour' : 'weekly');
    expect(definition.modelScope).toEqual({ kind: 'family', key: group.scope, complete: true });
    expect(definition.remainingPercent).toBeCloseTo((bucket.remainingFraction - accountOffset) * 100);
    expect(definition.cycleEndMs).toBe(t0 + bucket.resetOffsetSeconds * 1000);
    const persisted = value.entries.flatMap((entry) => entry.windows).find((item) => item.provider_window_id === definition.providerWindowId);
    expect(persisted).toMatchObject({ window_kind: definition.kind, duration_seconds: bucket.durationSeconds, model_scope_key: group.scope, cycle_end_ms: definition.cycleEndMs });
  }
  expect(value.definitions).toHaveLength(4);
}

describe('Antigravity real HTTP behavior (non-demo)', () => {
  it('A01 executes quota/subscription, serialization, display and snapshot code', async () => {
    expect(__DEMO_SITE__).toBe(false); expect(isDemoMode()).toBe(false);
    requireComplete(await run());
    expect(requests).toHaveLength(2);
    for (const wire of requests) {
      expect(hubError(wire.header)).toBeNull();
      expect(wire).toMatchObject({ authIndex: 'ag-fixture-a', method: 'POST', header: { Authorization: 'Bearer $TOKEN$' } });
      expect(wire.url).toMatch(/^https:\/\/(daily-cloudcode-pa|cloudcode-pa)\.(sandbox\.)?googleapis\.com\/v1internal:/);
      if (wire.url.endsWith(':retrieveUserQuotaSummary')) expect(JSON.parse(wire.data)).toEqual({ project: 'fixture-project-a' });
    }
  });
  it.each(['antigravity/cli/1.0.13', 'antigravity/hub/2.22.0 darwin/arm64', 'missing', 'malformed'])('A02/A03 kills wrong wire UA %s with missing Gemini', async (ua) => {
    scenario = `ua:${ua}`;
    const value = await run();
    expect(requests.length).toBeGreaterThan(0);
    expect(value.result.groups.some((group) => /claude/i.test(group.label))).toBe(true);
    expect(() => requireComplete(value)).toThrow('missing_gemini_window');
  });
  it('A04 detects a partial summary even with a valid UA', async () => {
    scenario = 'partial'; const value = await run();
    expect(requests.every((wire) => !hubError(wire.header))).toBe(true);
    expect(() => requireComplete(value)).toThrow('missing_gemini_window');
    expect(requests.some((wire) => wire.url.endsWith(':fetchAvailableModels'))).toBe(false);
  });
  it('A05 retries summary endpoints while retaining the UA', async () => {
    scenario = 'fallback'; requireComplete(await run());
    const summaries = requests.filter((wire) => wire.url.endsWith(':retrieveUserQuotaSummary'));
    expect(summaries).toHaveLength(2);
    expect(summaries[0].url).toContain('daily-cloudcode-pa.googleapis.com');
    expect(summaries[1].url).toContain('daily-cloudcode-pa.sandbox.googleapis.com');
    requests.forEach((wire) => expect(hubError(wire.header)).toBeNull());
  });
  it('A06 preserves models-only observations without inventing windows', async () => {
    scenario = 'models'; const value = await run();
    expect(value.definitions).toHaveLength(2);
    expect(value.definitions.every((item) => item.kind === 'unknown' && item.durationSeconds === null)).toBe(true);
    expect(value.entries[0].windows.every((item) => item.window_kind === 'unknown')).toBe(true);
    expect(requests.filter((wire) => wire.url.endsWith(':retrieveUserQuotaSummary'))).toHaveLength(3);
    requests.forEach((wire) => expect(hubError(wire.header)).toBeNull());
  });
  it('A07 detects lost window duration even when labels infer the kind', async () => {
    scenario = 'missingWindow';
    const value = await run(); expect(() => requireComplete(value)).toThrow('missing_window_duration');
  });
  it('A08 stops quota retries on 429 and produces no successful result', async () => {
    scenario = 'quota429'; await expect(run()).rejects.toMatchObject({ status: 429 });
    expect(requests.filter((wire) => !wire.url.endsWith(':loadCodeAssist'))).toHaveLength(1);
  });
  it.each(['subscription429', 'subscription403'])('A09 retains quota after %s', async (mode) => {
    scenario = mode; const value = await run(); requireComplete(value);
    expect(value.result.subscription).toBeNull();
    expect(Boolean(value.result.rateLimited)).toBe(mode === 'subscription429');
  });
  it.each(['empty', 'unknown'])('A10 retains inventory semantics for %s', async (mode) => {
    scenario = mode; const value = await run();
    expect(value.result.groups).toEqual([]);
    expect(value.result.quotaInventoryObserved).toBe(mode === 'empty');
    expect(value.definitions).toEqual([]);
  });
  it('A12 isolates different auth indices with the same filename', async () => {
    const a = await run();
    const b = await run({ ...fixture.accounts[1], name: fixture.accounts[0].name });
    requireComplete(a); requireComplete(b, 0.1);
    expect(a.entries[0].account.auth_index).toBe('ag-fixture-a');
    expect(b.entries[0].account.auth_index).toBe('ag-fixture-b');
    expect(a.entries[0].row_key).not.toBe(b.entries[0].row_key);
  });
  it.each([
    ['antigravity/hub/2.9.0 darwin/arm64', 'ua_below_minimum'],
    ['antigravity/hub/2.100.0 darwin/arm64', null],
    ['antigravity/hub/2.23.0 darwin/arm64', null],
  ])('independent policy compares version numerically: %s', (ua, error) => expect(hubError({ 'user-agent': ua })).toBe(error));
  it('rejects conflicting case variants of User-Agent', () => expect(hubError({ 'User-Agent': 'a', 'user-agent': 'b' })).toBe('ua_conflicting'));
});
