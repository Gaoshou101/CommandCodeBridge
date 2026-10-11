import type { OAuthProvider } from './oauth';

export const isCommandCodeProvider = (provider: string): boolean =>
  provider === 'commandcode' || provider === 'commandcodebridge';

export const commandCodeCallbackBody = (
  provider: OAuthProvider,
  redirectUrl: string,
  state?: string
): { state: string; redirect_url: string } => {
  if (!isCommandCodeProvider(provider) || !state) {
    throw new Error('Start CommandCode login again before submitting its callback URL.');
  }
  let url: URL;
  try {
    url = new URL(redirectUrl.trim());
  } catch {
    throw new Error('Paste the full CommandCode localhost callback URL.');
  }
  if (url.protocol !== 'http:' || !['localhost', '127.0.0.1'].includes(url.hostname) ||
      url.port !== '45879' || url.pathname !== '/callback' || url.username || url.password || url.hash) {
    throw new Error('Paste the full CommandCode localhost callback URL.');
  }
  if (url.searchParams.getAll('state').length !== 1 || url.searchParams.get('state') !== state) {
    throw new Error('The callback belongs to a different login. Start again and copy its new callback URL.');
  }
  return { state, redirect_url: redirectUrl.trim() };
};
