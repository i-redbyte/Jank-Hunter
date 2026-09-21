#!/usr/bin/env python3
"""Verify one packaged mapping identity across real main/secondary Android processes."""
import argparse
import json
import re
import shutil
import subprocess
import time
import zipfile
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("fixture", type=Path)
parser.add_argument("output", type=Path)
parser.add_argument("--adb", required=True)
parser.add_argument("--serial", required=True)
parser.add_argument("--cli", type=Path)
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=False)
adb = [args.adb, "-s", args.serial]
subprocess.run(adb + ["install", "-r", str(args.fixture / "consumer/app/build/outputs/apk/release/app-release.apk")], check=True)
subprocess.run(adb + ["shell", "am", "start", "-S", "-W", "-n", "com.example.jhsmoke/.MainActivity"], check=True)
logs = args.output / "logs"
logs.mkdir()
for role, process in (("main", "com.example.jhsmoke"), ("remote", "com.example.jhsmoke:identity")):
    for _ in range(25):
        time.sleep(0.5)
        result = subprocess.run(adb + ["shell", "pidof", process], capture_output=True, text=True)
        pid = result.stdout.strip()
        if not pid.isdigit():
            continue
        log = subprocess.check_output(adb + ["logcat", "-d", "--pid=" + pid], text=True)
        if "FATAL EXCEPTION" in log:
            raise RuntimeError(log)
        match = re.search(r"EXECUTION PASS role=" + role + r" archive=(\S+)", log)
        if match:
            break
    else:
        raise RuntimeError("Identity probe failed: " + role)
    (args.output / (role + ".log")).write_text(log)
    remote = match.group(1)
    assert remote.startswith("/storage/emulated/0/Android/data/com.example.jhsmoke/files/identity-" + role + "-")
    archive = args.output / (role + ".zip")
    subprocess.run(adb + ["pull", remote, str(archive)], check=True)
    with zipfile.ZipFile(archive) as source:
        entries = source.infolist()
        assert 1 <= len(entries) <= 256 and sum(entry.file_size for entry in entries) <= 64 << 20
        for entry in entries:
            assert Path(entry.filename).name == entry.filename and entry.filename.endswith(".jhlog")
            target = logs / entry.filename
            assert not target.exists(), "Processes produced colliding log names"
            target.write_bytes(source.read(entry))
mapping = args.output / "mapping.txt"
shutil.copy(args.fixture / "consumer/app/build/outputs/mapping/release/mapping.txt", mapping)
cli = args.cli or args.fixture / "jankhunter"
with (args.output / "summary.json-output").open("w") as output:
    subprocess.run([str(cli), "inspect", *map(str, sorted(logs.glob("*.jhlog"))), "--all-sessions", "--mapping", str(mapping), "--json"], stdout=output, check=True)
summary, _ = json.JSONDecoder().raw_decode((args.output / "summary.json-output").read_text())
assert summary["MappingIdentity"]["Status"] == "verified", summary["MappingIdentity"]
assert summary["MappingIdentity"]["VerifiedLogs"] >= 2, summary["MappingIdentity"]
assert summary["Acquisition"]["DistinctProcessInstances"] == 2, summary["Acquisition"]
result = {"MappingIdentity": summary["MappingIdentity"], "Acquisition": summary["Acquisition"]}
(args.output / "verified.json").write_text(json.dumps(result, indent=2))
print(json.dumps(result))
