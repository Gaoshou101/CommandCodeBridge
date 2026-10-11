"""Read-only check that the public CPAMP URL actually serves the installed custom panel.

python scripts/verify_cpamp_panel.py https://your-panel/management.html --expected /absolute/path/management.html
No management credential is needed or sent.
"""
import argparse
import hashlib
import pathlib
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('url')
parser.add_argument('--expected', type=pathlib.Path, help='The custom management.html that should be served')
args = parser.parse_args()
request = urllib.request.Request(args.url, headers={'Cache-Control': 'no-cache', 'User-Agent': 'CommandCodeBridge-panel-verification'})
with urllib.request.urlopen(request, timeout=30) as response:
    page = response.read(16 * 1024 * 1024)
    served_url = response.url
markers = {'native_quota': b'data-commandcode-native-quota', 'native_callback': b'commandcode_callback_placeholder', 'post_receiver_instructions': b'CommandCodeBridge v0.2.4'}
print('URL:', served_url)
print('SHA256:', hashlib.sha256(page).hexdigest())
passed = True
for name, marker in markers.items():
    present = marker in page
    print(name + ':', 'PASS' if present else 'MISSING')
    passed = passed and present
if args.expected:
    matches = hashlib.sha256(page).digest() == hashlib.sha256(args.expected.read_bytes()).digest()
    print('expected_page:', 'MATCH' if matches else 'DIFFERENT')
    passed = passed and matches
if not passed:
    print('The served page is not the required custom panel. Check PANEL_PATH in the running Manager Server process, file readability, the proxy target and caches.')
    raise SystemExit(1)
print('PASS: the actual served page contains native CommandCode quota and callback support')
