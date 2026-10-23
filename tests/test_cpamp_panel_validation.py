"""Offline regression coverage for source, ZIP, release and deployment gates."""
import hashlib
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'integrations/cpamp'))
from panel_validation import load_compatibility, validate_checkout, validate_panel, validate_panel_provenance

VERSION = '9.8.7'


def page(identity):
    return (f'<html data-commandcode-native-quota data-commandcode-native-windows>'
            f'<meta name="commandcodebridge-capabilities" content="quota recent-requests quota-windows">'
            f'CommandCodeBridge v{VERSION} /commandcodebridge/auth/callback '
            f'commandcode_callback_placeholder commandcode_open_receiver {identity}</html>').encode()


def run(*command, cwd=None):
    return subprocess.run(command, cwd=cwd, capture_output=True, text=True, encoding='utf-8')


def git(folder, *args):
    result = run('git', '-C', str(folder), *args)
    if result.returncode:
        raise AssertionError(result.stderr)
    return result.stdout.strip()


def init_repo(folder):
    git(folder, 'init', '--quiet')
    git(folder, 'config', 'user.name', 'Fixture')
    git(folder, 'config', 'user.email', 'fixture@example.invalid')
    git(folder, 'config', 'commit.gpgsign', 'false')
    git(folder, 'config', 'core.hooksPath', str(folder / 'no-hooks'))
    git(folder, 'add', '.')
    git(folder, 'commit', '--quiet', '-m', 'fixture baseline')
    return git(folder, 'rev-parse', 'HEAD')


class PanelValidationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='cpamp-gates-')
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.bridge = self.root / 'bridge'
        self.checkout = self.root / 'cpamp'
        self.checkout.mkdir()
        (self.checkout / 'LICENSE').write_text('fixture CPAMP license')
        self.commit = init_repo(self.checkout)
        self.baseline = load_compatibility(ROOT)
        self.baseline['cpamp_source_commit'] = self.commit
        shutil.copytree(ROOT / 'scripts', self.bridge / 'scripts', ignore=shutil.ignore_patterns('__pycache__'))
        shutil.copytree(ROOT / 'integrations/cpamp', self.bridge / 'integrations/cpamp', ignore=shutil.ignore_patterns('__pycache__'))
        (self.bridge / 'internal/bridge/panel-assets').mkdir(parents=True)
        (self.bridge / 'internal/bridge/types.go').write_text(f'const Version = "{VERSION}"\n')
        (self.bridge / 'internal/bridge/compatibility.json').write_text(json.dumps(self.baseline))
        for name in ('LICENSE', 'NOTICE', 'README.md', 'config.example.yaml', 'docs/DEPLOYMENT.md', 'docs/VALIDATION.md'):
            target = self.bridge / name
            target.parent.mkdir(exist_ok=True)
            target.write_text('fixture content')
        (self.bridge / '.gitignore').write_text('dist/\n__pycache__/\ninternal/bridge/panel-assets/*\n')
        init_repo(self.bridge)
        self.archive = self.bridge / f'dist/commandcodebridge_{VERSION}_cpamp-web.zip'

    def script(self, name, *arguments):
        return run(sys.executable, str(self.bridge / 'scripts' / name), *(str(arg) for arg in arguments), cwd=self.bridge)

    def assert_result(self, result, passed):
        self.assertEqual(result.returncode == 0, passed, result.stdout + result.stderr)
        return result.stdout + result.stderr

    def write_page(self, content):
        path = self.checkout / 'apps/web/dist/index.html'
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)

    def make_archive(self, content, **overrides):
        self.archive.parent.mkdir(exist_ok=True)
        build = {
            'artifact': 'cpamp-web', 'version': VERSION, 'compatibility': self.baseline,
            'cpamp_source_commit': self.commit, 'integration_source_commit': git(self.bridge, 'rev-parse', 'HEAD'),
            'integration_source_dirty': False, 'management_html_sha256': hashlib.sha256(content).hexdigest(),
        }
        build.update(overrides)
        with zipfile.ZipFile(self.archive, 'w') as bundle:
            bundle.writestr('BUILD.json', json.dumps(build))
            bundle.writestr('management.html', content)
            bundle.writestr('LICENSE-CPAMP', 'fixture license')
            bundle.writestr('LICENSE-CommandCodeBridge', 'fixture license')
        self.archive.with_suffix('.zip.sha256').write_text(hashlib.sha256(self.archive.read_bytes()).hexdigest() + '  ' + self.archive.name)

    def test_package_and_embed_identity_fixtures(self):
        for identity, valid in (
            ('antigravity/cli/1.0.13', False),
            ('antigravity/hub/2.23.0 darwin/arm64', True),
            ('antigravity/hub/${e} ${t}', True),
            ('antigravity/hub/${e} antigravity/cli/1.0.13', False),
            ('missing identity', False),
        ):
            with self.subTest(identity=identity):
                self.write_page(page(identity))
                output = self.assert_result(self.script('package_cpamp.py', self.checkout), valid)
                if not valid:
                    self.assertIn('antigravity/hub/', output)
                    self.assertIn('#986', output)
                self.make_archive(page(identity))
                output = self.assert_result(self.script('embed_cpamp_panel.py', self.archive), valid)
                if not valid:
                    self.assertIn('antigravity/hub/', output)
                    self.assertIn('#986', output)

    def test_embed_requires_matching_source_baseline_and_digest(self):
        content = page('antigravity/hub/${version}')
        for overrides in (
            {'cpamp_source_commit': '0' * 40},
            {'compatibility': dict(self.baseline, cpamp_source_commit='0' * 40)},
            {'management_html_sha256': '0' * 64},
            {'version': '0.0.1'},
        ):
            with self.subTest(overrides=overrides):
                self.make_archive(content, **overrides)
                self.assert_result(self.script('embed_cpamp_panel.py', self.archive), False)
        self.make_archive(content)
        self.assert_result(self.script('embed_cpamp_panel.py', self.archive), True)

    def test_checkout_threshold_uses_git_ancestry_and_config(self):
        self.assertEqual(validate_checkout(self.checkout, self.baseline, exact=True), self.commit)
        (self.checkout / 'next').write_text('descendant')
        git(self.checkout, 'add', 'next')
        git(self.checkout, 'commit', '--quiet', '-m', 'descendant')
        descendant = git(self.checkout, 'rev-parse', 'HEAD')
        self.assertEqual(validate_checkout(self.checkout, self.baseline), descendant)
        with self.assertRaises(ValueError):
            validate_checkout(self.checkout, self.baseline, exact=True)
        newer = dict(self.baseline, cpamp_source_commit=descendant)
        self.assertEqual(validate_checkout(self.checkout, newer, exact=True), descendant)
        git(self.checkout, 'checkout', '--detach', self.commit)
        with self.assertRaises(ValueError):
            validate_checkout(self.checkout, newer)

    def test_package_rejects_off_baseline_checkout(self):
        self.write_page(page('antigravity/hub/2.23.0'))
        (self.checkout / 'next').write_text('descendant')
        git(self.checkout, 'add', 'next')
        git(self.checkout, 'commit', '--quiet', '-m', 'descendant')
        self.assertIn('compatibility.json', self.assert_result(self.script('package_cpamp.py', self.checkout), False))

    def test_native_packages_release_gate_and_shipped_verifier(self):
        content = page('antigravity/hub/${e} ${t}')
        self.write_page(content)
        self.assert_result(self.script('package_cpamp.py', self.checkout), True)
        self.assert_result(self.script('embed_cpamp_panel.py', self.archive), True)
        output = self.bridge / 'dist'
        for target, extension in (('linux-amd64', 'so'), ('linux-arm64', 'so'), ('windows-amd64', 'dll'), ('darwin-arm64', 'dylib')):
            folder = output / target
            folder.mkdir()
            library = folder / ('commandcodebridge.' + extension)
            # A real executable also permits package.py's optional ELF inspection.
            shutil.copyfile(sys.executable, library)
            (folder / ('commandcodebridge-login.exe' if target.startswith('windows') else 'commandcodebridge-login')).write_bytes(b'fixture helper')
            self.assert_result(self.script('package.py', library, '--target', target), True)
        self.assert_result(self.script('checksums.py', output, '--include-cpamp'), True)
        # Exercise the verifier and apply baseline loader from an extracted ZIP.
        extracted = self.root / 'extracted'
        with zipfile.ZipFile(self.archive) as bundle:
            bundle.extractall(extracted)
        self.assertEqual(load_compatibility(extracted), self.baseline)
        served = extracted / 'management.html'
        verifier = extracted / 'verify_cpamp_panel.py'
        for identity, valid in (('antigravity/hub/${e}', True), ('antigravity/cli/1.0.13', False)):
            served.write_bytes(page(identity))
            self.assert_result(run(sys.executable, str(verifier), served.as_uri(), '--expected', str(served)), valid)
        # A self-consistent digest is insufficient when the HTML carries old UA.
        self.make_archive(page('antigravity/cli/1.0.13'))
        output_text = self.assert_result(self.script('checksums.py', output, '--include-cpamp'), False)
        self.assertIn('#986', output_text)

    def test_integration_and_version_markers_remain_required(self):
        content = page('antigravity/hub/${e}')
        for marker in (b'data-commandcode-native-quota', b'data-commandcode-native-windows',
                       b'commandcode_callback_placeholder', b'recent-requests'):
            with self.assertRaises(ValueError):
                validate_panel(content.replace(marker, b''), VERSION)
        with self.assertRaises(ValueError):
            validate_panel(content, '0.0.1')


if __name__ == '__main__':
    unittest.main()
