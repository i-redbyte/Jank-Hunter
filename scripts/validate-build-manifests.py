#!/usr/bin/env python3
"""Verify post-package evidence from direct AGP producers, without assemble/bundle wrappers."""
import hashlib
import json
import sys
from pathlib import Path
from zipfile import ZipFile


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


def validate(build: Path, variant: str, kind: str) -> None:
    generated = build / "generated/jankhunter" / variant
    manifest = json.loads((generated / f"build-manifest-{kind}.json").read_text())
    assert manifest["format"] == 1 and manifest["kind"] == "build-manifest"
    assert manifest["variant"] == variant
    mapping = build / "outputs/mapping" / variant / "mapping.txt"
    assert manifest["mappingSha256"] == (digest(mapping) if mapping.exists() else "")
    expected = {"artifact-metadata.json", "class-graph.jsonl", "lambda-captures.jsonl",
                "instrumentation-diagnostics.jsonl", "android-components-catalog.jsonl", "di-catalog.jsonl"}
    assert {item["file"] for item in manifest["artifacts"]} == expected
    for item in manifest["artifacts"]:
        assert digest(generated / item["file"]) == item["sha256"], item
    package_directory = build / "outputs" / ("apk" if kind == "apk" else "bundle") / variant
    assert manifest["packages"], "No packaged application"
    assert {item["file"] for item in manifest["packages"]} == {p.name for p in package_directory.glob(f"*.{kind}")}
    for item in manifest["packages"]:
        package = package_directory / item["file"]
        assert digest(package) == item["sha256"], item
        with ZipFile(package) as archive:
            prefix = "base/" if kind == "aab" else ""
            identity = archive.read(f"{prefix}assets/jankhunter/build-identity-v1.txt")
        assert hashlib.sha256(identity).hexdigest() == manifest["identityAssetSha256"]
        fields = dict(line.split("=", 1) for line in identity.decode("ascii").splitlines())
        assert fields["symbol-namespace"] == manifest["symbolNamespace"]
        assert fields["mapping-sha256"] == manifest["mappingSha256"]


if __name__ == "__main__":
    for variant, kind in (("debug", "apk"), ("release", "apk"), ("release", "aab")):
        validate(Path(sys.argv[1]), variant, kind)
        print(f"Direct packaging manifest PASS: {variant}/{kind}")
