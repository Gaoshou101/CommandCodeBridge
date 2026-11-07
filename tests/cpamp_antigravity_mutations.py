"""Build a quarantined low-version production page and require a semantic browser failure."""
import argparse
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('checkout', type=pathlib.Path)
    parser.add_argument('--manager', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, default=ROOT / 'dist/antigravity-mutation')
    args = parser.parse_args()
    checkout = args.checkout.resolve()
    source = checkout / 'apps/web'
    temporary_root = checkout / 'dist'
    temporary_root.mkdir(exist_ok=True)
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='antigravity-mutation-', dir=temporary_root) as temporary:
        scratch = pathlib.Path(temporary).resolve()
        assert scratch.is_relative_to(temporary_root.resolve())
        # An ancestor node_modules supplies the existing locked dependencies; no second install.
        shutil.copytree(source, scratch / 'web', ignore=shutil.ignore_patterns('node_modules', 'dist', 'dist-demo', 'behavior-tests'))
        constants = scratch / 'web/src/utils/quota/constants.ts'
        original = constants.read_text(encoding='utf-8')
        mutated, count = re.subn(r"(export const ANTIGRAVITY_HUB_VERSION = )'[^']+'", r"\g<1>'2.22.0'", original)
        assert count == 1, 'mutation_anchor_changed'
        constants.write_text(mutated, encoding='utf-8')
        env = os.environ.copy()
        env.update({'VERSION': 'behavior-test-mutation', 'DEMO_SITE': 'false', 'VITE_DEMO_SITE': 'false'})
        subprocess.run(['node', str(checkout / 'node_modules/vite/bin/vite.js'), 'build'], cwd=scratch / 'web', env=env, check=True)
        panel = scratch / 'web/dist/index.html'
        assert b'antigravity/hub/' in panel.read_bytes(), 'mutation_did_not_build'
        subprocess.run([sys.executable, str(ROOT / 'tests/cpamp_antigravity_behavior.py'),
                        '--manager', str(args.manager.resolve()), '--panel', str(panel),
                        '--scenario', 'full', '--expected-defect', 'missing_gemini_window',
                        '--output', str(args.output.resolve())], check=True)
        report = json.loads((args.output / 'full.json').read_text())
        assert report['status'] == 'passed' and report['detected'] == 'missing_gemini_window'
        assert report['requests'] and all(call['ua_error'] == 'ua_below_minimum' for call in report['requests'])
        assert all('hub/2.22.0 ' in call['original_ua'] for call in report['requests'])
        print('PASS: source Hub version downgrade killed by production browser/window contract')


if __name__ == '__main__':
    main()
