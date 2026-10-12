"""Verify live settings, host reconfiguration and optional versioned library upgrade.

Only isolated fixtures and mock upstreams are used; the CPA PID must not change.
Pass --previous-archive with a released plugin ZIP to verify an actual upgrade.
"""
import argparse
import hashlib
import json
import os
import pathlib
import shutil
import tempfile
import urllib.error
import zipfile

from cpa_e2e import MockUpstream, ROOT, VERSION
from cpa_harness import API, CPAHost, chat_body, chat_model

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--previous-archive', type=pathlib.Path)
args = parser.parse_args()
current = pathlib.Path(os.environ.get('BRIDGE_LIBRARY', ROOT / 'dist' / 'commandcodebridge.so')).resolve()

with tempfile.TemporaryDirectory(prefix='commandcodebridge-reload-') as temporary:
    previous, previous_version = current, VERSION
    if args.previous_archive:
        with zipfile.ZipFile(args.previous_archive) as archive:
            manifest = json.loads(archive.read('BUILD.json'))
            previous_version = manifest['version']
            assert previous_version != VERSION, 'Upgrade needs two distinct versions'
            payload = archive.read('commandcodebridge' + current.suffix)
            assert hashlib.sha256(payload).hexdigest() == manifest['library_sha256']
        previous = pathlib.Path(temporary) / ('commandcodebridge-v' + previous_version + current.suffix)
        previous.write_bytes(payload)

    with CPAHost(MockUpstream, library=previous, expected_version=previous_version) as host:
        pid = host.process.pid
        credential = host.credential()
        host.models([chat_model('hot-reload-model')])
        host.call('/v1/chat/completions', chat_body('hot-reload-model'), management=False)
        host.wait(lambda: host.call(API + '/statistics')['totals']['requests'] == 1, 'initial request accounting')
        settings = {'timeout_seconds': 45, 'usage_refresh_seconds': 90, 'log_retention': 40, 'zdr': True}
        host.call(API + '/config', settings, method='PUT')
        assert all(host.call(API + '/config')[key] == value for key, value in settings.items())
        saved = (host.data / 'settings.json').read_bytes()
        try:
            host.call(API + '/config', {'timeout_seconds': 1}, method='PUT')
        except urllib.error.HTTPError as error:
            assert error.code == 400
        else:
            raise AssertionError('Invalid live settings were accepted')
        assert (host.data / 'settings.json').read_bytes() == saved
        assert host.call(API + '/config')['timeout_seconds'] == 45

        # Exercise CPA's own config watcher / plugin.reconfigure path after the
        # console has saved settings, preserving its models and request options.
        path = (host.folder / 'fixture-manager').as_posix()
        host.call('/v0/management/plugins/commandcodebridge/config', {'cpamp_install_dir': path}, method='PATCH')
        host.wait(lambda: host.call(API + '/config')['cpamp_install_dir'] == path, 'host config hot reload')
        assert host.call(API + '/config')['timeout_seconds'] == 45

        if args.previous_archive:
            # The host selects versioned files; never overwrite a loaded library.
            destination = host.folder / 'plugins' / ('commandcodebridge-v' + VERSION + current.suffix)
            staging = destination.with_suffix(destination.suffix + '.pending')
            shutil.copy2(current, staging)
            staging.replace(destination)
            host.call('/v0/management/plugins/commandcodebridge/config', {'version': VERSION}, method='PATCH')
            def upgraded():
                try:
                    return host.call(API + '/status')['version'] == VERSION
                except (OSError, urllib.error.HTTPError):
                    return False
            host.wait(upgraded, 'versioned plugin hot upgrade', timeout=30)
            assert host.call(API + '/statistics')['totals']['requests'] == 1
            assert all(host.call(API + '/config')[key] == value for key, value in settings.items())
            assert host.call(API + '/config')['cpamp_install_dir'] == path
            assert host.call(API + '/logs')['items'], 'Hot upgrade lost the persisted request'

        assert any(item['id'] == credential for item in host.call(API + '/credentials')['credentials'])
        host.call('/v1/chat/completions', chat_body('hot-reload-model'), management=False)
        host.wait(lambda: host.call(API + '/statistics')['totals']['requests'] == 2, 'post-reload request accounting')
        assert host.process.pid == pid and host.process.poll() is None, 'CPA restarted during reload'
        print('PASS: live settings, invalid-setting rollback, host reconfigure, credentials/models/accounting retained; same CPA PID' + (f'; plugin {previous_version} -> {VERSION}' if args.previous_archive else ''))
