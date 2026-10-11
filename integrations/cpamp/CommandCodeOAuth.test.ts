import { describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock('./client', () => ({ apiClient: { get: mocks.get }, createScopedApiRequestConfig: () => ({}) }));
import { oauthApi } from './oauth';

describe('CommandCode login URLs', () => {
  it('resolves a native relative resource against the selected CPA and proxy prefix', async () => {
    mocks.get.mockResolvedValue({ url: '/v0/resource/plugins/commandcodebridge/login?state=fixture', state: 'fixture' });
    const result = await oauthApi.startAuth('commandcode', { apiBase: 'https://panel.example/cpa/v0/management', managementKey: 'fixture-key' });
    expect(result.url).toBe('https://panel.example/cpa/v0/resource/plugins/commandcodebridge/login?state=fixture');
  });
  it('preserves an explicit public URL returned by the plugin', async () => {
    mocks.get.mockResolvedValue({ url: 'https://public.example/proxy/v0/resource/plugins/commandcodebridge/login?state=fixture' });
    const result = await oauthApi.startAuth('commandcode', { apiBase: 'https://other.example', managementKey: 'fixture-key' });
    expect(result.url).toContain('https://public.example/proxy/');
  });
});
