"""Package a previously built native library, without local credentials."""
import argparse
import gzip
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
TARGETS = {"linux-amd64": ".so", "linux-arm64": ".so", "windows-amd64": ".dll", "darwin-arm64": ".dylib"}
parser = argparse.ArgumentParser()
parser.add_argument("library", type=pathlib.Path)
parser.add_argument("--target", required=True, choices=TARGETS, help="e.g. linux-amd64")
source_version = re.search(r'const Version = "([^"]+)"', (ROOT / "internal/bridge/types.go").read_text(encoding="utf-8")).group(1)
parser.add_argument("--version", default=source_version)
args = parser.parse_args()
args.library = args.library.resolve()
if not args.library.is_file() or args.library.suffix != TARGETS[args.target]:
    parser.error("library must exist and match the target platform extension")
helper = args.library.parent / ('commandcodebridge-login.exe' if args.target.startswith('windows-') else 'commandcodebridge-login')
if not helper.is_file():
    parser.error("build the matching commandcodebridge-login executable before packaging")
panel_file = ROOT / 'internal/bridge/panel-assets/management.html.gz'
if not panel_file.is_file():
    parser.error('embed the matching CPAMP panel before building and packaging')
panel_page = gzip.decompress(panel_file.read_bytes())
if ('CommandCodeBridge v' + args.version).encode() not in panel_page:
    parser.error('embedded CPAMP panel version mismatch')
if not all(c.isalnum() or c in "-._" for c in args.target + args.version):
    parser.error("invalid target or version")
output = ROOT / "dist"
output.mkdir(exist_ok=True)
goos, goarch = args.target.split("-")
archive = output / f"commandcodebridge_{args.version}_{goos}_{goarch}.zip"
def git_value(*arguments):
    try:
        result = subprocess.run(["git", *arguments], cwd=ROOT, capture_output=True, text=True, check=True)
        return result.stdout.strip()
    except (FileNotFoundError, subprocess.CalledProcessError):
        return None


commit = git_value("rev-parse", "HEAD")
status = git_value("status", "--porcelain")
manifest = {
    "compatibility": json.loads((ROOT / "internal/bridge/compatibility.json").read_text(encoding="utf-8")),
    "version": args.version,
    "target": args.target,
    "abi_version": 1,
    "rpc_schema": 6,
    "repository": "https://github.com/Gaoshou101/CommandCodeBridge",
    "source_commit": commit,
    "source_dirty": bool(status) if commit is not None and status is not None else None,
    "library_sha256": hashlib.sha256(args.library.read_bytes()).hexdigest(),
    "login_helper": helper.name,
    "login_helper_sha256": hashlib.sha256(helper.read_bytes()).hexdigest(),
    "native_panel_sha256": hashlib.sha256(panel_page).hexdigest(),
}
if args.target.startswith("linux-") and shutil.which("readelf"):
    symbols = subprocess.run(["readelf", "--version-info", str(args.library)], capture_output=True, text=True, check=True).stdout
    versions = set(re.findall(r"GLIBC_([0-9]+(?:\.[0-9]+)+)", symbols))
    if versions:
        manifest["glibc_minimum"] = max(versions, key=lambda value: tuple(int(part) for part in value.split(".")))
with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as bundle:
    bundle.write(args.library, args.library.name)
    bundle.write(helper, helper.name)
    bundle.write(ROOT / 'scripts/verify_cpamp_panel.py', 'verify_cpamp_panel.py')
    bundle.write(ROOT / 'internal/bridge/panel-assets/LICENSE-CPAMP', 'LICENSE-CPAMP')
    for name in ("README.md", "config.example.yaml", "LICENSE", "NOTICE"):
        bundle.write(ROOT / name, name)
    for name in ("DEPLOYMENT.md", "VALIDATION.md"):
        bundle.write(ROOT / "docs" / name, "docs/" + name)
    for file in (ROOT / "integrations" / "cpamp").glob("*"):
        if file.is_file():
            bundle.write(file, "integrations/cpamp/" + file.name)
    bundle.writestr("BUILD.json", json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
digest = hashlib.sha256(archive.read_bytes()).hexdigest()
archive.with_suffix(".zip.sha256").write_text(f"{digest}  {archive.name}\n", encoding="utf-8")
print(archive)
