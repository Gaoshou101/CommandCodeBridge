"""Package a previously built native library, without local credentials."""
import argparse
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("library", type=pathlib.Path)
parser.add_argument("--target", required=True, help="e.g. linux-amd64")
parser.add_argument("--version", default="0.1.0")
args = parser.parse_args()
args.library = args.library.resolve()
if not args.library.is_file() or args.library.suffix not in (".so", ".dll", ".dylib"):
    parser.error("library must be an existing .so, .dll or .dylib")
if not all(c.isalnum() or c in "-._" for c in args.target + args.version):
    parser.error("invalid target or version")
output = ROOT / "dist"
output.mkdir(exist_ok=True)
archive = output / f"commandcodebridge-{args.version}-{args.target}.zip"
def git_value(*arguments):
    try:
        result = subprocess.run(["git", *arguments], cwd=ROOT, capture_output=True, text=True, check=True)
        return result.stdout.strip()
    except (FileNotFoundError, subprocess.CalledProcessError):
        return None


commit = git_value("rev-parse", "HEAD")
status = git_value("status", "--porcelain")
manifest = {
    "version": args.version,
    "target": args.target,
    "abi_version": 1,
    "rpc_schema": 6,
    "repository": "https://github.com/Gaoshou101/CommandCodeBridge",
    "source_commit": commit,
    "source_dirty": bool(status) if commit is not None and status is not None else None,
    "library_sha256": hashlib.sha256(args.library.read_bytes()).hexdigest(),
}
if args.target.startswith("linux-") and shutil.which("readelf"):
    symbols = subprocess.run(["readelf", "--version-info", str(args.library)], capture_output=True, text=True, check=True).stdout
    versions = set(re.findall(r"GLIBC_([0-9]+(?:\.[0-9]+)+)", symbols))
    if versions:
        manifest["glibc_minimum"] = max(versions, key=lambda value: tuple(int(part) for part in value.split(".")))
with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as bundle:
    bundle.write(args.library, args.library.name)
    for name in ("README.md", "config.example.yaml", "LICENSE", "NOTICE"):
        bundle.write(ROOT / name, name)
    bundle.writestr("BUILD.json", json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
digest = hashlib.sha256(archive.read_bytes()).hexdigest()
archive.with_suffix(".zip.sha256").write_text(f"{digest}  {archive.name}\n", encoding="utf-8")
print(archive)
