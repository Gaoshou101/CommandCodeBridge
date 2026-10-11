"""Exercise native OAuth login and QuotaProvider through real CPA endpoints."""
import json
import argparse
import pathlib
import threading
import urllib.error
import urllib.parse
import urllib.request

from cpa_e2e import MockUpstream
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from cpa_harness import CPAHost, API

parser = argparse.ArgumentParser()
parser.add_argument('--serve', action='store_true', help='Keep isolated fixtures running for browser verification')
parser.add_argument('--panel', type=pathlib.Path, help='Built CPAMP index.html to serve in the fixture')
args = parser.parse_args()


with CPAHost(MockUpstream) as host:
    login = host.call('/v0/management/commandcode-auth-url?is_webui=true')
    assert login['status'] == 'ok' and len(login['state']) >= 40, login
    official = urllib.parse.urlparse(login['url'])
    params = urllib.parse.parse_qs(official.query)
    assert official.scheme == 'https' and official.netloc == 'commandcode.ai' and official.path == '/studio/auth/cli'
    assert params['callback'] == ['http://localhost:45879/callback'] and params['state'] == [login['state']] and params['mode'] == ['redirect']
    with urllib.request.urlopen(host.base + '/v0/resource/plugins/commandcodebridge/login?state=' + login['state']) as response:
        page = response.read().decode()
        assert response.headers['Referrer-Policy'] == 'no-referrer'
        assert 'studio/auth/cli' in page and "searchParams.set('mode','redirect')" in page
        assert '/*__CPA_AUTH__*/' not in page
        assert '__LOCALHOST_CALLBACK__' not in page and "set('callback','http://localhost:45879/callback')" in page
    status_path = '/v0/management/get-auth-status?state=' + login['state']
    assert host.call(status_path)['status'] == 'wait'
    body = {'state': login['state'], 'api_key': 'fixture-native-login-key'}
    try:
        urllib.request.urlopen(urllib.request.Request(host.base + API + '/auth/complete', data=json.dumps(body).encode(), headers={'Content-Type': 'application/json'}))
    except urllib.error.HTTPError as error:
        assert error.code == 401
    else:
        raise AssertionError('Manual key import bypassed management authentication')
    assert host.call(API + '/auth/complete', body)['success']
    assert host.call(status_path)['status'] == 'ok'
    host.wait(lambda: len(host.call('/v0/management/auth-files')['files']) == 1, 'native credential registration')
    credential = host.call('/v0/management/auth-files')['files'][0]
    assert credential['provider'] == 'commandcode', credential
    assert 'fixture-native-login-key' not in json.dumps(credential)
    providers = host.call('/v0/management/quota/providers')['providers']
    provider = next(p for p in providers if p['plugin_id'] == 'commandcodebridge')
    assert provider['supported_providers'] == ['commandcode'] and not provider['supports_reset']
    quota = host.call('/v0/management/quota/fetch', {'auth_index': credential['auth_index']})
    assert quota['subscription']['plan'] == 'pro', quota
    metrics = {m['key']: m['value'] for m in quota['summary']}
    assert metrics['remaining_credits'] == 45 and metrics['monthly_credits'] == 40 and metrics['monthly_used_credits'] == 10, metrics
    windows = {b['window']: b for g in quota['groups'] for b in g['buckets']}
    assert windows['fiveHour']['remainingFraction'] == .6 and windows['weekly']['remainingFraction'] == .7, windows
    assert 'monthly' not in windows and 'fixture-native-login-key' not in json.dumps(quota)
    # Exercise the official redirect callback without putting its key into logs.
    browser = host.call('/v0/management/commandcode-auth-url')
    callback = '/v0/resource/plugins/commandcodebridge/callback?' + urllib.parse.urlencode({'state': browser['state'], 'apiKey': 'fixture-browser-login-key', 'keyName': 'browser account'})
    with urllib.request.urlopen(host.base + callback) as response:
        assert '/login?' in response.url and 'apiKey' not in response.url
    assert host.call('/v0/management/get-auth-status?state=' + browser['state'])['status'] == 'ok'
    host.wait(lambda: len(host.call('/v0/management/auth-files')['files']) == 2, 'browser credential registration')
    # Remote browser handoff: the official page requires loopback, and the
    # native panel POSTs the whole callback without ever visiting it on CPA.
    remote = host.call('/v0/management/commandcode-auth-url')
    remote_callback = 'http://localhost:45879/callback?' + urllib.parse.urlencode({'state': remote['state'], 'apiKey': 'fixture-remote-login-key', 'keyName': 'remote account'})
    remote_body = {'state': remote['state'], 'redirect_url': remote_callback}
    try:
        urllib.request.urlopen(urllib.request.Request(host.base + API + '/auth/callback', data=json.dumps(remote_body).encode(), headers={'Content-Type': 'application/json'}))
    except urllib.error.HTTPError as error:
        assert error.code == 401
    else:
        raise AssertionError('Remote callback bypassed management authentication')
    host.error(API + '/auth/callback', {**remote_body, 'state': 'different-current-state'}, 400)
    host.error(API + '/auth/callback', {**remote_body, 'redirect_url': remote_callback.replace('localhost:45879', 'remote.example')}, 400)
    assert host.call('/v0/management/get-auth-status?state=' + remote['state'])['status'] == 'wait'
    assert host.call(API + '/auth/callback', remote_body)['status'] == 'ok'
    host.error(API + '/auth/callback', remote_body, 410)
    assert host.call('/v0/management/get-auth-status?state=' + remote['state'])['status'] == 'ok'
    host.wait(lambda: len(host.call('/v0/management/auth-files')['files']) == 3, 'remote credential registration')
    assert 'fixture-remote-login-key' not in json.dumps(host.call('/v0/management/auth-files'))
    cancelled = host.call('/v0/management/commandcode-auth-url')
    assert host.call(API + '/auth/callback', {'state': cancelled['state'], 'redirect_url': 'http://localhost:45879/callback?' + urllib.parse.urlencode({'state': cancelled['state'], 'error': 'access_denied'})})['status'] == 'ok'
    assert host.call('/v0/management/get-auth-status?state=' + cancelled['state'])['status'] == 'error'
    host.restart()
    assert len(host.call('/v0/management/auth-files')['files']) == 3
    assert len(host.call(API + '/credentials')['credentials']) == 3
    with open(host.folder / 'cpa.log', encoding='utf-8') as log:
        assert 'fixture-remote-login-key' not in log.read()
    print('PASS: native CPA localhost OAuth URL, remote callback POST, state/replay/cancellation/authentication, credential persistence, QuotaProvider discovery, plan/credits/windows and unknown monthly cap')
    if args.serve:
        class PanelProxy(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def proxy(self):
                if self.path.split('?', 1)[0] == '/management.html' and args.panel:
                    payload, status, headers = args.panel.read_bytes(), 200, {'Content-Type': 'text/html; charset=utf-8'}
                else:
                    body = self.rfile.read(int(self.headers.get('Content-Length', '0'))) or None
                    request = urllib.request.Request(host.base + self.path, data=body, method=self.command,
                        headers={key: value for key, value in self.headers.items() if key.lower() not in ('host', 'content-length', 'accept-encoding')})
                    try:
                        response = urllib.request.urlopen(request)
                    except urllib.error.HTTPError as error:
                        response = error
                    with response:
                        payload, status, headers = response.read(), response.code, dict(response.headers)
                self.send_response(status)
                for key, value in headers.items():
                    if key.lower() not in ('content-length', 'connection', 'transfer-encoding'):
                        self.send_header(key, value)
                self.send_header('Content-Length', str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            do_GET = do_POST = do_PUT = do_DELETE = do_OPTIONS = do_PATCH = proxy

        proxy = ThreadingHTTPServer(('0.0.0.0', 0), PanelProxy)
        print(f'BROWSER_FIXTURE=http://127.0.0.1:{proxy.server_port}/management.html', flush=True)
        try:
            proxy.serve_forever()
        except KeyboardInterrupt:
            pass
        finally:
            proxy.server_close()
