"""Production panel -> real Manager snapshots -> isolated SQLite, without cloud credentials.

python tests/cpamp_antigravity_behavior.py --manager /path/to/manager --archive /path/to/cpamp-web.zip
"""
import argparse
import contextlib
import copy
import hashlib
import json
import os
import pathlib
import re
import socket
import sqlite3
import subprocess
import tempfile
import threading
import time
import urllib.parse
import urllib.request
import zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = pathlib.Path(__file__).resolve().parents[1]
FIXTURE = json.loads((ROOT / 'integrations/cpamp/antigravity-behavior-fixtures.json').read_text())
ADMIN = 'fixture-panel-admin'
MANAGEMENT = 'fixture-cpa-management'
LOCAL = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def ua_error(headers):
    values = [value for key, value in headers.items() if key.lower() == 'user-agent']
    if not values:
        return 'ua_missing'
    if len(set(values)) != 1:
        return 'ua_conflicting'
    match = re.fullmatch(r'antigravity/hub/(\d+)\.(\d+)\.(\d+) [a-z0-9_-]+/[a-z0-9_-]+', values[0], re.I)
    if not match:
        return 'ua_invalid'
    if tuple(map(int, match.groups())) < tuple(FIXTURE['minimumHubVersion']):
        return 'ua_below_minimum'
    return None


class FakeCPA:
    def __init__(self, scenario='full'):
        self.scenario = scenario
        self.t0 = int(time.time()) * 1000
        self.calls = []
        self.paths = []
        self.egress = []
        self.auth_errors = []
        self.contract_errors = []
        self.freeze = False
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def send(self, payload, status=200):
                data = json.dumps(payload).encode()
                self.send_response(status)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(data)))
                self.send_header('X-Cpa-Version', 'v8.0.23')
                self.end_headers()
                self.wfile.write(data)

            def do_CONNECT(self):
                owner.egress.append(self.path)
                self.send({'error': 'external_network_denied'}, 502)

            def do_GET(self):
                if self.path.startswith(('http://', 'https://')):
                    owner.egress.append(self.path)
                    self.send({'error': 'external_network_denied'}, 502)
                    return
                owner.paths.append(self.path)
                path = urllib.parse.urlsplit(self.path).path
                if path.endswith('/auth-files'):
                    self.send({'files': FIXTURE['accounts']})
                elif path.endswith('/config'):
                    self.send({'debug': False, 'usage-statistics-enabled': False, 'remote-management': {}})
                elif path.endswith('/models'):
                    self.send({'object': 'list', 'data': []})
                elif path.endswith('/usage'):
                    self.send({'usage': {'apis': {}, 'total_requests': 0}})
                elif path.endswith('/usage/queue'):
                    self.send({'items': []})
                else:
                    self.send({})

            def do_POST(self):
                payload = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))) or '{}')
                owner.paths.append(self.path)
                if not self.path.endswith('/api-call'):
                    self.send({})
                    return
                if self.headers.get('Authorization') != 'Bearer ' + MANAGEMENT:
                    owner.auth_errors.append('invalid_management_authorization')
                header = dict(payload.get('header', {}))
                if header.get('Authorization') != 'Bearer $TOKEN$':
                    owner.contract_errors.append('missing_upstream_token_placeholder')
                original_ua = next((value for key, value in header.items() if key.lower() == 'user-agent'), None)
                if owner.scenario.startswith('ua:'):
                    header = {key: value for key, value in header.items() if key.lower() != 'user-agent'}
                    if owner.scenario != 'ua:missing':
                        header['User-Agent'] = owner.scenario[3:]
                error = ua_error(header)
                url = payload.get('url', '')
                if not re.fullmatch(r'https://(?:daily-cloudcode-pa(?:\.sandbox)?|cloudcode-pa)\.googleapis\.com/v1internal:(?:retrieveUserQuotaSummary|fetchAvailableModels|loadCodeAssist)', url):
                    owner.contract_errors.append('unexpected_upstream_endpoint')
                account = next((item for item in FIXTURE['accounts'] if item['auth_index'] == payload.get('authIndex')), None)
                if account is None or payload.get('method') != 'POST':
                    owner.contract_errors.append('invalid_request_account_or_method')
                if account and not url.endswith(':loadCodeAssist') and json.loads(payload.get('data', '{}')) != {'project': account['project_id']}:
                    owner.contract_errors.append('wrong_project')
                owner.calls.append({'authIndex': payload.get('authIndex'), 'method': payload.get('method'),
                                    'url': url, 'original_ua': original_ua,
                                    'effective_ua': next((v for k, v in header.items() if k.lower() == 'user-agent'), None),
                                    'ua_error': error, 'data': payload.get('data')})
                if owner.freeze:
                    self.send({'status_code': 503, 'body': '{"error":"unexpected_refresh_during_restore"}'})
                    return
                subscription = url.endswith(':loadCodeAssist')
                models = url.endswith(':fetchAvailableModels')
                status = 200
                if subscription:
                    body = FIXTURE['subscription']
                elif owner.scenario == 'quota429':
                    status, body = 429, {'error': 'rate limit'}
                elif owner.scenario == 'models' and not models:
                    status, body = 404, {}
                elif models:
                    body = {'models': FIXTURE['models']}
                else:
                    groups = copy.deepcopy(FIXTURE['groups'])
                    if error or owner.scenario == 'partial':
                        groups = [group for group in groups if group['scope'] != 'gemini']
                    for group in groups:
                        group.pop('scope')
                        for bucket in group['buckets']:
                            offset = bucket.pop('resetOffsetSeconds')
                            bucket.pop('durationSeconds')
                            bucket['resetTime'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime((owner.t0 + offset * 1000) / 1000))
                            if payload.get('authIndex') == FIXTURE['accounts'][1]['auth_index']:
                                bucket['remainingFraction'] -= .1
                            if owner.scenario == 'missingWindow':
                                bucket.pop('window')
                    body = {'groups': groups}
                self.send({'status_code': status, 'header': {'Date': time.strftime('%a, %d %b %Y %H:%M:%S GMT', time.gmtime(owner.t0 / 1000))}, 'body': json.dumps(body)})

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.url = f'http://127.0.0.1:{self.server.server_port}'

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)


