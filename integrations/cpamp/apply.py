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
model = root / 'apps/web/src/features/accounts/model'
constants = root / 'apps/web/src/features/authFiles/constants.ts'
changes = {
    accounts: [
        ("import { Button } from '@/components/ui/Button';", "import { Button } from '@/components/ui/Button';\nimport { CommandCodeQuotaCard } from './components/CommandCodeQuotaCard';"),
        ("import { CommandCodeQuotaCard } from './components/CommandCodeQuotaCard';", "import { CommandCodeQuotaCard } from './components/CommandCodeQuotaCard';\nimport { getCommandCodeQuota, refreshCommandCodeQuota, useCommandCodeQuotaRevision } from './components/commandCodeQuotaStore';\nimport { applyCommandCodeQuotaToRow, buildCommandCodeQuotaWindows } from './model/commandCodeQuotaWindows';"),
        ("import { getCommandCodeQuota, refreshCommandCodeQuota, useCommandCodeQuotaRevision }", "import { getCommandCodeQuota, refreshCommandCodeQuota, useCommandCodeQuotaRevision, useCommandCodeCredentials }") ,
        ("export function AccountsPage() {", "export function AccountsPage() {\n  const commandCodeQuotaRevision = useCommandCodeQuotaRevision();"),
        ('credentialStatusBoundaries\n      ),', 'credentialStatusBoundaries\n      ).map(row => applyCommandCodeQuotaToRow(row, getCommandCodeQuota(row.raw, authFilesRequestScope))),'),
        ('      accountQuotaOverrides,\n      baseQuotaStores,\n      files,', '      accountQuotaOverrides,\n      commandCodeQuotaRevision,\n      authFilesRequestScope,\n      baseQuotaStores,\n      files,'),
        ('      switch (row.provider) {\n        case CODEX_CONFIG.type: {\n          const state = getActiveCodexQuota(row.raw);', '''      switch (row.provider) {
        case 'commandcode': {
          const state = getCommandCodeQuota(row.raw, authFilesRequestScope);
          if (state.data && !state.error && !state.pending) fetchedAtMs = state.observedAtMs;
          inventoryMode = 'partial';
          break;
        }
        case CODEX_CONFIG.type: {
          const state = getActiveCodexQuota(row.raw);'''),
        ('[baseQuotaStores, getActiveCodexQuota, getCredentialEvidenceBoundary]', '[baseQuotaStores, commandCodeQuotaRevision, authFilesRequestScope, getActiveCodexQuota, getCredentialEvidenceBoundary]'),
        ('(row: AccountRow): AccountQuotaDisplayWindow[] => {\n      return buildAccountQuotaDisplayWindows', "(row: AccountRow): AccountQuotaDisplayWindow[] => {\n      if (row.provider === 'commandcode') return buildCommandCodeQuotaWindows(getCommandCodeQuota(row.raw, authFilesRequestScope));\n      return buildAccountQuotaDisplayWindows"),
        ('[baseQuotaStores, getDisplayCodexQuota, t, translateQuotaWindowLabel]', '[baseQuotaStores, commandCodeQuotaRevision, authFilesRequestScope, getDisplayCodexQuota, t, translateQuotaWindowLabel]'),
        ('        quotaRevision: accountQuotaRefreshRevision,', '        quotaRevision: accountQuotaRefreshRevision,\n        commandCodeQuotaRevision,'),
        ('      accountQuotaRefreshRevision,\n      featureAvailability.checking,', '      accountQuotaRefreshRevision,\n      commandCodeQuotaRevision,\n      featureAvailability.checking,'),
        ("      switch (row.provider) {\n        case CODEX_CONFIG.type: {\n          // Any quota refresh", """      switch (row.provider) {
        case 'commandcode': {
          const state = await refreshCommandCodeQuota(row.raw, authFilesRequestScope);
          return state.error ? { status: 'error', error: state.error } : state.data ? { status: 'success' } : { status: 'ignored' };
        }
        case CODEX_CONFIG.type: {
          // Any quota refresh"""),
        ('{ctx.mainListWindows.length > 0 ? (\n                      <div className={styles.accountGridCardQuotaList}>', "{row.provider === 'commandcode' ? (\n                      <CommandCodeQuotaCard file={row.raw} compact />\n                    ) : ctx.mainListWindows.length > 0 ? (\n                      <div className={styles.accountGridCardQuotaList}>"),
        ('className={styles.accountGridCardQuota}\n                    title={ctx.quotaWindowTitle}\n                    role={isSelectionMode ? undefined : \'button\'}\n                    tabIndex={isSelectionMode ? undefined : 0}', "className={styles.accountGridCardQuota}\n                    title={row.provider === 'commandcode' ? undefined : ctx.quotaWindowTitle}\n                    role={isSelectionMode || row.provider === 'commandcode' ? undefined : 'button'}\n                    tabIndex={isSelectionMode || row.provider === 'commandcode' ? undefined : 0}"),
        ('const resetCreditsAriaSuffix =\n                      ctx.hasCodexResetCredits', "if (row.provider === 'commandcode') {\n                      return <div className={styles.accountCardBusiness}><CommandCodeQuotaCard file={row.raw} compact /></div>;\n                    }\n                    const resetCreditsAriaSuffix =\n                      ctx.hasCodexResetCredits"),
        ('detailView={detailView}\n            windowUsageError=', 'detailView={detailView}\n            commandCodeFile={selectedRow.raw}\n            windowUsageError='),
        ('  const rows = useMemo(\n', "  useCommandCodeCredentials(files, authFilesRequestScope, activeView === 'accounts' && connectionStatus === 'connected');\n\n  const rows = useMemo(\n"),
        ('    return evidenceMap;\n  }, [accountHistoryByRowKey]);', '''    files.forEach(file => {
      if ((file.provider ?? file.type) !== 'commandcode') return;
      const state = getCommandCodeQuota(file, authFilesRequestScope);
      if (state.recentRequests === undefined) return;
      evidenceMap.set(getAuthFileSelectionKey(file), {
        latestRequest: state.recentRequests[0] ?? null,
        recentRequests: state.recentRequests,
      });
    });
    return evidenceMap;
  }, [accountHistoryByRowKey, files, commandCodeQuotaRevision, authFilesRequestScope]);'''),
        ('latestRequest={ctx.accountHistory?.latest_request}\n                      recentRequests={ctx.accountHistory?.recent_requests}\n                      loading={accountHistoryLoading && !ctx.accountHistory}\n                      unavailable={Boolean(ctx.accountHistoryError)}', '''latestRequest={row.provider === 'commandcode' ? requestEvidenceBySelectionKey.get(row.selectionKey)?.latestRequest : ctx.accountHistory?.latest_request}
                      recentRequests={row.provider === 'commandcode' ? getCommandCodeQuota(row.raw, authFilesRequestScope).recentRequests ?? ctx.accountHistory?.recent_requests : ctx.accountHistory?.recent_requests}
                      loading={accountHistoryLoading && !ctx.accountHistory && !(row.provider === 'commandcode' && getCommandCodeQuota(row.raw, authFilesRequestScope).recentRequests)}
                      unavailable={Boolean(ctx.accountHistoryError) && !(row.provider === 'commandcode' && getCommandCodeQuota(row.raw, authFilesRequestScope).recentRequests)}'''),
    ],
    tab: [
        ("import { Button } from '@/components/ui/Button';", "import { Button } from '@/components/ui/Button';\nimport type { AuthFileItem } from '@/types';\nimport { CommandCodeQuotaCard } from '../CommandCodeQuotaCard';"),
        ('interface AccountQuotaTabProps {', 'interface AccountQuotaTabProps {\n  commandCodeFile?: AuthFileItem;'),
        ('export function AccountQuotaTab({\n  detailView,', 'export function AccountQuotaTab({\n  detailView,\n  commandCodeFile,'),
        ('<div className={styles.quotaTab} data-account-quota-tab="true">', '<div className={styles.quotaTab} data-account-quota-tab="true" data-commandcode-native-windows={detailView.identity.provider === \'commandcode\' ? \'true\' : undefined}>'),
        ('<div className={styles.quotaTabActions}>', '<div className={styles.quotaTabActions}>\n          {detailView.identity.provider === \'commandcode\' && commandCodeFile && <CommandCodeQuotaCard file={commandCodeFile} refreshOnly />}'),
    ],
    constants: [
        ("'xai' | 'devin' | 'meta';", "'xai' | 'devin' | 'meta' | 'commandcode';"),
        ("  'meta',\n]);", "  'meta',\n  'commandcode',\n]);"),
    ],
    model / 'accountQuotaDisplayWindows.ts': [
        ("  | 'summary';", "  | 'summary'\n  | 'commandcode';"),
    ],
    model / 'accountHealthEvidence.ts': [
        ("  if (statusCode === 401) return 'credential_failure';", "  // CPA reports local routing configuration errors as 401; reauthentication cannot fix them.\n  if (/\\bmissing provider baseurl\\b/.test(failureText)) return 'neutral';\n  if (statusCode === 401) return 'credential_failure';"),
    ],
    model / 'accountHealthEvidence.test.ts': [
        ("const makeRequest = (", """it('does not treat a local provider configuration error as an expired credential', () => {
  expect(classifyAccountRequestEvidence({ timestamp_ms: 2000, failed: true, fail_status_code: 401,
    fail_summary: 'missing provider baseURL' })).toBe('neutral');
  expect(classifyAccountRequestEvidence({ timestamp_ms: 2000, failed: true, fail_status_code: 401,
    fail_summary: 'invalid API key' })).toBe('credential_failure');
});

const makeRequest = ("""),
    ],
    model / 'accountQuotaWindowDefinitions.ts': [
        ("  ranges.push({\n    period: 'previous',\n    fromMs: definition.cycleStartMs - durationMs,", "  // A calendar billing period does not establish the preceding period's length.\n  // Use a recorded previous cycle when available instead of inventing its dates.\n  if (definition.provider === 'commandcode' && definition.windowMode === 'calendar') {\n    return ranges.filter(isValidUsageRange);\n  }\n  ranges.push({\n    period: 'previous',\n    fromMs: definition.cycleStartMs - durationMs,"),
    ],
    model / 'accountQuotaSnapshots.ts': [
        ("['codex', 'claude', 'antigravity', 'kimi', 'xai', 'devin', 'meta'].includes(row.provider)", "['codex', 'claude', 'antigravity', 'kimi', 'xai', 'devin', 'meta', 'commandcode'].includes(row.provider)"),
    ],
    model / 'accountQuotaWindowUsagePresentation.ts': [
        ('  const canForecastCurrentWindow =\n    !hasLifecycleEvidence || (currentForecastEligible && window.stale !== true);', "  const canForecastCurrentWindow =\n    !(row.provider === 'commandcode' && row.quota.status === 'error') &&\n    (!hasLifecycleEvidence || (currentForecastEligible && window.stale !== true));"),
    ],
    root / 'apps/manager-server/internal/service/quotasnapshot/service.go': [
        ('validProviders      = stringSet("codex", "claude", "antigravity", "kimi", "xai", "devin", "meta")', 'validProviders      = stringSet("codex", "claude", "antigravity", "kimi", "xai", "devin", "meta", "commandcode")'),
        ('func New(st *store.Store) *Service {', '''// SupportsCommandCode reports the actual allowlist without reading or writing snapshots.
func SupportsCommandCode() bool {
    return validProviders["commandcode"]
}

func New(st *store.Store) *Service {'''),
    ],
    root / 'apps/manager-server/internal/http/controller/health/handler.go': [
        ('"net/http"', '"net/http"\n\n    quotasnapshotsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/quotasnapshot"'),
        ('map[string]any{"ok": true, "service": h.ServiceID}', 'map[string]any{"ok": true, "service": h.ServiceID, "capabilities": map[string]bool{"commandcode_quota_snapshots_v1": quotasnapshotsvc.SupportsCommandCode()}}'),
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
integration_root = source.parents[1]
version_source = integration_root / 'internal/bridge/types.go'
if version_source.is_file():
    version = re.search(r'const Version = "([^"]+)"', version_source.read_text(encoding='utf-8')).group(1)
else:
    # Release archives include BUILD.json rather than the Go source tree.
    version = json.loads((integration_root / 'BUILD.json').read_text(encoding='utf-8'))['version']
translations = {
    'zh-CN': (f'CommandCodeBridge v{version}：官网以 POST 正文回传凭证。先在浏览器所在电脑运行安装包内 commandcodebridge-login，将本卡片的官方授权链接粘贴到接收器。授权后复制接收器页面的认证结果，在下方提交。裸 localhost 地址不包含凭证。', '粘贴本地接收器复制的认证结果', '认证结果'),
    'zh-TW': (f'CommandCodeBridge v{version}：官網以 POST 本文回傳憑證。先在瀏覽器所在電腦執行安裝包內 commandcodebridge-login，將本卡片的官方授權連結貼到接收器。授權後複製接收器頁面的認證結果，在下方提交。裸 localhost 網址不包含憑證。', '貼上本地接收器複製的認證結果', '認證結果'),
    'en': (f'CommandCodeBridge v{version}: Studio sends credentials in a POST body. Run commandcodebridge-login on your browser computer, paste this card’s official authorization link into it, then copy the receiver’s result and submit it below. A bare localhost URL has no credentials.', 'Paste the result copied from the local receiver', 'Authentication result'),
}
receiver_labels = {'zh-CN': '打开本地接收器', 'zh-TW': '開啟本地接收器', 'en': 'Open local receiver'}
page_index = root / 'apps/web/index.html'
changes[page_index] = [('</head>', '<meta name="commandcodebridge-capabilities" content="quota recent-requests quota-windows">\n</head>')]
for locale, (hint, placeholder, label) in translations.items():
    changes[root / f'apps/web/src/i18n/locales/{locale}.json'] = [
        ('"plugin_oauth_hint":', '"commandcode_oauth_hint": ' + json.dumps(hint, ensure_ascii=False) + ',\n    "commandcode_callback_placeholder": ' + json.dumps(placeholder, ensure_ascii=False) + ',\n    "commandcode_callback_label": ' + json.dumps(label, ensure_ascii=False) + ',\n    "commandcode_open_receiver": ' + json.dumps(receiver_labels[locale], ensure_ascii=False) + ',\n    "plugin_oauth_hint":'),
    ]
prepared = {}
for path, replacements in changes.items():
    if not path.is_file():
        raise SystemExit(f'Unsupported CPAMP source: required file missing: {path.relative_to(root)}. Verified baseline: dbf61ddf438f399558722df3467355c42af9005d. No prepared files have been written.')
    text = path.read_text(encoding='utf-8').replace('\r\n', '\n')
    if path.parent.name == 'locales':
        text = re.sub(r'^    "commandcode_(?:oauth_hint|callback_placeholder|callback_label|open_receiver)":.*\n', '', text, flags=re.MULTILINE)
    if path == oauth_page_test:
        text = re.sub(r"^  it\('shows the native remote callback input for CommandCode and submits its captured state', async \(\) => \{\n.*?^  \}\);\n\n", '', text, flags=re.MULTILINE | re.DOTALL)
    # Migrate the earlier local draft, which nested the refresh button inside
    # CPAMP's table detail button. Keep CommandCode outside that trigger.
    if path == accounts:
        # Normalize the previous integration import so this script stays
        # idempotent when updating an already patched source checkout.
        text = text.replace('useCommandCodeQuotaRevision, useCommandCodeCredentials }', 'useCommandCodeQuotaRevision }')
        text = text.replace("latestRequest={row.provider === 'commandcode' ? getCommandCodeQuota(row.raw, authFilesRequestScope).recentRequests?.[0] ?? ctx.accountHistory?.latest_request : ctx.accountHistory?.latest_request}", "latestRequest={row.provider === 'commandcode' ? requestEvidenceBySelectionKey.get(row.selectionKey)?.latestRequest : ctx.accountHistory?.latest_request}")
        text = text.replace("{row.provider === 'commandcode' ? (\n                          <CommandCodeQuotaCard file={row.raw} compact />\n                        ) : ctx.mainListWindows.length > 0 ? (\n                          ctx.mainListWindows.map", '{ctx.mainListWindows.length > 0 ? (\n                          ctx.mainListWindows.map')
    if path == tab:
        text = text.replace("\n      {detailView.identity.provider === 'commandcode' && commandCodeFile && <CommandCodeQuotaCard file={commandCodeFile} />}", '')
    for old, new in replacements:
        if new in text:
            continue
        if text.count(old) != 1:
            raise SystemExit(f'Unsupported CPAMP source: insertion anchor missing or ambiguous in {path.relative_to(root)}. Verified baseline: dbf61ddf438f399558722df3467355c42af9005d. Run --check against matching source before upgrading; no prepared files have been written.')
        text = text.replace(old, new, 1)
    prepared[path] = text
if not args.check:
    for path, text in prepared.items():
        path.write_text(text, encoding='utf-8', newline='\n')
    target = root / 'apps/web/src/features/accounts/components'
    for name in ['CommandCodeQuotaCard.tsx', 'CommandCodeQuotaCard.module.scss', 'CommandCodeQuotaCard.test.tsx', 'commandCodeQuotaStore.ts', 'commandCodeQuotaStore.test.ts', 'commandCodeQuotaPolicy.ts']:
        shutil.copy2(source / name, target / name)
    for name in ['commandCodeQuotaWindows.ts', 'commandCodeQuotaWindows.test.ts']:
        shutil.copy2(source / name, model / name)
    shutil.copy2(source / 'commandcode_quota_test.go.fragment', root / 'apps/manager-server/internal/service/quotasnapshot/commandcode_quota_test.go')
    shutil.copy2(source / 'commandcode_health_test.go.fragment', root / 'apps/manager-server/internal/http/controller/health/commandcode_health_test.go')
    shutil.copy2(source / 'CommandCodeOAuth.test.ts', root / 'apps/web/src/services/api/CommandCodeOAuth.test.ts')
    shutil.copy2(source / 'commandCodeOAuth.ts', root / 'apps/web/src/services/api/commandCodeOAuth.ts')
print('PASS: CPAMP native credential grid and quota detail integration' + (' compatibility check' if args.check else ' applied'))
