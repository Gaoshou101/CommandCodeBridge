"""Package a previously built native library, without local credentials."""
import argparse
import hashlib
import pathlib
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
with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as bundle:
    bundle.write(args.library, args.library.name)
    for name in ("README.md", "config.example.yaml", "LICENSE", "NOTICE"):
        bundle.write(ROOT / name, name)
digest = hashlib.sha256(archive.read_bytes()).hexdigest()
archive.with_suffix(".zip.sha256").write_text(f"{digest}  {archive.name}\n", encoding="utf-8")
print(archive)