class Manager:
    def __init__(self, binary, panel, folder, upstream):
        self.binary, self.folder = binary, folder
        self.url = f'http://127.0.0.1:{free_port()}'
        self.db = folder / 'data/usage.sqlite'
        self.log = (folder / 'manager.log').open('w', encoding='utf-8')
        self.process = None
        self.env = os.environ.copy()
        for key in list(self.env):
            if key.startswith(('CPA_', 'USAGE_', 'CPAMP_')) or key in ('HTTP_ADDR', 'PANEL_PATH', 'COLLECTOR_MODE', 'DATA_KEY', 'DATA_KEY_PATH'):
                self.env.pop(key)
        self.env.update({'HTTP_ADDR': self.url.removeprefix('http://'), 'USAGE_DATA_DIR': str(folder / 'data'),
                         'USAGE_DB_PATH': str(self.db), 'PANEL_PATH': str(panel),
                         'CPA_MANAGER_ADMIN_KEY': ADMIN, 'CPA_UPSTREAM_URL': upstream.url,
                         'CPA_MANAGEMENT_KEY': MANAGEMENT, 'CPAMP_UPDATE_CHECK_ENABLED': 'false',
                         'HTTP_PROXY': upstream.url, 'HTTPS_PROXY': upstream.url, 'ALL_PROXY': upstream.url,
                         'NO_PROXY': '127.0.0.1,localhost', 'no_proxy': '127.0.0.1,localhost'})
        # Avoid inherited config and secrets; all generated state stays in the temporary directory.
        self.env['CPA_MANAGER_CONFIG'] = str(folder / 'config.json')

    def start(self):
        self.process = subprocess.Popen([str(self.binary)], cwd=self.folder, env=self.env, stdout=self.log, stderr=self.log,
                                        creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise RuntimeError('Manager exited: ' + (self.folder / 'manager.log').read_text())
            try:
                self.request('/health')
                return
            except OSError:
                time.sleep(.05)
        raise RuntimeError('manager_not_ready')

    def request(self, path, body=None):
        req = urllib.request.Request(self.url + path, data=None if body is None else json.dumps(body).encode(),
                                     headers={'Authorization': 'Bearer ' + ADMIN, 'Content-Type': 'application/json'})
        with LOCAL.open(req, timeout=5) as response:
            return json.load(response)

    def stop(self):
        if self.process and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)

    def close(self):
        self.stop()
        self.log.close()


