"""Apply the native CommandCode quota card to a CPAMP source checkout.

python integrations/cpamp/apply.py /path/to/CPA-Manager-Plus [--check]
All insertion anchors are validated before any source is changed.
"""
import argparse
import json
import pathlib
import shutil

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('checkout', type=pathlib.Path)
parser.add_argument('--check', action='store_true', help='Verify compatibility without changing files')
args = parser.parse_args()
root = args.checkout.resolve()
source = pathlib.Path(__file__).resolve().parent
accounts = root / 'apps/web/src/features/accounts/AccountsPage.tsx'
tab = root / 'apps/web/src/features/accounts/components/accountDetail/AccountQuotaTab.tsx'
oauth = root / 'apps/web/src/services/api/oauth.ts'
oauth_page = root / 'apps/web/src/features/oauth/OAuthPage.tsx'
oauth_page_test = root / 'apps/web/src/features/oauth/OAuthPage.test.tsx'
changes = {
    accounts: [
        ("import { Button } from '@/components/ui/Button';", "import { Button } from '@/components/ui/Button';\nimport { CommandCodeQuotaCard } from './components/CommandCodeQuotaCard';"),
        ('{ctx.mainListWindows.length > 0 ? (\n                      <div className={styles.accountGridCardQuotaList}>', "{row.provider === 'commandcode' ? (\n                      <CommandCodeQuotaCard file={row.raw} compact />\n                    ) : ctx.mainListWindows.length > 0 ? (\n                      <div className={styles.accountGridCardQuotaList}>"),
        ('className={styles.accountGridCardQuota}\n                    title={ctx.quotaWindowTitle}\n                    role={isSelectionMode ? undefined : \'button\'}\n                    tabIndex={isSelectionMode ? undefined : 0}', "className={styles.accountGridCardQuota}\n                    title={row.provider === 'commandcode' ? undefined : ctx.quotaWindowTitle}\n                    role={isSelectionMode || row.provider === 'commandcode' ? undefined : 'button'}\n                    tabIndex={isSelectionMode || row.provider === 'commandcode' ? undefined : 0}"),
        ('const resetCreditsAriaSuffix =\n                      ctx.hasCodexResetCredits', "if (row.provider === 'commandcode') {\n                      return <div className={styles.accountCardBusiness}><CommandCodeQuotaCard file={row.raw} compact /></div>;\n                    }\n                    const resetCreditsAriaSuffix =\n                      ctx.hasCodexResetCredits"),
        ('detailView={detailView}\n            windowUsageError=', 'detailView={detailView}\n            commandCodeFile={selectedRow.raw}\n            windowUsageError='),
    ],
    tab: [
        ("import { Button } from '@/components/ui/Button';", "import { Button } from '@/components/ui/Button';\nimport type { AuthFileItem } from '@/types';\nimport { CommandCodeQuotaCard } from '../CommandCodeQuotaCard';"),
        ('interface AccountQuotaTabProps {', 'interface AccountQuotaTabProps {\n  commandCodeFile?: AuthFileItem;'),
        ('export function AccountQuotaTab({\n  detailView,', 'export function AccountQuotaTab({\n  detailView,\n  commandCodeFile,'),
        ('<div className={styles.quotaTab} data-account-quota-tab="true">', '<div className={styles.quotaTab} data-account-quota-tab="true">\n      {detailView.identity.provider === \'commandcode\' && commandCodeFile && <CommandCodeQuotaCard file={commandCodeFile} />}'),
    ],
    oauth: [
        ("import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';", "import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';\nimport { normalizeApiBase } from '@/utils/connection';"),
        ("import { normalizeApiBase } from '@/utils/connection';", "import { normalizeApiBase } from '@/utils/connection';\nimport { commandCodeCallbackBody, isCommandCodeProvider } from './commandCodeOAuth';"),
        ('params: Object.keys(params).length ? params : undefined,\n    });', '''params: Object.keys(params).length ? params : undefined,
    }).then(response => {
      if ((provider === 'commandcode' || provider === 'commandcodebridge') && response.url.startsWith('/') && requestScope?.apiBase) {
        const base = normalizeApiBase(requestScope.apiBase);
        return { ...response, url: base + response.url };
      }
      return response;
    });'''),
        ('''    redirectUrl: string,
    requestScope?: ApiClientRequestScope
  ) => {
    return apiClient.post<OAuthCallbackResponse>(''', '''    redirectUrl: string,
    requestScope?: ApiClientRequestScope,
    state?: string
  ) => {
    if (isCommandCodeProvider(provider)) {
      return apiClient.post<OAuthCallbackResponse>(
        '/commandcodebridge/auth/callback',
        commandCodeCallbackBody(provider, redirectUrl, state),
        requestScope ? createScopedApiRequestConfig(requestScope) : undefined
      );
    }
    return apiClient.post<OAuthCallbackResponse>('''),
    ],
    oauth_page: [
        ("import { validateDevinCallback } from './devinOAuth';", "import { validateDevinCallback } from './devinOAuth';\nimport { isCommandCodeProvider } from '@/services/api/commandCodeOAuth';"),
        ("hint: t('auth_login.plugin_oauth_hint', { plugin: title }),", "hint: t(isCommandCodeProvider(resolvePluginOAuthProviderId(plugin)) ? 'auth_login.commandcode_oauth_hint' : 'auth_login.plugin_oauth_hint', { plugin: title }),"),
        ('supportsCallback: false,\n              isPlugin: true,', 'supportsCallback: isCommandCodeProvider(resolvePluginOAuthProviderId(plugin)),\n              isPlugin: true,'),
        ('await oauthApi.submitCallback(provider, redirectUrl, callbackAttempt.requestScope);', '''if (isCommandCodeProvider(provider)) {
        await oauthApi.submitCallback(provider, redirectUrl, callbackAttempt.requestScope, state);
      } else {
        await oauthApi.submitCallback(provider, redirectUrl, callbackAttempt.requestScope);
      }'''),
        ("callbackStatus: 'success',\n        callbackError: undefined,", "callbackStatus: 'success',\n        callbackUrl: isCommandCodeProvider(provider) ? '' : providerState?.callbackUrl,\n        callbackError: undefined,"),
        ("<Input\n                        label={t(", "<Input\n                        type={isCommandCodeProvider(provider.id) ? 'password' : 'text'}\n                        autoComplete={isCommandCodeProvider(provider.id) ? 'off' : undefined}\n                        label={t("),
        ("placeholder={t(\n                          provider.id === 'devin'", "placeholder={t(\n                          isCommandCodeProvider(provider.id)\n                            ? 'auth_login.commandcode_callback_placeholder'\n                            : provider.id === 'devin'"),
    ],
    oauth_page_test: [
        ("apiBase: 'http://cpa-a.local:8317',", "apiBase: 'http://cpa-a.local:8317',\n      supportsPlugin: false,\n      pluginsList: vi.fn(),"),
        ('supportsPlugin: false,\n  });', 'supportsPlugin: pageMocks.supportsPlugin,\n  });'),
        ('list: vi.fn(async () => ({ plugins: [] })),', 'list: pageMocks.pluginsList,'),
        ("pageMocks.apiBase = 'http://cpa-a.local:8317';", "pageMocks.apiBase = 'http://cpa-a.local:8317';\n    pageMocks.supportsPlugin = false;\n    pageMocks.pluginsList.mockReset();\n    pageMocks.pluginsList.mockResolvedValue({ plugins: [] });"),
        ("  it('discards a late start response and clears loading after the connection changes',", (source / 'CommandCodeOAuthPage.test.fragment').read_text(encoding='utf-8') + "  it('discards a late start response and clears loading after the connection changes',"),
    ],
}
# Keep the remote handoff instructions alongside the native providers' text.
translations = {
    'zh-CN': ('打开官方授权页面。完成后会跳转到 localhost；若页面无法连接，这是远程登录的正常步骤。复制地址栏的完整回调 URL，粘贴到下方并提交。', 'http://localhost:45879/callback?apiKey=…&state=…'),
    'zh-TW': ('開啟官方授權頁面。完成後會跳轉到 localhost；若頁面無法連線，這是遠端登入的正常步驟。複製網址列的完整回呼 URL，貼到下方並提交。', 'http://localhost:45879/callback?apiKey=…&state=…'),
    'en': ('Open the official authorization page. After login, a localhost connection error is expected in remote mode. Copy the full callback URL from the address bar, paste it below, and submit.', 'http://localhost:45879/callback?apiKey=…&state=…'),
}
for locale, (hint, placeholder) in translations.items():
    changes[root / f'apps/web/src/i18n/locales/{locale}.json'] = [
        ('"plugin_oauth_hint":', '"commandcode_oauth_hint": ' + json.dumps(hint, ensure_ascii=False) + ',\n    "commandcode_callback_placeholder": ' + json.dumps(placeholder, ensure_ascii=False) + ',\n    "plugin_oauth_hint":'),
    ]
