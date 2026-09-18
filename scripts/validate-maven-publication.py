#!/usr/bin/env python3
"""Check the published SDK/plugin coordinates and the actual Maven CLI archive."""

import argparse
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import tempfile
import xml.etree.ElementTree as ET
import zipfile


def require(condition, message):
    if not condition:
        raise ValueError(message)


def publication(repository, group, artifact, version, extension, classifier=""):
    base = repository / group.replace(".", "/") / artifact / version
    pom = base / f"{artifact}-{version}.pom"
    require(pom.is_file(), f"Missing publication: {pom}")
    root = ET.parse(pom).getroot()
    ns = {"m": "http://maven.apache.org/POM/4.0.0"}
    for key, expected in (("groupId", group), ("artifactId", artifact), ("version", version)):
        require(root.findtext(f"m:{key}", namespaces=ns) == expected, f"Wrong {key}: {pom}")
    suffix = f"-{classifier}" if classifier else ""
    artifact_path = base / f"{artifact}-{version}{suffix}.{extension}"
    require(artifact_path.is_file() and artifact_path.stat().st_size > 0, f"Missing artifact: {artifact_path}")
    return artifact_path, root, ns


def validate(repository, version, cli_version, run_cli):
    for name, extension in (
        ("annotations", "jar"), ("runtime", "aar"), ("okhttp3", "aar"),
        ("workmanager", "aar"), ("android-sdk", "aar"), ("gradle-plugin", "jar"),
    ):
        artifact, _, _ = publication(repository, "io.jankhunter", f"jankhunter-{name}", version, extension)
        if name == "gradle-plugin":
            with zipfile.ZipFile(artifact) as jar:
                metadata = jar.read("io/jankhunter/gradle/jankhunter-plugin.properties").decode()
                require(f"jankHunterVersion={version}" in metadata.splitlines(), "Wrong embedded plugin version")

    _, marker, ns = publication(
        repository, "io.jankhunter.android", "io.jankhunter.android.gradle.plugin", version, "pom",
    )
    dependencies = [
        tuple(node.findtext(f"m:{key}", namespaces=ns) for key in ("groupId", "artifactId", "version"))
        for node in marker.findall("m:dependencies/m:dependency", ns)
    ]
    require(dependencies == [("io.jankhunter", "jankhunter-gradle-plugin", version)], "Wrong plugin marker dependency")
    archive, _, _ = publication(
        repository, "io.jankhunter", "jankhunter-cli", cli_version, "tar.gz", "darwin-arm64",
    )
    with tarfile.open(archive) as tar:
        members = [member for member in tar.getmembers() if not member.isdir()]
        names = [member.name for member in members]
        r8 = [
            name for name in names
            if name.count("/") == 1 and name.startswith("jankhunter-retrace/r8lib-") and name.endswith(".jar")
        ]
        require(len(r8) == 1, f"Missing or duplicate R8 jar: {archive}")
        expected = {
            "jankhunter", "jankhunter-retrace/jankhunter-retrace.jar",
            "jankhunter-retrace/R8-LICENSE.txt", "jankhunter-retrace/NOTICE.txt", r8[0],
        }
        require(set(names) == expected and len(names) == len(expected), f"Incomplete CLI archive: {names}")
        require(all(member.isfile() and member.size > 0 for member in members), "Invalid CLI archive entries")
        require(tar.getmember("jankhunter").mode & 0o111, "CLI binary is not executable")
        if run_cli:
            require(platform.system() == "Darwin" and platform.machine() == "arm64", "--run-cli requires macOS arm64")
            with tempfile.TemporaryDirectory(prefix="jankhunter-published-cli-") as directory:
                target = Path(directory)
                for member in members:
                    output = target / member.name
                    output.parent.mkdir(parents=True, exist_ok=True)
                    with tar.extractfile(member) as source, output.open("wb") as destination:
                        shutil.copyfileobj(source, destination)
                binary = target / "jankhunter"
                binary.chmod(0o755)
                env = dict(os.environ)
                env.pop("JANK_HUNTER_RETRACE_HOME", None)

                def run(*args):
                    return subprocess.run(
                        [str(binary), *args], cwd=target, env=env, check=True,
                        capture_output=True, text=True, timeout=60,
                    )

                require(
                    run("version").stdout.splitlines()[0] == f"Jank Hunter CLI {cli_version}",
                    "Wrong packaged CLI version",
                )
                run("sample", "--out", "sample.jhlog")
                (target / "mapping.txt").write_text("com.example.Foo -> a:\n")
                run("inspect", "sample.jhlog", "--mapping", "mapping.txt", "--allow-unverified-mapping", "--out", "report.html")
                require((target / "report.html").stat().st_size > 0, "Missing report from published CLI")
    print(f"Verified Maven SDK/plugin {version}, CLI {cli_version}; extracted CLI mapping: {run_cli}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--cli-version")
    parser.add_argument("--run-cli", action="store_true")
    args = parser.parse_args()
    try:
        validate(args.repository, args.version, args.cli_version or args.version, args.run_cli)
    except (ValueError, OSError, KeyError, ET.ParseError, tarfile.TarError, zipfile.BadZipFile, subprocess.SubprocessError) as error:
        detail = error.stderr if isinstance(error, subprocess.CalledProcessError) else str(error)
        parser.exit(1, f"Maven publication validation failed: {detail}\n")


if __name__ == "__main__":
    main()
