#!/usr/bin/env python3
"""Alternate two benchmark APKs; results are ART regression signals, not production budgets."""
import argparse
import hashlib
import json
import subprocess
import time
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("baseline", type=Path)
parser.add_argument("candidate", type=Path)
parser.add_argument("output", type=Path)
parser.add_argument("--adb", required=True)
parser.add_argument("--serial", required=True)
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=False)
adb = [args.adb, "-s", args.serial]
metadata = {label: hashlib.sha256(path.read_bytes()).hexdigest() for label, path in (("baseline", args.baseline), ("candidate", args.candidate))}
metadata["fingerprint"] = subprocess.check_output(adb + ["shell", "getprop", "ro.build.fingerprint"], text=True).strip()
(args.output / "inputs.json").write_text(json.dumps(metadata, indent=2))
rows = []
for round_index in range(3):
    for label, apk in (("baseline", args.baseline), ("candidate", args.candidate)):
        subprocess.run(adb + ["install", "-r", str(apk)], check=True, stdout=subprocess.DEVNULL)
        subprocess.run(adb + ["shell", "am", "start", "-S", "-W", "-n", "com.example.jhsmoke/.MainActivity"], check=True, stdout=subprocess.DEVNULL)
        pid = subprocess.check_output(adb + ["shell", "pidof", "com.example.jhsmoke"], text=True).strip()
        assert pid.isdigit(), pid
        for _ in range(40):
            time.sleep(0.5)
            log = subprocess.check_output(adb + ["logcat", "-d", "--pid=" + pid], text=True)
            if "FATAL EXCEPTION" in log:
                raise RuntimeError(log)
            if "JHSTALLBENCH: EXECUTION PASS" in log:
                break
        else:
            raise RuntimeError("ART benchmark did not complete")
        (args.output / f"{label}-{round_index}.log").write_text(log)
        result_lines = [line.split("RESULT ", 1)[1] for line in log.splitlines() if "JHSTALLBENCH: RESULT " in line]
        assert len(result_lines) == 6, result_lines
        for line in result_lines:
            row = {"label": label, "round": round_index, "pid": pid}
            row.update(dict(item.split("=", 1) for item in line.split()))
            rows.append(row)
        memory = subprocess.run(adb + ["shell", "dumpsys", "meminfo", pid], capture_output=True, text=True, check=True)
        (args.output / f"{label}-{round_index}-meminfo.txt").write_text(memory.stdout)
        print(json.dumps({"label": label, "round": round_index, "pid": pid, "completed": True}), flush=True)
(args.output / "results.json").write_text(json.dumps(rows, indent=2))
