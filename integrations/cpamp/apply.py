"""Apply the native CommandCode quota card to a CPAMP source checkout.

python integrations/cpamp/apply.py /path/to/CPA-Manager-Plus [--check]
All insertion anchors are validated before any source is changed.
"""
import argparse
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
        ('params: Object.keys(params).length ? params : undefined,\n    });', '''params: Object.keys(params).length ? params : undefined,
    }).then(response => {
      if ((provider === 'commandcode' || provider === 'commandcodebridge') && response.url.startsWith('/') && requestScope?.apiBase) {
        const base = normalizeApiBase(requestScope.apiBase);
        return { ...response, url: base + response.url };
      }
      return response;
    });'''),
    ],
}
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
print('PASS: CPAMP native credential grid and quota detail integration' + (' compatibility check' if args.check else ' applied'))
