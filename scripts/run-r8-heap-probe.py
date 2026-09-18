#!/usr/bin/env python3
"""Capture a real minified HPROF and verify ordinary inherited-field Retrace evidence."""
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
parser.add_argument("--cli", type=Path, required=True)
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=False)
adb = [args.adb, "-s", args.serial]
subprocess.run(adb + ["install", "-r", str(args.fixture / "consumer/app/build/outputs/apk/release/app-release.apk")], check=True)
subprocess.run(adb + ["shell", "am", "start", "-S", "-W", "-n", "com.example.jhsmoke/.MainActivity"], check=True)
for _ in range(40):
    time.sleep(0.5)
    pid = subprocess.run(adb + ["shell", "pidof", "com.example.jhsmoke"], capture_output=True, text=True).stdout.strip()
    if not pid.isdigit():
        continue
    log = subprocess.check_output(adb + ["logcat", "-d", "--pid=" + pid], text=True)
    if "FATAL EXCEPTION" in log:
        raise RuntimeError(log)
    match = re.search(r"EXECUTION PASS heap=(\S+) archive=(\S+)", log)
    if match:
        break
else:
    raise RuntimeError("Heap fixture did not complete")
(args.output / "device.log").write_text(log)
for remote, local in zip(match.groups(), ("heap.hprof", "logs.zip")):
    assert remote.startswith("/storage/emulated/0/Android/data/com.example.jhsmoke/files/heap-")
    subprocess.run(adb + ["pull", remote, str(args.output / local)], check=True)
assert 0 < (args.output / "heap.hprof").stat().st_size <= 128 << 20
with zipfile.ZipFile(args.output / "logs.zip") as archive:
    entries = archive.infolist()
    assert 1 <= len(entries) <= 256 and sum(e.file_size for e in entries) <= 64 << 20
    for entry in entries:
        assert Path(entry.filename).name == entry.filename and entry.filename.endswith(".jhlog")
        target = args.output / entry.filename
        assert not target.exists()
        target.write_bytes(archive.read(entry))
mapping = args.output / "mapping.txt"
shutil.copy(args.fixture / "consumer/app/build/outputs/mapping/release/mapping.txt", mapping)
with (args.output / "report.json-output").open("w") as output:
    subprocess.run([str(args.cli), "inspect", *map(str, sorted(args.output.glob("*.jhlog"))), "--mapping", str(mapping),
                    "--heap-dump", str(args.output / "heap.hprof"), "--json", "--out", str(args.output / "report.html")], stdout=output, check=True)
summary, _ = json.JSONDecoder().raw_decode((args.output / "report.json-output").read_text())
assert summary["MappingIdentity"]["Status"] == "verified"
target_class = "com.example.jhsmoke.HeapExecutionProbe$Target"
leaks = [leak for leak in summary["MemoryLeaks"] if leak["ClassName"] == target_class]
assert len(leaks) == 1, summary["MemoryLeaks"]
leak = leaks[0]
assert leak["HeapClassEvidence"] is not None, leak
assert leak["WatchedObjectAssociation"] == "unknown", leak
heap = leak["HeapClassEvidence"]
paths = [heap["reference_path"]] + (heap.get("alternative_paths") or [])
fields = [item for path in paths for item in (path or []) if item.get("field_name") == "ordinaryParent"]
assert fields, paths
for field in fields:
    assert field["declaring_class"] == "com.example.jhsmoke.HeapExecutionProbe$Base", field
    assert field["field_retrace"]["status"] == "resolved", field
    assert field["field_retrace"]["runtime_name"] != "ordinaryParent", field
print("Minified HPROF ordinary inherited field Retrace PASS:", args.output)