prepared = {}
for path, replacements in changes.items():
    text = path.read_text(encoding='utf-8').replace('\r\n', '\n')
    # Migrate the earlier local draft, which nested the refresh button inside
    # CPAMP's table detail button. Keep CommandCode outside that trigger.
    if path == accounts:
        text = text.replace("{row.provider === 'commandcode' ? (\n                          <CommandCodeQuotaCard file={row.raw} compact />\n                        ) : ctx.mainListWindows.length > 0 ? (\n                          ctx.mainListWindows.map", '{ctx.mainListWindows.length > 0 ? (\n                          ctx.mainListWindows.map')
    for old, new in replacements:
        if new in text:
            continue
        if text.count(old) != 1:
            raise SystemExit(f'Unsupported CPAMP source: insertion anchor missing or ambiguous in {path.name}')
        text = text.replace(old, new, 1)
    prepared[path] = text
if not args.check:
    for path, text in prepared.items():
        path.write_text(text, encoding='utf-8', newline='\n')
    target = root / 'apps/web/src/features/accounts/components'
    for name in ['CommandCodeQuotaCard.tsx', 'CommandCodeQuotaCard.module.scss', 'CommandCodeQuotaCard.test.tsx']:
        shutil.copy2(source / name, target / name)
    shutil.copy2(source / 'CommandCodeOAuth.test.ts', root / 'apps/web/src/services/api/CommandCodeOAuth.test.ts')
    shutil.copy2(source / 'commandCodeOAuth.ts', root / 'apps/web/src/services/api/commandCodeOAuth.ts')
print('PASS: CPAMP native credential grid and quota detail integration' + (' compatibility check' if args.check else ' applied'))
