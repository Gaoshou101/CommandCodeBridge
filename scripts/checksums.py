"""Verify four-platform packages and assemble CPA's required checksums.txt."""
import argparse
import hashlib
import json
import pathlib
import zipfile

TARGETS = {"linux-amd64": "so", "linux-arm64": "so", "windows-amd64": "dll", "darwin-arm64": "dylib"}
parser = argparse.ArgumentParser()
parser.add_argument("directory", type=pathlib.Path)
parser.add_argument("--include-cpamp", action="store_true", help="Also require the CPAMP frontend from the same integration source")
args = parser.parse_args()
archives = sorted(args.directory.rglob("commandcodebridge_*.zip"))
if len(archives) != len(TARGETS) + int(args.include_cpamp):
    parser.error("expected four platform packages" + (" and one CPAMP frontend" if args.include_cpamp else ""))
seen = set()
sources = set()
lines = []
panel_source = None
native_panels = set()
panel_digest = None
compatibilities = set()
for archive in archives:
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    expected, name = archive.with_suffix(".zip.sha256").read_text(encoding="utf-8").split()
    if (digest, archive.name) != (expected, name):
        parser.error(f"checksum mismatch: {archive.name}")
    with zipfile.ZipFile(archive) as bundle:
        build = json.loads(bundle.read("BUILD.json"))
        compatibility = build.get("compatibility")
        if not isinstance(compatibility, dict) or compatibility.get("schema") != 1 or not compatibility.get("cpa_baseline") or not compatibility.get("cpamp_source_commit"):
            parser.error("package compatibility baseline missing")
        compatibilities.add(json.dumps(compatibility, sort_keys=True))
        if build.get("artifact") == "cpamp-web":
            if not args.include_cpamp or panel_source is not None:
                parser.error("unexpected or duplicate CPAMP frontend")
            if archive.name != f"commandcodebridge_{build['version']}_cpamp-web.zip":
                parser.error("invalid CPAMP archive filename")
            if hashlib.sha256(bundle.read("management.html")).hexdigest() != build["management_html_sha256"]:
                parser.error("CPAMP frontend checksum mismatch")
            if build["integration_source_dirty"] is not False or not build["integration_source_commit"] or not build["cpamp_source_commit"]:
                parser.error("CPAMP release requires clean, traceable integration source")
            for license_name in ["LICENSE-CPAMP", "LICENSE-CommandCodeBridge"]:
                if not bundle.read(license_name).strip():
                    parser.error("CPAMP release requires licenses")
            panel_source = (build["version"], build["integration_source_commit"])
            panel_digest = build['management_html_sha256']
            lines.append(f"{digest}  {archive.name}\n")
            continue
        target = build["target"]
        if target not in TARGETS or target in seen:
            parser.error("invalid or duplicate platform")
        goos, goarch = target.split("-")
        if archive.name != f"commandcodebridge_{build['version']}_{goos}_{goarch}.zip":
            parser.error("archive does not use CPA's required filename")
        library = f"commandcodebridge.{TARGETS[target]}"
        if hashlib.sha256(bundle.read(library)).hexdigest() != build["library_sha256"]:
            parser.error("library checksum mismatch")
        helper = 'commandcodebridge-login.exe' if goos == 'windows' else 'commandcodebridge-login'
        if build.get('login_helper') != helper or hashlib.sha256(bundle.read(helper)).hexdigest() != build.get('login_helper_sha256'):
            parser.error("local POST receiver missing or checksum mismatch")
        if not build.get('native_panel_sha256'):
            parser.error('native CPAMP panel provenance missing')
        if not bundle.read('LICENSE-CPAMP').strip():
            parser.error('embedded native panel requires the CPAMP license')
        native_panels.add(build['native_panel_sha256'])
        if build["source_dirty"] is not False or not build["source_commit"]:
            parser.error("release requires clean, traceable source")
        seen.add(target)
        sources.add((build["version"], build["source_commit"]))
    lines.append(f"{digest}  {archive.name}\n")
if seen != set(TARGETS) or len(sources) != 1:
    parser.error("platform packages must share one version and source commit")
if args.include_cpamp and panel_source not in sources:
    parser.error("CPAMP frontend must share the native packages' version and integration source commit")
if len(native_panels) != 1 or (args.include_cpamp and panel_digest not in native_panels):
    parser.error('all native plugins must embed the exact released CPAMP panel')
if len(compatibilities) != 1:
    parser.error('all packages must share one compatibility baseline')
output = args.directory / "checksums.txt"
output.write_text("".join(lines), encoding="utf-8")
print(f"Verified four platform packages" + (" and CPAMP frontend" if args.include_cpamp else "") + f"; wrote {output}")
