import { beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('./client', () => ({ apiClient: { get: mocks.get, post: mocks.post }, createScopedApiRequestConfig: (scope: unknown) => ({ scope }) }));
import { oauthApi } from './oauth';
import { isCommandCodeProvider } from './commandCodeOAuth';

beforeEach(() => { mocks.get.mockReset(); mocks.post.mockReset(); });

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
  it('preserves the official localhost authorization URL on a remote CPA', async () => {
    const url = 'https://commandcode.ai/studio/auth/cli?callback=http%3A%2F%2Flocalhost%3A45879%2Fcallback&state=fixture&mode=redirect';
    mocks.get.mockResolvedValue({ url, state: 'fixture' });
    const result = await oauthApi.startAuth('commandcode', { apiBase: 'https://panel.example/cpa', managementKey: 'fixture-key' });
    expect(result.url).toBe(url);
  });
});

describe('CommandCode remote callback', () => {
  const scope = { apiBase: 'https://panel.example/cpa/v0/management', managementKey: 'fixture-key' };
  const redirect = 'http://localhost:45879/callback?apiKey=fixture-key&keyName=remote&state=fixture';
  it.each(['commandcode', 'commandcodebridge'])('submits %s through an authenticated plugin POST pinned to the CPA and current state', async provider => {
    mocks.post.mockResolvedValue({ status: 'ok' });
    await oauthApi.submitCallback(provider, redirect, scope, 'fixture');
    expect(mocks.post).toHaveBeenCalledWith('/commandcodebridge/auth/callback', { state: 'fixture', redirect_url: redirect }, { scope });
    expect(isCommandCodeProvider(provider)).toBe(true);
  });
  it('rejects a callback for an earlier login without contacting CPA', () => {
    expect(() => oauthApi.submitCallback('commandcode', redirect, scope, 'other')).toThrow('different login');
    expect(mocks.post).not.toHaveBeenCalled();
  });
  it.each(['https://remote.example/callback?apiKey=fixture-key&state=fixture', redirect + '&state=other', redirect + '#secret', 'fixture-key'])('rejects invalid callbacks without leaking their URL in errors', bad => {
    expect(() => oauthApi.submitCallback('commandcode', bad, scope, 'fixture')).toThrow();
    expect(mocks.post).not.toHaveBeenCalled();
  });
  it('preserves the built-in Codex callback contract', async () => {
    await oauthApi.submitCallback('codex', 'http://localhost:1455/auth/callback?code=fixture&state=codex', scope);
    expect(mocks.post).toHaveBeenCalledWith('/oauth-callback', { provider: 'codex', redirect_url: 'http://localhost:1455/auth/callback?code=fixture&state=codex' }, { scope });
  });
});
