"""Apply the native CommandCode quota card to a CPAMP source checkout.

python integrations/cpamp/apply.py /path/to/CPA-Manager-Plus [--check]
All insertion anchors are validated before any source is changed.
"""
import argparse
import json
import pathlib
import re
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
        ("onClick={() => window.open(state.url, '_blank', 'noopener,noreferrer')}", "onClick={() => window.open(isCommandCodeProvider(provider.id) ? 'http://127.0.0.1:45879' : state.url, '_blank', 'noopener,noreferrer')}"),
        ("{getProviderActionText(provider.id, 'open_link')}", "{isCommandCodeProvider(provider.id) ? t('auth_login.commandcode_open_receiver') : getProviderActionText(provider.id, 'open_link')}"),
        ('await oauthApi.submitCallback(provider, redirectUrl, callbackAttempt.requestScope);', '''if (isCommandCodeProvider(provider)) {
        await oauthApi.submitCallback(provider, redirectUrl, callbackAttempt.requestScope, state);
      } else {
        await oauthApi.submitCallback(provider, redirectUrl, callbackAttempt.requestScope);
      }'''),
        ("callbackStatus: 'success',\n        callbackError: undefined,", "callbackStatus: 'success',\n        callbackUrl: isCommandCodeProvider(provider) ? '' : providerState?.callbackUrl,\n        callbackError: undefined,"),
        ("<Input\n                        label={t(", "<Input\n                        type={isCommandCodeProvider(provider.id) ? 'password' : 'text'}\n                        autoComplete={isCommandCodeProvider(provider.id) ? 'off' : undefined}\n                        label={t("),
        ("label={t(\n                          provider.id === 'devin'", "label={t(\n                          isCommandCodeProvider(provider.id)\n                            ? 'auth_login.commandcode_callback_label'\n                            : provider.id === 'devin'"),
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
    'zh-CN': ('CommandCodeBridge v0.2.4：官网以 POST 正文回传凭证。先在浏览器所在电脑运行安装包内 commandcodebridge-login，将本卡片的官方授权链接粘贴到接收器。授权后复制接收器页面的认证结果，在下方提交。裸 localhost 地址不包含凭证。', '粘贴本地接收器复制的认证结果', '认证结果'),
    'zh-TW': ('CommandCodeBridge v0.2.4：官網以 POST 本文回傳憑證。先在瀏覽器所在電腦執行安裝包內 commandcodebridge-login，將本卡片的官方授權連結貼到接收器。授權後複製接收器頁面的認證結果，在下方提交。裸 localhost 網址不包含憑證。', '貼上本地接收器複製的認證結果', '認證結果'),
    'en': ('CommandCodeBridge v0.2.4: Studio sends credentials in a POST body. Run commandcodebridge-login on your browser computer, paste this card’s official authorization link into it, then copy the receiver’s result and submit it below. A bare localhost URL has no credentials.', 'Paste the result copied from the local receiver', 'Authentication result'),
}
receiver_labels = {'zh-CN': '打开本地接收器', 'zh-TW': '開啟本地接收器', 'en': 'Open local receiver'}
for locale, (hint, placeholder, label) in translations.items():
    changes[root / f'apps/web/src/i18n/locales/{locale}.json'] = [
        ('"plugin_oauth_hint":', '"commandcode_oauth_hint": ' + json.dumps(hint, ensure_ascii=False) + ',\n    "commandcode_callback_placeholder": ' + json.dumps(placeholder, ensure_ascii=False) + ',\n    "commandcode_callback_label": ' + json.dumps(label, ensure_ascii=False) + ',\n    "commandcode_open_receiver": ' + json.dumps(receiver_labels[locale], ensure_ascii=False) + ',\n    "plugin_oauth_hint":'),
    ]
prepared = {}
for path, replacements in changes.items():
    text = path.read_text(encoding='utf-8').replace('\r\n', '\n')
    if path.parent.name == 'locales':
        text = re.sub(r'^    "commandcode_(?:oauth_hint|callback_placeholder|callback_label|open_receiver)":.*\n', '', text, flags=re.MULTILINE)
    if path == oauth_page_test:
        text = re.sub(r"^  it\('shows the native remote callback input for CommandCode and submits its captured state', async \(\) => \{\n.*?^  \}\);\n\n", '', text, flags=re.MULTILINE | re.DOTALL)
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
