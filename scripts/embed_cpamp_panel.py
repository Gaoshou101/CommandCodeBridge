"""Embed the matching verified CPAMP release page in the native plugin build.

python scripts/embed_cpamp_panel.py /path/to/commandcodebridge_VERSION_cpamp-web.zip
The generated compressed asset is ignored by Git and needed before Go build.
"""
import argparse
import gzip
import hashlib
import json
import pathlib
import re
import zipfile

root = pathlib.Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('archive', type=pathlib.Path)
args = parser.parse_args()
version = re.search(r'const Version = "([^"]+)"', (root / 'internal/bridge/types.go').read_text()).group(1)
with zipfile.ZipFile(args.archive) as archive:
    manifest = json.loads(archive.read('BUILD.json'))
    page = archive.read('management.html')
    cpamp_license = archive.read('LICENSE-CPAMP')
    if manifest.get('artifact') != 'cpamp-web' or manifest.get('version') != version:
        parser.error('frontend version must match the native plugin source')
    if hashlib.sha256(page).hexdigest() != manifest['management_html_sha256']:
        parser.error('frontend digest mismatch')
    if len(page) > 16 * 1024 * 1024 or b'data-commandcode-native-quota' not in page or ('CommandCodeBridge v' + version).encode() not in page:
        parser.error('frontend is missing the current native integration')
target = root / 'internal/bridge/panel-assets/management.html.gz'
target.write_bytes(gzip.compress(page, mtime=0))
(target.parent / 'LICENSE-CPAMP').write_bytes(cpamp_license)
print('PASS: embedded verified CPAMP native panel', version, hashlib.sha256(page).hexdigest())