def require_complete(windows, offset, t0):
    for group in FIXTURE['groups']:
        for bucket in group['buckets']:
            window = next((item for item in windows if item['provider_window_id'].endswith(':' + bucket['bucketId'])), None)
            if window is None:
                raise AssertionError('missing_gemini_window' if group['scope'] == 'gemini' else 'missing_claude_window')
            if window.get('duration_seconds') != bucket['durationSeconds']:
                raise AssertionError('missing_window_duration')
            assert window['window_kind'] == ('five_hour' if bucket['window'] == '5h' else 'weekly')
            assert window['model_scope_kind'] == 'family' and window['model_scope_key'] == group['scope']
            assert abs(window['remaining_percent'] - (bucket['remainingFraction'] - offset) * 100) < 1e-6
            assert abs(window['used_percent'] - (1 - bucket['remainingFraction'] + offset) * 100) < 1e-6
            assert window['cycle_end_ms'] == t0 + bucket['resetOffsetSeconds'] * 1000
    assert len(windows) == 4


def snapshot_identity(entry):
    return {key: entry[key] for key in ('row_key', 'provider', 'account')}


def stable_windows(result):
    fields = ('provider_window_id', 'window_kind', 'duration_seconds', 'model_scope_key',
              'remaining_percent', 'used_percent', 'observed_at_ms', 'cycle_end_ms')
    return [[{key: window.get(key) for key in fields} for window in item['windows']] for item in result['items']]


