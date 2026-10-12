"""Install the bundled panel through real CPA and verify actual CPAMP binary HTTP.

CPA_BINARY=/path/to/cpa CPAMP_BINARY=/path/to/cpa-manager-plus python3 tests/cpamp_native_panel.py
Uses isolated directories, loopback ports, fixture credentials and no user data.
"""
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import time
import urllib.request

from cpa_e2e import MockUpstream, free_port
from cpa_harness import CPAHost, API

manager_source = pathlib.Path(os.environ['CPAMP_BINARY']).resolve()
local = urllib.request.build_opener(urllib.request.ProxyHandler({}))

with tempfile.TemporaryDirectory(prefix='commandcodebridge-real-cpamp-') as temporary:
    root = pathlib.Path(temporary)
    runtime = root / 'runtime' / 'cpa-manager-plus_fixture'
    runtime.mkdir(parents=True)
    binary = runtime / ('cpa-manager-plus.exe' if os.name == 'nt' else 'cpa-manager-plus')
    shutil.copy2(manager_source, binary)
    binary.chmod(0o755)
    port = free_port()
    config = runtime / 'config.json'
    original = {'httpAddr': f'127.0.0.1:{port}', 'dataDir': str(root / 'existing-data'), 'adminKeyFile': 'private-original-key', 'unknownField': {'keep': 7}}
    config.write_text(json.dumps(original), encoding='utf-8')
    (root / 'run.sh').write_text('# CPAMP_RUNTIME_PACKAGE=cpa-manager-plus_fixture\n', encoding='utf-8')
    env = os.environ.copy()
    env['CPA_MANAGER_CONFIG'] = str(config)
    env['CPA_MANAGER_ADMIN_KEY'] = 'fixture-manager-admin-key'
    for name in ('PANEL_PATH', 'HTTP_ADDR', 'USAGE_DATA_DIR', 'USAGE_DB_PATH', 'CPA_UPSTREAM_URL', 'CPA_MANAGEMENT_KEY', 'CPA_MANAGEMENT_KEY_FILE'):
        env.pop(name, None)
    process = None

    def page():
        with local.open(f'http://127.0.0.1:{port}/management.html', timeout=3) as response:
            return response.read()

    def start(log):
        global process
        process = subprocess.Popen([str(binary)], cwd=runtime, env=env, stdout=log, stderr=log,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
        deadline = time.monotonic() + 25
        while time.monotonic() < deadline:
            assert process.poll() is None, 'CPAMP binary exited'
            try:
                return page()
            except OSError:
                time.sleep(.1)
        raise AssertionError('CPAMP listener did not start')

    def stop():
        if process is not None and process.poll() is None:
            process.terminate()
            process.wait(timeout=15)

    with (root / 'manager.log').open('w', encoding='utf-8') as log:
        try:
            before = start(log)
            with CPAHost(MockUpstream) as host:
                if os.name == 'nt':
                    host.call(API + '/config', {'cpamp_install_dir': str(root)}, method='PUT')
                detected = host.call(API + '/cpamp/panel')
                assert detected['config_path'] == str(config) and detected['available'] and not detected['active']
                if os.name != 'nt':
                    assert detected['source'] == 'running_process' and detected['pid'] == process.pid
                result = host.call(API + '/cpamp/panel/install', {})
                assert result['installed'] and result['restart_required'] and not result['active']
                assert page() == before, 'Embedded running manager should wait for restart'
                saved = json.loads(config.read_text())
                assert {k: v for k, v in saved.items() if k != 'panelPath'} == original
                intended = pathlib.Path(saved['panelPath']).read_bytes()
                stop()
                assert start(log) == intended
                assert host.call(API + '/cpamp/panel')['active']
                assert host.call(API + '/cpamp/panel/install', {}).get('backups', []) == []
                # Existing PANEL_PATH is authoritative and read on every request.
                # Replace its page without pretending JSON can override the env.
                stop()
                external = root / 'environment-panel.html'
                external.write_text('<html>prior external panel</html>')
                env['PANEL_PATH'] = str(external)
                start(log)
                if os.name != 'nt':
                    result = host.call(API + '/cpamp/panel/install', {})
                    assert result['active'] and not result['restart_required'] and page() == intended
                    assert json.loads(config.read_text()) == saved
                host.call(API + '/config', {'cpamp_install_dir': ''}, method='PUT')
            assert 'fixture-manager-admin-key' not in (root / 'manager.log').read_text()
            print('PASS: real CPA -> native CPAMP discovery -> bundled panel/config backup -> manager restart -> exact HTTP page; existing PANEL_PATH hot replacement')
        finally:
            stop()
