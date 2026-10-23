"""Package an already built CPAMP single-file frontend and its provenance."""
import argparse
import hashlib
import json
import pathlib
import re
import subprocess
import sys
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'integrations/cpamp'))
from panel_validation import load_compatibility, validate_checkout, validate_panel
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("checkout", type=pathlib.Path, help="CPAMP source checkout used to build the page")
args = parser.parse_args()
checkout = args.checkout.resolve()
panel = checkout / "apps/web/dist/index.html"
license_file = checkout / "LICENSE"
if not panel.is_file() or not license_file.is_file():
    parser.error("checkout must contain a built apps/web/dist/index.html and LICENSE")
page = panel.read_bytes()


def git_value(folder, *arguments):
    result = subprocess.run(["git", *arguments], cwd=folder, capture_output=True, text=True, check=True)
    return result.stdout.strip()


version = re.search(r'const Version = "([^"]+)"', (ROOT / "internal/bridge/types.go").read_text(encoding="utf-8")).group(1)
try:
    compatibility = load_compatibility(ROOT)
    validate_panel(page, version)
    cpamp_commit = validate_checkout(checkout, compatibility, exact=True)
except ValueError as error:
    parser.error(str(error))
manifest = {
    "compatibility": compatibility,
    "version": version,
    "artifact": "cpamp-web",
    "cpamp_repository": "https://github.com/seakee/CPA-Manager-Plus",
    "cpamp_source_commit": cpamp_commit,
    "cpamp_source_dirty": bool(git_value(checkout, "status", "--porcelain")),
    "integration_source_commit": git_value(ROOT, "rev-parse", "HEAD"),
    "integration_source_dirty": bool(git_value(ROOT, "status", "--porcelain")),
    "management_html_sha256": hashlib.sha256(page).hexdigest(),
    "install": "Set PANEL_PATH in the running CPAMP Manager Server process to the absolute readable management.html path. Verify the served page with verify_cpamp_panel.py. See README.md.",
}
output = ROOT / "dist"
output.mkdir(exist_ok=True)
archive = output / f"commandcodebridge_{version}_cpamp-web.zip"
with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as bundle:
    bundle.writestr("management.html", page)
    bundle.write(license_file, "LICENSE-CPAMP")
    bundle.write(ROOT / "LICENSE", "LICENSE-CommandCodeBridge")
    bundle.write(ROOT / "integrations/cpamp/README.md", "README.md")
    bundle.write(ROOT / "scripts/verify_cpamp_panel.py", "verify_cpamp_panel.py")
    for path in (ROOT / "integrations/cpamp").iterdir():
        if path.is_file():
            bundle.write(path, "integrations/cpamp/" + path.name)
    bundle.writestr("BUILD.json", json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    bundle.writestr("checksums.txt", manifest["management_html_sha256"] + "  management.html\n")
digest = hashlib.sha256(archive.read_bytes()).hexdigest()
archive.with_suffix(".zip.sha256").write_text(f"{digest}  {archive.name}\n", encoding="utf-8")
print(archive)
