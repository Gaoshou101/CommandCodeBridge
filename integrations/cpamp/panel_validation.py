"""Shared CPAMP identity and provenance gates for source and release tools."""
import json
import pathlib
import re
import subprocess

MAX_PANEL_BYTES = 16 * 1024 * 1024


def load_compatibility(root):
    """Use the source baseline, or the same baseline carried in a release ZIP."""
    path = pathlib.Path(root) / 'internal/bridge/compatibility.json'
    if path.is_file():
        baseline = json.loads(path.read_text(encoding='utf-8'))
    else:
        baseline = json.loads((pathlib.Path(root) / 'BUILD.json').read_text(encoding='utf-8'))['compatibility']
    if baseline.get('schema') != 1 or not re.fullmatch(r'[0-9a-f]{40}', baseline.get('cpamp_source_commit', '')):
        raise ValueError('invalid CPAMP source baseline in compatibility.json')
    return baseline


def validate_antigravity_identity(page):
    # The built UA may be a template literal. Never require a rendered version.
    if b'antigravity/hub/' not in page or b'antigravity/cli/' in page:
        raise ValueError('page must send antigravity/hub/ and must not contain antigravity/cli/; '
                         'rebuild from the compatibility.json CPAMP baseline with upstream #986 '
                         'to preserve Gemini quota windows')


def validate_panel(page, version=None):
    if len(page) > MAX_PANEL_BYTES:
        raise ValueError('CPAMP panel exceeds the size limit')
    validate_antigravity_identity(page)
    for marker in (b'data-commandcode-native-quota', b'data-commandcode-native-windows',
                   b'commandcodebridge-capabilities', b'recent-requests',
                   b'/commandcodebridge/auth/callback', b'commandcode_callback_placeholder'):
        if marker not in page:
            raise ValueError('page is missing the native integration marker: ' + marker.decode())
    if version is not None and ('CommandCodeBridge v' + version).encode() not in page:
        raise ValueError('CPAMP panel version differs from the native plugin source')


def validate_panel_provenance(manifest, baseline):
    if manifest.get('compatibility') != baseline:
        raise ValueError('frontend compatibility baseline differs from native source')
    if manifest.get('cpamp_source_commit') != baseline['cpamp_source_commit']:
        raise ValueError('frontend source commit differs from the compatibility.json CPAMP baseline')


def validate_checkout(checkout, baseline, exact=False):
    """Compare Git identities/ancestry, never the lexical ordering of SHA strings."""
    def git(*arguments):
        return subprocess.run(['git', '-C', str(checkout), *arguments],
                              capture_output=True, text=True, check=True).stdout.strip()

    if git('rev-parse', '--show-prefix'):
        raise ValueError('CPAMP checkout must be its own Git repository')
    commit = git('rev-parse', 'HEAD')
    pinned = baseline['cpamp_source_commit']
    if exact:
        if commit != pinned:
            raise ValueError(f'CPAMP checkout must match compatibility.json baseline {pinned}; got {commit}')
    elif subprocess.run(['git', '-C', str(checkout), 'merge-base', '--is-ancestor', pinned, commit],
                        capture_output=True).returncode != 0:
        raise ValueError(f'CPAMP checkout must contain compatibility.json baseline {pinned}')
    return commit