def run_scenario(browser, binary, content, output, scenario, expected_defect=None):
    from playwright.sync_api import expect
    label = re.sub(r'[^a-zA-Z0-9_-]', '_', scenario)
    report = {'scenario': scenario, 'status': 'running', 'page_sha256': hashlib.sha256(content).hexdigest()}
    with tempfile.TemporaryDirectory(prefix='cpamp-behavior-') as temporary:
        folder = pathlib.Path(temporary)
        panel = folder / 'management.html'
        panel.write_bytes(content)
        with FakeCPA(scenario) as upstream:
            manager = Manager(binary, panel, folder, upstream)
            context = browser.new_context(locale='en-US')
            page = context.new_page()
            blocked, errors, writes = [], [], []
            try:
                manager.start()
                with LOCAL.open(manager.url + '/management.html') as response:
                    assert response.read() == content, 'served_page_mismatch'

                def route_request(route):
                    origin = urllib.parse.urlsplit(route.request.url)
                    if origin.scheme in ('data', 'blob') or f'{origin.scheme}://{origin.netloc}' in (manager.url, upstream.url):
                        route.continue_()
                    else:
                        blocked.append(route.request.url)
                        route.abort()

                def capture(response):
                    if response.url.endswith('/v0/management/quota-snapshots') and response.request.method == 'POST':
                        assert response.status == 200, 'snapshot_write_failed'
                        writes.extend(json.loads(response.request.post_data)['entries'])

                context.route('**/*', route_request)
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.on('response', capture)
                page.goto(manager.url + '/management.html#/login')
                page.wait_for_load_state('networkidle')
                page.locator('input[type=password]').fill(ADMIN)
                page.locator('input[type=password]').press('Enter')
                page.wait_for_url(lambda url: '#/login' not in url, timeout=15000)
                page.goto(manager.url + '/management.html#/accounts')
                page.wait_for_load_state('networkidle')
                accounts = FIXTURE['accounts'] if scenario == 'full' and not expected_defect else FIXTURE['accounts'][:1]
                query_accounts = []
                for index, account in enumerate(accounts):
                    card = page.locator('[data-account-card]').filter(has_text=account['name'])
                    refresh = card.get_by_role('button', name='Refresh quota', exact=True)
                    with page.expect_response(lambda response: response.url.endswith('/api-call') and ':retrieveUserQuotaSummary' in (response.request.post_data or '')):
                        refresh.click()
                    expect(refresh).to_be_enabled()
                    page.wait_for_load_state('networkidle')
                    if scenario == 'quota429':
                        assert not writes, 'failed_query_wrote_snapshot'
                        expect(card).not_to_contain_text('80%')
                        assert len([call for call in upstream.calls if not call['url'].endswith(':loadCodeAssist')]) == 1
                        break
                    if scenario == 'full' and not expected_defect:
                        for group in FIXTURE['groups']:
                            for bucket in group['buckets']:
                                cell = card.locator(f'[data-account-quota-window$=":{bucket["bucketId"]}"]')
                                expect(cell).to_contain_text(f'{round((bucket["remainingFraction"] - index * .1) * 100)}%')
                                assert 'Reset:' in cell.get_attribute('title')
                    card.get_by_role('button', name=f"Open {account['name']} detail", exact=True).click()
                    with page.expect_response(lambda response: response.url.endswith('/v0/management/quota-snapshots') and response.request.method == 'POST', timeout=10000) as write:
                        page.locator('#accounts-detail-tab-quota').click()
                    assert write.value.status == 200
                    page.wait_for_load_state('networkidle')
                    entry = next(item for item in writes if item['account']['auth_index'] == account['auth_index'])
                    query_accounts.append(snapshot_identity(entry))
                    defect = expected_defect or ('missing_gemini_window' if scenario.startswith('ua:') or scenario == 'partial' else 'missing_window_duration' if scenario == 'missingWindow' else None)
                    if defect:
                        try:
                            require_complete(entry['windows'], index * .1, upstream.t0)
                        except AssertionError as error:
                            assert str(error) == defect, f'wrong_defect: {error}'
                            report['detected'] = defect
                        else:
                            raise AssertionError('mutation_survived')
                        if scenario.startswith('ua:'):
                            assert upstream.calls and all(call['ua_error'] for call in upstream.calls), 'wrong_ua_not_delivered'
                    elif scenario == 'models':
                        assert len(entry['windows']) == 2
                        assert all(window['window_kind'] == 'unknown' and not window.get('duration_seconds') for window in entry['windows'])
                    else:
                        require_complete(entry['windows'], index * .1, upstream.t0)
                    page.goto(manager.url + '/management.html#/accounts')
                    page.wait_for_load_state('networkidle')

                if query_accounts:
                    before = manager.request('/v0/management/quota-snapshots/query', {'accounts': query_accounts})
                    assert len(before['items']) == len(query_accounts)
                    if scenario == 'full' and not expected_defect:
                        for index, item in enumerate(before['items']):
                            require_complete(item['windows'], index * .1, upstream.t0)
                        assert before['items'][0]['account_key'] != before['items'][1]['account_key']
                        same_name_other_index = copy.deepcopy(query_accounts[0])
                        same_name_other_index['account']['auth_index'] = FIXTURE['accounts'][1]['auth_index']
                        same_name_other_index['row_key'] = 'same-name-other-index'
                        isolated = manager.request('/v0/management/quota-snapshots/query', {'accounts': [same_name_other_index]})
                        assert not isolated['items'][0]['windows'], 'same_filename_borrowed_other_auth_index'
                    with contextlib.closing(sqlite3.connect(f'file:{manager.db.as_posix()}?mode=ro', uri=True)) as database:
                        database.row_factory = sqlite3.Row
                        rows = [dict(row) for row in database.execute('SELECT account_key,provider_window_id,window_kind,model_scope_key FROM account_quota_windows')]
                    expected_rows = {(item['account_key'], window['provider_window_id'], window['window_kind'], window.get('model_scope_key'))
                                     for item in before['items'] for window in item['windows']}
                    assert {(row['account_key'], row['provider_window_id'], row['window_kind'], row['model_scope_key']) for row in rows} == expected_rows
                    report['database_windows'] = rows
                    report['snapshots'] = stable_windows(before)
                    if scenario in ('full', 'models') and not expected_defect:
                        upstream.freeze = True
                        call_count = len(upstream.calls)
                        manager.stop()
                        manager.start()
                        after = manager.request('/v0/management/quota-snapshots/query', {'accounts': query_accounts})
                        assert stable_windows(after) == stable_windows(before), 'restart_changed_snapshot'
                        page.reload()
                        page.wait_for_load_state('networkidle')
                        if page.locator('input[type=password]').count():
                            page.locator('input[type=password]').fill(ADMIN)
                            page.locator('input[type=password]').press('Enter')
                            page.wait_for_url(lambda url: '#/login' not in url, timeout=15000)
                            page.goto(manager.url + '/management.html#/accounts')
                            page.wait_for_load_state('networkidle')
                        first = FIXTURE['accounts'][0]
                        page.get_by_role('button', name=f"Open {first['name']} detail", exact=True).click()
                        with page.expect_response(lambda response: response.url.endswith('/quota-snapshots/query')) as restored:
                            page.locator('#accounts-detail-tab-quota').click()
                        assert restored.value.status == 200
                        page.wait_for_load_state('networkidle')
                        assert stable_windows(restored.value.json()) == [stable_windows(before)[0]], 'page_restore_changed_snapshot'
                        if scenario == 'full':
                            cards = page.locator('[data-quota-card-mode="model"]')
                            expect(cards).to_have_count(4)
                            for group in FIXTURE['groups']:
                                family = 'Gemini' if group['scope'] == 'gemini' else 'Claude'
                                for bucket in group['buckets']:
                                    matching = cards.filter(has_text=re.compile(family, re.I)).filter(
                                        has=page.locator('strong[title]', has_text=re.compile('Weekly' if bucket['window'] == 'weekly' else '5', re.I)))
                                    expect(matching).to_have_count(1)
                                    expect(matching).to_contain_text(f'{round(bucket["remainingFraction"] * 100)}%')
                        assert len(upstream.calls) == call_count, 'unexpected_refresh_during_restore'
                        report['restart_restore'] = 'passed'
                assert not blocked, 'browser_external_network: ' + repr(blocked)
                assert not errors, 'browser_runtime_errors: ' + repr(errors)
                assert not upstream.auth_errors, upstream.auth_errors
                assert not upstream.contract_errors, upstream.contract_errors
                # Proxy-denied background requests cannot reach the internet; record attempts separately.
                report['blocked_manager_egress'] = upstream.egress
                if scenario == 'full' and not expected_defect:
                    assert upstream.calls and all(call['ua_error'] is None for call in upstream.calls), 'invalid_actual_ua'
                report.update({'status': 'passed', 'requests': upstream.calls, 't0': upstream.t0})
                page.screenshot(path=str(output / f'{label}.png'), full_page=True)
                print('PASS:', scenario, report.get('detected', report.get('restart_restore', 'behavior')), flush=True)
            except Exception as error:
                with contextlib.suppress(Exception):
                    page.screenshot(path=str(output / f'{label}-failure.png'), full_page=True)
                    (output / f'{label}-failure.html').write_text(page.content(), encoding='utf-8')
                report.update({'status': 'failed', 'error': str(error).replace(ADMIN, '[REDACTED]').replace(MANAGEMENT, '[REDACTED]'),
                               'requests': upstream.calls, 't0': upstream.t0})
                raise
            finally:
                try:
                    context.close()
                finally:
                    manager.close()
                    (output / f'{label}.json').write_text(json.dumps(report, indent=2), encoding='utf-8')
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manager', type=pathlib.Path, required=True)
    parser.add_argument('--archive', type=pathlib.Path)
    parser.add_argument('--panel', type=pathlib.Path, help='Test-only mutation page; never packaged for release')
    parser.add_argument('--output', type=pathlib.Path, default=ROOT / 'dist/antigravity-behavior')
    parser.add_argument('--scenario', action='append', help='Default: full positive and all browser fault controls')
    parser.add_argument('--expected-defect', choices=['missing_gemini_window', 'missing_window_duration'])
    args = parser.parse_args()
    from playwright.sync_api import sync_playwright
    args.output.mkdir(parents=True, exist_ok=True)
    manifest = None
    if args.panel:
        content = args.panel.read_bytes()
    else:
        if not args.archive:
            parser.error('--archive or --panel is required')
        with zipfile.ZipFile(args.archive) as archive:
            content = archive.read('management.html')
            manifest = json.loads(archive.read('BUILD.json'))
            assert hashlib.sha256(content).hexdigest() == manifest['management_html_sha256']
    if args.panel and args.archive:
        with zipfile.ZipFile(args.archive) as archive:
            assert content == archive.read('management.html'), 'installed_page_differs_from_archive'
            manifest = json.loads(archive.read('BUILD.json'))
    if manifest:
        compatibility = json.loads((ROOT / 'internal/bridge/compatibility.json').read_text())
        assert manifest['cpamp_source_commit'] == compatibility['cpamp_source_commit'], 'manager_source_baseline_mismatch'
        assert hashlib.sha256(content).hexdigest() == manifest['management_html_sha256']
    scenarios = args.scenario or ['full', 'ua:antigravity/cli/1.0.13', 'ua:antigravity/hub/2.22.0 darwin/arm64', 'ua:missing', 'partial', 'missingWindow', 'models', 'quota429']
    valid_scenarios = {'full', 'partial', 'missingWindow', 'models', 'quota429'}
    if any(scenario not in valid_scenarios and not scenario.startswith('ua:') for scenario in scenarios):
        parser.error('unknown behavior scenario')
    reports = []
    status = 'failed'
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        try:
            for scenario in scenarios:
                reports.append(run_scenario(browser, args.manager.resolve(), content, args.output, scenario, args.expected_defect))
            status = 'passed'
        finally:
            browser.close()
            (args.output / 'summary.json').write_text(json.dumps({
                'status': status, 'cpamp_source_commit': (manifest or {}).get('cpamp_source_commit'),
                'scenarios': reports,
            }, indent=2), encoding='utf-8')


if __name__ == '__main__':
    main()
