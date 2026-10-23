"""Read-only check that the public CPAMP URL actually serves the installed custom panel.

python scripts/verify_cpamp_panel.py https://your-panel/management.html --expected /absolute/path/management.html
No management credential is needed or sent.
"""
import argparse
import hashlib
import pathlib
import sys
import urllib.request

root = pathlib.Path(__file__).resolve().parent
if not (root / 'integrations/cpamp/panel_validation.py').is_file():
    root = root.parent
sys.path.insert(0, str(root / 'integrations/cpamp'))
from panel_validation import MAX_PANEL_BYTES, validate_panel

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('url')
parser.add_argument('--expected', type=pathlib.Path, help='The custom management.html that should be served')
args = parser.parse_args()
request = urllib.request.Request(args.url, headers={'Cache-Control': 'no-cache', 'User-Agent': 'CommandCodeBridge-panel-verification'})
with urllib.request.urlopen(request, timeout=30) as response:
    page = response.read(MAX_PANEL_BYTES + 1)
    served_url = response.url
markers = {'native_quota': b'data-commandcode-native-quota', 'native_window_history': b'data-commandcode-native-windows', 'native_callback': b'commandcode_callback_placeholder', 'post_receiver_instructions': b'commandcode_open_receiver'}
print('URL:', served_url)
print('SHA256:', hashlib.sha256(page).hexdigest())
passed = True
try:
    validate_panel(page)
    print('antigravity_hub_and_integration: PASS')
except ValueError as error:
    print('panel_validation: FAIL:', error)
    passed = False
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
