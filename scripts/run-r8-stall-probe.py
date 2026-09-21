#!/usr/bin/env python3
"""Run an already built external fixture; preserve its exact mapping beside the captured log."""
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
parser.add_argument("--variant", choices=("debug", "release"), default="release")
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=False)
adb = [args.adb, "-s", args.serial]
subprocess.run(adb + ["install", "-r", str(args.fixture / f"consumer/app/build/outputs/apk/{args.variant}/app-{args.variant}.apk")], check=True)
subprocess.run(adb + ["shell", "am", "start", "-S", "-W", "-n", "com.example.jhsmoke/.MainActivity"], check=True)
pid = subprocess.check_output(adb + ["shell", "pidof", "com.example.jhsmoke"], text=True).strip()
assert pid.isdigit(), pid
for _ in range(25):
    time.sleep(1)
    log = subprocess.check_output(adb + ["logcat", "-d", "--pid=" + pid], text=True)
    if "FATAL EXCEPTION" in log or "EXECUTION FAIL" in log:
        raise RuntimeError(log)
    if "EXECUTION PASS archive=" in log:
        break
else:
    raise RuntimeError("Stall probe did not finish")
(args.output / "runtime.log").write_text(log)
remote = re.search(r"EXECUTION PASS archive=(\S+)", log).group(1)
assert remote.startswith("/storage/emulated/0/Android/data/com.example.jhsmoke/files/stall-execution-")
subprocess.run(adb + ["pull", remote, str(args.output / "logs.zip")], check=True)
with zipfile.ZipFile(args.output / "logs.zip") as archive:
    assert len(archive.infolist()) <= 256
    assert sum(entry.file_size for entry in archive.infolist()) <= 64 << 20
    for entry in archive.infolist():
        assert Path(entry.filename).name == entry.filename and entry.filename.endswith(".jhlog")
        assert entry.file_size <= 64 << 20
        (args.output / entry.filename).write_bytes(archive.read(entry))
with (args.output / "events.jsonl").open("w") as stream:
    subprocess.run([str(args.fixture / "jankhunter"), "export", *map(str, args.output.glob("*.jhlog"))], stdout=stream, check=True)
if args.variant == "release":
    shutil.copy(args.fixture / "consumer/app/build/outputs/mapping/release/mapping.txt", args.output / "mapping.txt")
print(json.dumps({"output": str(args.output), "pid": pid, "variant": args.variant}))
