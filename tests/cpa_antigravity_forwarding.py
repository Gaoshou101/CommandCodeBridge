"""Verify real CPA /api-call HTTP UA and $TOKEN$ forwarding using a fresh fake Antigravity credential."""
import argparse
import json
import os
import pathlib
import subprocess
import tempfile
import time

from cpamp_antigravity_behavior import FakeCPA, LOCAL, free_port
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cpa', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, default=pathlib.Path('dist/antigravity-forwarding.json'))
    args = parser.parse_args()
    token = 'fixture-antigravity-access-token'
    headers_seen = []
    with FakeCPA() as server, tempfile.TemporaryDirectory(prefix='antigravity-forward-') as temporary:
        original_post = server.server.RequestHandlerClass.do_POST

        def receiver(handler):
            if handler.path == '/quota':
                handler.rfile.read(int(handler.headers.get('Content-Length', '0')))
                headers_seen.append({'ua': handler.headers.get('User-Agent'),
                                     'token_matched': handler.headers.get('Authorization') == 'Bearer ' + token})
                handler.send({'groups': []})
            else:
                original_post(handler)

        server.server.RequestHandlerClass.do_POST = receiver
        folder = pathlib.Path(temporary)
        auth = folder / 'auth'
        auth.mkdir()
        (auth / 'antigravity-fixture.json').write_text(json.dumps({
            'type': 'antigravity', 'email': 'fixture@example.test', 'project_id': 'fixture-project',
            'access_token': token, 'expired': '2099-01-01T00:00:00Z',
        }), encoding='utf-8')
        port = free_port()
        config = folder / 'config.yaml'
        config.write_text(f'''host: "127.0.0.1"
port: {port}
auth-dir: "{auth.as_posix()}"
remote-management:
  allow-remote: false
  secret-key: "fixture-management-key"
  disable-control-panel: true
proxy-url: "{server.url}"
api-keys: ["fixture-client-key"]
request-retry: 0
logging-to-file: false
''', encoding='utf-8')

        def call(path, body=None):
            req = urllib.request.Request(f'http://127.0.0.1:{port}/v0/management' + path,
                                         data=None if body is None else json.dumps(body).encode(),
                                         headers={'Authorization': 'Bearer fixture-management-key', 'Content-Type': 'application/json'})
            with LOCAL.open(req, timeout=5) as response:
                return json.load(response)

        with (folder / 'cpa.log').open('w', encoding='utf-8') as log:
            process = subprocess.Popen([str(args.cpa.resolve()), '-config', str(config)], cwd=folder, stdout=log, stderr=log,
                                       creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
            try:
                deadline = time.monotonic() + 20
                while True:
                    assert process.poll() is None, 'CPA exited before forwarding test'
                    try:
                        files = call('/auth-files')['files']
                        if files:
                            break
                    except OSError:
                        pass
                    if time.monotonic() > deadline:
                        raise RuntimeError('cpa_auth_fixture_not_ready')
                    time.sleep(.05)
                index = next(item['auth_index'] for item in files if item['name'] == 'antigravity-fixture.json')
                for header_name in ('User-Agent', 'user-agent'):
                    response = call('/api-call', {'authIndex': index, 'method': 'POST', 'url': server.url + '/quota',
                                                 'proxy_url': 'none', 'header': {header_name: 'antigravity/hub/2.23.0 darwin/arm64',
                                                 'Authorization': 'Bearer $TOKEN$'}, 'data': '{"project":"fixture-project"}'})
                    assert response['status_code'] == 200
                assert len(headers_seen) == 2
                assert all(item['ua'] == 'antigravity/hub/2.23.0 darwin/arm64' and item['token_matched'] for item in headers_seen)
                args.output.parent.mkdir(parents=True, exist_ok=True)
                args.output.write_text(json.dumps({'status': 'passed', 'headers': headers_seen, 'blocked_egress': server.egress}, indent=2))
                print('PASS: real CPA forwards HTTP User-Agent and resolves $TOKEN$ from fixture Antigravity auth')
            finally:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)


if __name__ == '__main__':
    main()
